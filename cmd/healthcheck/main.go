package main

import (
	"fmt"
	"net/http"
	"os"
	"time"
)

func main() {
	addr := os.Getenv("HTTP_ADDR")
	if addr == "" {
		addr = ":8080"
	}
	url := fmt.Sprintf("http://localhost%s/healthz", addr)

	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		os.Exit(1)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		os.Exit(0)
	}
	os.Exit(1)
}
