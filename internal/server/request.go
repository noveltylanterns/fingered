package server

import (
	"bufio"
	"errors"
	"io"
	"strings"
	"unicode/utf8"
)

const (
	InvalidRequestBody      = "Error: Invalid Request\r\n"
	NoContentBody           = "Error: No content configured for this request.\r\n"
	CreditsBody             = "\r\n_____________________________\r\nfinger://lanterns.io/fingered\r\n"
	maxTargetComponentBytes = 64
	maxTargetChainDepth     = 16
	maxFlagNameBytes        = 32
	maxFlagValueBytes       = 64
	maxFlagsPerRequest      = 16
)

var (
	errInvalidRequest = errors.New("invalid request")
	errLineTooLong    = errors.New("request line too long")
)

type Protocol string

const (
	ProtocolFinger  Protocol = "finger"
	ProtocolFingers Protocol = "fingers"
)

type Flag struct {
	Name  string
	Value string
	Bare  bool
}

type Request struct {
	Raw       string
	Canonical string
	Target    string
	Flags     []Flag
}

func readLine(r *bufio.Reader, max int) (string, bool, error) {
	var b strings.Builder
	for {
		ch, err := r.ReadByte()
		if err != nil {
			if errors.Is(err, io.EOF) {
				if b.Len() == 0 {
					return "", false, io.EOF
				}
				return b.String(), false, io.EOF
			}
			return "", false, err
		}
		if b.Len() >= max+2 {
			return "", true, errLineTooLong
		}
		_ = b.WriteByte(ch)
		if ch == '\n' {
			return b.String(), true, nil
		}
	}
}

func trimLineEnding(s string) string {
	s = strings.TrimSuffix(s, "\n")
	s = strings.TrimSuffix(s, "\r")
	return s
}

func parseRequest(rawLine string, protocol Protocol, extendFinger bool) (Request, error) {
	switch protocol {
	case ProtocolFinger:
		return parseFingerRequest(rawLine, extendFinger)
	case ProtocolFingers:
		return parseFingersRequest(rawLine)
	default:
		return Request{}, errInvalidRequest
	}
}

func parseFingerRequest(rawLine string, extendFinger bool) (Request, error) {
	line := trimLineEnding(rawLine)
	if line == "" {
		return Request{Raw: "", Canonical: ""}, nil
	}

	if extendFinger {
		return parseStructuredRequest(line, ProtocolFinger)
	}

	req := Request{Raw: line}
	if line == "/W" {
		req.Canonical = "/W"
		return req, nil
	}

	target := line
	if strings.HasPrefix(line, "/W") {
		if !strings.HasPrefix(line, "/W ") {
			return Request{}, errInvalidRequest
		}
		target = line[3:]
		if target == "" || strings.TrimSpace(target) != target {
			return Request{}, errInvalidRequest
		}
		req.Canonical = "/W " + target
	} else {
		req.Canonical = target
	}

	if !validTargetComponent(target) {
		return Request{}, errInvalidRequest
	}
	req.Target = target
	return req, nil
}

func parseFingersRequest(rawLine string) (Request, error) {
	if !strings.HasSuffix(rawLine, "\r\n") {
		return Request{}, errInvalidRequest
	}
	line := strings.TrimSuffix(rawLine, "\r\n")
	if !utf8.ValidString(line) {
		return Request{}, errInvalidRequest
	}
	if line == "" {
		return Request{Raw: "", Canonical: ""}, nil
	}
	return parseStructuredRequest(line, ProtocolFingers)
}

func parseStructuredRequest(line string, protocol Protocol) (Request, error) {
	if !validRequestRunes(line) {
		return Request{}, errInvalidRequest
	}

	tokens := strings.Fields(line)
	if len(tokens) == 0 {
		return Request{}, errInvalidRequest
	}

	req := Request{Raw: line}
	seenFlags := make(map[string]struct{})
	var targetToken string

	for _, token := range tokens {
		if strings.HasPrefix(token, "/") {
			if targetToken != "" {
				return Request{}, errInvalidRequest
			}
			flag, err := parseFlag(token)
			if err != nil {
				return Request{}, err
			}
			if _, exists := seenFlags[flag.Name]; exists {
				continue
			}
			if len(req.Flags) >= maxFlagsPerRequest {
				return Request{}, errInvalidRequest
			}
			seenFlags[flag.Name] = struct{}{}
			req.Flags = append(req.Flags, flag)
			continue
		}
		if targetToken != "" {
			return Request{}, errInvalidRequest
		}
		targetToken = token
	}

	if targetToken != "" {
		target, err := parseTarget(targetToken, protocol)
		if err != nil {
			return Request{}, err
		}
		req.Target = target
	}

	req.Canonical = buildCanonicalRequest(req.Flags, req.Target)
	return req, nil
}

func validRequestRunes(line string) bool {
	for _, r := range line {
		switch {
		case r == ' ':
			continue
		case r < 0x21 || r > 0x7e:
			return false
		}
	}
	return true
}

func parseFlag(token string) (Flag, error) {
	if len(token) < 2 || token[0] != '/' {
		return Flag{}, errInvalidRequest
	}
	body := token[1:]
	if strings.Count(body, "=") > 1 {
		return Flag{}, errInvalidRequest
	}
	if idx := strings.IndexByte(body, '='); idx >= 0 {
		name := body[:idx]
		value := body[idx+1:]
		if !validFlagName(name) || !validFlagValue(value) {
			return Flag{}, errInvalidRequest
		}
		return Flag{Name: name, Value: value}, nil
	}
	if !validFlagName(body) {
		return Flag{}, errInvalidRequest
	}
	return Flag{Name: body, Bare: true}, nil
}

func validFlagName(s string) bool {
	if len(s) == 0 || len(s) > maxFlagNameBytes {
		return false
	}
	for i := 0; i < len(s); i++ {
		if !isFlagRune(s[i]) {
			return false
		}
	}
	return true
}

func validFlagValue(s string) bool {
	if len(s) == 0 || len(s) > maxFlagValueBytes {
		return false
	}
	for i := 0; i < len(s); i++ {
		if !isFlagRune(s[i]) {
			return false
		}
	}
	return true
}

func isFlagRune(b byte) bool {
	return isAlphaNum(b) || b == '-' || b == '_'
}

func parseTarget(token string, protocol Protocol) (string, error) {
	switch protocol {
	case ProtocolFinger:
		if strings.Contains(token, "@") || !validTargetComponent(token) {
			return "", errInvalidRequest
		}
		return token, nil
	case ProtocolFingers:
		parts := strings.Split(token, "@")
		if len(parts) > maxTargetChainDepth {
			return "", errInvalidRequest
		}
		for _, part := range parts {
			if !validTargetComponent(part) {
				return "", errInvalidRequest
			}
		}
		return strings.Join(parts, "@"), nil
	default:
		return "", errInvalidRequest
	}
}

func validTargetComponent(s string) bool {
	if len(s) == 0 || len(s) > maxTargetComponentBytes {
		return false
	}
	lower := strings.ToLower(s)
	if strings.HasSuffix(lower, ".txt") || strings.HasSuffix(lower, ".cgi") {
		return false
	}
	if s[0] == '.' || s[len(s)-1] == '.' || strings.Contains(s, "..") {
		return false
	}
	for i := 0; i < len(s); i++ {
		if !isTargetRune(s[i]) {
			return false
		}
	}
	return true
}

func isTargetRune(b byte) bool {
	return isAlphaNum(b) || b == '-' || b == '_' || b == '.' || b == '~'
}

func isAlphaNum(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9')
}

func buildCanonicalRequest(flags []Flag, target string) string {
	parts := make([]string, 0, len(flags)+1)
	for _, flag := range flags {
		if flag.Bare {
			parts = append(parts, "/"+flag.Name)
			continue
		}
		parts = append(parts, "/"+flag.Name+"="+flag.Value)
	}
	if target != "" {
		parts = append(parts, target)
	}
	return strings.Join(parts, " ")
}
