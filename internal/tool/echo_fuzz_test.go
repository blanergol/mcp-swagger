package tool

import "testing"

// FuzzInputMessageNoPanic protects message parser robustness for arbitrary string payloads.
func FuzzInputMessageNoPanic(f *testing.F) {
	seeds := []string{
		"",
		"a",
		"hello",
		"  spaced  ",
		"\n\t",
		"{\"message\":\"x\"}",
		"🙂",
		"\x00",
		"very-long-" + "xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx",
	}
	for _, seed := range seeds {
		f.Add(seed)
	}

	f.Fuzz(func(_ *testing.T, raw string) {
		_, _ = inputMessage(raw)
		_, _ = inputMessage(map[string]any{"message": raw})
		_, _ = inputMessage(map[string]string{"message": raw})
	})
}
