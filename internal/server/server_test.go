package server

import (
	"bufio"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"fingered/internal/config"
)

func TestMissingTemplatesAreLoggedWhenSkipped(t *testing.T) {
	tmp := t.TempDir()
	docRoot := filepath.Join(tmp, "docroot")
	logRoot := filepath.Join(tmp, "logs")

	if err := os.MkdirAll(docRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll(docRoot) error = %v", err)
	}
	if err := os.MkdirAll(logRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll(logRoot) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(docRoot, "index.txt"), []byte("INDEX\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(index.txt) error = %v", err)
	}

	cfg := config.Default()
	cfg.BindIP = netip.MustParseAddr("127.0.0.1")
	cfg.Port = 79
	cfg.DocRoot = docRoot
	cfg.TPLEnable = true
	cfg.CGIEnable = false
	cfg.CreditsEnable = false
	cfg.LogRoot = logRoot
	cfg.LogErrors = true
	cfg.LogRequests = false

	srv, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer func() {
		if err := srv.Close(); err != nil {
			t.Fatalf("Close() error = %v", err)
		}
	}()

	body, outcome := srv.buildValidResponse(
		Request{},
		listenerMode{protocol: ProtocolFinger, port: cfg.Port, docRoot: cfg.DocRoot, cgiEnable: cfg.CGIEnable},
		netip.MustParseAddr("127.0.0.1"),
		netip.MustParseAddr("127.0.0.1"),
	)

	if got := string(body); got != "INDEX\r\n" {
		t.Fatalf("buildValidResponse() body = %q, want %q", got, "INDEX\r\n")
	}
	if outcome != "hit" {
		t.Fatalf("buildValidResponse() outcome = %q, want %q", outcome, "hit")
	}

	logBytes, err := os.ReadFile(filepath.Join(logRoot, "error.log"))
	if err != nil {
		t.Fatalf("ReadFile(error.log) error = %v", err)
	}
	logText := string(logBytes)
	if !strings.Contains(logText, "skipping .header wrapper: no valid template found") {
		t.Fatalf("error log missing header skip message: %q", logText)
	}
	if !strings.Contains(logText, "skipping .footer wrapper: no valid template found") {
		t.Fatalf("error log missing footer skip message: %q", logText)
	}
}

func TestMissingStaticContentDoesNotLogError(t *testing.T) {
	tmp := t.TempDir()
	docRoot := filepath.Join(tmp, "docroot")
	logRoot := filepath.Join(tmp, "logs")

	if err := os.MkdirAll(docRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll(docRoot) error = %v", err)
	}
	if err := os.MkdirAll(logRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll(logRoot) error = %v", err)
	}

	cfg := config.Default()
	cfg.BindIP = netip.MustParseAddr("127.0.0.1")
	cfg.Port = 79
	cfg.DocRoot = docRoot
	cfg.TPLEnable = false
	cfg.CGIEnable = false
	cfg.CreditsEnable = false
	cfg.LogRoot = logRoot
	cfg.LogErrors = true
	cfg.LogRequests = false

	srv, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer func() {
		if err := srv.Close(); err != nil {
			t.Fatalf("Close() error = %v", err)
		}
	}()

	body, outcome := srv.buildValidResponse(
		Request{Target: "nosuchentry", Canonical: "nosuchentry"},
		listenerMode{protocol: ProtocolFinger, port: cfg.Port, docRoot: cfg.DocRoot, cgiEnable: cfg.CGIEnable},
		netip.MustParseAddr("127.0.0.1"),
		netip.MustParseAddr("127.0.0.1"),
	)

	if got := string(body); got != NoContentBody {
		t.Fatalf("buildValidResponse() body = %q, want %q", got, NoContentBody)
	}
	if outcome != "miss" {
		t.Fatalf("buildValidResponse() outcome = %q, want %q", outcome, "miss")
	}

	logBytes, err := os.ReadFile(filepath.Join(logRoot, "error.log"))
	if err != nil {
		t.Fatalf("ReadFile(error.log) error = %v", err)
	}
	if len(logBytes) != 0 {
		t.Fatalf("error log = %q, want empty", string(logBytes))
	}
}

func TestOpenRegularReadNoFollowMissingFile(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "nosuchentry.txt")

	f, exists, err := openRegularReadNoFollow(path)
	if f != nil {
		_ = f.Close()
	}
	if err != nil {
		t.Fatalf("openRegularReadNoFollow() error = %v", err)
	}
	if exists {
		t.Fatal("openRegularReadNoFollow() exists = true, want false")
	}
}

func TestServeConnOversizedRequestReturnsInvalid(t *testing.T) {
	tmp := t.TempDir()
	docRoot := filepath.Join(tmp, "docroot")
	if err := os.MkdirAll(docRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll(docRoot) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(docRoot, "index.txt"), []byte("INDEX\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(index.txt) error = %v", err)
	}

	cfg := config.Default()
	cfg.BindIP = netip.MustParseAddr("127.0.0.1")
	cfg.Port = 79
	cfg.DocRoot = docRoot
	cfg.CreditsEnable = false
	cfg.LogErrors = false
	cfg.LogRequests = false
	cfg.ProxyProtocol = false

	srv := &Server{cfg: cfg}
	serverConn, clientConn := net.Pipe()
	done := make(chan struct{})
	go func() {
		defer close(done)
		srv.serveConn(serverConn, listenerMode{protocol: ProtocolFinger, port: cfg.Port, docRoot: cfg.DocRoot, cgiEnable: cfg.CGIEnable})
	}()

	payload := strings.Repeat("a", cfg.MaxRequestBytes+32) + "\r\n"
	if _, err := clientConn.Write([]byte(payload)); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	reply, err := bufio.NewReader(clientConn).ReadString('\n')
	if err != nil {
		t.Fatalf("ReadString() error = %v", err)
	}
	if reply != InvalidRequestBody {
		t.Fatalf("reply = %q, want %q", reply, InvalidRequestBody)
	}

	if err := clientConn.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	<-done
}
