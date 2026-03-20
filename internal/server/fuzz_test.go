package server

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func FuzzParseFingerRequest(f *testing.F) {
	seeds := []struct {
		line   string
		extend bool
	}{
		{"\r\n", false},
		{"alice\r\n", false},
		{"/W alice\r\n", false},
		{"/PLAN alice\r\n", true},
		{"/PLAN /mode=full alice\r\n", true},
		{"hello.txt\r\n", false},
		{"bad/request\r\n", false},
	}
	for _, seed := range seeds {
		f.Add(seed.line, seed.extend)
	}

	f.Fuzz(func(t *testing.T, line string, extend bool) {
		req, err := parseRequest(line, ProtocolFinger, extend)
		if err != nil {
			return
		}
		if strings.Contains(req.Canonical, "\r") || strings.Contains(req.Canonical, "\n") {
			t.Fatalf("canonical request contains line breaks: %q", req.Canonical)
		}
		if req.Target != "" && !validTargetComponent(req.Target) {
			t.Fatalf("accepted invalid target: %q", req.Target)
		}
		if !extend && len(req.Flags) > 0 {
			t.Fatalf("plain finger accepted flags with tpl_extend disabled: %+v", req)
		}
		if len(req.Flags) > maxFlagsPerRequest {
			t.Fatalf("accepted too many flags: %d", len(req.Flags))
		}
	})
}

func FuzzParseFingersRequest(f *testing.F) {
	seeds := []string{
		"\r\n",
		"alice\r\n",
		"/PLAN alice\r\n",
		"/mode=full alice\r\n",
		"/PLAN alice@people\r\n",
		"/mode=full alice@host1@host2\r\n",
		"/PLAN\r\n",
		"alice@@people\r\n",
		"alice\n",
	}
	for _, seed := range seeds {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, line string) {
		req, err := parseRequest(line, ProtocolFingers, false)
		if err != nil {
			return
		}
		if !strings.HasSuffix(line, "\r\n") {
			t.Fatalf("accepted non-CRLF request: %q", line)
		}
		if !utf8.ValidString(trimLineEnding(line)) {
			t.Fatalf("accepted invalid utf-8 line: %q", line)
		}
		if strings.Contains(req.Canonical, "\r") || strings.Contains(req.Canonical, "\n") {
			t.Fatalf("canonical request contains line breaks: %q", req.Canonical)
		}
		if req.Target != "" && !validTargetComponent(req.Target) {
			t.Fatalf("accepted invalid target component: %q", req.Target)
		}
		if len(req.Flags) > maxFlagsPerRequest {
			t.Fatalf("accepted too many flags: %d", len(req.Flags))
		}
	})
}

func FuzzSanitizeBody(f *testing.F) {
	seeds := [][]byte{
		[]byte("hello\n"),
		[]byte("a\r\nb\rc\n\tz"),
		[]byte{0xff, '\n'},
		[]byte{0x00, 'x', '\n'},
	}
	for _, seed := range seeds {
		f.Add(seed, false)
		f.Add(seed, true)
	}

	f.Fuzz(func(t *testing.T, body []byte, utf8Required bool) {
		got, err := sanitizeBody(body, utf8Required)
		if err != nil {
			return
		}
		if strings.ContainsRune(string(got), '\x00') {
			t.Fatal("sanitized body contains NUL")
		}
		if strings.ContainsRune(string(got), '\x7f') {
			t.Fatal("sanitized body contains DEL")
		}
		if utf8Required && !utf8.Valid(got) {
			t.Fatal("utf8-required sanitizeBody returned invalid UTF-8")
		}
	})
}

func FuzzParseProxyLine(f *testing.F) {
	seeds := []string{
		"PROXY TCP4 198.51.100.10 203.0.113.4 40000 79\r\n",
		"PROXY TCP6 2001:db8::10 2001:db8::20 40000 79\r\n",
		"PROXY TCP4 2001:db8::10 203.0.113.4 40000 79\r\n",
		"PROXY TCP4 198.51.100.10 203.0.113.4 0 79\r\n",
		"PROXY TCP4 198.51.100.10 203.0.113.4 40000 79\n",
		"PROXY UNKNOWN 198.51.100.10 203.0.113.4 40000 79\r\n",
	}
	for _, seed := range seeds {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, line string) {
		src, err := parseProxyLine(line)
		if err != nil {
			return
		}
		fields := strings.Fields(trimLineEnding(line))
		if len(fields) != 6 {
			t.Fatalf("accepted malformed PROXY line: %q", line)
		}
		switch fields[1] {
		case "TCP4":
			if !src.Is4() {
				t.Fatalf("accepted non-IPv4 source for TCP4: %q", line)
			}
		case "TCP6":
			if !src.Is6() {
				t.Fatalf("accepted non-IPv6 source for TCP6: %q", line)
			}
		default:
			t.Fatalf("accepted unsupported PROXY network: %q", line)
		}
	})
}

func FuzzSanitizeLog(f *testing.F) {
	seeds := []string{
		"",
		"hello",
		"line1\r\nline2\tz",
		strings.Repeat("a", maxLogFieldBytes+32),
	}
	for _, seed := range seeds {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, s string) {
		got := sanitizeLog(s)
		if got == "" {
			t.Fatal("sanitizeLog returned empty string")
		}
		if strings.ContainsAny(got, "\r\n\t") {
			t.Fatalf("sanitizeLog left raw control characters in %q", got)
		}
		if len(got) > maxLogFieldBytes {
			t.Fatalf("sanitizeLog returned %d bytes, want <= %d", len(got), maxLogFieldBytes)
		}
	})
}
