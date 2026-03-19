# fingered Specification

This document is the working specification for `fingered`, a minimal Finger daemon written in Go and shipped as a self-contained binary.

## Goals

- Provide a lean Finger server with minimal dependencies.
- Build as a self-contained Go binary so end users do not need Go installed.
- Stay conservative with the Finger protocol and keep behavior simple.
- Avoid name-based virtual hosting or any runtime hostname resolution.
- Keep the request/response window as short as possible.
- Treat all client input as untrusted and validate or reject it strictly.
- Keep filesystem exposure narrow and prevent traversal, injection, and unintended execution.

## Non-Goals

- No directory-style content tree.
- No hostname-based routing.
- No outbound Finger client behavior or remote content retrieval.
- No general templating or shell execution.
- No requirement to support non-Linux targets as a primary deployment environment.

## Protocol Scope

`fingered` is Finger-compatible on the wire. The response body is plain text and the server closes the connection immediately after responding.

Behavioral rules:

- Accept one TCP connection.
- Read one bounded request line.
- Produce one plain-text response with `CRLF` line endings.
- Close the connection immediately.
- Emit no greeting banner.
- Emit no version string.
- Emit no service-name disclosure.

The service is document-oriented, but it remains conservative with Finger semantics:

- Empty request is valid.
- `/W` is accepted as a verbose request marker for compatibility.
- `/W` does not change the selected resource.
- Custom flags do not change the selected resource.
- Plaintext Finger and TLS `fingers` both support relay chains only when `relay_enable = yes`.
- On TLS `fingers`, URI path segments are mapped into the same `target@relay1@relay2...` request style used by classic Finger relays.
- `finger://` and `fingers://` therefore share the same relay meaning; `fingers://` only changes transport and URI syntax.

## Listener Model

`fingered` does not do DNS-based dispatch and does not multiplex multiple sites by hostname.

- One daemon instance may bind one plaintext Finger listener and one optional TLS `fingers` listener.
- `bind_ip` must be a literal IPv4 or IPv6 address.
- No hostnames are accepted in configuration.
- If the operator wants additional addresses beyond the optional TLS companion listener, they run multiple instances with separate configs.

This covers both deployment modes:

- Direct mode: `fingered` binds the public IP on port `79`.
- Proxied mode: `fingered` binds `127.0.0.1` or `::1` on a high port, and nginx stream listens on port `79`.

## TLS Service Selection

Configuration keys:

- `tls_enable = no|yes_both|yes_strict`
- `tls_port`
- `tls_port_out`
- `tls_cert`
- `tls_key`
- `tls_doc_root`

Defaults:

- `tls_enable = no`
- `tls_port = 8179`
- `tls_port_out = tls_port`

Mode rules:

- `tls_enable = no` runs only plaintext Finger on `bind_ip:port`.
- `tls_enable = yes_both` runs plaintext Finger on `bind_ip:port` and TLS `fingers` on `bind_ip:tls_port`.
- `tls_enable = yes_strict` runs only TLS `fingers` on `bind_ip:tls_port`.

TLS rules:

- `tls_cert` and `tls_key` are required whenever `tls_enable` is not `no`.
- `tls_cert` and `tls_key` are ignored when `tls_enable = no`.
- `tls_doc_root` defaults to `doc_root` when unset.
- `fingered` does not require any particular public CA model for `fingers`; operators may use CA-signed, self-signed, locally trusted, pinned, or private-CA certificates.
- `fingered` v1 does not require client certificates and does not define any application behavior based on them.
- The TLS listener is opt-in because `fingers://` remains experimental.
- Default installs must ship with `tls_enable = no`.

## Request Grammar

The backend content namespace is flat. Plaintext Finger requests are flat. TLS `fingers` URIs may contain multiple path segments, but after URI mapping those segments become the same emitted `target@relay...` request form used by classic Finger relay syntax.

All user names, target names, and host-like chain components are untrusted inputs.

Shared component sanitization rules:

- any request component used as a user, host-like segment, selector, or target must be parsed and validated before any lookup or CGI handoff
- raw request substrings must never be used directly as filenames, CGI target names, or log fields
- the TLS `fingers` listener accepts exactly one UTF-8 request line terminated by `CRLF`
- a TLS `fingers` request using `LF` without `CRLF` is invalid
- malformed UTF-8 on the TLS `fingers` listener is invalid
- request target components follow the `fingers` draft path-segment character class: letters, digits, dash (`-`), underscore (`_`), period (`.`), and tilde (`~`)
- authority-host syntax in the `fingers` draft allows letters, digits, dash (`-`), underscore (`_`), and period (`.`), but `fingered` does not use URI authority hosts for routing
- percent-encoding is not supported and `%` is always invalid in request components
- all accepted request components are re-serialized into a canonical form before backend use
- maximum component length is `64` bytes
- maximum chained target depth for `fingers` is `16` components

Configuration key:

- `relay_enable = yes|no`
- `extend_finger = yes|no`
- `port_out`
- `tls_port_out`

Default:

- `relay_enable = no`
- `extend_finger = no`
- `port_out = port`
- `tls_port_out = tls_port`

Accepted plaintext Finger requests by default:

- empty request
- `/W`
- `target`
- `/W target`

If `extend_finger = yes`, the plaintext Finger listener also accepts the additional `fingers`-style flag syntax:

- zero or more flags before an optional target
- bare flags like `/PLAN`
- variable flags like `/mode=full`
- multiple flags before a target, such as `/PLAN /mode=full target`

Compatibility rules:

- `relay_enable` affects both plaintext Finger and TLS `fingers`
- `extend_finger` affects only the plaintext Finger listener
- `fingers` requests use their own flag syntax regardless of `extend_finger`
- relay chains are invalid on either listener unless `relay_enable = yes`
- default installs must keep `extend_finger = no`
- extra flags never change static file selection or CGI target selection by themselves

Flag validation and sanitization rules:

- the daemon must parse flags into structured tokens before any CGI handoff
- the raw incoming flag text must never be forwarded directly to CGI
- bare flag names may contain only `[A-Za-z0-9_-]`
- variable flag names may contain only `[A-Za-z0-9_-]`
- variable flag values may contain only `[A-Za-z0-9_-]`
- bare and variable flag names must be `1..32` bytes
- variable flag values must be `1..64` bytes
- a request may contain at most `16` flags
- duplicate flag names keep only the first occurrence
- later duplicate flags are ignored
- all single-character flags are reserved by the `fingers` draft
- `fingered` must not assign built-in permanent meaning to single-character flags other than legacy `/W` compatibility behavior on plaintext Finger
- invalid flags are rejected with `Error: Invalid Request`; they are never stripped and forwarded
- canonical flag serialization uses exactly one ASCII space between tokens
- the canonical CGI request line is rebuilt from parsed tokens; original spacing and token boundaries are discarded

Plaintext Finger target grammar:

- exactly one validated target component when a target is present
- allowed characters: letters, digits, dash (`-`), underscore (`_`), period (`.`), tilde (`~`)
- maximum target length: `64` bytes
- `@` is not allowed on plaintext Finger unless `relay_enable = yes`

When `relay_enable = yes`, plaintext Finger also accepts relay chains in the historic request form:

- `target@hop1`
- `target@hop1@hop2`
- `target@hop1@hop2@hop3`

Relay chain rules:

- relay chains are accepted on plaintext Finger and TLS `fingers` only when `relay_enable = yes`
- on plaintext Finger, relay chains are encoded directly in the opaque request string
- on TLS `fingers`, URI path segments are mapped into the same emitted `target@relay...` request string
- `finger://example.com/alice@198.51.100.10@203.0.113.20` is valid when `relay_enable = yes`
- each relay hop must be a literal IPv4 address only in v1
- relay hops must be public routable IPv4 addresses
- relay hops must not be loopback, private RFC1918, link-local, multicast, unspecified, or broadcast-style addresses
- maximum relay depth is `4`
- no hostname resolution is performed for relay hops
- on TLS `fingers`, the same relay semantics apply after URI mapping has emitted the `target@relay...` expression

Rejected input:

- `/` except the exact leading `/W` form
- `\`
- `%`
- `@` relay syntax on either listener when `relay_enable = no`
- spaces inside a target component
- tabs
- control characters
- NUL bytes
- explicit extensions such as `.txt` or `.cgi`

Input handling rules:

- Only the transport line ending is stripped from the request.
- Requests are validated with a strict allowlist grammar.
- Unsupported symbols are never mapped to content.
- Invalid requests are rejected, not rewritten into some other target.
- Invalid request data must not be reflected back to the client.

This means:

- `finger://example.com/` maps to the empty request
- `finger://example.com/foo` maps to target `foo`
- `finger://example.com/john.doe` maps to target `john.doe`
- `finger://example.com/~user` maps to target `~user`
- `finger://example.com/foo/bar` is invalid
- `finger://example.com/foo.txt` is invalid
- `finger://example.com/alice@198.51.100.10` is valid only when `relay_enable = yes`

When `extend_finger = yes`, plaintext Finger may also accept request lines such as:

- `/PLAN`
- `/PLAN foo`
- `/PLAN /mode=full foo`

TLS `fingers` request grammar:

- zero or more validated flags before an optional target
- the target, when present, is one or more validated target components joined by `@`
- each target component may contain only letters, digits, dash (`-`), underscore (`_`), period (`.`), and tilde (`~`)
- `@` is a separator only; it is not part of any component
- empty target components are invalid
- a `fingers` target chain is canonicalized by rejoining validated components with single `@` separators
- empty requests and flag-only requests are valid on `fingers`
- the `fingers` authority host is conceptually the outermost relay host in the updated URI mapping, even though the server receives only the emitted target-and-relay expression on the wire
- a `fingers` target chain with one component is a local target
- a `fingers` target chain with relay hops is handled exactly like classic Finger relay syntax when `relay_enable = yes`
- a `fingers` target chain with relay hops is invalid when `relay_enable = no`

## Content Mapping

The content model is a flat document root.

Root selection rules:

- plaintext Finger requests resolve against `doc_root`
- TLS `fingers` requests resolve against `tls_doc_root` when set
- if `tls_doc_root` is unset, TLS `fingers` requests also resolve against `doc_root`

Selection rules:

- target selection is based only on the validated target or empty request
- flags do not alter static file lookup
- flags do not alter CGI target lookup
- flag-only requests map to the empty target and therefore resolve as `index.txt` then `index.cgi`
- a single-component TLS `fingers` target is canonicalized and then treated as one flat local target string for backend lookup
- any plaintext Finger or TLS `fingers` request with relay hops bypasses local content lookup and is handled by the relay path when `relay_enable = yes`

- empty request -> `index.txt`
- `/W` -> `index.txt`
- `foo` -> `foo.txt`
- `/W foo` -> `foo.txt`
- `alice` on TLS `fingers` -> `alice.txt`

If `cgi_enable = yes`, fallback lookup is allowed:

- empty request -> `index.txt`, then `index.cgi`
- `foo` -> `foo.txt`, then `foo.cgi`
- `alice` on TLS `fingers` -> `alice.txt`, then `alice.cgi`

The public target namespace never exposes `.txt` or `.cgi`.

## Relay Handling

Configuration key:

- `relay_enable = yes|no`

Default:

- `relay_enable = no`

Behavior when enabled:

- relay is available on plaintext Finger and TLS `fingers`
- relay applies only to validated request forms containing relay hops
- local static lookup and local CGI lookup are skipped for relay requests
- plaintext Finger relay connects only to the final hop in the chain on TCP port `port_out`
- TLS `fingers` relay connects only to the final hop in the chain on TCP port `tls_port_out`
- plaintext Finger relay sends the remainder of the chain exactly once in classic Finger style
- TLS `fingers` relay sends the remainder of the chain exactly once in `fingers` request style over TLS

Example:

- client request to `fingered`: `alice@198.51.100.10@203.0.113.20`
- `fingered` connects to `203.0.113.20:79`
- `fingered` sends `alice@198.51.100.10<CRLF>`
- the downstream Finger server may then relay further according to classic Finger behavior

Equivalent TLS `fingers` example:

- URI requested by the client: `fingers://203.0.113.20/198.51.100.10/alice`
- request received by `fingered`: `alice@198.51.100.10<CRLF>`
- `fingered` connects to `198.51.100.10:8179` over TLS
- `fingered` sends `alice<CRLF>`
- the downstream `fingers` server may then relay further according to the same right-to-left relay model

Security rules:

- no DNS lookups for relay hops
- no hostnames in relay hops
- only public literal IPv4 relay hops are allowed in v1
- relay uses the same request, read, write, and response-size limits as local handling
- relay responses are sanitized the same way as all other response bodies
- relay failures return `Error: No content configured for this request.`
- malformed or disallowed relay hops return `Error: Invalid Request`

## Response Templates

Configuration key:

- `tpl_enable = yes|no`

Default:

- `tpl_enable = no`

Template wrapping is disabled by default for performance reasons.

When `tpl_enable = yes`, `fingered` may wrap valid responses with optional header and footer fragments stored under the effective content root for that listener.

Static template lookup:

- `header.txt`
- `footer.txt`

If `cgi_enable = yes`, dynamic template lookup is also allowed:

- `header.cgi`
- `footer.cgi`

Template resolution rules:

- For the top wrapper, prefer `header.txt`.
- If `header.txt` does not exist and `cgi_enable = yes`, try `header.cgi`.
- For the bottom wrapper, prefer `footer.txt`.
- If `footer.txt` does not exist and `cgi_enable = yes`, try `footer.cgi`.
- If neither file exists for a wrapper position, omit that wrapper silently.
- If a selected wrapper file exists but is unreadable, invalid, times out, or fails sanitization, omit that wrapper and log an error if `log_errors = yes`.

Template application rules:

- Templates are applied only to valid Finger requests.
- `Error: Invalid Request` must remain the exact standalone body with no header or footer.
- Successful content responses may be wrapped.
- `Error: No content configured for this request.` may be wrapped when the request itself was valid.
- `credits_enable` applies to all valid responses, including the generic no-content response.
- If `credits_enable = yes`, append the credits byline exactly once, after the fully assembled response body and after any footer include.
- The credits byline must not be emitted after the header fragment or after the main content as a separate intermediate step.
- Template output is treated as plain text and is subject to the same sanitization and `CRLF` normalization rules as other response content.

Credits byline text:

`Powered by Fingered`

`finger://lanterns.io/fingered`

## Error Response

There is no HTTP-style status layer. `fingered` uses two generic plain-text error bodies:

`Error: Invalid Request`

and

`Error: No content configured for this request.`

`Error: Invalid Request` is used for:

- malformed request line
- any request outside the accepted Finger grammar
- unsupported symbols or path forms
- explicit extension requests such as `.txt` or `.cgi`
- relay syntax on either listener when `relay_enable = no`
- disallowed or malformed relay hops
- malformed `fingers` target chains such as empty `@` components
- malformed UTF-8 on TLS `fingers`
- a TLS `fingers` request terminated with `LF` instead of `CRLF`

`Error: No content configured for this request.` is used for:

- missing `index.txt`
- missing target
- CGI target not present
- CGI execution failure
- CGI output rejected during sanitization

The server sends the applicable message as plain text with `CRLF` line endings and closes the connection.

If the peer connects and does not deliver a complete request line before timeout, the daemon may close the connection silently rather than emit any identifying output.

## Response Rules

All successful responses are plain text.

- File-backed responses stream plaintext from the selected `.txt` file.
- CGI-backed responses return sanitized plaintext emitted on `stdout`.
- TLS `fingers` responses are UTF-8 plaintext as defined by the draft.
- When `tpl_enable = yes`, optional header and footer content may be prepended and appended around valid responses.
- When `credits_enable = yes`, append the credits byline once at the very end of the response.
- No protocol headers are added.
- Line endings are normalized to `CRLF`.
- NUL bytes are not permitted in output.

Recommended response safeguards:

- Cap total response size.
- Cap CGI stdout size.
- Apply write deadlines.
- Count header, main content, footer, and credits together against `max_response_bytes`.
- If a valid response would exceed `max_response_bytes`, do not truncate it; instead return the exact standalone body `Error: No content configured for this request.` with no templates and no credits.

## Filesystem Rules

The effective content root for a request is:

- `doc_root` for plaintext Finger
- `tls_doc_root` for TLS `fingers` when configured
- otherwise `doc_root`

Rules:

- Only regular files may be served or executed.
- Symlinks must be refused.
- FIFOs, sockets, devices, and directories must be refused.
- Target resolution must never escape the effective content root.
- Request strings must never be treated as raw paths.

For static content, `fingered` should derive the filename internally:

- `index.txt`
- `<target>.txt`

For CGI fallback, `fingered` should derive:

- `index.cgi`
- `<target>.cgi`

## CGI Mode

Configuration key:

- `cgi_enable = yes|no`

Default:

- `cgi_enable = no`

Behavior:

- CGI is consulted only if the corresponding `.txt` file does not exist.
- CGI output is treated as plain-text Finger response content.
- No HTTP headers are expected or parsed.
- If no CGI target or CGI template is selected, request flags are ignored and discarded after validation.
- If a CGI target is selected, only the canonical validated request line may be passed on `stdin`.
- `stdin` contains exactly one sanitized request line terminated with `LF`.
- that canonical line is reconstructed from validated tokens only
- raw client bytes, raw spacing, duplicate flags, and rejected tokens never reach CGI
- for plaintext Finger without `extend_finger`, CGI stdin contains only the validated classic request form
- for plaintext Finger with `extend_finger`, CGI stdin may include sanitized extra flags
- for TLS `fingers`, CGI stdin may include sanitized flags native to that protocol
- If a CGI script chooses not to read or parse that request line, the flags have no effect.
- No request data is passed through environment variables.
- No request data is passed as command-line arguments.
- The request only determines which local `.cgi` file is selected after strict validation.
- CGI templates, when selected, receive the same canonical sanitized request line as CGI content handlers.
- No other client-supplied data path into CGI exists.
- Only `stdout` is returned to the client.
- `stderr` is not returned to the client and may be logged as an error.
- CGI execution is subject to `cgi_timeout_ms`.
- CGI stdout is subject to `cgi_max_stdout_bytes`.

### CGI Security Requirements

The CGI path must be confined so it cannot execute content outside the effective content root for the active listener.

Required design:

- Validate the target and all target components before any filesystem lookup.
- Resolve only `/<canonical-target>.cgi` or `/index.cgi` relative to the effective content root.
- Refuse symlinks.
- Refuse non-regular files.
- Require the execute bit on `.cgi` targets.
- Never invoke a shell.
- Execute directly with `execve` or `fexecve`.
- Pass a fixed, minimal execution context only.
- Do not expose client IP, peer IP, environment metadata, or any request-derived data to CGI except the single canonical request line on `stdin`.
- Treat CGI as server-side content generation with one narrowly defined request input path only.
- Reject any request whose flags cannot be losslessly represented in the daemon's canonical sanitized form.

Preferred containment model:

- Run the CGI child in a chroot rooted at the effective content root.
- `chdir(effective_doc_root)` then `chroot(".")` in the child before `exec`.
- Execute `/<canonical-target>.cgi` or `/index.cgi` inside that jail.
- Clear the environment completely by default.
- No CGI environment allowlist is defined in v1.
- Apply strict timeouts and output caps.

Implications:

- Static binaries are the cleanest CGI targets.
- Shebang scripts only work if their interpreter exists inside the jail.
- This is acceptable and preferable to broadening the execution surface.

The daemon itself does not need to run in a global chroot by default. The CGI child jail is the preferred containment boundary because it keeps normal access to `/etc/fingered/` and `/home/finger/logs/fingered/` simple.

The malware pattern where a local process runs `finger host` and then executes the returned text is a client-side abuse pattern, not a server-side daemon exploit. `fingered` must therefore remain strictly server-only:

- it does not initiate outbound Finger connections
- it does not fetch remote content
- it does not pipe response bodies into any interpreter
- it does not transform requests into commands

## Logging

Logging is optional and file-based.

Configuration keys:

- `log_root`
- `log_umask`
- `log_format`
- `log_errors`
- `log_requests`

Behavior:

- `log_root` defaults to `/home/finger/logs/fingered/`.
- `log_umask` controls permissions for created log files.
- If `log_umask` is unset, default to `0007`, which yields log files with mode `0660`.
- If `log_format` is unset, default to `rfc5424`.
- Supported `log_format` values are `rfc5424` and `rfc3164`.
- `log_errors` is `yes|no`.
- `log_requests` is `yes|no`.
- If `log_errors = yes`, write `error.log` under `log_root`.
- If `log_requests = yes`, write `access.log` under `log_root`.
- If either log toggle is `no`, that log file is not opened or written.
- If an enabled log file cannot be created or opened at startup, fail startup.

`log_root` only matters when at least one of `log_errors` or `log_requests` is `yes`.

Hostname lookups are not performed. Logs include IP addresses only.

Each access log entry should include:

- timestamp
- protocol: `finger` or `fingers`
- listener port
- `client_ip`
- `peer_ip`
- request form or canonical target
- outcome: `hit`, `miss`, `invalid`, `refused`, `cgi_hit`, `cgi_fail`, `relay_hit`, `relay_fail`
- bytes sent
- duration

Each error log entry should include:

- timestamp
- severity
- protocol: `finger` or `fingers`
- listener port
- `client_ip` if available
- `peer_ip` if available
- sanitized request fragment if relevant
- error summary

Log safety requirements:

- Sanitize request-derived data before logging.
- Strip or escape `CR`, `LF`, tabs, and control bytes.
- Cap logged request length.

## Real Client IP and Proxying

When running behind nginx stream on localhost, `fingered` must be able to log the real client IP rather than only the loopback peer.

Configuration keys:

- `proxy_protocol = yes|no`
- `trusted_proxy_ips = ip1,ip2,...`

Rules:

- Default `proxy_protocol = no`.
- Only trust PROXY protocol headers from peers whose immediate TCP IP is in `trusted_proxy_ips`.
- If a trusted PROXY header is present, use its source address as `client_ip`.
- Always keep the immediate TCP peer as `peer_ip`.
- If PROXY protocol is disabled or untrusted, `client_ip` equals `peer_ip`.

This allows:

- direct public deployment without a proxy
- localhost deployment behind nginx stream while preserving the original client IP

## Timeouts and Limits

Recommended defaults:

- `read_timeout_ms = 1000`
- `write_timeout_ms = 1000`
- `max_request_bytes = 256`
- `cgi_timeout_ms = 1000`
- `cgi_max_stdout_bytes = 262144`
- `max_response_bytes = 262144`

Additional required limits:

- maximum target component length: `64`

The implementation should close connections aggressively on timeout or malformed input.

Recommended invalid-input behavior:

- if a complete request line is received and it is invalid, return `Error: Invalid Request`
- if no complete request line is received before timeout, close silently

## Build and Distribution

Implementation language:

- Go

Build goals:

- self-contained binary
- minimal dependencies
- portable across most Linux systems

Preferred build profile:

- Go standard library only where practical
- `CGO_ENABLED=0`
- stripped release builds

Example build:

```bash
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w -buildid=" -o fingered ./cmd/fingered
```

Release targets should include at least:

- `linux/386`
- `linux/amd64`
- `linux/arm64`
- `linux/riscv64`

## Configuration File Format

Primary config path:

- `/etc/fingered/fingered.conf`

Format:

- line-oriented `key = value`
- comments with `#` or `;`
- no JSON

Unknown configuration keys should be treated as startup errors.

### Required Keys

- `bind_ip`
- `port`
- `doc_root`

### Optional Keys

- `relay_enable`
- `extend_finger`
- `tls_enable`
- `tls_port`
- `port_out`
- `tls_port_out`
- `tls_cert`
- `tls_key`
- `tls_doc_root`
- `read_timeout_ms`
- `write_timeout_ms`
- `max_request_bytes`
- `cgi_timeout_ms`
- `cgi_max_stdout_bytes`
- `max_response_bytes`
- `cgi_enable`
- `tpl_enable`
- `credits_enable`
- `log_root`
- `log_umask`
- `log_format`
- `log_errors`
- `log_requests`
- `proxy_protocol`
- `trusted_proxy_ips`

### Sample Config

```conf
bind_ip = 127.0.0.1
port = 7979

doc_root = /home/finger/app/finger/

relay_enable = no
extend_finger = no

tls_enable = no
tls_port = 8179
port_out = 79
tls_port_out = 8179
# tls_cert = /etc/fingered/tls/fingered.crt
# tls_key = /etc/fingered/tls/fingered.key
# tls_doc_root = /home/finger/app/fingers/

read_timeout_ms = 1000
write_timeout_ms = 1000
max_request_bytes = 256
cgi_timeout_ms = 1000
cgi_max_stdout_bytes = 262144
max_response_bytes = 262144

cgi_enable = no
tpl_enable = no
credits_enable = yes

log_root = /home/finger/logs/fingered/
log_umask = 0007
log_format = rfc5424
log_errors = yes
log_requests = no

proxy_protocol = yes
trusted_proxy_ips = 127.0.0.1,::1
```

## System Service and Installation

An installer script named `install_fingered.sh` must accompany the daemon.

Purpose:

- prepare the runtime environment
- create the content-owner and service users
- install the default config and directories
- optionally install a systemd unit

Script requirements:

- must be run as root
- supports `--nosysd`
- `--nosysd` skips systemd unit creation and enablement

Installer actions:

- install `fingered` to `/usr/local/sbin/fingered`
- `install_fingered.sh` accepts `--arch 386|amd64|arm64|riscv64`
- default installer architecture is `amd64` when `--arch` is omitted
- create `/etc/fingered/`
- create `/etc/fingered/tls/`
- create normal user `finger`
- set `finger` comment to `finger document root`
- set `finger` shell to `/sbin/nologin`
- create home directory `/home/finger/`
- create document root `/home/finger/app/finger/`
- create log directory `/home/finger/logs/fingered/`
- create system user `fingered`
- set `fingered` comment to `finger server daemon`
- create no home directory for `fingered`
- set `fingered` shell to `/sbin/nologin`
- create system group `fingered` if needed
- add `fingered` to group `finger`
- create `/home/finger/logs/fingered/error.log` if the installed sample config enables `log_errors = yes`
- create `/home/finger/logs/fingered/access.log` only if the installed sample config enables `log_requests = yes`
- install sample `/etc/fingered/fingered.conf`
- install the sample config with `tls_enable = no`
- set ownership and permissions appropriately
- do not create `/srv/fingered/`

Recommended ownership:

- config readable by root and service user as needed
- `/etc/fingered/tls/` owned by `root:fingered`
- `/home/finger/` owned by `finger:finger`
- `doc_root` owned by `finger:finger`
- any configured `tls_doc_root` should also be owned by `finger:finger`
- `/home/finger/logs/fingered/` owned by `finger:finger`
- `fingered` reads `doc_root` through membership in group `finger`
- if `tls_doc_root` differs from `doc_root`, `fingered` reads that tree through the same group membership
- log files owned by `fingered:finger`
- the `finger` group must have write access to log files
- `fingered` writes logs through its primary user and group membership in `finger`

Recommended permissions:

- use `umask 027` while creating the content tree
- `/etc/fingered/tls/` mode `0750`
- TLS certificate and key files should be `0640` and owned by `root:fingered`
- directories in the content tree should be `0750`
- files in the content tree should be `0640`
- `/home/finger/logs/fingered/` mode `2770`
- the setgid bit on `/home/finger/logs/fingered/` should keep new log files in group `finger`
- log files should default to `0660` when `log_umask` is unset

### systemd Unit Requirements

When systemd setup is enabled, the unit should:

- run as `fingered`
- execute `/usr/local/sbin/fingered -config /etc/fingered/fingered.conf`
- restart on failure
- grant only the minimal capabilities needed

Recommended capability set:

- `CAP_NET_BIND_SERVICE` for direct binding to port `79`
- `CAP_SYS_CHROOT` for CGI child jailing

Recommended hardening baseline:

- `User=fingered`
- `Group=fingered`
- `SupplementaryGroups=finger`
- `NoNewPrivileges=yes`
- `UMask=0027`
- `AmbientCapabilities=CAP_NET_BIND_SERVICE CAP_SYS_CHROOT`
- `CapabilityBoundingSet=CAP_NET_BIND_SERVICE CAP_SYS_CHROOT`
- `PrivateTmp=yes`
- `ProtectSystem=strict`
- `ReadOnlyPaths=/home/finger/app/finger`
- `ReadWritePaths=/home/finger/logs/fingered`
- `ProtectKernelTunables=yes`
- `ProtectKernelModules=yes`
- `ProtectControlGroups=yes`
- `RestrictAddressFamilies=AF_INET AF_INET6 AF_UNIX`
- `MemoryDenyWriteExecute=yes`
- `LockPersonality=yes`
- `RestrictRealtime=yes`
- `RestrictSUIDSGID=yes`
- `RemoveIPC=yes`

If `tls_doc_root` differs from `doc_root`, the unit must also allow read access to that configured TLS content tree.

If `--nosysd` is used, the script should still complete user and directory setup and print that equivalent privileges are required for binding low ports or using CGI chroot manually.

## nginx Stream Deployment Example

Local backend mode:

```conf
bind_ip = 127.0.0.1
port = 7979
doc_root = /home/finger/app/finger/
tls_enable = no
cgi_enable = no
proxy_protocol = yes
trusted_proxy_ips = 127.0.0.1,::1
```

nginx stream example:

```nginx
stream {
    server {
        listen 79;
        proxy_pass 127.0.0.1:7979;
        proxy_protocol on;
        proxy_timeout 5s;
    }
}
```

Direct public mode:

```conf
bind_ip = 203.0.113.10
port = 79
doc_root = /home/finger/app/finger/
tls_enable = no
cgi_enable = no
proxy_protocol = no
```

Dual-service mode:

```conf
bind_ip = 203.0.113.10
port = 79
doc_root = /home/finger/app/finger/
tls_enable = yes_both
tls_port = 8179
tls_cert = /etc/fingered/tls/fingered.crt
tls_key = /etc/fingered/tls/fingered.key
tls_doc_root = /home/finger/app/fingers/
cgi_enable = no
proxy_protocol = no
```

## Security Smoke Tests

`fingered` v1 should ship with a smoke-test plan that can be run locally and from remote machines. These tests improve confidence and catch regressions, but they do not prove that exploitation is impossible.

Required test categories:

- Protocol conformance:
  valid empty request, valid target, valid `/W`, valid `/W target`, valid `fingers` target chains, valid `fingers` `CRLF` request framing, response closes immediately, no banner before request.
- Invalid request rejection:
  `.txt`, `.cgi`, `@` on plaintext Finger, malformed `fingers` `@` chains, `LF`-only `fingers` requests, malformed UTF-8 on `fingers`, `/`, `\\`, `%`, spaces, tabs, control bytes, invalid target-component characters, oversized request line.
- Timeout behavior:
  connect and send nothing, partial line only, very slow sender; daemon should timeout and remain healthy.
- Content mapping:
  `index.txt`, `<target>.txt`, canonical `fingers` target-chain mapping, missing content path, generic no-content response, no extension exposure.
- Plaintext relay:
  disabled by default, same relay semantics on `finger://` and `fingers://`, literal public IPv4 hops only, no DNS resolution, malformed/disallowed hops rejected, relay failures mapped to the generic no-content response.
- CGI confinement:
  `.cgi` only when `.txt` is missing, no argv/env request injection, CGI stdin receives only the canonical sanitized request line, execute-bit required, chroot confinement works, symlink targets refused.
- Flag sanitization:
  invalid flag names, invalid flag values, duplicate flags, reserved single-character flags left without daemon-defined semantics, excess flag count, mixed raw spacing, and raw control-byte attempts must either be canonicalized safely or rejected as invalid; raw client bytes must never be forwarded unchanged to CGI.
- Template and credits behavior:
  header/footer optional, wrappers skipped on failure, credits appended once at absolute end, invalid requests unwrapped.
- Response size limits:
  oversize static body, oversize CGI body, oversize assembled response with templates and credits.
- Logging:
  startup fails if enabled logs cannot open, request/error entries sanitize control characters, `client_ip`/`peer_ip` handling works with and without PROXY protocol.
- Resilience:
  repeated malformed requests, parallel malformed connections, daemon does not crash, panic, hang, or stop accepting new requests.

Recommended local smoke tooling:

- `finger` for normal protocol requests
- `nc` or `socat` for raw request crafting
- shell loops for repeated malformed inputs

Recommended remote smoke coverage:

- run the same protocol and malformed-input probes from another machine
- test both direct mode and nginx-stream proxied mode
- verify that only the intended port is reachable and that the daemon exposes no service banner

Recommended fuzz-style smoke pass:

- send randomized ASCII and mixed control-byte payloads over many short-lived TCP connections
- confirm the daemon stays up, does not panic, and does not produce unexpected output classes
- repeat with `cgi_enable = no` and `cgi_enable = yes`

## Security Summary

The implementation must assume hostile input and hostile network peers.

Mandatory safeguards:

- validate request syntax before lookup
- bound request length
- do not rewrite invalid targets into valid ones
- no raw path usage from client input
- no outbound relay unless `relay_enable = yes`
- when relay is enabled, no DNS resolution and no non-public relay hops
- no shell execution
- no request-derived argv or env for CGI
- only one sanitized canonical request line on CGI stdin, and only when a CGI target is actually selected
- no symlink following
- no serving or executing non-regular files
- keep all static and CGI resolution inside the effective content root for the active listener
- sanitize log data
- normalize or reject unsafe output bytes
- emit no banner, version, or service-identifying preface
- use strict timeouts
- close connections immediately after reply

## Open Implementation Decisions

No material protocol or deployment decisions remain open in this specification. Implementation details and test harness structure remain to be written.
