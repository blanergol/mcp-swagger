package netguard

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"strings"
	"time"
)

var (
	// ErrInvalidURL означает, что URL имеет неверный формат или недопустимую схему.
	ErrInvalidURL = errors.New("invalid url")
	// ErrHostNotAllowed означает, что host отсутствует в allowlist.
	ErrHostNotAllowed = errors.New("host is not allowlisted")
	// ErrPrivateNetwork означает, что host разрешается в приватную/локальную сеть.
	ErrPrivateNetwork = errors.New("host resolves to private network")
	// ErrHostResolution означает ошибку DNS-резолвинга host.
	ErrHostResolution = errors.New("host resolution failed")
)

// Guard валидирует URL-цели по allowlist и ограничениям приватных сетей.
type Guard struct {
	allowedExact  map[string]struct{}
	allowedSuffix []string

	blockPrivate bool
	resolver     *net.Resolver
	dnsTimeout   time.Duration
}

// extraBlockedPrefixes дополняет стандартные netip-проверки сетями,
// которые небезопасно разрешать для SSRF-сценариев.
var extraBlockedPrefixes = []netip.Prefix{
	netip.MustParsePrefix("100.64.0.0/10"), // CGNAT
	netip.MustParsePrefix("198.18.0.0/15"), // benchmark
	netip.MustParsePrefix("240.0.0.0/4"),   // reserved
	netip.MustParsePrefix("2001:db8::/32"), // doc range
}

// New создает Guard с детерминированной проверкой host и DNS-политикой.
func New(cfg Config) *Guard {
	exact, suffix := normalizeAllowlist(cfg.AllowedHosts)
	return &Guard{
		allowedExact:  exact,
		allowedSuffix: suffix,
		blockPrivate:  cfg.BlockPrivateNetworks,
		resolver:      net.DefaultResolver,
		dnsTimeout:    2 * time.Second,
	}
}

// ValidateURL проверяет полный http(s)-URL: схему, host, allowlist и network policy.
func (g *Guard) ValidateURL(ctx context.Context, rawURL string) error {
	trimmed := strings.TrimSpace(rawURL)
	if trimmed == "" {
		return fmt.Errorf("%w: empty", ErrInvalidURL)
	}
	parsed, err := url.Parse(trimmed)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidURL, err)
	}
	scheme := strings.ToLower(strings.TrimSpace(parsed.Scheme))
	if scheme != "http" && scheme != "https" {
		return fmt.Errorf("%w: unsupported scheme %q", ErrInvalidURL, parsed.Scheme)
	}
	host := strings.TrimSpace(parsed.Hostname())
	if host == "" {
		return fmt.Errorf("%w: missing host", ErrInvalidURL)
	}
	return g.ValidateHost(ctx, host)
}

// ValidateHost проверяет host на allowlist и запрет приватных/локальных адресов.
func (g *Guard) ValidateHost(ctx context.Context, host string) error {
	normalized := normalizeHost(host)
	if normalized == "" {
		return fmt.Errorf("%w: empty host", ErrInvalidURL)
	}

	if !g.isAllowed(normalized) {
		return fmt.Errorf("%w: %s", ErrHostNotAllowed, normalized)
	}
	if !g.blockPrivate {
		return nil
	}

	if ip, err := netip.ParseAddr(normalized); err == nil {
		if isBlockedIP(ip.Unmap()) {
			return fmt.Errorf("%w: %s", ErrPrivateNetwork, normalized)
		}
		return nil
	}

	lookupCtx := ctx
	var cancel context.CancelFunc
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		lookupCtx, cancel = context.WithTimeout(ctx, g.dnsTimeout)
		defer cancel()
	}

	addrs, err := g.resolver.LookupNetIP(lookupCtx, "ip", normalized)
	if err != nil {
		return fmt.Errorf("%w: %s: %v", ErrHostResolution, normalized, err)
	}
	if len(addrs) == 0 {
		return fmt.Errorf("%w: %s: no records", ErrHostResolution, normalized)
	}

	for _, addr := range addrs {
		if isBlockedIP(addr.Unmap()) {
			return fmt.Errorf("%w: %s resolved to %s", ErrPrivateNetwork, normalized, addr.String())
		}
	}
	return nil
}

// isAllowed применяет allowlist-логику.
// Если allowlist пустой, разрешаются все host, а ограничения накладывает только blockPrivate.
func (g *Guard) isAllowed(host string) bool {
	if len(g.allowedExact) == 0 && len(g.allowedSuffix) == 0 {
		return true
	}
	if _, ok := g.allowedExact[host]; ok {
		return true
	}
	for _, suffix := range g.allowedSuffix {
		if host == suffix || strings.HasSuffix(host, "."+suffix) {
			return true
		}
	}
	return false
}

// normalizeAllowlist делит allowlist на точные host и wildcard-суффиксы (*.example.com).
func normalizeAllowlist(rawHosts []string) (map[string]struct{}, []string) {
	exact := make(map[string]struct{})
	suffix := make([]string, 0)
	for _, entry := range rawHosts {
		item := strings.TrimSpace(strings.ToLower(entry))
		if item == "" {
			continue
		}
		if strings.HasPrefix(item, "*.") {
			value := normalizeHost(strings.TrimPrefix(item, "*."))
			if value == "" {
				continue
			}
			suffix = append(suffix, value)
			continue
		}
		value := normalizeHost(item)
		if value == "" {
			continue
		}
		exact[value] = struct{}{}
	}
	return exact, suffix
}

// normalizeHost приводит host к форме для сопоставления:
// lower-case, без схемы, без порта, без IPv6-скобок и zone-id.
func normalizeHost(raw string) string {
	value := strings.TrimSpace(strings.ToLower(raw))
	if value == "" {
		return ""
	}
	if strings.Contains(value, "://") {
		if parsed, err := url.Parse(value); err == nil {
			value = parsed.Hostname()
		}
	}
	if host, _, err := net.SplitHostPort(value); err == nil {
		value = host
	}
	value = strings.Trim(value, "[]")
	if i := strings.LastIndex(value, "%"); i > 0 {
		value = value[:i]
	}
	return strings.TrimSpace(value)
}

// isBlockedIP блокирует адреса локальных/приватных/служебных диапазонов,
// которые могут привести к SSRF во внутреннюю инфраструктуру.
func isBlockedIP(ip netip.Addr) bool {
	if !ip.IsValid() {
		return true
	}
	if ip.IsLoopback() ||
		ip.IsPrivate() ||
		ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() ||
		ip.IsMulticast() ||
		ip.IsUnspecified() {
		return true
	}
	for _, prefix := range extraBlockedPrefixes {
		if prefix.Contains(ip) {
			return true
		}
	}
	return false
}
