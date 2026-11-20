package main

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"sort"
	"time"
)

// sortDeep recursively sorts object keys (arrays keep order) — mirrors the
// NOWPayments IPN signature algorithm (and @pnzj-dev/nowpayments).
func sortDeep(v any) any {
	switch x := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		out := map[string]any{}
		for _, k := range keys {
			out[k] = sortDeep(x[k])
		}
		return out
	case []any:
		for i := range x {
			x[i] = sortDeep(x[i])
		}
		return x
	default:
		return v
	}
}

// SignIPN produces the HMAC-SHA512 hex signature over the deep-sorted JSON.
func SignIPN(secret string, payload any) (string, error) {
	sorted := sortDeep(payload)
	canonical, err := json.Marshal(sorted)
	if err != nil {
		return "", err
	}
	mac := hmac.New(sha512.New, []byte(secret))
	mac.Write(canonical)
	return hex.EncodeToString(mac.Sum(nil)), nil
}

// SendIPN posts the signed payload to the callback URL and returns the signature.
func SendIPN(secret, callbackURL string, payload map[string]any) (string, error) {
	sig, err := SignIPN(secret, payload)
	if err != nil {
		return "", err
	}
	body, _ := json.Marshal(payload)
	client := http.Client{Timeout: 5 * time.Second}
	req, err := http.NewRequest(http.MethodPost, callbackURL, bytes.NewReader(body))
	if err != nil {
		return sig, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-nowpayments-sig", sig)
	_, err = client.Do(req) // best-effort delivery; signature is what matters
	return sig, err
}
