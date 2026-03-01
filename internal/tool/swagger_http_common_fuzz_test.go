package tool

import (
	"encoding/json"
	"testing"
)

// FuzzToolInputParsersNoPanic protects parser stability for mixed legacy/new payload shapes.
func FuzzToolInputParsersNoPanic(f *testing.F) {
	seeds := []string{
		`{}`,
		`{"operationId":"getUser","params":{"path":{"id":"1"}}}`,
		`{"operationID":"getUser","pathParams":{"id":"1"}}`,
		`{"operationId":"getUser","queryParams":{"limit":10}}`,
		`{"operationId":"getUser","params":{"query":{"status":200}}}`,
		`{"operationId":"getUser","status":200}`,
		`{"operationId":"getUser","params":{"headers":{"Content-Type":"application/json"}}}`,
		`{"operationId":"getUser","params":{"body":{"x":1}}}`,
		`{"operationId":"getUser","body":"{\"x\":1}"}`,
		`{"operationId":"getUser","contentType":"application/json"}`,
		`{"operationId":"getUser","params":{"query":{"include":"endpoints,usage"}}}`,
		`{"query":"search","method":"get"}`,
		`{"params":{"query":{"query":"search","tag":"users","limit":"5"}}}`,
		`{"params":{"query":{"status":"404"}},"operationId":"x"}`,
		`{"operationId":"x","params":{"query":{"status":"abc"}}}`,
		`{"operationId":"x","params":{"headers":{"X":true}}}`,
		`{"operationId":"x","params":{"path":{"id":true}}}`,
		`{"operationId":"x","params":{"query":{"a":{"b":1}}}}`,
		`{"operationId":"x","params":"bad"}`,
		`{"operationId":123}`,
		`{"operationId":"x","baseURL":"https://api.example.com"}`,
		`{"operationId":"x","confirmationId":"abc"}`,
		`{"operationId":"x","confirmationToken":"abc"}`,
		`{"operationId":"x","params":{"query":{"confirmationId":"abc"}}}`,
		`{"operationId":"x","params":{"query":{"confirmationToken":"abc"}}}`,
		`[]`,
		`"text"`,
		`null`,
		`{"operationId":"x","params":{"body":[1,2,3]}}`,
		`{"operationId":"x","params":{"body":{"nested":{"a":[1,2,3]}}}}`,
		`{"operationId":"x","params":{"query":{"include":["a","b","A"]}}}`,
	}
	for _, seed := range seeds {
		f.Add(seed)
	}

	f.Fuzz(func(_ *testing.T, raw string) {
		var input any = raw
		if json.Valid([]byte(raw)) {
			_ = json.Unmarshal([]byte(raw), &input)
		}

		_, _ = parseToolInputEnvelope(input, false)
		_, _ = parseToolInputEnvelope(input, true)
		_, _ = parseExecuteInput(input)
		_, _ = parseSearchInput(input)
		_, _ = parsePlanCallInput(input)
		_, _ = parseValidateResponseInput(input)
	})
}

// FuzzStatusAndBodyNormalizationNoPanic protects status/body normalization against malformed values.
func FuzzStatusAndBodyNormalizationNoPanic(f *testing.F) {
	statusSeeds := []string{
		"",
		"200",
		" 404 ",
		"abc",
		"-1",
		"0",
		"999",
		"1e3",
		"NaN",
		"∞",
		"{",
		"[]",
		"true",
		"false",
		"null",
		"  ",
		"1000000",
		"2.5",
		"\n200\n",
		"\t500\t",
		"001",
		"+200",
		"-200",
		"2147483647",
		"-2147483648",
		"200 OK",
		"status=200",
		"🙂",
		"/etc/passwd",
		"\\x00",
		"\"200\"",
	}
	for _, seed := range statusSeeds {
		f.Add(seed)
	}

	f.Fuzz(func(_ *testing.T, raw string) {
		_, _ = parseStatusCode(raw)
		_ = normalizeResponseBody("", "", raw)
		_ = normalizeResponseBody("application/json", "", raw)
		_ = normalizeResponseBody("text/plain", "text", raw)
		_ = normalizeResponseBody("application/octet-stream", "base64", []byte(raw))
	})
}
