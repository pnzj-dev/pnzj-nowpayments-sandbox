package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"
)

func newTestServer(t *testing.T) (*fiber.App, *Store, Config) {
	t.Helper()
	cfg := LoadConfig()
	cfg.LatencyMin = 0
	cfg.LatencyMax = 0
	cfg.DataMode = "offline"
	cfg.DBPath = ":memory:"
	cfg.IPNSecret = "test-secret"
	store, err := NewStore(cfg.DBPath)
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	data := NewDataService(cfg.DataMode, cfg.CurrenciesRefresh)
	s := &Server{cfg: cfg, store: store, data: data}
	app := fiber.New()
	app.Get("/v1/currencies", s.currencies)
	app.Get("/v1/estimate", s.estimate)
	app.Post("/v1/payment", s.createPayment)
	app.Get("/v1/payment/:id", s.getPayment)
	app.Get("/v1/payment", s.listPayments)
	return app, store, cfg
}

func post(t *testing.T, app *fiber.App, path, body string) (int, []byte) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, b
}

func get(t *testing.T, app *fiber.App, path string) (int, []byte) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, b
}

func TestCreateAndGetPayment(t *testing.T) {
	app, _, _ := newTestServer(t)
	code, body := post(t, app, "/v1/payment", `{"price_amount":10,"price_currency":"usd","pay_currency":"btc","order_id":"o-1"}`)
	if code != 200 {
		t.Fatalf("create payment status=%d body=%s", code, body)
	}
	var p map[string]any
	_ = json.Unmarshal(body, &p)
	if p["payment_status"] != "waiting" {
		t.Fatalf("expected waiting, got %v", p["payment_status"])
	}
	id := fmt.Sprintf("%v", p["payment_id"])
	code, body = get(t, app, "/v1/payment/"+id)
	if code != 200 {
		t.Fatalf("get payment status=%d", code)
	}
	_ = json.Unmarshal(body, &p)
	if p["order_id"] != "o-1" {
		t.Fatalf("order_id mismatch: %v", p["order_id"])
	}
}

func TestEstimate(t *testing.T) {
	app, _, _ := newTestServer(t)
	code, body := get(t, app, "/v1/estimate?amount=100&currency_from=usd&currency_to=btc")
	if code != 200 {
		t.Fatalf("estimate status=%d", code)
	}
	var e map[string]any
	_ = json.Unmarshal(body, &e)
	if e["currency_to"] != "btc" {
		t.Fatalf("currency_to mismatch: %v", e["currency_to"])
	}
	if e["estimated_amount"].(float64) <= 0 {
		t.Fatalf("estimated_amount should be > 0")
	}
}

func TestCurrenciesOffline(t *testing.T) {
	app, _, _ := newTestServer(t)
	code, body := get(t, app, "/v1/currencies")
	if code != 200 {
		t.Fatalf("currencies status=%d", code)
	}
	var c map[string]any
	_ = json.Unmarshal(body, &c)
	cur, ok := c["currencies"].([]any)
	if !ok || len(cur) == 0 {
		t.Fatalf("expected currencies list")
	}
}

func TestIPNSignatureRoundTrip(t *testing.T) {
	payload := map[string]any{"payment_id": 1, "payment_status": "paid", "order_id": "x"}
	sig, err := SignIPN("test-secret", payload)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	// Recompute over a re-ordered payload — must match (deep-sorted canonicalization)
	reordered := map[string]any{"order_id": "x", "payment_status": "paid", "payment_id": 1}
	sig2, _ := SignIPN("test-secret", reordered)
	if sig != sig2 {
		t.Fatalf("signature not order-independent: %s != %s", sig, sig2)
	}
	// Wrong secret must differ
	sig3, _ := SignIPN("other-secret", payload)
	if sig == sig3 {
		t.Fatalf("signature should differ for wrong secret")
	}
	if len(sig) != 128 {
		t.Fatalf("expected 128 hex chars, got %d", len(sig))
	}
}
