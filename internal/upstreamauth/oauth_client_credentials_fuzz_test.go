package upstreamauth

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"
	"time"
)

// FuzzParseExpiresInNoPanic protects expires_in parser robustness and basic numeric invariants.
func FuzzParseExpiresInNoPanic(f *testing.F) {
	seeds := []string{
		"",
		"0",
		"1",
		"5",
		"10",
		"60",
		"120",
		"3600",
		"86400",
		"-1",
		"-60",
		"abc",
		"1.5",
		"1e3",
		" 42 ",
		"\t90\n",
		"+15",
		"001",
		"2147483647",
		"9223372036854775807",
		"-9223372036854775808",
		"NaN",
		"Infinity",
		"true",
		"false",
		"null",
		"[]",
		"{}",
		"\"60\"",
		"🙂",
		"/etc/passwd",
	}
	for _, seed := range seeds {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, raw string) {
		_ = parseExpiresIn(raw)
		_ = parseExpiresIn(json.Number(raw))
		if i, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64); err == nil {
			d := parseExpiresIn(i)
			if d%time.Second != 0 {
				t.Fatalf("duration must be second-aligned: %s", d)
			}
		}
	})
}
