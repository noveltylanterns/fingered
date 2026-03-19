package server

import (
	"bufio"
	"strings"
	"testing"
)

func TestParseFingerRequestValid(t *testing.T) {
	tests := []struct {
		line      string
		extend    bool
		target    string
		canonical string
	}{
		{"\r\n", false, "", ""},
		{"alice\r\n", false, "alice", "alice"},
		{"john.doe\r\n", false, "john.doe", "john.doe"},
		{"~user\r\n", false, "~user", "~user"},
		{"/W\r\n", false, "", "/W"},
		{"/W john.doe\r\n", false, "john.doe", "/W john.doe"},
		{"/PLAN\r\n", true, "", "/PLAN"},
		{"/PLAN /mode=full alice\r\n", true, "alice", "/PLAN /mode=full alice"},
		{"/PLAN /mode=full /PLAN alice\r\n", true, "alice", "/PLAN /mode=full alice"},
	}

	for _, tt := range tests {
		req, err := parseRequest(tt.line, ProtocolFinger, tt.extend)
		if err != nil {
			t.Fatalf("parseRequest(%q) error = %v", tt.line, err)
		}
		if req.Target != tt.target || req.Canonical != tt.canonical {
			t.Fatalf("parseRequest(%q) = %+v", tt.line, req)
		}
	}
}

func TestParseFingerRequestInvalid(t *testing.T) {
	tests := []struct {
		line   string
		extend bool
	}{
		{"foo.txt\r\n", false},
		{"foo.cgi\r\n", false},
		{"foo/bar\r\n", false},
		{"foo@bar\r\n", false},
		{"alice smith\r\n", false},
		{"..\r\n", false},
		{".hidden\r\n", false},
		{"hidden.\r\n", false},
		{"/W  alice\r\n", false},
		{"/PLAN / bad\r\n", true},
	}

	for _, tt := range tests {
		if _, err := parseRequest(tt.line, ProtocolFinger, tt.extend); err == nil {
			t.Fatalf("parseRequest(%q) error = nil, want invalid", tt.line)
		}
	}
}

func TestParseFingersRequestValid(t *testing.T) {
	req, err := parseRequest("/PLAN /mode=full alice@people\r\n", ProtocolFingers, false)
	if err != nil {
		t.Fatalf("parseRequest() error = %v", err)
	}
	if req.Target != "alice@people" {
		t.Fatalf("Target = %q, want %q", req.Target, "alice@people")
	}
	if req.Canonical != "/PLAN /mode=full alice@people" {
		t.Fatalf("Canonical = %q", req.Canonical)
	}
}

func TestParseFingersRequestRejectsLFOnly(t *testing.T) {
	if _, err := parseRequest("/PLAN alice\n", ProtocolFingers, false); err == nil {
		t.Fatal("parseRequest() error = nil, want invalid")
	}
}

func TestParseFingersRequestRejectsMalformedChain(t *testing.T) {
	if _, err := parseRequest("alice@@people\r\n", ProtocolFingers, false); err == nil {
		t.Fatal("parseRequest() error = nil, want invalid")
	}
}

func TestSanitizeBody(t *testing.T) {
	got, err := sanitizeBody([]byte("a\r\nb\rc\n\tz"), false)
	if err != nil {
		t.Fatalf("sanitizeBody() error = %v", err)
	}
	want := "a\r\nb\r\nc\r\n\tz"
	if string(got) != want {
		t.Fatalf("sanitizeBody() = %q, want %q", string(got), want)
	}
}

func TestSanitizeBodyRejectsInvalidUTF8WhenRequired(t *testing.T) {
	if _, err := sanitizeBody([]byte{0xff, '\n'}, true); err == nil {
		t.Fatal("sanitizeBody() error = nil, want invalid utf-8 error")
	}
}

func TestCreditsBodyFormat(t *testing.T) {
	want := "\r\n_____________________________\r\nfinger://lanterns.io/fingered\r\n"
	if CreditsBody != want {
		t.Fatalf("CreditsBody = %q, want %q", CreditsBody, want)
	}
}

func TestDiscardLineComplete(t *testing.T) {
	r := bufio.NewReader(strings.NewReader("rest of line\r\nnext"))
	complete, err := discardLine(r)
	if err != nil {
		t.Fatalf("discardLine() error = %v", err)
	}
	if !complete {
		t.Fatal("discardLine() complete = false, want true")
	}
	next, err := r.ReadString('t')
	if err != nil {
		t.Fatalf("ReadString() error = %v", err)
	}
	if next != "next" {
		t.Fatalf("remaining buffered data = %q, want %q", next, "next")
	}
}

func TestDiscardLineEOF(t *testing.T) {
	r := bufio.NewReader(strings.NewReader("unterminated"))
	complete, err := discardLine(r)
	if err == nil {
		t.Fatal("discardLine() error = nil, want EOF")
	}
	if complete {
		t.Fatal("discardLine() complete = true, want false")
	}
}
