package server

import (
	"errors"
	"net/netip"
	"os"
	"path/filepath"
	"reflect"
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
	got, err := parseProxyLine("PROXY TCP4 8.8.8.8 1.1.1.1 40000 79\r\n")
	if err != nil {
		t.Fatalf("parseProxyLine() error = %v", err)
	}
	want := netip.MustParseAddr("8.8.8.8")
	if got != want {
		t.Fatalf("parseProxyLine() = %v, want %v", got, want)
	}
}

func TestParseProxyLineValidTCP6(t *testing.T) {
	got, err := parseProxyLine("PROXY TCP6 2606:4700::1 2606:4700::2 40000 79\r\n")
	if err != nil {
		t.Fatalf("parseProxyLine() error = %v", err)
	}
	want := netip.MustParseAddr("2606:4700::1")
	if got != want {
		t.Fatalf("parseProxyLine() = %v, want %v", got, want)
	}
}

func TestParseProxyLineRejectsMismatchedFamily(t *testing.T) {
	if _, err := parseProxyLine("PROXY TCP4 2606:4700::1 1.1.1.1 40000 79\r\n"); err == nil {
		t.Fatal("parseProxyLine() error = nil, want invalid request")
	}
}

func TestParseProxyLineRejectsInvalidPort(t *testing.T) {
	if _, err := parseProxyLine("PROXY TCP4 8.8.8.8 1.1.1.1 0 79\r\n"); err == nil {
		t.Fatal("parseProxyLine() error = nil, want invalid request")
	}
}

func TestParseProxyLineRejectsInvalidDestination(t *testing.T) {
	if _, err := parseProxyLine("PROXY TCP6 2606:4700::1 1.1.1.1 40000 79\r\n"); err == nil {
		t.Fatal("parseProxyLine() error = nil, want invalid request")
	}
}

func TestParseProxyLineRejectsLFOnly(t *testing.T) {
	if _, err := parseProxyLine("PROXY TCP4 8.8.8.8 1.1.1.1 40000 79\n"); err == nil {
		t.Fatal("parseProxyLine() error = nil, want invalid request")
	}
}

func TestParseProxyLineRejectsNonPublicSourceIPs(t *testing.T) {
	cases := []string{
		"PROXY TCP4 127.0.0.1 1.1.1.1 40000 79\r\n",
		"PROXY TCP4 10.0.0.1 1.1.1.1 40000 79\r\n",
		"PROXY TCP4 192.168.1.1 1.1.1.1 40000 79\r\n",
		"PROXY TCP4 172.16.0.1 1.1.1.1 40000 79\r\n",
		"PROXY TCP4 0.0.0.0 1.1.1.1 40000 79\r\n",
		"PROXY TCP4 255.255.255.255 1.1.1.1 40000 79\r\n",
		"PROXY TCP4 169.254.1.1 1.1.1.1 40000 79\r\n",
		"PROXY TCP4 224.0.0.1 1.1.1.1 40000 79\r\n",
		"PROXY TCP4 100.64.0.1 1.1.1.1 40000 79\r\n",
		"PROXY TCP4 198.18.0.1 1.1.1.1 40000 79\r\n",
		"PROXY TCP4 192.0.2.1 1.1.1.1 40000 79\r\n",
		"PROXY TCP4 198.51.100.1 1.1.1.1 40000 79\r\n",
		"PROXY TCP4 203.0.113.1 1.1.1.1 40000 79\r\n",
		"PROXY TCP6 ::1 2606:4700::1 40000 79\r\n",
		"PROXY TCP6 fc00::1 2606:4700::1 40000 79\r\n",
		"PROXY TCP6 2001:db8::1 2606:4700::1 40000 79\r\n",
		"PROXY TCP6 ::ffff:192.168.1.1 2606:4700::1 40000 79\r\n",
		"PROXY TCP6 ::ffff:127.0.0.1 2606:4700::1 40000 79\r\n",
		// Strict whitespace: tabs, double spaces, leading space
		"PROXY\tTCP4\t8.8.8.8\t1.1.1.1\t1234\t79\r\n",
		"PROXY  TCP4  8.8.8.8  1.1.1.1  1234  79\r\n",
		" PROXY TCP4 8.8.8.8 1.1.1.1 1234 79\r\n",
		// IPv6 zone IDs
		"PROXY TCP6 2606:4700::1%eth0 2606:4700::2 1234 79\r\n",
	}
	for _, line := range cases {
		if _, err := parseProxyLine(line); err == nil {
			t.Errorf("parseProxyLine(%q) error = nil, want rejection", line)
		}
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

func TestDropCGIPrivilegesClearsSupplementaryGroupsBeforeCapabilities(t *testing.T) {
	oldSetNoNewPrivs := cgiSetNoNewPrivs
	oldClearSupplementaryGroups := cgiClearSupplementaryGroups
	oldClearAmbientCaps := cgiClearAmbientCaps
	oldDropCaps := cgiDropCaps
	defer func() {
		cgiSetNoNewPrivs = oldSetNoNewPrivs
		cgiClearSupplementaryGroups = oldClearSupplementaryGroups
		cgiClearAmbientCaps = oldClearAmbientCaps
		cgiDropCaps = oldDropCaps
	}()

	var calls []string
	cgiSetNoNewPrivs = func() error {
		calls = append(calls, "no_new_privs")
		return nil
	}
	cgiClearSupplementaryGroups = func() error {
		calls = append(calls, "groups")
		return nil
	}
	cgiClearAmbientCaps = func() error {
		calls = append(calls, "ambient")
		return nil
	}
	cgiDropCaps = func() error {
		calls = append(calls, "caps")
		return nil
	}

	if err := dropCGIPrivileges(); err != nil {
		t.Fatalf("dropCGIPrivileges() error = %v", err)
	}

	want := []string{"no_new_privs", "groups", "ambient", "caps"}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("dropCGIPrivileges() calls = %v, want %v", calls, want)
	}
}

func TestDropCGIPrivilegesFailsWhenClearingSupplementaryGroupsFails(t *testing.T) {
	oldSetNoNewPrivs := cgiSetNoNewPrivs
	oldClearSupplementaryGroups := cgiClearSupplementaryGroups
	oldClearAmbientCaps := cgiClearAmbientCaps
	oldDropCaps := cgiDropCaps
	defer func() {
		cgiSetNoNewPrivs = oldSetNoNewPrivs
		cgiClearSupplementaryGroups = oldClearSupplementaryGroups
		cgiClearAmbientCaps = oldClearAmbientCaps
		cgiDropCaps = oldDropCaps
	}()

	cgiSetNoNewPrivs = func() error { return nil }
	cgiClearSupplementaryGroups = func() error { return errors.New("setgroups denied") }
	cgiClearAmbientCaps = func() error {
		t.Fatal("cgiClearAmbientCaps should not be called after group-drop failure")
		return nil
	}
	cgiDropCaps = func() error {
		t.Fatal("cgiDropCaps should not be called after group-drop failure")
		return nil
	}

	err := dropCGIPrivileges()
	if err == nil {
		t.Fatal("dropCGIPrivileges() error = nil, want group-drop failure")
	}
	if !strings.Contains(err.Error(), "clear supplementary groups") {
		t.Fatalf("dropCGIPrivileges() error = %v, want group-drop context", err)
	}
}
