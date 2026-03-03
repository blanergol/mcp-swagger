package tool

import "testing"

func TestBuildTargetURLPreservesBasePathPrefix(t *testing.T) {
	t.Parallel()

	finalURL, err := buildTargetURL(
		"https://petstore3.swagger.io/api/v3",
		"/pet/findByStatus",
		map[string]string{"status": "available"},
		false,
	)
	if err != nil {
		t.Fatalf("buildTargetURL returned error: %v", err)
	}

	const want = "https://petstore3.swagger.io/api/v3/pet/findByStatus?status=available"
	if finalURL != want {
		t.Fatalf("unexpected final URL: got %q want %q", finalURL, want)
	}
}

func TestBuildTargetURLRejectsAbsolutePathURLs(t *testing.T) {
	t.Parallel()

	_, err := buildTargetURL(
		"https://api.example.com/base",
		"https://evil.example.com/override",
		nil,
		false,
	)
	if err == nil {
		t.Fatalf("expected error for absolute path URL")
	}
	if err.Error() != "path must be relative" {
		t.Fatalf("unexpected error: %v", err)
	}
}
