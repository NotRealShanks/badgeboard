package web

import (
	"context"
	"net"
	"testing"
)

func TestSSRF_ValidatePublishedSheetURL(t *testing.T) {
	tests := []struct {
		name    string
		url     string
		wantErr bool
	}{
		{
			name:    "valid sheet published csv",
			url:     "https://docs.google.com/spreadsheets/d/e/2PACX-1vSampleID123/pub?output=csv",
			wantErr: false,
		},
		{
			name:    "valid sheet with additional gid",
			url:     "https://docs.google.com/spreadsheets/d/e/2PACX-1vSampleID123/pub?gid=0&single=true&output=csv",
			wantErr: false,
		},
		{
			name:    "reject non-https scheme",
			url:     "http://docs.google.com/spreadsheets/d/e/2PACX-1vSampleID123/pub?output=csv",
			wantErr: true,
		},
		{
			name:    "reject userinfo trick",
			url:     "https://docs.google.com@evil.com/spreadsheets/d/e/2PACX-1vSampleID123/pub?output=csv",
			wantErr: true,
		},
		{
			name:    "reject wrong host",
			url:     "https://evil.com/spreadsheets/d/e/2PACX-1vSampleID123/pub?output=csv",
			wantErr: true,
		},
		{
			name:    "reject ip literal decimal",
			url:     "https://127.0.0.1/spreadsheets/d/e/2PACX-1vSampleID123/pub?output=csv",
			wantErr: true,
		},
		{
			name:    "reject ip literal private",
			url:     "https://192.168.1.1/spreadsheets/d/e/2PACX-1vSampleID123/pub?output=csv",
			wantErr: true,
		},
		{
			name:    "reject ip literal ipv6",
			url:     "https://[::1]/spreadsheets/d/e/2PACX-1vSampleID123/pub?output=csv",
			wantErr: true,
		},
		{
			name:    "reject wrong path (not published sheet)",
			url:     "https://docs.google.com/spreadsheets/d/12345/edit?output=csv",
			wantErr: true,
		},
		{
			name:    "reject missing output=csv query param",
			url:     "https://docs.google.com/spreadsheets/d/e/2PACX-1vSampleID123/pub?output=html",
			wantErr: true,
		},
		{
			name:    "reject empty url",
			url:     "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ValidatePublishedSheetURL(tt.url)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidatePublishedSheetURL(%q) err = %v, wantErr %v", tt.url, err, tt.wantErr)
			}
		})
	}
}

func TestSSRF_IsPrivateOrLoopbackIP(t *testing.T) {
	tests := []struct {
		ip   string
		want bool
	}{
		{"127.0.0.1", true},
		{"127.0.1.5", true},
		{"10.0.0.1", true},
		{"172.16.0.1", true},
		{"172.31.255.255", true},
		{"192.168.0.1", true},
		{"169.254.1.1", true},
		{"100.64.0.1", true}, // CGNAT
		{"0.0.0.0", true},
		{"::1", true},
		{"fc00::1", true},
		{"fe80::1", true},
		{"8.8.8.8", false},       // Google DNS
		{"142.250.190.46", false}, // Google public IP
		{"1.1.1.1", false},       // Cloudflare DNS
	}

	for _, tt := range tests {
		ip := net.ParseIP(tt.ip)
		if ip == nil {
			t.Fatalf("failed to parse IP %q", tt.ip)
		}
		got := IsPrivateOrLoopbackIP(ip)
		if got != tt.want {
			t.Errorf("IsPrivateOrLoopbackIP(%s) = %v, want %v", tt.ip, got, tt.want)
		}
	}
}

func TestSSRF_SafeDialContextBlocksPrivateIP(t *testing.T) {
	dial := SafeDialContext()
	ctx := context.Background()

	// Direct attempt to connect to loopback
	_, err := dial(ctx, "tcp", "127.0.0.1:80")
	if err == nil {
		t.Errorf("expected SafeDialContext to block 127.0.0.1:80, but got nil error")
	}
}

