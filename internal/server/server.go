package server

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf8"

	"fingered/internal/config"
)

type Server struct {
	cfg       config.Config
	logger    *Logger
	tlsConfig *tls.Config
}

type listenerMode struct {
	protocol  Protocol
	port      int
	docRoot   string
	cgiEnable bool
	tls       bool
}

type prepareAction struct {
	err        error
	silent     bool
	logSummary string
}

func New(cfg config.Config) (*Server, error) {
	logger, err := OpenLogger(cfg)
	if err != nil {
		return nil, err
	}

	srv := &Server{cfg: cfg, logger: logger}
	if cfg.TLSEnabled() {
		cert, err := tls.LoadX509KeyPair(cfg.TLSCert, cfg.TLSKey)
		if err != nil {
			_ = logger.Close()
			return nil, err
		}
		srv.tlsConfig = &tls.Config{
			Certificates: []tls.Certificate{cert},
			MinVersion:   tls.VersionTLS12,
		}
	}
	return srv, nil
}

func (s *Server) Close() error {
	if s.logger == nil {
		return nil
	}
	return s.logger.Close()
}

func (s *Server) Run(ctx context.Context) error {
	modes := s.listenerModes()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	errCh := make(chan error, len(modes))
	var wg sync.WaitGroup
	for _, mode := range modes {
		mode := mode
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := s.runListener(ctx, mode); err != nil && ctx.Err() == nil {
				select {
				case errCh <- err:
				default:
				}
				cancel()
			}
		}()
	}

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case err := <-errCh:
		<-done
		return err
	case <-done:
		return nil
	}
}

func (s *Server) listenerModes() []listenerMode {
	modes := make([]listenerMode, 0, 2)
	if s.cfg.PlainEnabled() {
		modes = append(modes, listenerMode{
			protocol:  ProtocolFinger,
			port:      s.cfg.Port,
			docRoot:   s.cfg.DocRoot,
			cgiEnable: s.cfg.CGIEnable,
		})
	}
	if s.cfg.TLSEnabled() {
		modes = append(modes, listenerMode{
			protocol:  ProtocolFingers,
			port:      s.cfg.TLSPort,
			docRoot:   s.cfg.EffectiveTLSDocRoot(),
			cgiEnable: s.cfg.EffectiveTLSCGIEnable(),
			tls:       true,
		})
	}
	return modes
}

func (s *Server) runListener(ctx context.Context, mode listenerMode) error {
	addr := net.JoinHostPort(s.cfg.BindIP.String(), strconv.Itoa(mode.port))
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	defer ln.Close()

	var wg sync.WaitGroup
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = ln.Close()
		case <-done:
		}
	}()
	defer close(done)

	for {
		conn, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil {
				break
			}
			var netErr net.Error
			if errors.As(err, &netErr) && netErr.Temporary() {
				time.Sleep(50 * time.Millisecond)
				continue
			}
			return err
		}
		wg.Add(1)
		go func(c net.Conn) {
			defer wg.Done()
			s.serveConn(c, mode)
		}(conn)
	}
	wg.Wait()
	return nil
}

func (s *Server) serveConn(conn net.Conn, mode listenerMode) {
	activeConn := conn
	defer func() {
		_ = activeConn.Close()
	}()

	start := time.Now()
	peerIP := peerAddr(conn.RemoteAddr())
	clientIP := peerIP

	reader, wrappedConn, resolvedClientIP, action := s.prepareConn(conn, mode, peerIP)
	if action.logSummary != "" && s.logger != nil {
		s.logger.Error(string(mode.protocol), mode.port, clientIP, peerIP, action.logSummary)
	}
	if action.silent {
		return
	}
	if action.err != nil {
		s.writeAndLog(activeConn, mode, clientIP, peerIP, "", "invalid", []byte(InvalidRequestBody), start)
		return
	}
	if wrappedConn != nil {
		activeConn = wrappedConn
	}
	clientIP = resolvedClientIP

	if err := activeConn.SetReadDeadline(time.Now().Add(s.cfg.ReadTimeout)); err != nil {
		return
	}

	line, complete, err := readLine(reader, s.cfg.MaxRequestBytes)
	if errors.Is(err, errLineTooLong) {
		complete, err = discardLine(reader)
		if shouldSilentlyClose(err, complete) {
			return
		}
		s.writeAndLog(activeConn, mode, clientIP, peerIP, "", "invalid", []byte(InvalidRequestBody), start)
		return
	}
	if shouldSilentlyClose(err, complete) {
		return
	}
	if err != nil {
		s.writeAndLog(activeConn, mode, clientIP, peerIP, "", "invalid", []byte(InvalidRequestBody), start)
		return
	}

	req, err := parseRequest(line, mode.protocol, s.cfg.ExtendFinger)
	if err != nil {
		s.writeAndLog(activeConn, mode, clientIP, peerIP, trimLineEnding(line), "invalid", []byte(InvalidRequestBody), start)
		return
	}

	body, outcome := s.buildValidResponse(req, mode, clientIP, peerIP)
	s.writeAndLog(activeConn, mode, clientIP, peerIP, req.Canonical, outcome, body, start)
}

func (s *Server) prepareConn(conn net.Conn, mode listenerMode, peerIP netip.Addr) (*bufio.Reader, net.Conn, netip.Addr, prepareAction) {
	clientIP := peerIP
	reader := bufio.NewReader(conn)

	if mode.tls {
		if err := conn.SetReadDeadline(time.Now().Add(s.cfg.ReadTimeout)); err != nil {
			return nil, nil, clientIP, prepareAction{silent: true}
		}
		if s.cfg.ProxyProtocol && s.isTrustedProxy(peerIP) {
			line, complete, err := readLine(reader, 108)
			if shouldSilentlyClose(err, complete) {
				return nil, nil, clientIP, prepareAction{silent: true}
			}
			if err != nil {
				return nil, nil, clientIP, prepareAction{
					silent:     true,
					logSummary: fmt.Sprintf("proxy header read failed: %v", err),
				}
			}
			ip, err := parseProxyLine(line)
			if err != nil {
				return nil, nil, clientIP, prepareAction{
					silent:     true,
					logSummary: "invalid PROXY header on tls listener",
				}
			}
			clientIP = ip
		}

		tlsConn := tls.Server(&bufferedConn{Conn: conn, reader: reader}, s.tlsConfig)
		if err := tlsConn.SetDeadline(time.Now().Add(s.cfg.ReadTimeout)); err != nil {
			return nil, nil, clientIP, prepareAction{silent: true}
		}
		if err := tlsConn.Handshake(); err != nil {
			return nil, nil, clientIP, prepareAction{
				silent:     true,
				logSummary: fmt.Sprintf("tls handshake failed: %v", err),
			}
		}
		_ = tlsConn.SetDeadline(time.Time{})
		return bufio.NewReader(tlsConn), tlsConn, clientIP, prepareAction{}
	}

	if s.cfg.ProxyProtocol && s.isTrustedProxy(peerIP) {
		line, complete, err := readLine(reader, 108)
		if shouldSilentlyClose(err, complete) {
			return nil, nil, clientIP, prepareAction{silent: true}
		}
		if err != nil {
			return nil, nil, clientIP, prepareAction{err: err}
		}
		ip, err := parseProxyLine(line)
		if err != nil {
			return nil, nil, clientIP, prepareAction{err: err}
		}
		clientIP = ip
	}

	return reader, conn, clientIP, prepareAction{}
}

func shouldSilentlyClose(err error, complete bool) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, io.EOF) && !complete {
		return true
	}
	if ne, ok := err.(net.Error); ok && ne.Timeout() {
		return true
	}
	return false
}

func (s *Server) writeAndLog(conn net.Conn, mode listenerMode, clientIP, peerIP netip.Addr, request, outcome string, body []byte, start time.Time) {
	_ = conn.SetWriteDeadline(time.Now().Add(s.cfg.WriteTimeout))
	n, err := conn.Write(body)
	if err != nil && s.logger != nil {
		s.logger.Error(string(mode.protocol), mode.port, clientIP, peerIP, fmt.Sprintf("write failed: %v", err))
	}
	if s.logger != nil {
		s.logger.Access(string(mode.protocol), mode.port, clientIP, peerIP, request, outcome, n, time.Since(start))
	}
}

func (s *Server) buildValidResponse(req Request, mode listenerMode, clientIP, peerIP netip.Addr) ([]byte, string) {
	mainBody, outcome := s.resolveMainContent(req, mode, clientIP, peerIP)
	header, footer := s.resolveTemplates(req, mode, clientIP, peerIP)
	assembled := joinSegments(header, mainBody, footer)
	if s.cfg.CreditsEnable {
		assembled = joinSegments(assembled, []byte(CreditsBody))
	}
	if len(assembled) > s.cfg.MaxResponseBytes {
		return []byte(NoContentBody), "miss"
	}
	return assembled, outcome
}

func (s *Server) resolveMainContent(req Request, mode listenerMode, clientIP, peerIP netip.Addr) ([]byte, string) {
	staticName := buildContentName(req.Target, ".txt")
	body, exists, err := s.loadStatic(mode.docRoot, staticName, mode.protocol == ProtocolFingers)
	if exists {
		if err != nil {
			s.logContentError(mode, clientIP, peerIP, staticName, err)
			return []byte(NoContentBody), "miss"
		}
		return body, "hit"
	}

	if !mode.cgiEnable {
		return []byte(NoContentBody), "miss"
	}

	cgiName := buildContentName(req.Target, ".cgi")
	body, exists, err = s.runCGI(mode.docRoot, cgiName, req, mode.protocol == ProtocolFingers)
	if !exists {
		return []byte(NoContentBody), "miss"
	}
	if err != nil {
		s.logContentError(mode, clientIP, peerIP, cgiName, err)
		return []byte(NoContentBody), "cgi_fail"
	}
	return body, "cgi_hit"
}

func (s *Server) resolveTemplates(req Request, mode listenerMode, clientIP, peerIP netip.Addr) ([]byte, []byte) {
	if !s.cfg.TPLEnable {
		return nil, nil
	}
	return s.resolveTemplate(".header", req, mode, clientIP, peerIP), s.resolveTemplate(".footer", req, mode, clientIP, peerIP)
}

func (s *Server) resolveTemplate(name string, req Request, mode listenerMode, clientIP, peerIP netip.Addr) []byte {
	staticName := name + ".txt"
	body, exists, err := s.loadStatic(mode.docRoot, staticName, mode.protocol == ProtocolFingers)
	if exists {
		if err != nil {
			s.logContentError(mode, clientIP, peerIP, staticName, err)
			s.logTemplateSkip(mode, clientIP, peerIP, name)
			return nil
		}
		return body
	}
	if !mode.cgiEnable {
		s.logTemplateSkip(mode, clientIP, peerIP, name)
		return nil
	}
	cgiName := name + ".cgi"
	body, exists, err = s.runCGI(mode.docRoot, cgiName, req, mode.protocol == ProtocolFingers)
	if !exists {
		s.logTemplateSkip(mode, clientIP, peerIP, name)
		return nil
	}
	if err != nil {
		s.logContentError(mode, clientIP, peerIP, cgiName, err)
		s.logTemplateSkip(mode, clientIP, peerIP, name)
		return nil
	}
	return body
}

func buildContentName(target, suffix string) string {
	if target == "" {
		return "index" + suffix
	}
	return target + suffix
}

func (s *Server) loadStatic(root, name string, utf8Required bool) ([]byte, bool, error) {
	fullPath := filepath.Join(root, name)
	f, exists, err := openRegularReadNoFollow(fullPath)
	if err != nil {
		return nil, exists, err
	}
	if !exists {
		return nil, false, nil
	}
	defer f.Close()

	raw, err := io.ReadAll(io.LimitReader(f, int64(s.cfg.MaxResponseBytes+1)))
	if err != nil {
		return nil, true, err
	}
	if len(raw) > s.cfg.MaxResponseBytes {
		return nil, true, fmt.Errorf("content exceeds max_response_bytes")
	}
	body, err := sanitizeBody(raw, utf8Required)
	if err != nil {
		return nil, true, err
	}
	return body, true, nil
}

func (s *Server) runCGI(root, name string, req Request, utf8Required bool) ([]byte, bool, error) {
	fullPath := filepath.Join(root, name)
	cgiFile, exists, err := openRegularExecNoFollow(fullPath)
	if err != nil {
		return nil, exists, err
	}
	if !exists {
		return nil, false, nil
	}
	defer cgiFile.Close()

	ctx, cancel := context.WithTimeout(context.Background(), s.cfg.CGITimeout)
	defer cancel()

	binPath, err := os.Executable()
	if err != nil {
		return nil, true, err
	}
	jailPath := "/" + filepath.Base(name)
	cmd := exec.CommandContext(ctx, binPath, "-internal-cgi-helper", "-internal-cgi-root", root, "-internal-cgi-argv0", jailPath)
	cmd.Env = []string{}
	cmd.Stdin = bytes.NewReader([]byte(req.Canonical + "\n"))
	cmd.ExtraFiles = []*os.File{cgiFile}

	var stdout cappedBuffer
	stdout.max = s.cfg.CGIMaxStdoutBytes
	var stderr cappedBuffer
	stderr.max = 4096
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err = cmd.Run()
	if ctx.Err() == context.DeadlineExceeded {
		return nil, true, fmt.Errorf("cgi timeout")
	}
	if stdout.exceeded {
		return nil, true, fmt.Errorf("cgi output exceeds cgi_max_stdout_bytes")
	}
	if err != nil {
		if stderr.Len() > 0 {
			return nil, true, fmt.Errorf("cgi failed: %s", sanitizeLog(stderr.String()))
		}
		return nil, true, fmt.Errorf("cgi failed: %w", err)
	}
	body, err := sanitizeBody(stdout.Bytes(), utf8Required)
	if err != nil {
		return nil, true, err
	}
	return body, true, nil
}

func sanitizeBody(raw []byte, utf8Required bool) ([]byte, error) {
	if bytes.IndexByte(raw, 0) >= 0 {
		return nil, fmt.Errorf("content contains NUL")
	}
	if utf8Required && !utf8.Valid(raw) {
		return nil, fmt.Errorf("content is not valid utf-8")
	}
	raw = bytes.ReplaceAll(raw, []byte("\r\n"), []byte("\n"))
	raw = bytes.ReplaceAll(raw, []byte("\r"), []byte("\n"))

	out := make([]byte, 0, len(raw)+8)
	for _, b := range raw {
		switch {
		case b == '\n':
			out = append(out, '\r', '\n')
		case b == '\t' || b >= 0x20:
			out = append(out, b)
		default:
			out = append(out, '?')
		}
	}
	return out, nil
}

func joinSegments(parts ...[]byte) []byte {
	var out []byte
	for _, part := range parts {
		if len(part) == 0 {
			continue
		}
		if len(out) > 0 && !bytes.HasSuffix(out, []byte("\r\n")) {
			out = append(out, '\r', '\n')
		}
		out = append(out, part...)
	}
	if len(out) > 0 && !bytes.HasSuffix(out, []byte("\r\n")) {
		out = append(out, '\r', '\n')
	}
	return out
}

func (s *Server) logContentError(mode listenerMode, clientIP, peerIP netip.Addr, name string, err error) {
	if s.logger != nil {
		s.logger.Error(string(mode.protocol), mode.port, clientIP, peerIP, fmt.Sprintf("%s: %v", name, err))
	}
}

func (s *Server) logTemplateSkip(mode listenerMode, clientIP, peerIP netip.Addr, name string) {
	if s.logger != nil {
		s.logger.Error(string(mode.protocol), mode.port, clientIP, peerIP, fmt.Sprintf("skipping %s wrapper: no valid template found", name))
	}
}

func (s *Server) isTrustedProxy(addr netip.Addr) bool {
	for _, ip := range s.cfg.TrustedProxyIPs {
		if ip == addr {
			return true
		}
	}
	return false
}

func parseProxyLine(line string) (netip.Addr, error) {
	if !strings.HasSuffix(line, "\r\n") {
		return netip.Addr{}, errInvalidRequest
	}
	s := strings.TrimSuffix(line, "\r\n")
	fields := strings.Fields(s)
	if len(fields) != 6 {
		return netip.Addr{}, errInvalidRequest
	}
	if fields[0] != "PROXY" {
		return netip.Addr{}, errInvalidRequest
	}
	if fields[1] != "TCP4" && fields[1] != "TCP6" {
		return netip.Addr{}, errInvalidRequest
	}
	src, err := parseProxyAddr(fields[1], fields[2])
	if err != nil {
		return netip.Addr{}, errInvalidRequest
	}
	if _, err := parseProxyAddr(fields[1], fields[3]); err != nil {
		return netip.Addr{}, errInvalidRequest
	}
	if err := parseProxyPort(fields[4]); err != nil {
		return netip.Addr{}, errInvalidRequest
	}
	if err := parseProxyPort(fields[5]); err != nil {
		return netip.Addr{}, errInvalidRequest
	}
	return src, nil
}

func parseProxyAddr(network, value string) (netip.Addr, error) {
	ip, err := netip.ParseAddr(value)
	if err != nil {
		return netip.Addr{}, errInvalidRequest
	}
	switch network {
	case "TCP4":
		if !ip.Is4() {
			return netip.Addr{}, errInvalidRequest
		}
	case "TCP6":
		if !ip.Is6() {
			return netip.Addr{}, errInvalidRequest
		}
	default:
		return netip.Addr{}, errInvalidRequest
	}
	return ip, nil
}

func parseProxyPort(value string) error {
	port, err := strconv.Atoi(value)
	if err != nil {
		return errInvalidRequest
	}
	if port < 1 || port > 65535 {
		return errInvalidRequest
	}
	return nil
}

func peerAddr(addr net.Addr) netip.Addr {
	if addr == nil {
		return netip.Addr{}
	}
	host, _, err := net.SplitHostPort(addr.String())
	if err != nil {
		return netip.Addr{}
	}
	ip, err := netip.ParseAddr(host)
	if err != nil {
		return netip.Addr{}
	}
	return ip
}

type cappedBuffer struct {
	bytes.Buffer
	max      int
	exceeded bool
}

func (b *cappedBuffer) Write(p []byte) (int, error) {
	if b.exceeded {
		return len(p), nil
	}
	if b.Len()+len(p) > b.max {
		remain := b.max - b.Len()
		if remain > 0 {
			_, _ = b.Buffer.Write(p[:remain])
		}
		b.exceeded = true
		return len(p), nil
	}
	return b.Buffer.Write(p)
}

type bufferedConn struct {
	net.Conn
	reader *bufio.Reader
}

func (c *bufferedConn) Read(p []byte) (int, error) {
	return c.reader.Read(p)
}

func openRegularReadNoFollow(path string) (*os.File, bool, error) {
	info, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, true, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, true, fmt.Errorf("symlinks are not allowed")
	}
	if !info.Mode().IsRegular() {
		return nil, true, fmt.Errorf("not a regular file")
	}

	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if err != nil {
		if errors.Is(err, syscall.ENOENT) {
			return nil, false, nil
		}
		if errors.Is(err, syscall.ELOOP) {
			return nil, true, fmt.Errorf("symlinks are not allowed")
		}
		return nil, true, err
	}
	file := os.NewFile(uintptr(fd), path)
	info, err = file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, true, err
	}
	if !info.Mode().IsRegular() {
		_ = file.Close()
		return nil, true, fmt.Errorf("not a regular file")
	}
	return file, true, nil
}

func openRegularExecNoFollow(path string) (*os.File, bool, error) {
	fd, err := syscall.Open(path, cgiOpenFlags, 0)
	if err != nil {
		if errors.Is(err, syscall.ENOENT) {
			return nil, false, nil
		}
		return nil, true, err
	}
	file := os.NewFile(uintptr(fd), path)
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, true, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		_ = file.Close()
		return nil, true, fmt.Errorf("symlinks are not allowed")
	}
	if !info.Mode().IsRegular() {
		_ = file.Close()
		return nil, true, fmt.Errorf("not a regular file")
	}
	if info.Mode().Perm()&0111 == 0 {
		_ = file.Close()
		return nil, true, fmt.Errorf("execute bit is required")
	}
	return file, true, nil
}
