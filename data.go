package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

// DataService serves currency/rate reference data. In "live" mode it fetches
// the real NOWPayments public endpoints on a TTL; in "offline" mode (or when a
// fetch fails) it serves bundled deterministic data.
type DataService struct {
	mode string
	mu   sync.RWMutex

	currencies     json.RawMessage
	fullCurrencies json.RawMessage
	merchantCoins  json.RawMessage
	fixedRate      json.RawMessage
	lastRefresh    time.Time
	refreshEvery   time.Duration
}

func NewDataService(mode string, refresh time.Duration) *DataService {
	d := &DataService{mode: mode, refreshEvery: refresh}
	d.loadOffline()
	d.refresh()
	return d
}

func (d *DataService) loadOffline() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.currencies = json.RawMessage(`{"currencies":["btc","eth","ltc","usdttrc20","usdc","usdterc20","trx","doge","bnb","xrp"]}`)
	d.merchantCoins = json.RawMessage(`{"selectedCurrencies":["btc","eth","ltc","usdttrc20","usdc","trx"]}`)
	d.fullCurrencies = json.RawMessage(`{"currencies":[{"id":1,"code":"btc","name":"Bitcoin","enable":true},{"id":2,"code":"eth","name":"Ethereum","enable":true},{"id":3,"code":"usdttrc20","name":"Tether (TRC20)","enable":true}]}`)
	d.fixedRate = json.RawMessage(`{"currencies":[{"min_amount":1,"max_amount":50000,"currency":"btc"},{"min_amount":0.5,"max_amount":10000,"currency":"usdttrc20"}]}`)
}

func fetchJSON(url string, out *json.RawMessage) error {
	client := http.Client{Timeout: 8 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	*out = json.RawMessage(body)
	return nil
}

func (d *DataService) refresh() {
	if d.mode != "live" {
		return
	}
	d.mu.Lock()
	d.lastRefresh = time.Now()
	d.mu.Unlock()

	var c, fc, mc, fr json.RawMessage
	const base = "https://api.nowpayments.io/v1"
	// These endpoints are public (no API key needed).
	fetchJSON(base+"/currencies", &c)
	fetchJSON(base+"/full-currencies", &fc)
	fetchJSON(base+"/merchant/coins", &mc)
	fetchJSON(base+"/currencies?fixed_rate=true", &fr)

	d.mu.Lock()
	if len(c) > 0 {
		d.currencies = c
	}
	if len(fc) > 0 {
		d.fullCurrencies = fc
	}
	if len(mc) > 0 {
		d.merchantCoins = mc
	}
	if len(fr) > 0 {
		d.fixedRate = fr
	}
	d.mu.Unlock()
}

// StartRefreshLoop refreshes on a ticker (no-op in offline mode).
func (d *DataService) StartRefreshLoop(stop <-chan struct{}) {
	if d.mode != "live" {
		return
	}
	t := time.NewTicker(d.refreshEvery)
	defer t.Stop()
	for {
		select {
		case <-t.C:
			d.refresh()
		case <-stop:
			return
		}
	}
}

func (d *DataService) snapshot(key string) json.RawMessage {
	d.mu.RLock()
	defer d.mu.RUnlock()
	switch key {
	case "currencies":
		return d.currencies
	case "full":
		return d.fullCurrencies
	case "merchant":
		return d.merchantCoins
	case "fixed_rate":
		return d.fixedRate
	}
	return nil
}

// SimulateQuote returns a plausible estimate/min-amount for a pair using the
// reference rate table (live estimate is attempted by the server in live mode).
func SimulateQuote(from, to string, amount float64) (minAmount float64, estimated float64) {
	// convert from -> usd -> to
	usd := amount * usdRate(from)
	if amount == 0 {
		usd = usdRate(to) // min-amount expressed in the target coin
	}
	estimated = usd / usdRate(to)
	minAmount = 0.5 / usdRate(to)
	return minAmount, estimated
}
