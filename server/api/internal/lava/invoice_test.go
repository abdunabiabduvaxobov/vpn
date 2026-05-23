package lava

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCreateInvoice_HappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v3/invoice" {
			t.Errorf("expected /api/v3/invoice, got %s", r.URL.Path)
		}
		if r.Header.Get("X-Api-Key") != "test-key" {
			t.Errorf("expected X-Api-Key=test-key, got %s", r.Header.Get("X-Api-Key"))
		}
		var req CreateInvoiceRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req.Email != "alice@example.com" || req.OfferID != "off-1" || req.Currency != "USD" {
			t.Errorf("unexpected request body: %+v", req)
		}
		w.WriteHeader(200)
		paymentURL := "https://app.lava.top/pay/abc"
		_ = json.NewEncoder(w).Encode(InvoiceResponse{
			ID:          "inv-123",
			Status:      "in-progress",
			AmountTotal: InvoiceAmount{Amount: 5.0, Currency: "USD"},
			PaymentURL:  &paymentURL,
		})
	}))
	defer srv.Close()

	c := newWithBaseURL("test-key", srv.URL)
	out, err := c.CreateInvoice(context.Background(), CreateInvoiceRequest{
		Email: "alice@example.com", OfferID: "off-1", Currency: "USD", Periodicity: "MONTHLY",
	})
	if err != nil {
		t.Fatalf("CreateInvoice: %v", err)
	}
	if out.ID != "inv-123" || out.Status != "in-progress" {
		t.Errorf("unexpected response: %+v", out)
	}
	if out.PaymentURL == nil || *out.PaymentURL != "https://app.lava.top/pay/abc" {
		t.Errorf("unexpected paymentUrl: %v", out.PaymentURL)
	}
}

func TestCreateInvoice_LavaError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(422)
		_, _ = w.Write([]byte(`{"message":"invalid offer"}`))
	}))
	defer srv.Close()
	c := newWithBaseURL("test-key", srv.URL)
	_, err := c.CreateInvoice(context.Background(), CreateInvoiceRequest{Email: "a", OfferID: "off", Currency: "USD"})
	if err == nil {
		t.Fatalf("expected error on 422 response")
	}
}

func TestGetInvoice_HappyPath(t *testing.T) {
	expectedPath := "/api/v2/invoices/inv-456"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != expectedPath {
			t.Errorf("expected %s, got %s", expectedPath, r.URL.Path)
		}
		w.WriteHeader(200)
		exp := "2026-06-23T10:00:00Z"
		_ = json.NewEncoder(w).Encode(InvoiceDetailResponse{
			ID:                  "inv-456",
			Status:              "COMPLETED",
			Type:                "SUBSCRIPTION_FIRST_INVOICE",
			SubscriptionDetails: &InvoiceSubscriptionDetails{ExpiredAt: &exp},
		})
	}))
	defer srv.Close()
	c := newWithBaseURL("test-key", srv.URL)
	got, err := c.GetInvoice(context.Background(), "inv-456")
	if err != nil {
		t.Fatalf("GetInvoice: %v", err)
	}
	if got.ID != "inv-456" || got.Status != "COMPLETED" {
		t.Errorf("unexpected response: %+v", got)
	}
	if got.SubscriptionDetails == nil || got.SubscriptionDetails.ExpiredAt == nil {
		t.Errorf("expected SubscriptionDetails.ExpiredAt to be set")
	}
}
