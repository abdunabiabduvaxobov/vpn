package lava

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestListProducts_PaginationDrain(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		next := ""
		var items []ProductsItem
		switch {
		case calls == 1:
			items = []ProductsItem{{Type: "PRODUCT", Data: ProductItemResponse{ID: "p1", Type: "SUBSCRIPTION"}}}
			next = "cursor2"
		case calls == 2:
			if !strings.Contains(r.URL.RawQuery, "nextPage=cursor2") {
				t.Errorf("expected nextPage=cursor2 in query, got %q", r.URL.RawQuery)
			}
			items = []ProductsItem{
				{Type: "POST", Data: ProductItemResponse{ID: "post-x"}}, // should be filtered out
				{Type: "PRODUCT", Data: ProductItemResponse{ID: "p2"}},
			}
			next = "" // last page
		default:
			t.Fatalf("unexpected extra call %d", calls)
		}
		var nextPtr *string
		if next != "" {
			nextPtr = &next
		}
		w.WriteHeader(200)
		_ = json.NewEncoder(w).Encode(ProductsResponse{Items: items, NextPage: nextPtr})
	}))
	defer srv.Close()
	c := newWithBaseURL("k", srv.URL)
	out, err := c.ListProducts(context.Background())
	if err != nil {
		t.Fatalf("ListProducts: %v", err)
	}
	if len(out) != 2 {
		t.Fatalf("expected 2 products (POST type filtered out), got %d", len(out))
	}
	if out[0].ID != "p1" || out[1].ID != "p2" {
		t.Errorf("unexpected IDs: %s,%s", out[0].ID, out[1].ID)
	}
	if calls != 2 {
		t.Errorf("expected 2 HTTP calls (cursor drain), got %d", calls)
	}
}
