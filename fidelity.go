package main

import (
	"crypto/rand"
	"math/big"
	mrand "math/rand"
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
// offline/deterministic fallback; live estimate is preferred in live mode.
var usdRates = map[string]float64{
	"btc": 95000, "eth": 3200, "ltc": 90, "doge": 0.30, "usdttrc20": 1, "usdc": 1,
	"usdterc20": 1, "trx": 0.24, "bnb": 600, "xrp": 2.3, "sol": 180, "kas": 0.12,
	"not": 0.008, "peipei": 0.000001, "tet": 0.5, "bat": 0.18, "hoge": 0.00002,
}

func usdRate(currency string) float64 {
	if r, ok := usdRates[currency]; ok && r > 0 {
		return r
	}
	return 1.0
}
