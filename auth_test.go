package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"
)

func authTestApp(cfg Config) *fiber.App {
	app := fiber.New()
	s := &Server{cfg: cfg}
	app.Get("/v1/status", func(c fiber.Ctx) error { return c.JSON(fiber.Map{"message": "OK"}) })
	app.Post("/v1/auth", s.auth)
	api := app.Group("/v1", s.keyAuth)
	api.Get("/currencies", func(c fiber.Ctx) error { return c.JSON(fiber.Map{"ok": true}) })
	api.Get("/balance", s.bearerAuth, func(c fiber.Ctx) error { return c.JSON(fiber.Map{"ok": true}) })
	return app
}

func req(app *fiber.App, method, path string, headers map[string]string, body string) (int, []byte) {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		r.Header.Set("Content-Type", "application/json")
	}
	for k, v := range headers {
		r.Header.Set(k, v)
	}
	resp, _ := app.Test(r)
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, b
}

func TestStatusPublicNoKey(t *testing.T) {
	cfg := LoadConfig()
	cfg.LatencyMin, cfg.LatencyMax = 0, 0
	code, _ := req(authTestApp(cfg), http.MethodGet, "/v1/status", nil, "")
	if code != 200 {
		t.Fatalf("status should be public, got %d", code)
	}
}

func TestCurrenciesRequiresKey(t *testing.T) {
	cfg := LoadConfig()
	cfg.LatencyMin, cfg.LatencyMax = 0, 0
	// strict mode
	cfg.APIKeys = []string{"sandbox-key"}
	app := authTestApp(cfg)

	if code, _ := req(app, http.MethodGet, "/v1/currencies", nil, ""); code != 401 {
		t.Fatalf("missing key -> expected 401, got %d", code)
	}
	if code, _ := req(app, http.MethodGet, "/v1/currencies", map[string]string{"x-api-key": "wrong"}, ""); code != 401 {
		t.Fatalf("wrong key -> expected 401, got %d", code)
	}
	if code, _ := req(app, http.MethodGet, "/v1/currencies", map[string]string{"x-api-key": "sandbox-key"}, ""); code != 200 {
		t.Fatalf("correct key -> expected 200, got %d", code)
	}
}

func TestLenientModeAnyKey(t *testing.T) {
	cfg := LoadConfig()
	cfg.LatencyMin, cfg.LatencyMax = 0, 0
	app := authTestApp(cfg) // APIKeys empty = lenient
	if code, _ := req(app, http.MethodGet, "/v1/currencies", map[string]string{"x-api-key": "anything"}, ""); code != 200 {
		t.Fatalf("lenient mode: any key -> 200, got %d", code)
	}
	if code, _ := req(app, http.MethodGet, "/v1/currencies", nil, ""); code != 401 {
		t.Fatalf("lenient mode: missing key -> 401, got %d", code)
	}
}

func TestBearerProtected(t *testing.T) {
	cfg := LoadConfig()
	cfg.LatencyMin, cfg.LatencyMax = 0, 0
	app := authTestApp(cfg)
	key := map[string]string{"x-api-key": "k"}

	if code, _ := req(app, http.MethodGet, "/v1/balance", key, ""); code != 401 {
		t.Fatalf("balance without JWT -> expected 401, got %d", code)
	}
	if code, _ := req(app, http.MethodGet, "/v1/balance", merge(key, map[string]string{"Authorization": "Bearer garbage.token.here"}), ""); code != 401 {
		t.Fatalf("balance with bad JWT -> expected 401, got %d", code)
	}
	// get a real token from /v1/auth
	_, body := req(app, http.MethodPost, "/v1/auth", nil, `{"email":"a@b.c","password":"pw"}`)
	var a struct {
		Token string `json:"token"`
	}
	_ = json.Unmarshal(body, &a)
	if a.Token == "" {
		t.Fatalf("no token from /v1/auth")
	}
	if code, _ := req(app, http.MethodGet, "/v1/balance", merge(key, map[string]string{"Authorization": "Bearer " + a.Token}), ""); code != 200 {
		t.Fatalf("balance with valid JWT -> expected 200, got %d", code)
	}
}

func merge(a, b map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range a {
		out[k] = v
	}
	for k, v := range b {
		out[k] = v
	}
	return out
}
