package netguard

import "context"

// Validator проверяет исходящие URL/host на соответствие SSRF-ограничениям.
type Validator interface {
	ValidateURL(ctx context.Context, rawURL string) error
	ValidateHost(ctx context.Context, host string) error
}

// Config задает allowlist хостов и политику блокировки приватных сетей.
type Config struct {
	AllowedHosts         []string
	BlockPrivateNetworks bool
}
