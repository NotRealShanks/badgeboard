package web

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

var (
	ErrInvalidScheme   = errors.New("ssrf: only https scheme is permitted")
	ErrInvalidHost     = errors.New("ssrf: host is not permitted")
	ErrUserInfoNotAllowed = errors.New("ssrf: userinfo in URL is forbidden")
	ErrPrivateIP       = errors.New("ssrf: private, loopback, or non-global IP address resolution is forbidden")
	ErrIPLiteral       = errors.New("ssrf: IP literal hostnames are forbidden")
	ErrPayloadTooLarge = errors.New("ssrf: response payload exceeded 1 MB limit")
)

// AllowedHosts defines hosts that the server is permitted to contact.
var AllowedHosts = map[string]bool{
	"docs.google.com":                true,
	"www.skills.google":              true,
	"skills.google":                  true,
	"cloudskillsboost.google":        true,
	"www.cloudskillsboost.google":    true,
}

// ValidatePublishedSheetURL ensures the URL is a strictly valid Google Sheets published CSV URL.
func ValidatePublishedSheetURL(rawURL string) (*url.URL, error) {
	if strings.TrimSpace(rawURL) == "" {
		return nil, errors.New("published sheet URL is empty")
	}

	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("parsing URL: %w", err)
	}

	if u.Scheme != "https" {
		return nil, ErrInvalidScheme
	}

	if u.User != nil {
		return nil, ErrUserInfoNotAllowed
	}

	hostname := strings.ToLower(u.Hostname())
	if hostname != "docs.google.com" {
		return nil, fmt.Errorf("%w: %s (only docs.google.com is allowed for sheets)", ErrInvalidHost, hostname)
	}

	// Reject IP literals
	if net.ParseIP(hostname) != nil {
		return nil, ErrIPLiteral
	}

	// Must match path pattern: /spreadsheets/d/e/.../pub
	if !strings.HasPrefix(u.Path, "/spreadsheets/d/e/") || !strings.Contains(u.Path, "/pub") {
		return nil, errors.New("invalid published sheet path: must match https://docs.google.com/spreadsheets/d/e/<id>/pub?output=csv")
	}

	// Must request output=csv
	q := u.Query()
	output := strings.ToLower(q.Get("output"))
	if output != "csv" {
		return nil, errors.New("published sheet URL must have query parameter output=csv")
	}

	return u, nil
}

// IsPrivateOrLoopbackIP returns true if the IP is private, loopback, link-local, or unspecified.
func IsPrivateOrLoopbackIP(ip net.IP) bool {
	if ip == nil {
		return true
	}
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() {
		return true
	}

	// Check 100.64.0.0/10 (CGNAT)
	ipv4 := ip.To4()
	if ipv4 != nil {
		if ipv4[0] == 100 && (ipv4[1]&0xC0) == 64 {
			return true
		}
		// 0.0.0.0/8
		if ipv4[0] == 0 {
			return true
		}
	}

	return false
}

// SafeDialContext creates a dialer that resolves DNS and strictly rejects private/loopback destinations.
func SafeDialContext() func(ctx context.Context, network, addr string) (net.Conn, error) {
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, err
		}

		// Reject IP literals in host string directly
		if ip := net.ParseIP(host); ip != nil {
			if IsPrivateOrLoopbackIP(ip) {
				return nil, ErrPrivateIP
			}
		}

		// Resolve host via DNS
		ips, err := net.DefaultResolver.LookupIP(ctx, "ip", host)
		if err != nil {
			return nil, fmt.Errorf("resolving host %s: %w", host, err)
		}
		if len(ips) == 0 {
			return nil, fmt.Errorf("no IP addresses resolved for %s", host)
		}

		for _, ip := range ips {
			if IsPrivateOrLoopbackIP(ip) {
				return nil, fmt.Errorf("%w: %s resolves to %s", ErrPrivateIP, host, ip.String())
			}
		}

		// Connect to first verified IP
		dialer := net.Dialer{Timeout: 10 * time.Second}
		return dialer.DialContext(ctx, network, net.JoinHostPort(ips[0].String(), port))
	}
}

// NewSafeHTTPClient creates an http.Client configured to prevent SSRF and disallow cross-origin redirects.
func NewSafeHTTPClient(timeout time.Duration) *http.Client {
	transport := &http.Transport{
		DialContext:           SafeDialContext(),
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          10,
		IdleConnTimeout:       30 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}

	client := &http.Client{
		Transport: transport,
		Timeout:   timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return errors.New("too many redirects")
			}
			// Strict host allowlist check on redirects
			destHost := strings.ToLower(req.URL.Hostname())
			if !AllowedHosts[destHost] {
				return fmt.Errorf("%w: redirect to %s disallowed", ErrInvalidHost, destHost)
			}
			if req.URL.Scheme != "https" {
				return ErrInvalidScheme
			}
			return nil
		},
	}

	return client
}

// FetchPublishedSheetCSV fetches the published CSV from docs.google.com securely.
func FetchPublishedSheetCSV(ctx context.Context, targetURL string) ([]byte, error) {
	u, err := ValidatePublishedSheetURL(targetURL)
	if err != nil {
		return nil, err
	}

	client := NewSafeHTTPClient(15 * time.Second)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	req.Header.Set("User-Agent", "badgeboard/1.0 (+https://github.com/NotRealShanks/badgeboard)")
	req.Header.Set("Accept", "text/csv, text/plain;q=0.9, */*;q=0.8")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching sheet CSV: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Google Sheets responded with HTTP %d %s", resp.StatusCode, resp.Status)
	}

	// Enforce 1 MB limit
	const maxSheetSize = 1 * 1024 * 1024
	lr := io.LimitReader(resp.Body, maxSheetSize+1)
	body, err := io.ReadAll(lr)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}
	if len(body) > maxSheetSize {
		return nil, ErrPayloadTooLarge
	}

	return body, nil
}
