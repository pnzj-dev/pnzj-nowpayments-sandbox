package main

import (
	"crypto/rand"
	"math/big"
	mrand "math/rand"
	"sync"
)

// Realistic reference data so sandbox responses are shape/value-compatible
// with the real NOWPayments API.

const base58Alphabet = "123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz"
const bech32Charset = "023456789acdefghjklmnpqrstuvwxyz"

func randFrom(charset string, n int) string {
	b := make([]byte, n)
	max := big.NewInt(int64(len(charset)))
	for i := range b {
		idx, _ := rand.Int(rand.Reader, max)
		b[i] = charset[idx.Int64()]
	}
	return string(b)
}

func hexChars(n int) string {
	return randFrom("0123456789abcdef", n)
}

// Address returns a plausible address for a NOWPayments currency code.
func Address(currency string) string {
	switch currency {
	case "btc":
		return "bc1q" + randFrom(bech32Charset, 38)
	case "ltc":
		return "ltc1q" + randFrom(bech32Charset, 38)
	case "eth", "usdc", "usdterc20", "usdcbase", "ethbsc", "bnb":
		return "0x" + hexChars(40)
	case "trx", "usdttrc20":
		return "T" + randFrom(base58Alphabet, 33)
	case "doge":
		return "D" + randFrom(base58Alphabet, 33)
	case "xrp":
		return "r" + randFrom(base58Alphabet, 33)
	case "sol", "usdcsol":
		return randFrom(base58Alphabet, 43)
	default:
		return "0x" + hexChars(40)
	}
}

// Network returns a realistic network name for a currency.
func Network(currency string) string {
	switch currency {
	case "usdttrc20", "trx":
		return "trc20"
	case "usdterc20", "eth", "usdc":
		return "erc20"
	case "usdcbase", "ethbsc":
		return "evm"
	case "btc":
		return "bitcoin"
	case "ltc":
		return "litecoin"
	case "doge":
		return "dogecoin"
	default:
		return currency
	}
}

// LargeID returns a realistic 10-digit payment/invoice/payout id.
func LargeID(seq int64) int64 {
	base := int64(5_000_000_000) + int64(mrand.Intn(1_000_000_000))
	return base + seq
}

// TokenID returns a realistic invoice token id (hex).
func TokenID() string {
	return hexChars(24)
}

// USD-per-coin reference rates (plausible late-2026 values). Used only as the
// offline/deterministic fallback; live rates (DIA, no API key) are preferred
// in live mode and refreshed alongside currencies.
var usdRates = map[string]float64{
	"btc": 95000, "eth": 3200, "ltc": 90, "doge": 0.30, "usdttrc20": 1, "usdc": 1,
	"usdterc20": 1, "trx": 0.24, "bnb": 600, "xrp": 2.3, "sol": 180, "kas": 0.12,
	"not": 0.008, "peipei": 0.000001, "tet": 0.5, "bat": 0.18, "hoge": 0.00002,
}

// diaSymbols maps NOWPayments currency codes to DIA quotation symbols.
var diaSymbols = map[string]string{
	"btc": "BTC", "eth": "ETH", "ltc": "LTC", "doge": "DOGE", "trx": "TRX",
	"bnb": "BNB", "xrp": "XRP", "sol": "SOL", "kas": "KAS", "bat": "BAT",
}

// RateStore holds live USD rates with a thread-safe read path.
type RateStore struct {
	mu    sync.RWMutex
	rates map[string]float64
}

func NewRateStore() *RateStore {
	return &RateStore{rates: map[string]float64{}}
}

func (r *RateStore) Get(cur string) (float64, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	v, ok := r.rates[cur]
	return v, ok
}

func (r *RateStore) Set(cur string, v float64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.rates[cur] = v
}

func isStablecoin(cur string) bool {
	switch cur {
	case "usdttrc20", "usdc", "usdterc20", "usdt", "usdcbase", "usdcsol", "usd1", "usd1sol":
		return true
	}
	return false
}

// usdRate returns the live rate when available, else the fallback table, else 1.
func usdRate(rates *RateStore, cur string) float64 {
	if isStablecoin(cur) {
		return 1.0
	}
	if v, ok := rates.Get(cur); ok && v > 0 {
		return v
	}
	if v, ok := usdRates[cur]; ok && v > 0 {
		return v
	}
	return 1.0
}
