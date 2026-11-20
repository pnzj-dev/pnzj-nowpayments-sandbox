package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gofiber/fiber/v3"
)

func main() {
	cfg := LoadConfig()
	store, err := NewStore(cfg.DBPath)
	if err != nil {
		log.Fatalf("store: %v", err)
	}
	defer store.Close()

	data := NewDataService(cfg.DataMode, cfg.CurrenciesRefresh)
	stop := make(chan struct{})
	go data.StartRefreshLoop(stop)
	go RunLifecycle(store, cfg, stop)

	app := fiber.New(fiber.Config{
	})

	// simulated latency middleware
	app.Use(func(c fiber.Ctx) error {
		d := cfg.LatencyMin + time.Duration(rand.Int63n(int64(cfg.LatencyMax-cfg.LatencyMin)+1))
		time.Sleep(d)
		return c.Next()
	})

	s := &Server{cfg: cfg, store: store, data: data}

	api := app.Group("/v1")
	api.Get("/status", func(c fiber.Ctx) error { return c.JSON(fiber.Map{"message": "OK"}) })
	api.Post("/auth", s.auth)

	api.Get("/currencies", s.currencies)
	api.Get("/full-currencies", func(c fiber.Ctx) error { return rawJSON(c, s.data.snapshot("full")) })
	api.Get("/merchant/coins", func(c fiber.Ctx) error { return rawJSON(c, s.data.snapshot("merchant")) })
	api.Get("/min-amount", s.minAmount)
	api.Get("/estimate", s.estimate)

	api.Post("/payment", s.createPayment)
	api.Get("/payment", s.listPayments)
	api.Get("/payment/:id", s.getPayment)
	api.Post("/payment/:id/update-merchant-estimate", s.updateEstimate)

	api.Post("/invoice", s.createInvoice)
	api.Post("/invoice-payment", s.createPayment)

	api.Get("/balance", s.balance)

	api.Post("/payout", s.createPayout)
	api.Get("/payout", s.listPayouts)
	api.Get("/payout/fee", s.payoutFee)
	api.Post("/payout/validate-address", s.validateAddress)
	api.Get("/payout-withdrawal/min-amount/:coin", s.minPayoutAmount)
	api.Post("/payout/:id/verify", s.verifyPayout)
	api.Post("/payout/:id/cancel", s.cancelPayout)
	api.Post("/payout/:id/cancel-batch", s.cancelPayout)
	api.Get("/payout/:id", s.getPayout)

	api.Post("/subscriptions", s.createSubscription)
	api.Get("/subscriptions", s.listSubscriptions)
	api.Get("/subscriptions/plans", s.listPlans)
	api.Post("/subscriptions/plans", s.createPlan)
	api.Patch("/subscriptions/plans/:id", s.updatePlan)
	api.Get("/subscriptions/plans/:id", s.getPlan)
	api.Get("/subscriptions/:id", s.getSubscription)
	api.Delete("/subscriptions/:id", s.cancelSubscription)

	api.Post("/sub-partner/balance", s.createSubPartnerAccount)
	api.Get("/sub-partner/balance/:id", s.getSubPartnerBalance)
	api.Get("/sub-partner", s.listSubPartners)
	api.Get("/sub-partner/transfers", s.listTransfers)
	api.Get("/sub-partner/transfer/:id", s.getTransfer)
	api.Post("/sub-partner/transfer", s.createTransfer)
	api.Post("/sub-partner/payment", s.subPartnerOp)
	api.Post("/sub-partner/deposit", s.subPartnerOp)
	api.Post("/sub-partner/write-off", s.subPartnerOp)
	api.Get("/sub-partner/payments", s.listSubPartnerPayments)

	api.Post("/conversion", s.createConversion)
	api.Get("/conversion", s.listConversions)
	api.Get("/conversion/:id", s.getConversion)

	// fiat payouts: minimal catalogs (empty), echo accounts/payouts
	api.Get("/fiat-payouts/providers", func(c fiber.Ctx) error { return c.JSON(fiber.Map{"providers": []any{}}) })
	api.Get("/fiat-payouts/fiat-currencies", func(c fiber.Ctx) error { return c.JSON(fiber.Map{"currencies": []any{}}) })
	api.Get("/fiat-payouts/crypto-currencies", func(c fiber.Ctx) error { return c.JSON(fiber.Map{"currencies": []any{}}) })
	api.Get("/fiat-payouts/payment-methods", func(c fiber.Ctx) error { return c.JSON(fiber.Map{"methods": []any{}}) })
	api.Post("/fiat-payouts/account", func(c fiber.Ctx) error { return c.JSON(fiber.Map{"id": "sandbox-fiat-account"}) })
	api.Get("/fiat-payouts/accounts", func(c fiber.Ctx) error { return c.JSON(fiber.Map{"accounts": []any{}}) })
	api.Post("/fiat-payouts", func(c fiber.Ctx) error { return c.JSON(fiber.Map{"id": "sandbox-fiat-payout", "status": "processing"}) })
	api.Get("/fiat-payouts", func(c fiber.Ctx) error { return c.JSON(fiber.Map{"payouts": []any{}}) })

	go func() {
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
		<-sig
		close(stop)
		_ = app.Shutdown()
	}()

	log.Printf("pnzj-nowpayments-sandbox listening on :%s (mode=%s)", cfg.Port, cfg.DataMode)
	if err := app.Listen(":" + cfg.Port); err != nil {
		log.Fatal(err)
	}
}

func rawJSON(c fiber.Ctx, raw json.RawMessage) error {
	if len(raw) == 0 {
		return c.JSON(fiber.Map{})
	}
	c.Type("application/json")
	return c.Send(raw)
}

type Server struct {
	cfg   Config
	store *Store
	data  *DataService
}

// ---- auth ----

func (s *Server) auth(c fiber.Ctx) error {
	type creds struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	var in creds
	_ = c.Bind().Body(&in)
	now := time.Now()
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))
	payload := base64.RawURLEncoding.EncodeToString([]byte(fmt.Sprintf(`{"exp":%d,"sub":"%s"}`, now.Add(24*time.Hour).Unix(), in.Email)))
	signing := header + "." + payload
	mac := hmac.New(sha256.New, []byte(s.cfg.JWTSecret))
	mac.Write([]byte(signing))
	sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return c.JSON(fiber.Map{"token": signing + "." + sig})
}

// ---- reference data ----

func (s *Server) currencies(c fiber.Ctx) error {
	if c.Query("fixed_rate") == "true" {
		return rawJSON(c, s.data.snapshot("fixed_rate"))
	}
	return rawJSON(c, s.data.snapshot("currencies"))
}

func (s *Server) minAmount(c fiber.Ctx) error {
	from := c.Query("currency_from")
	to := c.Query("currency_to")
	min, _ := SimulateQuote(from, to, 0)
	return c.JSON(fiber.Map{
		"currency_from": from, "currency_to": to, "min_amount": min,
	})
}

func (s *Server) estimate(c fiber.Ctx) error {
	from := c.Query("currency_from")
	to := c.Query("currency_to")
	amount := parseFloat(c.Query("amount"))
	_, estimated := SimulateQuote(from, to, amount)
	return c.JSON(fiber.Map{
		"currency_from": from, "currency_to": to,
		"amount_from": amount, "estimated_amount": estimated,
	})
}

// ---- payments ----

func (s *Server) createPayment(c fiber.Ctx) error {
	var in map[string]any
	_ = c.Bind().Body(&in)
	id := s.store.NextID("payment")
	now := time.Now().UTC().Format(time.RFC3339)
	p := Payment{
		PaymentID:    parseInt64(id),
		PaymentStatus: "waiting",
		PayAddress:   fmt.Sprintf("sandbox-%s-%s", coinCode(in["pay_currency"]), id),
		PriceAmount:  num(in["price_amount"]),
		PriceCurrency: str(in["price_currency"], "usd"),
		PayCurrency:  str(in["pay_currency"], "btc"),
		PayAmount:    num(in["pay_amount"]),
		OrderID:      str(in["order_id"], ""),
		OrderDescription: str(in["order_description"], ""),
		IPNCallbackURL: str(in["ipn_callback_url"], ""),
		CreatedAt:    now,
		UpdatedAt:    now,
		Network:      "sandbox",
	}
	_ = s.store.Put("payment", id, p)
	return c.JSON(p)
}

func (s *Server) getPayment(c fiber.Ctx) error {
	var p Payment
	if !s.store.Get("payment", c.Params("id"), &p) {
		return c.Status(404).JSON(fiber.Map{"message": "Payment not found"})
	}
	return c.JSON(p)
}

func (s *Server) listPayments(c fiber.Ctx) error {
	items := []json.RawMessage{}
	for _, raw := range s.store.List("payment") {
		items = append(items, raw)
	}
	return c.JSON(fiber.Map{"data": rawSlice(items), "limit": len(items), "page": 0, "pagesCount": 1, "total": len(items)})
}

func (s *Server) updateEstimate(c fiber.Ctx) error {
	var p Payment
	if !s.store.Get("payment", c.Params("id"), &p) {
		return c.Status(404).JSON(fiber.Map{"message": "Payment not found"})
	}
	p.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	_ = s.store.Put("payment", c.Params("id"), p)
	return c.JSON(p)
}

func (s *Server) createInvoice(c fiber.Ctx) error {
	var in map[string]any
	_ = c.Bind().Body(&in)
	id := s.store.NextID("invoice")
	inv := fiber.Map{
		"id": parseInt64(id), "token_id": "sandbox-" + id,
		"order_id": str(in["order_id"], ""),
		"order_description": str(in["order_description"], ""),
		"price_amount": num(in["price_amount"]),
		"price_currency": str(in["price_currency"], "usd"),
		"pay_currency": str(in["pay_currency"], "btc"),
		"ipn_callback_url": str(in["ipn_callback_url"], ""),
		"invoice_url": fmt.Sprintf("%s/invoice/sandbox-%s", s.cfg.BaseURL, id),
		"success_url": str(in["success_url"], ""),
		"cancel_url": str(in["cancel_url"], ""),
		"created_at": time.Now().UTC().Format(time.RFC3339),
		"updated_at": time.Now().UTC().Format(time.RFC3339),
	}
	_ = s.store.Put("invoice", id, inv)
	return c.JSON(inv)
}

func (s *Server) balance(c fiber.Ctx) error {
	return c.JSON(fiber.Map{
		"balance": map[string]any{"btc": 1.0, "usdttrc20": 1000.0},
	})
}

// ---- payouts ----

func (s *Server) createPayout(c fiber.Ctx) error {
	var in struct {
		Withdrawals []struct {
			Address  string  `json:"address"`
			Currency string  `json:"currency"`
			Amount   float64 `json:"amount"`
		} `json:"withdrawals"`
		IPNCallbackURL string `json:"ipn_callback_url"`
	}
	_ = c.Bind().Body(&in)
	id := s.store.NextID("payout")
	now := time.Now().UTC().Format(time.RFC3339)
	addr, cur, amt := "", "", 0.0
	if len(in.Withdrawals) > 0 {
		addr, cur, amt = in.Withdrawals[0].Address, in.Withdrawals[0].Currency, in.Withdrawals[0].Amount
	}
	p := Payout{BatchWithdrawalID: parseInt64(id), Status: "pending", Address: addr, Currency: cur, Amount: amt, IPNCallbackURL: in.IPNCallbackURL, CreatedAt: now, UpdatedAt: now}
	_ = s.store.Put("payout", id, p)
	return c.JSON(fiber.Map{"batch_withdrawal_id": p.BatchWithdrawalID, "status": p.Status})
}

func (s *Server) verifyPayout(c fiber.Ctx) error {
	var p Payout
	if !s.store.Get("payout", c.Params("id"), &p) {
		return c.Status(404).JSON(fiber.Map{"message": "Payout not found"})
	}
	p.Status = "sending"
	_ = s.store.Put("payout", c.Params("id"), p)
	return c.JSON(fiber.Map{"batch_withdrawal_id": p.BatchWithdrawalID, "status": "sending", "verified": true})
}

func (s *Server) getPayout(c fiber.Ctx) error {
	var p Payout
	if !s.store.Get("payout", c.Params("id"), &p) {
		return c.Status(404).JSON(fiber.Map{"message": "Payout not found"})
	}
	return c.JSON(p)
}

func (s *Server) listPayouts(c fiber.Ctx) error {
	return c.JSON(fiber.Map{"payouts": rawSlice(s.store.List("payout"))})
}

func (s *Server) payoutFee(c fiber.Ctx) error {
	return c.JSON(fiber.Map{"currency": c.Query("currency"), "fee": 0.0001})
}

func (s *Server) validateAddress(c fiber.Ctx) error {
	var in struct {
		Address  string `json:"address"`
		Currency string `json:"currency"`
	}
	_ = c.Bind().Body(&in)
	return c.JSON(fiber.Map{"address": in.Address, "currency": in.Currency, "valid": true})
}

func (s *Server) minPayoutAmount(c fiber.Ctx) error {
	return c.JSON(fiber.Map{"currency": c.Params("coin"), "min_amount": 0.0005})
}

func (s *Server) cancelPayout(c fiber.Ctx) error {
	var p Payout
	if s.store.Get("payout", c.Params("id"), &p) {
		p.Status = "cancelled"
		_ = s.store.Put("payout", c.Params("id"), p)
		return c.JSON(fiber.Map{"batch_withdrawal_id": p.BatchWithdrawalID, "status": "cancelled"})
	}
	return c.Status(404).JSON(fiber.Map{"message": "Payout not found"})
}

// ---- subscriptions ----

func (s *Server) createSubscription(c fiber.Ctx) error {
	var in map[string]any
	_ = c.Bind().Body(&in)
	id := s.store.NextID("subscription")
	sub := fiber.Map{
		"id": id, "subscription_id": id, "status": "ACTIVE",
		"customer_id": str(in["customer_id"], ""), "plan_id": str(in["plan_id"], ""),
		"plan_name": str(in["plan_name"], ""), "period": str(in["period"], "monthly"),
		"price_amount": num(in["price_amount"]), "price_currency": str(in["price_currency"], "usd"),
	}
	_ = s.store.Put("subscription", id, sub)
	return c.JSON(sub)
}

func (s *Server) getSubscription(c fiber.Ctx) error {
	var sub fiber.Map
	if !s.store.Get("subscription", c.Params("id"), &sub) {
		return c.Status(404).JSON(fiber.Map{"message": "Subscription not found"})
	}
	return c.JSON(sub)
}

func (s *Server) listSubscriptions(c fiber.Ctx) error {
	return c.JSON(fiber.Map{"result": rawSlice(s.store.List("subscription"))})
}

func (s *Server) cancelSubscription(c fiber.Ctx) error {
	var sub map[string]any
	if !s.store.Get("subscription", c.Params("id"), &sub) {
		return c.Status(404).JSON(fiber.Map{"message": "Subscription not found"})
	}
	sub["status"] = "CANCELLED"
	_ = s.store.Put("subscription", c.Params("id"), sub)
	return c.JSON(sub)
}

func (s *Server) createPlan(c fiber.Ctx) error {
	var in map[string]any
	_ = c.Bind().Body(&in)
	id := s.store.NextID("plan")
	plan := fiber.Map{"id": id, "title": str(in["plan_name"], "Sandbox Plan"), "interval_day": "30"}
	_ = s.store.Put("plan", id, plan)
	return c.JSON(plan)
}

func (s *Server) updatePlan(c fiber.Ctx) error {
	var in map[string]any
	_ = c.Bind().Body(&in)
	plan := fiber.Map{"id": c.Params("id"), "title": str(in["plan_name"], "Sandbox Plan"), "interval_day": "30"}
	_ = s.store.Put("plan", c.Params("id"), plan)
	return c.JSON(plan)
}

func (s *Server) getPlan(c fiber.Ctx) error {
	var plan map[string]any
	if !s.store.Get("plan", c.Params("id"), &plan) {
		return c.Status(404).JSON(fiber.Map{"message": "Plan not found"})
	}
	return c.JSON(plan)
}

func (s *Server) listPlans(c fiber.Ctx) error {
	return c.JSON(fiber.Map{"result": rawSlice(s.store.List("plan"))})
}

// ---- sub-partner (custody) ----

func (s *Server) createSubPartnerAccount(c fiber.Ctx) error {
	var in map[string]any
	_ = c.Bind().Body(&in)
	id := str(in["id"], s.store.NextID("subpartner"))
	_ = s.store.Put("subpartner", id, fiber.Map{"id": id, "balance": map[string]any{"usdttrc20": 1000.0}})
	return c.JSON(fiber.Map{"id": id})
}

func (s *Server) getSubPartnerBalance(c fiber.Ctx) error {
	var acct map[string]any
	if !s.store.Get("subpartner", c.Params("id"), &acct) {
		return c.Status(404).JSON(fiber.Map{"message": "Account not found"})
	}
	return c.JSON(acct)
}

func (s *Server) listSubPartners(c fiber.Ctx) error {
	return c.JSON(fiber.Map{"result": rawSlice(s.store.List("subpartner"))})
}

func (s *Server) listTransfers(c fiber.Ctx) error { return c.JSON(fiber.Map{"result": []any{}}) }
func (s *Server) getTransfer(c fiber.Ctx) error {
	return c.JSON(fiber.Map{"id": c.Params("id"), "status": "COMPLETED"})
}
func (s *Server) createTransfer(c fiber.Ctx) error {
	id := s.store.NextID("transfer")
	_ = s.store.Put("transfer", id, fiber.Map{"id": id, "status": "COMPLETED"})
	return c.JSON(fiber.Map{"id": id, "status": "COMPLETED"})
}
func (s *Server) subPartnerOp(c fiber.Ctx) error {
	id := s.store.NextID("subpartner-op")
	return c.JSON(fiber.Map{"id": id, "status": "COMPLETED"})
}
func (s *Server) listSubPartnerPayments(c fiber.Ctx) error {
	return c.JSON(fiber.Map{"result": []any{}})
}

// ---- conversions ----

func (s *Server) createConversion(c fiber.Ctx) error {
	var in map[string]any
	_ = c.Bind().Body(&in)
	id := s.store.NextID("conversion")
	conv := fiber.Map{
		"id": id, "status": "finished",
		"from_currency": str(in["from_currency"], "btc"), "to_currency": str(in["to_currency"], "usdttrc20"),
		"amount": num(in["amount"]),
	}
	_ = s.store.Put("conversion", id, conv)
	return c.JSON(conv)
}

func (s *Server) getConversion(c fiber.Ctx) error {
	var conv map[string]any
	if !s.store.Get("conversion", c.Params("id"), &conv) {
		return c.Status(404).JSON(fiber.Map{"message": "Conversion not found"})
	}
	return c.JSON(conv)
}

func (s *Server) listConversions(c fiber.Ctx) error {
	return c.JSON(fiber.Map{"result": rawSlice(s.store.List("conversion")), "count": 0})
}

// ---- helpers ----

func str(v any, def string) string {
	if s, ok := v.(string); ok && s != "" {
		return s
	}
	return def
}

func num(v any) float64 {
	switch x := v.(type) {
	case float64:
		return x
	case int:
		return float64(x)
	case json.Number:
		f, _ := x.Float64()
		return f
	}
	return 0
}

func parseInt64(s string) int64 {
	var n int64
	fmt.Sscanf(s, "%d", &n)
	return n
}

func parseFloat(s string) float64 {
	var f float64
	fmt.Sscanf(s, "%f", &f)
	return f
}

func coinCode(v any) string {
	c := str(v, "btc")
	if len(c) > 6 {
		c = c[:6]
	}
	return c
}

func rawSlice(items []json.RawMessage) []json.RawMessage {
	if items == nil {
		return []json.RawMessage{}
	}
	return items
}
