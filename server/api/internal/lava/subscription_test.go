package lava

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCancelSubscription_QueryParams(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "DELETE" {
			t.Errorf("expected DELETE, got %s", r.Method)
		}
		if r.URL.Path != "/api/v1/subscriptions" {
			t.Errorf("expected /api/v1/subscriptions, got %s", r.URL.Path)
		}
		q := r.URL.Query()
		if q.Get("contractId") != "contract-x" {
			t.Errorf("expected contractId=contract-x, got %q", q.Get("contractId"))
		}
		if q.Get("email") != "alice+test@example.com" {
			t.Errorf("expected url-encoded email, got %q", q.Get("email"))
		}
		// + must be encoded in the raw query but decoded by url.Query() — accept either form.
		if !strings.Contains(r.URL.RawQuery, "contractId=contract-x") {
			t.Errorf("expected RawQuery to contain contractId=contract-x, got %q", r.URL.RawQuery)
		}
		w.WriteHeader(200)
	}))
	defer srv.Close()
	c := newWithBaseURL("k", srv.URL)
	if err := c.CancelSubscription(context.Background(), "contract-x", "alice+test@example.com"); err != nil {
		t.Fatalf("CancelSubscription: %v", err)
	}
}

func TestCancelSubscription_LavaError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(404)
		_, _ = w.Write([]byte(`{"message":"contract not found"}`))
	}))
	defer srv.Close()
	c := newWithBaseURL("k", srv.URL)
	if err := c.CancelSubscription(context.Background(), "missing", "a@b.c"); err == nil {
		t.Fatalf("expected error on 404")
	}
}
