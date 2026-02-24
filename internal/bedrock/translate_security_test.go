package bedrock

import (
	"encoding/base64"
	"net"
	"strings"
	"testing"
)

func TestIsPrivateOrLocalIP(t *testing.T) {
	tests := []struct {
		ip      string
		private bool
	}{
		{ip: "127.0.0.1", private: true},
		{ip: "10.0.0.1", private: true},
		{ip: "192.168.1.1", private: true},
		{ip: "172.20.1.1", private: true},
		{ip: "8.8.8.8", private: false},
		{ip: "::1", private: true},
		{ip: "2606:4700:4700::1111", private: false},
	}
	for _, tt := range tests {
		t.Run(tt.ip, func(t *testing.T) {
			ip := net.ParseIP(tt.ip)
			got := isPrivateOrLocalIP(ip)
			if got != tt.private {
				t.Fatalf("isPrivateOrLocalIP(%s) = %v, want %v", tt.ip, got, tt.private)
			}
		})
	}
}

func TestParseDataURL_MaxBytes(t *testing.T) {
	raw := strings.Repeat("a", 128)
	encoded := base64.StdEncoding.EncodeToString([]byte(raw))
	dataURL := "data:image/png;base64," + encoded

	if _, _, err := parseDataURL(dataURL, 64); err == nil {
		t.Fatal("expected size validation error")
	}
	if _, data, err := parseDataURL(dataURL, 256); err != nil {
		t.Fatalf("unexpected error: %v", err)
	} else if len(data) != len(raw) {
		t.Fatalf("expected decoded len %d, got %d", len(raw), len(data))
	}
}

func TestValidateRemoteImageHost(t *testing.T) {
	if err := validateRemoteImageHost("localhost"); err == nil || !strings.Contains(err.Error(), "private or local") {
		t.Fatalf("expected localhost to be blocked, got %v", err)
	}
	if err := validateRemoteImageHost("invalid.invalid"); err == nil {
		t.Fatalf("expected DNS resolution error")
	}
}
