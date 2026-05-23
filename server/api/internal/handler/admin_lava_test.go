package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"vpnapp/server/api/internal/lava"

	"github.com/gofiber/fiber/v2"
	"go.uber.org/zap"
)

func TestAdminListLavaProducts_FlattensProductOfferPrice(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v2/products" {
			t.Errorf("expected /api/v2/products, got %s", r.URL.Path)
		}
		w.WriteHeader(200)
		proName := "Pro"
		_ = json.NewEncoder(w).Encode(lava.ProductsResponse{
			Items: []lava.ProductsItem{
				{Type: "PRODUCT", Data: lava.ProductItemResponse{
					ID: "prod-1", Title: &proName, Type: "SUBSCRIPTION",
					Offers: []lava.ProductOffer{
						{ID: "off-month-usd", Name: "Monthly", Prices: []lava.ProductOfferPrice{
							{Amount: 5.00, Currency: "USD", Periodicity: "MONTHLY"},
							{Amount: 499.0, Currency: "RUB", Periodicity: "MONTHLY"},
						}},
						{ID: "off-year-usd", Name: "Yearly", Prices: []lava.ProductOfferPrice{
							{Amount: 49.99, Currency: "USD", Periodicity: "PERIOD_YEAR"},
						}},
					},
				}},
			},
		})
	}))
	defer srv.Close()
	client := lava.NewForTest("test-key", srv.URL)

	app := fiber.New()
	app.Get("/api/v1/admin/lava/products", AdminListLavaProducts(zap.NewNop(), client))
	resp, err := app.Test(httptest.NewRequest("GET", "/api/v1/admin/lava/products", nil))
	if err != nil || resp.StatusCode != 200 {
		buf := new(bytes.Buffer)
		_, _ = buf.ReadFrom(resp.Body)
		t.Fatalf("expected 200, got %d body=%s err=%v", resp.StatusCode, buf.String(), err)
	}
	var body struct {
		Data []lavaProductRow `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// Expect 3 rows: month-USD, month-RUB, year-USD.
	if len(body.Data) != 3 {
		t.Fatalf("expected 3 flattened rows, got %d: %+v", len(body.Data), body.Data)
	}
	// Check first row shape.
	if body.Data[0].ProductName != "Pro" || body.Data[0].OfferID != "off-month-usd" || body.Data[0].Periodicity != "MONTHLY" {
		t.Errorf("unexpected first row: %+v", body.Data[0])
	}
	// Second row should be month-RUB.
	if body.Data[1].Currency != "RUB" || body.Data[1].Amount != 499.0 {
		t.Errorf("unexpected second row: %+v", body.Data[1])
	}
	// Third row should be year-USD with off-year-usd.
	if body.Data[2].OfferID != "off-year-usd" || body.Data[2].Periodicity != "PERIOD_YEAR" {
		t.Errorf("unexpected third row: %+v", body.Data[2])
	}
}

func TestAdminListLavaProducts_LavaError_Returns502(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		_, _ = w.Write([]byte(`{"message":"upstream error"}`))
	}))
	defer srv.Close()
	client := lava.NewForTest("test-key", srv.URL)

	app := fiber.New()
	app.Get("/api/v1/admin/lava/products", AdminListLavaProducts(zap.NewNop(), client))
	resp, err := app.Test(httptest.NewRequest("GET", "/api/v1/admin/lava/products", nil))
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != 502 {
		t.Errorf("expected 502 on lava error, got %d", resp.StatusCode)
	}
}
