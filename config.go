package main

import (
	"os"
	"strconv"
	"strings"
	"time"
)

// Config is read from environment variables (sane defaults for a sandbox).
type Config struct {
	Port                string
	IPNSecret           string
	LatencyMin          time.Duration
	LatencyMax          time.Duration
	AutoPayAfter        time.Duration
	PayoutFinishAfter   time.Duration
	DataMode            string // "live" | "offline"
	CurrenciesRefresh   time.Duration
	DBPath              string
	BaseURL             string
	JWTSecret           string
	APIKeys             []string
	FixedRateCurrencies []string
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envDur(key string, def time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return def
}

func envInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func LoadConfig() Config {
	return Config{
		Port:              env("PORT", "8787"),
		IPNSecret:         env("IPN_SECRET", "sandbox-ipn-secret"),
		LatencyMin:        envDur("LATENCY_MS_MIN", 50*time.Millisecond),
		LatencyMax:        envDur("LATENCY_MS_MAX", 300*time.Millisecond),
		AutoPayAfter:      envDur("AUTO_PAY_AFTER", 30*time.Second),
		PayoutFinishAfter: envDur("PAYOUT_FINISH_AFTER", 60*time.Second),
		DataMode:          env("DATA_MODE", "live"),
		CurrenciesRefresh: envDur("CURRENCIES_REFRESH", time.Hour),
		DBPath:            env("DB_PATH", "./sandbox.db"),
		BaseURL:           env("BASE_URL", "http://localhost:8787"),
		JWTSecret:         env("JWT_SECRET", "sandbox-jwt-secret"),
		APIKeys:           splitCSV(env("API_KEYS", "")),
	}
}

func splitCSV(s string) []string {
	if s == "" {
		return nil
	}
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
