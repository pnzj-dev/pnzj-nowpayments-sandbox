package main

import (
	"encoding/json"
	"fmt"
)

func jsonUnmarshal(raw json.RawMessage, out any) error {
	return json.Unmarshal(raw, out)
}

func itoa64(n int64) string { return fmt.Sprintf("%d", n) }
