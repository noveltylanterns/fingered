# fingered

`fingered` is a small Finger daemon written in Go by GPT-5.4, and shipped as a self-contained Linux binary. The `finger://` protocol is a simple concept, so reimplementing software around it is trivial for a machine.

The real question is: Can the machine produce a `finger://` utility that won't fall apart? How much time will be required to debug and pentest it? Will the code be maintainable in 6 months? These are the questions we need answers for.


## Features

- Traditional `finger://` listener
- Experimental TLS [fingers://](https://github.com/noveltylanterns/finger) listener
- Strict request validation
- Serve finger:// content with a simple folder of .txt files.
- Serve dynamic finger:// content with CGI scripts.
- Optional CGI-capable header & footer templates.
- Optional access and error logging.
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
