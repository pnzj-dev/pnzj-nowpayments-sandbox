package main

import (
	"log"
	"time"
)

// Payment represents the simulated NOWPayments payment shape.
type Payment struct {
	PaymentID             int64   `json:"payment_id"`
	InvoiceID             *string `json:"invoice_id"`
	PaymentStatus         string  `json:"payment_status"`
	PayAddress            string  `json:"pay_address"`
	PayinExtraID          string  `json:"payin_extra_id"`
	PriceAmount           float64 `json:"price_amount"`
	PriceCurrency         string  `json:"price_currency"`
	PayAmount             float64 `json:"pay_amount"`
	ActuallyPaid          float64 `json:"actually_paid"`
	ActuallyPaidAtFiat    float64 `json:"actually_paid_at_fiat"`
	PayCurrency           string  `json:"pay_currency"`
	OrderID               string  `json:"order_id"`
	OrderDescription      string  `json:"order_description"`
	IPNCallbackURL        string  `json:"ipn_callback_url"`
	CreatedAt             string  `json:"created_at"`
	UpdatedAt             string  `json:"updated_at"`
	ExpirationEstimateDate string `json:"expiration_estimate_date"`
	Network               string  `json:"network"`
	NetworkPrecision      int     `json:"network_precision"`
}

type Payout struct {
	BatchWithdrawalID int64  `json:"batch_withdrawal_id"`
	Status            string `json:"status"`
	Address           string `json:"address"`
	Currency          string `json:"currency"`
	Amount            float64 `json:"amount"`
	IPNCallbackURL    string `json:"ipn_callback_url"`
	CreatedAt         string  `json:"created_at"`
	UpdatedAt         string  `json:"updated_at"`
}

// RunLifecycle advances waiting payments -> confirming -> paid and pending
// payouts -> sending -> finished, firing signed webhooks on transitions.
func RunLifecycle(store *Store, cfg Config, stop <-chan struct{}) {
	tick := time.NewTicker(5 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-tick.C:
			advancePayments(store, cfg)
			advancePayouts(store, cfg)
		case <-stop:
			return
		}
	}
}

func advancePayments(store *Store, cfg Config) {
	for _, raw := range store.List("payment") {
		var p Payment
		if err := jsonUnmarshal(raw, &p); err != nil {
			continue
		}
		created, err := time.Parse(time.RFC3339, p.CreatedAt)
		if err != nil {
			continue
		}
		age := time.Since(created)
		next := p.PaymentStatus
		switch p.PaymentStatus {
		case "waiting":
			if age >= cfg.AutoPayAfter {
				next = "paid"
			} else if age >= cfg.AutoPayAfter/2 {
				next = "confirming"
			}
		case "confirming":
			if age >= cfg.AutoPayAfter {
				next = "paid"
			}
		}
		if next != p.PaymentStatus {
			p.PaymentStatus = next
			p.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
			if next == "paid" {
				p.ActuallyPaid = p.PayAmount
			}
			_ = store.Put("payment", itoa64(p.PaymentID), p)
			if p.IPNCallbackURL != "" {
				go func(pay Payment, st string) {
					payload := map[string]any{
						"payment_id":     pay.PaymentID,
						"payment_status": st,
						"pay_address":    pay.PayAddress,
						"price_amount":   pay.PriceAmount,
						"price_currency": pay.PriceCurrency,
						"pay_amount":     pay.PayAmount,
						"actually_paid":  pay.ActuallyPaid,
						"pay_currency":   pay.PayCurrency,
						"order_id":       pay.OrderID,
					}
					if _, err := SendIPN(cfg.IPNSecret, pay.IPNCallbackURL, payload); err != nil {
						log.Printf("ipn send failed: %v", err)
					}
				}(p, next)
			}
		}
	}
}

func advancePayouts(store *Store, cfg Config) {
	for _, raw := range store.List("payout") {
		var p Payout
		if err := jsonUnmarshal(raw, &p); err != nil {
			continue
		}
		created, err := time.Parse(time.RFC3339, p.CreatedAt)
		if err != nil {
			continue
		}
		next := p.Status
		age := time.Since(created)
		switch p.Status {
		case "pending":
			if age >= cfg.PayoutFinishAfter {
				next = "finished"
			} else if age >= cfg.PayoutFinishAfter/2 {
				next = "sending"
			}
		case "sending":
			if age >= cfg.PayoutFinishAfter {
				next = "finished"
			}
		}
		if next != p.Status {
			p.Status = next
			p.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
			_ = store.Put("payout", itoa64(p.BatchWithdrawalID), p)
			if p.IPNCallbackURL != "" {
				go func(po Payout, st string) {
					payload := map[string]any{
						"batch_withdrawal_id": po.BatchWithdrawalID,
						"status":              st,
						"address":             po.Address,
						"currency":            po.Currency,
						"amount":              po.Amount,
					}
					_, _ = SendIPN(cfg.IPNSecret, po.IPNCallbackURL, payload)
				}(p, next)
			}
		}
	}
}
