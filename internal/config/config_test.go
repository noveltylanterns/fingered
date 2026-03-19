package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadAppliesDefaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "fingered.conf")
	content := "" +
		"bind_ip = 127.0.0.1\n" +
		"port = 7979\n" +
		"doc_root = /home/finger/app/public/\n"
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Port != 7979 {
		t.Fatalf("Port = %d, want 7979", cfg.Port)
	}
	if !cfg.BindIP.IsValid() || cfg.BindIP.String() != "127.0.0.1" {
		t.Fatalf("BindIP = %v", cfg.BindIP)
	}
	if cfg.CreditsEnable != true {
		t.Fatalf("CreditsEnable = %v, want true", cfg.CreditsEnable)
	}
	if cfg.TLSEnable != TLSEnableNo {
		t.Fatalf("TLSEnable = %q, want %q", cfg.TLSEnable, TLSEnableNo)
	}
	if cfg.TLSPort != 8179 {
		t.Fatalf("TLSPort = %d, want 8179", cfg.TLSPort)
	}
	if cfg.LogErrors != true {
		t.Fatalf("LogErrors = %v, want true", cfg.LogErrors)
	}
	if cfg.LogRequests != false {
		t.Fatalf("LogRequests = %v, want false", cfg.LogRequests)
	}
	if cfg.LogGroup != "finger" {
		t.Fatalf("LogGroup = %q, want %q", cfg.LogGroup, "finger")
	}
}

func TestLoadRejectsUnknownKey(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "fingered.conf")
	content := "" +
		"bind_ip = 127.0.0.1\n" +
		"port = 7979\n" +
		"doc_root = /home/finger/app/public/\n" +
		"nope = yes\n"
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	if _, err := Load(path); err == nil {
		t.Fatal("Load() error = nil, want unknown key error")
	}
}

func TestLoadRequiresTrustedProxyIPsWhenProxyEnabled(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "fingered.conf")
	content := "" +
		"bind_ip = 127.0.0.1\n" +
		"port = 7979\n" +
		"doc_root = /home/finger/app/public/\n" +
		"proxy_protocol = yes\n"
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	if _, err := Load(path); err == nil {
		t.Fatal("Load() error = nil, want trusted_proxy_ips validation error")
	}
}

func TestLoadRequiresTLSPathsWhenEnabled(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "fingered.conf")
	content := "" +
		"bind_ip = 127.0.0.1\n" +
		"port = 7979\n" +
		"doc_root = /home/finger/app/public/\n" +
		"tls_enable = yes_both\n"
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	if _, err := Load(path); err == nil {
		t.Fatal("Load() error = nil, want tls path validation error")
	}
}
