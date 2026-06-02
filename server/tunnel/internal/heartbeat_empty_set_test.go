package internal

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"go.uber.org/zap/zaptest"
)

// fakeReloader records every UUID set StartClientSync applies, so a test can
// assert whether an empty active-set ever reached ReloadClients (WR-02).
type fakeReloader struct {
	mu      sync.Mutex
	applied [][]string
}

func (f *fakeReloader) ReloadClients(uuids []string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	cp := append([]string(nil), uuids...)
	f.applied = append(f.applied, cp)
	return nil
}

func (f *fakeReloader) snapshot() [][]string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([][]string, len(f.applied))
	copy(out, f.applied)
	return out
}

// TestStartClientSync_RefusesEmptyActiveSet is the WR-02 guard: a transient
// empty active-set read must NOT reach ReloadClients (which would Close the live
// xray instance and drop every connection server-wide). The loop must keep the
// previous set, not advance lastETag, and recover once a non-empty read arrives.
func TestStartClientSync_RefusesEmptyActiveSet(t *testing.T) {
	// The API returns a non-empty set first (establishes a baseline reload),
	// then an EMPTY set with a NEW etag (the dangerous transient read), then a
	// non-empty set again. The guard must drop only the empty one.
	type resp struct {
		body string // JSON body
	}
	responses := []resp{
		{`{"uuids":["aaa"],"etag":"e1"}`},
		{`{"uuids":[],"etag":"e2-empty"}`},
		{`{"uuids":["bbb","ccc"],"etag":"e3"}`},
		// pad with the last response so late ticks keep returning it
		{`{"uuids":["bbb","ccc"],"etag":"e3"}`},
		{`{"uuids":["bbb","ccc"],"etag":"e3"}`},
	}

	var idx int
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		i := idx
		if idx < len(responses)-1 {
			idx++
		}
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(responses[i].body))
	}))
	defer srv.Close()

	rl := &fakeReloader{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	logger := zaptest.NewLogger(t)
	done := make(chan struct{})
	go func() {
		// apiBaseURL is concatenated with the vless-clients path; point it at the
		// test server's root and let the handler ignore the path.
		StartClientSync(ctx, rl, srv.URL, "srv-1", "secret", 10*time.Millisecond, 0, logger)
		close(done)
	}()

	// Let several ticks elapse so all three distinct etags are observed.
	time.Sleep(120 * time.Millisecond)
	cancel()
	<-done

	applied := rl.snapshot()
	if len(applied) == 0 {
		t.Fatal("expected at least one reload, got none")
	}
	for i, set := range applied {
		if len(set) == 0 {
			t.Fatalf("reload #%d applied an EMPTY active set — WR-02 guard failed: %#v", i, applied)
		}
	}
	// The final applied set must be the recovered non-empty one, proving the
	// loop did not get stuck after refusing the empty read.
	last := applied[len(applied)-1]
	if len(last) != 2 || last[0] != "bbb" || last[1] != "ccc" {
		t.Fatalf("expected final reload to be [bbb ccc] (recovery after empty), got %#v", last)
	}
}
