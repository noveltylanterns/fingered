# fingered

`fingered` is a small Finger daemon written in Go and shipped as a self-contained Linux binary.

The repo includes prebuilt binaries in `bin/` so users can run it locally without installing Go:

- `bin/fingered-386`
- `bin/fingered-amd64`
- `bin/fingered-arm64`
- `bin/fingered-riscv64`
- `bin/fingered-dev`

Current implementation highlights:

- plain `finger://` listener
- optional TLS `fingers://` listener
- strict request validation
- flat `doc_root` mapping to `.txt`
- optional `.cgi` fallback
- optional header/footer templates and credits footer
- optional access and error logging
- PROXY protocol support for nginx stream deployments

## Quick Start

Run the installer as root:

```bash
sudo ./bin/install_fingered.sh
```

That installs the default `amd64` build to `/usr/local/sbin/fingered`, installs `/etc/fingered/fingered.conf`, creates the `finger` and `fingered` users, and installs the systemd unit unless `--nosysd` is used.

Other packaged architectures:

```bash
sudo ./bin/install_fingered.sh --arch 386
sudo ./bin/install_fingered.sh --arch arm64
sudo ./bin/install_fingered.sh --arch riscv64
```

## Layout

- config: `/etc/fingered/fingered.conf`
- plaintext content: `/home/finger/app/public/`
- TLS cert/key directory: `/etc/fingered/tls/`
- logs: `/home/finger/logs/fingered/`

Sample config: [`contrib/fingered.conf.example`](contrib/fingered.conf.example)

## Local Build

```bash
go build -o ./bin/fingered-dev ./cmd/fingered
```

Release-style binaries can be built with `CGO_ENABLED=0` for the target architecture.

## Testing

Unit tests:

```bash
go test ./...
```

Local smoke test:

```bash
./scripts/smoke_local.sh
```

Remote smoke test:

```bash
./scripts/smoke_remote.sh <host> [port] [selector]
```

## License

`fingered` is licensed under the BSD 2-Clause License. See [`LICENSE`](LICENSE).
