package config

import (
	"bufio"
	"fmt"
	"net/netip"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	LogFormatRFC3164 = "rfc3164"
	LogFormatRFC5424 = "rfc5424"
	TLSEnableNo      = "no"
	TLSEnableYesBoth = "yes_both"
	TLSEnableStrict  = "yes_strict"
)

type Config struct {
	BindIP            netip.Addr
	Port              int
	DocRoot           string
	ExtendFinger      bool
	TLSEnable         string
	TLSPort           int
	TLSCert           string
	TLSKey            string
	TLSDocRoot        string
	ReadTimeout       time.Duration
	WriteTimeout      time.Duration
	MaxRequestBytes   int
	CGITimeout        time.Duration
	CGIMaxStdoutBytes int
	MaxResponseBytes  int
	CGIEnable         bool
	TPLEnable         bool
	CreditsEnable     bool
	LogRoot           string
	LogGroup          string
	LogUmask          int
	LogFormat         string
	LogErrors         bool
	LogRequests       bool
	ProxyProtocol     bool
	TrustedProxyIPs   []netip.Addr
}

func Default() Config {
	return Config{
		ExtendFinger:      false,
		TLSEnable:         TLSEnableNo,
		TLSPort:           8179,
		ReadTimeout:       time.Second,
		WriteTimeout:      time.Second,
		MaxRequestBytes:   256,
		CGITimeout:        time.Second,
		CGIMaxStdoutBytes: 262144,
		MaxResponseBytes:  262144,
		CGIEnable:         false,
		TPLEnable:         false,
		CreditsEnable:     true,
		LogRoot:           "/home/finger/logs/fingered/",
		LogGroup:          "finger",
		LogUmask:          0007,
		LogFormat:         LogFormatRFC5424,
		LogErrors:         true,
		LogRequests:       false,
		ProxyProtocol:     false,
	}
}

func Load(path string) (Config, error) {
	cfg := Default()
	file, err := os.Open(path)
	if err != nil {
		return Config{}, err
	}
	defer file.Close()

	required := map[string]bool{
		"bind_ip":  false,
		"port":     false,
		"doc_root": false,
	}

	allowed := map[string]bool{
		"bind_ip":              true,
		"port":                 true,
		"doc_root":             true,
		"extend_finger":        true,
		"tls_enable":           true,
		"tls_port":             true,
		"tls_cert":             true,
		"tls_key":              true,
		"tls_doc_root":         true,
		"read_timeout_ms":      true,
		"write_timeout_ms":     true,
		"max_request_bytes":    true,
		"cgi_timeout_ms":       true,
		"cgi_max_stdout_bytes": true,
		"max_response_bytes":   true,
		"cgi_enable":           true,
		"tpl_enable":           true,
		"credits_enable":       true,
		"log_root":             true,
		"log_group":            true,
		"log_umask":            true,
		"log_format":           true,
		"log_errors":           true,
		"log_requests":         true,
		"proxy_protocol":       true,
		"trusted_proxy_ips":    true,
	}

	scanner := bufio.NewScanner(file)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}

		key, value, ok := splitKV(line)
		if !ok {
			return Config{}, fmt.Errorf("%s:%d: invalid config line", path, lineNo)
		}
		if !allowed[key] {
			return Config{}, fmt.Errorf("%s:%d: unknown config key %q", path, lineNo, key)
		}
		if _, ok := required[key]; ok {
			required[key] = true
		}

		if err := applyValue(&cfg, key, value); err != nil {
			return Config{}, fmt.Errorf("%s:%d: %w", path, lineNo, err)
		}
	}
	if err := scanner.Err(); err != nil {
		return Config{}, err
	}
	for key, seen := range required {
		if !seen {
			return Config{}, fmt.Errorf("missing required config key %q", key)
		}
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func splitKV(line string) (string, string, bool) {
	idx := strings.IndexByte(line, '=')
	if idx < 0 {
		return "", "", false
	}
	key := strings.TrimSpace(line[:idx])
	value := strings.TrimSpace(line[idx+1:])
	if key == "" || value == "" {
		return "", "", false
	}
	return key, value, true
}

func applyValue(cfg *Config, key, value string) error {
	switch key {
	case "bind_ip":
		ip, err := netip.ParseAddr(value)
		if err != nil {
			return fmt.Errorf("invalid bind_ip: %w", err)
		}
		cfg.BindIP = ip
	case "port":
		n, err := parsePositiveInt(value)
		if err != nil {
			return fmt.Errorf("invalid port: %w", err)
		}
		cfg.Port = n
	case "doc_root":
		cfg.DocRoot = filepath.Clean(value)
	case "extend_finger":
		b, err := parseYesNo(value)
		if err != nil {
			return fmt.Errorf("invalid extend_finger: %w", err)
		}
		cfg.ExtendFinger = b
	case "tls_enable":
		mode, err := parseTLSEnable(value)
		if err != nil {
			return fmt.Errorf("invalid tls_enable: %w", err)
		}
		cfg.TLSEnable = mode
	case "tls_port":
		n, err := parsePositiveInt(value)
		if err != nil {
			return fmt.Errorf("invalid tls_port: %w", err)
		}
		cfg.TLSPort = n
	case "tls_cert":
		cfg.TLSCert = filepath.Clean(value)
	case "tls_key":
		cfg.TLSKey = filepath.Clean(value)
	case "tls_doc_root":
		cfg.TLSDocRoot = filepath.Clean(value)
	case "read_timeout_ms":
		d, err := parseMillis(value)
		if err != nil {
			return fmt.Errorf("invalid read_timeout_ms: %w", err)
		}
		cfg.ReadTimeout = d
	case "write_timeout_ms":
		d, err := parseMillis(value)
		if err != nil {
			return fmt.Errorf("invalid write_timeout_ms: %w", err)
		}
		cfg.WriteTimeout = d
	case "max_request_bytes":
		n, err := parsePositiveInt(value)
		if err != nil {
			return fmt.Errorf("invalid max_request_bytes: %w", err)
		}
		cfg.MaxRequestBytes = n
	case "cgi_timeout_ms":
		d, err := parseMillis(value)
		if err != nil {
			return fmt.Errorf("invalid cgi_timeout_ms: %w", err)
		}
		cfg.CGITimeout = d
	case "cgi_max_stdout_bytes":
		n, err := parsePositiveInt(value)
		if err != nil {
			return fmt.Errorf("invalid cgi_max_stdout_bytes: %w", err)
		}
		cfg.CGIMaxStdoutBytes = n
	case "max_response_bytes":
		n, err := parsePositiveInt(value)
		if err != nil {
			return fmt.Errorf("invalid max_response_bytes: %w", err)
		}
		cfg.MaxResponseBytes = n
	case "cgi_enable":
		b, err := parseYesNo(value)
		if err != nil {
			return fmt.Errorf("invalid cgi_enable: %w", err)
		}
		cfg.CGIEnable = b
	case "tpl_enable":
		b, err := parseYesNo(value)
		if err != nil {
			return fmt.Errorf("invalid tpl_enable: %w", err)
		}
		cfg.TPLEnable = b
	case "credits_enable":
		b, err := parseYesNo(value)
		if err != nil {
			return fmt.Errorf("invalid credits_enable: %w", err)
		}
		cfg.CreditsEnable = b
	case "log_root":
		cfg.LogRoot = filepath.Clean(value)
	case "log_group":
		group := strings.TrimSpace(value)
		if group == "" || strings.ContainsAny(group, " \t\r\n") {
			return fmt.Errorf("invalid log_group")
		}
		cfg.LogGroup = group
	case "log_umask":
		n, err := strconv.ParseInt(value, 8, 32)
		if err != nil {
			return fmt.Errorf("invalid log_umask: %w", err)
		}
		cfg.LogUmask = int(n)
	case "log_format":
		cfg.LogFormat = strings.ToLower(value)
	case "log_errors":
		b, err := parseYesNo(value)
		if err != nil {
			return fmt.Errorf("invalid log_errors: %w", err)
		}
		cfg.LogErrors = b
	case "log_requests":
		b, err := parseYesNo(value)
		if err != nil {
			return fmt.Errorf("invalid log_requests: %w", err)
		}
		cfg.LogRequests = b
	case "proxy_protocol":
		b, err := parseYesNo(value)
		if err != nil {
			return fmt.Errorf("invalid proxy_protocol: %w", err)
		}
		cfg.ProxyProtocol = b
	case "trusted_proxy_ips":
		if strings.TrimSpace(value) == "" {
			cfg.TrustedProxyIPs = nil
			return nil
		}
		parts := strings.Split(value, ",")
		ips := make([]netip.Addr, 0, len(parts))
		for _, part := range parts {
			ip, err := netip.ParseAddr(strings.TrimSpace(part))
			if err != nil {
				return fmt.Errorf("invalid trusted_proxy_ips entry: %w", err)
			}
			ips = append(ips, ip)
		}
		cfg.TrustedProxyIPs = ips
	default:
		return fmt.Errorf("unsupported config key %q", key)
	}
	return nil
}

func (c Config) Validate() error {
	if !c.BindIP.IsValid() {
		return fmt.Errorf("bind_ip is required")
	}
	if c.Port < 1 || c.Port > 65535 {
		return fmt.Errorf("port must be between 1 and 65535")
	}
	if c.DocRoot == "" || !filepath.IsAbs(c.DocRoot) {
		return fmt.Errorf("doc_root must be an absolute path")
	}
	if c.TLSEnable != TLSEnableNo && c.TLSEnable != TLSEnableYesBoth && c.TLSEnable != TLSEnableStrict {
		return fmt.Errorf("tls_enable must be %q, %q, or %q", TLSEnableNo, TLSEnableYesBoth, TLSEnableStrict)
	}
	if c.TLSDocRoot != "" && !filepath.IsAbs(c.TLSDocRoot) {
		return fmt.Errorf("tls_doc_root must be an absolute path when set")
	}
	if c.TLSEnabled() {
		if c.TLSPort < 1 || c.TLSPort > 65535 {
			return fmt.Errorf("tls_port must be between 1 and 65535")
		}
		if c.TLSCert == "" || !filepath.IsAbs(c.TLSCert) {
			return fmt.Errorf("tls_cert must be an absolute path when tls is enabled")
		}
		if c.TLSKey == "" || !filepath.IsAbs(c.TLSKey) {
			return fmt.Errorf("tls_key must be an absolute path when tls is enabled")
		}
		if c.TLSEnable == TLSEnableYesBoth && c.TLSPort == c.Port {
			return fmt.Errorf("tls_port must differ from port when tls_enable=yes_both")
		}
	}
	if c.MaxRequestBytes <= 0 {
		return fmt.Errorf("max_request_bytes must be > 0")
	}
	if c.CGIMaxStdoutBytes <= 0 {
		return fmt.Errorf("cgi_max_stdout_bytes must be > 0")
	}
	if c.MaxResponseBytes <= 0 {
		return fmt.Errorf("max_response_bytes must be > 0")
	}
	if c.ReadTimeout <= 0 || c.WriteTimeout <= 0 || c.CGITimeout <= 0 {
		return fmt.Errorf("timeouts must be > 0")
	}
	if c.LogFormat != LogFormatRFC3164 && c.LogFormat != LogFormatRFC5424 {
		return fmt.Errorf("log_format must be %q or %q", LogFormatRFC3164, LogFormatRFC5424)
	}
	if (c.LogErrors || c.LogRequests) && (c.LogRoot == "" || !filepath.IsAbs(c.LogRoot)) {
		return fmt.Errorf("log_root must be an absolute path when logging is enabled")
	}
	if c.LogGroup == "" {
		return fmt.Errorf("log_group must not be empty")
	}
	if c.ProxyProtocol && len(c.TrustedProxyIPs) == 0 {
		return fmt.Errorf("trusted_proxy_ips must be set when proxy_protocol=yes")
	}
	return nil
}

func parsePositiveInt(value string) (int, error) {
	n, err := strconv.Atoi(value)
	if err != nil {
		return 0, err
	}
	if n <= 0 {
		return 0, fmt.Errorf("must be > 0")
	}
	return n, nil
}

func parseMillis(value string) (time.Duration, error) {
	n, err := parsePositiveInt(value)
	if err != nil {
		return 0, err
	}
	return time.Duration(n) * time.Millisecond, nil
}

func parseYesNo(value string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "yes":
		return true, nil
	case "no":
		return false, nil
	default:
		return false, fmt.Errorf("must be yes or no")
	}
}

func parseTLSEnable(value string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case TLSEnableNo:
		return TLSEnableNo, nil
	case TLSEnableYesBoth:
		return TLSEnableYesBoth, nil
	case TLSEnableStrict:
		return TLSEnableStrict, nil
	default:
		return "", fmt.Errorf("must be %s, %s, or %s", TLSEnableNo, TLSEnableYesBoth, TLSEnableStrict)
	}
}

func (c Config) TLSEnabled() bool {
	return c.TLSEnable != TLSEnableNo
}

func (c Config) PlainEnabled() bool {
	return c.TLSEnable != TLSEnableStrict
}

func (c Config) EffectiveTLSDocRoot() string {
	if c.TLSDocRoot != "" {
		return c.TLSDocRoot
	}
	return c.DocRoot
}
