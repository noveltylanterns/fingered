# fingered

`fingered` is a small Finger daemon written in Go by GPT-5.4, and shipped as a self-contained Linux binary. Existing daemons for `finger://` are notoriously insecure & essentially abandonware. But the protocol itself is a simple platform-agnostic concept, so reimplementing new software around it is trivial for a machine.

The real question is: Can the machine produce a `finger://` utility that won't fall apart? How much time will be required to debug and pentest it? Will the code be maintainable in 6 months? These are the questions we need answers for.


## Features

- Traditional `finger://` listener
- Modern network-only `finger` client *(no local data exfiltration!)*
- Experimental TLS [fingers://](https://github.com/noveltylanterns/fingers) listener
- Strict request validation
- Serve `finger://` content with a simple folder of .txt files.
- Serve dynamic `finger://` content with CGI scripts.
- Optional CGI-capable header & footer templates.
- Optional access and error logging.
- PROXY protocol support for nginx stream deployments


## Quick Start

Run the installer as root:

```bash
sudo ./bin/install_fingered.sh
```

That installs the default `amd64` build to `/usr/local/sbin/fingered`, installs `/etc/fingered/fingered.conf`, creates the `finger` and `fingered` users, and installs the systemd unit unless `--nosysd` is used.
It also installs the bundled network-only client as `/usr/local/bin/finger`.

All packaged architectures:

```bash
sudo ./bin/install_fingered.sh --arch 386
sudo ./bin/install_fingered.sh --arch amd64
sudo ./bin/install_fingered.sh --arch arm64 (UNTESTED)
sudo ./bin/install_fingered.sh --arch riscv64 (UNTESTED)
```

And if you want to get rid of it:

```bash
sudo ./bin/uninstall_fingered.sh
```

That removes the installed daemon, bundled client, config, systemd unit, managed `/home/finger/` tree, and the `finger` / `fingered` accounts. If you pointed `doc_root`, `tls_doc_root`, or `log_root` at paths outside `/home/finger/`, those external paths are left alone and must be removed manually.

## After Install

The installer creates the users, config, and systemd unit, but it does not confirm that the daemon is serving content yet.

With the default config, `fingered` listens on `127.0.0.1:7979` with `proxy_protocol = no`.

1. Create a test page:

```bash
sudo sh -c 'printf "hello from fingered\n" > /home/finger/app/finger/index.txt'
sudo chown finger:finger /home/finger/app/finger/index.txt
sudo chmod 640 /home/finger/app/finger/index.txt
```

2. Start the service:

```bash
sudo systemctl start fingered
sudo systemctl status fingered --no-pager
```

3. Probe it locally:

```bash
printf '\r\n' | nc -w 2 127.0.0.1 7979
```

Expected output with the default config:

- the contents of `index.txt`
- the credits footer, because `tpl_credits = yes` by default

4. Check logs if needed:

```bash
sudo journalctl -u fingered -n 50 --no-pager
sudo tail -n 50 /home/finger/logs/fingered/error.log
```

5. Run the packaged remote-style smoke probe against the local service:

```bash
./scripts/smoke_remote.sh 127.0.0.1 7979
```

If you want request logging too, set `log_requests = yes` in `/etc/fingered/fingered.conf` and restart the service.

If you later put nginx stream in front of `fingered`, switch:

```conf
proxy_protocol = yes
trusted_proxy_ips = 127.0.0.1,::1
```


## Layout

- config: `/etc/fingered/fingered.conf`
- plaintext content: `/home/finger/app/finger/`
- TLS cert/key directory: `/etc/fingered/tls/`
- logs: `/home/finger/logs/fingered/`

Sample config: [`contrib/fingered.conf.example`](contrib/fingered.conf.example)

## Local Build

```bash
go build -o ./bin/fingered-dev ./cmd/fingered
```

Release-style binaries can be built with `CGO_ENABLED=0` for the target architecture.
