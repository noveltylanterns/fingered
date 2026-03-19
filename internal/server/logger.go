package server

import (
	"fmt"
	"net/netip"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"fingered/internal/config"
)

type Logger struct {
	host       string
	pid        int
	format     string
	errorsFile *os.File
	accessFile *os.File
	mu         sync.Mutex
}

func OpenLogger(cfg config.Config) (*Logger, error) {
	l := &Logger{
		host:   hostnameOrDash(),
		pid:    os.Getpid(),
		format: cfg.LogFormat,
	}
	if cfg.LogErrors {
		f, err := openLog(filepath.Join(cfg.LogRoot, "error.log"), cfg.LogUmask)
		if err != nil {
			return nil, err
		}
		l.errorsFile = f
	}
	if cfg.LogRequests {
		f, err := openLog(filepath.Join(cfg.LogRoot, "access.log"), cfg.LogUmask)
		if err != nil {
			if l.errorsFile != nil {
				_ = l.errorsFile.Close()
			}
			return nil, err
		}
		l.accessFile = f
	}
	return l, nil
}

func (l *Logger) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	var first error
	if l.errorsFile != nil {
		if err := l.errorsFile.Close(); err != nil && first == nil {
			first = err
		}
	}
	if l.accessFile != nil {
		if err := l.accessFile.Close(); err != nil && first == nil {
			first = err
		}
	}
	return first
}

func (l *Logger) Error(protocol string, port int, clientIP, peerIP netip.Addr, summary string) {
	if l == nil || l.errorsFile == nil {
		return
	}
	l.write(l.errorsFile, 3, "ERROR",
		fmt.Sprintf("protocol=%s port=%d client_ip=%s peer_ip=%s msg=%s",
			sanitizeLog(protocol),
			port,
			ipOrDash(clientIP),
			ipOrDash(peerIP),
			sanitizeLog(summary),
		))
}

func (l *Logger) Access(protocol string, port int, clientIP, peerIP netip.Addr, request, outcome string, bytesSent int, dur time.Duration) {
	if l == nil || l.accessFile == nil {
		return
	}
	l.write(l.accessFile, 6, "ACCESS",
		fmt.Sprintf("protocol=%s port=%d client_ip=%s peer_ip=%s request=%s outcome=%s bytes=%d duration_ms=%d",
			sanitizeLog(protocol),
			port,
			ipOrDash(clientIP),
			ipOrDash(peerIP),
			sanitizeLog(request),
			sanitizeLog(outcome),
			bytesSent,
			dur.Milliseconds(),
		))
}

func (l *Logger) write(f *os.File, severity int, kind, msg string) {
	line := ""
	now := time.Now()
	switch l.format {
	case config.LogFormatRFC3164:
		pri := 8 + severity
		line = fmt.Sprintf("<%d>%s %s fingered[%d]: %s %s\n",
			pri,
			now.Format("Jan _2 15:04:05"),
			l.host,
			l.pid,
			kind,
			msg,
		)
	default:
		pri := 8 + severity
		line = fmt.Sprintf("<%d>1 %s %s fingered %d - - %s %s\n",
			pri,
			now.UTC().Format(time.RFC3339),
			l.host,
			l.pid,
			kind,
			msg,
		)
	}

	l.mu.Lock()
	defer l.mu.Unlock()
	_, _ = f.WriteString(line)
}

func openLog(path string, umask int) (*os.File, error) {
	old := syscall.Umask(umask)
	defer syscall.Umask(old)
	fd, err := syscall.Open(path, syscall.O_CREAT|syscall.O_APPEND|syscall.O_WRONLY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0660)
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(fd), path)
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, err
	}
	if !info.Mode().IsRegular() {
		_ = f.Close()
		return nil, fmt.Errorf("log target is not a regular file")
	}
	return f, nil
}

func hostnameOrDash() string {
	host, err := os.Hostname()
	if err != nil || host == "" {
		return "-"
	}
	return sanitizeLog(host)
}

func ipOrDash(addr netip.Addr) string {
	if !addr.IsValid() {
		return "-"
	}
	return addr.String()
}

func sanitizeLog(s string) string {
	if s == "" {
		return "-"
	}
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == '\r' || r == '\n' || r == '\t':
			b.WriteByte(' ')
		case r < 0x20 || r == 0x7f:
			b.WriteString(`\x`)
			b.WriteString(strconv.FormatInt(int64(r), 16))
		default:
			b.WriteRune(r)
		}
	}
	out := strings.TrimSpace(b.String())
	if out == "" {
		return "-"
	}
	return out
}
