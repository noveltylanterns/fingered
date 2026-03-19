package server

import (
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSanitizeLogEscapesControls(t *testing.T) {
	got := sanitizeLog("line1\r\nline2\t\x01")
	if strings.ContainsAny(got, "\r\n\t") {
		t.Fatalf("sanitizeLog() left raw control characters in %q", got)
	}
	if !strings.Contains(got, `\x1`) {
		t.Fatalf("sanitizeLog() = %q, want escaped control byte", got)
	}
}

func TestSanitizeLogCapsLength(t *testing.T) {
	got := sanitizeLog(strings.Repeat("a", maxLogFieldBytes+64))
	if len(got) > maxLogFieldBytes {
		t.Fatalf("sanitizeLog() length = %d, want <= %d", len(got), maxLogFieldBytes)
	}
	if !strings.HasSuffix(got, logTruncationMark) {
		t.Fatalf("sanitizeLog() = %q, want truncation suffix", got)
	}
}

func TestParseProxyLineValidTCP4(t *testing.T) {
	got, err := parseProxyLine("PROXY TCP4 198.51.100.10 203.0.113.4 40000 79\r\n")
	if err != nil {
		t.Fatalf("parseProxyLine() error = %v", err)
	}
	want := netip.MustParseAddr("198.51.100.10")
	if got != want {
		t.Fatalf("parseProxyLine() = %v, want %v", got, want)
	}
}

func TestParseProxyLineValidTCP6(t *testing.T) {
	got, err := parseProxyLine("PROXY TCP6 2001:db8::10 2001:db8::20 40000 79\r\n")
	if err != nil {
		t.Fatalf("parseProxyLine() error = %v", err)
	}
	want := netip.MustParseAddr("2001:db8::10")
	if got != want {
		t.Fatalf("parseProxyLine() = %v, want %v", got, want)
	}
}

func TestParseProxyLineRejectsMismatchedFamily(t *testing.T) {
	if _, err := parseProxyLine("PROXY TCP4 2001:db8::10 203.0.113.4 40000 79\r\n"); err == nil {
		t.Fatal("parseProxyLine() error = nil, want invalid request")
	}
}

func TestParseProxyLineRejectsInvalidPort(t *testing.T) {
	if _, err := parseProxyLine("PROXY TCP4 198.51.100.10 203.0.113.4 0 79\r\n"); err == nil {
		t.Fatal("parseProxyLine() error = nil, want invalid request")
	}
}

func TestParseProxyLineRejectsInvalidDestination(t *testing.T) {
	if _, err := parseProxyLine("PROXY TCP6 2001:db8::10 203.0.113.4 40000 79\r\n"); err == nil {
		t.Fatal("parseProxyLine() error = nil, want invalid request")
	}
}

func TestValidateCGIHelper(t *testing.T) {
	root := filepath.Join(string(filepath.Separator), "srv", "fingered")
	if err := validateCGIHelper(root, "/index.cgi", 3); err != nil {
		t.Fatalf("validateCGIHelper() error = %v", err)
	}
	if err := validateCGIHelper("relative", "/index.cgi", 3); err == nil {
		t.Fatal("validateCGIHelper() error = nil, want invalid root")
	}
	if err := validateCGIHelper(root, "index.cgi", 3); err == nil {
		t.Fatal("validateCGIHelper() error = nil, want invalid argv0")
	}
	if err := validateCGIHelper(root, "/nested/index.cgi", 3); err == nil {
		t.Fatal("validateCGIHelper() error = nil, want invalid argv0")
	}
	if err := validateCGIHelper(root, "/index.cgi", 2); err == nil {
		t.Fatal("validateCGIHelper() error = nil, want invalid fd")
	}
}

func TestOpenRegularExecNoFollowRequiresExecutableRegularFile(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "index.cgi")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	f, exists, err := openRegularExecNoFollow(path)
	if f != nil {
		_ = f.Close()
	}
	if err != nil {
		t.Fatalf("openRegularExecNoFollow() error = %v", err)
	}
	if !exists {
		t.Fatal("openRegularExecNoFollow() exists = false, want true")
	}
}

func TestOpenRegularExecNoFollowRejectsNonExecutableFile(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "index.cgi")
	if err := os.WriteFile(path, []byte("plain\n"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	f, exists, err := openRegularExecNoFollow(path)
	if f != nil {
		_ = f.Close()
	}
	if !exists {
		t.Fatal("openRegularExecNoFollow() exists = false, want true")
	}
	if err == nil {
		t.Fatal("openRegularExecNoFollow() error = nil, want execute-bit failure")
	}
}
