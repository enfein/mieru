# Server Installation & Configuration

This guide covers the installation, configuration, and operation of **mita**, the proxy server component of the mieru proxy suite.

**mita** must run on Linux. This document provides three deployment paths:

1. **Docker** (recommended for most users) — fastest to deploy and easiest to manage.
2. **Package installation** — native Debian/RPM packages with systemd integration.
3. **Installation script** — automated setup for common distributions.

- [Prerequisites](#prerequisites)
- [One-Click Installation](#one-click-installation-recommended-for-systemd-systems)
- [Docker Deployment](#docker-deployment)
  - [Quick Start with Docker](#quick-start-with-docker)
  - [Docker Compose](#docker-compose)
  - [Managing the Container](#managing-the-container)
- [Package Installation](#package-installation)
  - [Download](#download)
  - [Install](#install)
  - [Permissions & Daemon Status](#permissions--daemon-status)
- [Configure the Proxy Server](#configure-the-proxy-server)
- [Start, Stop, and Reload](#start-stop-and-reload)
- [Advanced Settings](#advanced-settings)
  - [BBR Congestion Control](#bbr-congestion-control-algorithm)
  - [Outbound Proxy (Proxy Chain)](#configuring-outbound-proxy)
  - [DNS Policy](#dns-policy)
  - [Allow Private / Loopback Access](#allow-users-to-access-internal-network)
  - [Traffic Quotas](#limiting-user-traffic)
  - [Mandatory User Hint](#user-hint)
- [Time Synchronization](#optional-install-ntp-network-time-synchronization-service)

---

## Prerequisites

- A Linux server with a public IPv4 or IPv6 address.
- Root or `sudo` access.
- Firewall rules that allow inbound TCP and/or UDP traffic on the port(s) you intend to use (default range: 1025–65535).
- Accurate system time (see [NTP section](#optional-install-ntp-network-time-synchronization-service)).

> **Note on time synchronization**
> The client and server derive encryption keys from the username, password, and current system time. If the clocks drift apart, the server will be unable to decrypt client traffic. Enabling NTP on both sides is strongly recommended.

---

## One-Click Installation (Recommended for Systemd Systems)

For Debian / Ubuntu and RHEL / CentOS / Rocky Linux systems, the easiest way to install mita is the official one-click script. It automatically detects your architecture and OS, downloads the latest release from GitHub, installs the package, runs an interactive configuration wizard, configures the firewall, enables auto-start, and prints connection info.

```sh
curl -fsSL https://raw.githubusercontent.com/enfein/mieru/main/tools/install.sh | sudo bash -s -- install
```

Or download the script first and run interactively:

```sh
curl -fsSL -o install-mita.sh https://raw.githubusercontent.com/enfein/mieru/main/tools/install.sh
chmod +x install-mita.sh
sudo ./install-mita.sh
```

### Script commands

| Command | Description |
|---------|-------------|
| `install` | Install latest mita and run interactive configuration |
| `update` | Update to the latest release, preserving existing config |
| `uninstall` | Remove mita and optionally delete configuration |
| `reconfigure` | Rewrite `server_config.json` interactively and restart |
| `start` / `stop` / `restart` | Control the proxy service |
| `enable` / `disable` | Toggle systemd auto-start |
| `status` | Show daemon and proxy status |
| `info` | Display server IP, ports, users, and `mierus://` sharing links |
| `bbr` | Enable BBR congestion control (idempotent) |

---

## Docker Deployment

The official Docker image is published to the GitHub Container Registry and is the quickest way to get mita running.

**Registry:** `ghcr.io/enfein/mita:latest`  
**Source:** [deployments/docker/mita/Dockerfile](../deployments/docker/mita/Dockerfile)

### Quick Start with Docker

```sh
# 1. Create a local directory for the server config
mkdir -p ~/mita-config

# 2. Create server_config.json (see Configuration section below)
#    Example: nano ~/mita-config/server_config.json

# 3. Pull and run
sudo docker run -d \
  --name mita \
  --restart unless-stopped \
  -p 2012-2022:2012-2022/tcp \
  -p 2027:2027/tcp \
  -v ~/mita-config:/etc/mita:Z \
  ghcr.io/enfein/mita:latest \
  mita run
```

Then apply your configuration inside the running container:

```sh
sudo docker exec -it mita mita apply config /etc/mita/server_config.json
sudo docker exec -it mita mita start
sudo docker exec -it mita mita status
```

### Docker Compose

For production or long-term deployments, use Docker Compose.

Create `docker-compose.yml`:

```yaml
services:
  mita:
    image: ghcr.io/enfein/mita:latest
    container_name: mita
    restart: unless-stopped
    ports:
      - "2012-2022:2012-2022/tcp"
      - "2027:2027/tcp"
      # Add UDP if needed, e.g.:
      # - "3000-3010:3000-3010/udp"
    volumes:
      - ./mita-config:/etc/mita:Z
    command: mita run
```

Create `mita-config/server_config.json` (see [Configure the Proxy Server](#configure-the-proxy-server) for the full schema).

Start the stack:

```sh
docker compose up -d
```

Apply configuration and start the proxy service:

```sh
docker compose exec mita mita apply config /etc/mita/server_config.json
docker compose exec mita mita start
docker compose exec mita mita status
```

### Managing the Container

| Task | Command |
|------|---------|
| View logs | `docker logs --tail 100 -f mita` |
| Restart service | `docker restart mita` |
| Stop service | `docker exec mita mita stop` |
| Shell inside container | `docker exec -it mita sh` |
| Upgrade image | `docker pull ghcr.io/enfein/mita:latest && docker compose up -d` |

> **Tip:** Because the container runs `mita run` (foreground mode) rather than a systemd unit, `docker restart` is sufficient to restart the daemon. Configuration changes that require a restart can be applied with `docker compose exec mita mita stop` followed by the container restart.

---

## Package Installation

For users who prefer native packages with systemd integration.

### Download

The commands below automatically fetch the latest release tag from GitHub and download the corresponding package. Run the `TAG=...` line once, then copy the block for your platform.

```sh
# Fetch the latest release tag, e.g. v3.32.0
TAG=$(curl -s https://api.github.com/repos/enfein/mieru/releases/latest | grep '"tag_name":' | head -1 | sed -E 's/.*"([^"]+)".*/\1/')
VER="${TAG#v}"

# Debian / Ubuntu - x86_64
curl -LSO "https://github.com/enfein/mieru/releases/download/${TAG}/mita_${VER}_amd64.deb"
sudo dpkg -i "mita_${VER}_amd64.deb"

# Debian / Ubuntu - ARM 64
curl -LSO "https://github.com/enfein/mieru/releases/download/${TAG}/mita_${VER}_arm64.deb"
sudo dpkg -i "mita_${VER}_arm64.deb"

# Red Hat / CentOS / Rocky Linux - x86_64
curl -LSO "https://github.com/enfein/mieru/releases/download/${TAG}/mita-${VER}-1.x86_64.rpm"
sudo rpm -Uvh --force "mita-${VER}-1.x86_64.rpm"

# Red Hat / CentOS / Rocky Linux - ARM 64
curl -LSO "https://github.com/enfein/mieru/releases/download/${TAG}/mita-${VER}-1.aarch64.rpm"
sudo rpm -Uvh --force "mita-${VER}-1.aarch64.rpm"
```

> **Tip:** If `curl` is unavailable, run `TAG=$(wget -qO- https://api.github.com/repos/enfein/mieru/releases/latest | grep '"tag_name":' | head -1 | sed -E 's/.*"([^"]+)".*/\1/')` instead.

The same commands are used to upgrade an existing installation.

### Permissions & Daemon Status

```sh
# Debian / Ubuntu - x86_64
sudo dpkg -i mita_X.Y.Z_amd64.deb

# Debian / Ubuntu - ARM 64
sudo dpkg -i mita_X.Y.Z_arm64.deb

# Red Hat / CentOS / Rocky Linux - x86_64
sudo rpm -Uvh --force mita-X.Y.Z-1.x86_64.rpm

# Red Hat / CentOS / Rocky Linux - ARM 64
sudo rpm -Uvh --force mita-X.Y.Z-1.aarch64.rpm
```

The same commands are used to upgrade an existing installation.

### Permissions & Daemon Status

Add your user to the `mita` group so you can run `mita` commands without `sudo`:

```sh
sudo usermod -a -G mita $USER
# Log out and back in for the group change to take effect
exit
```

After reconnecting via SSH, verify the daemon:

```sh
systemctl status mita
```

If the output contains `active (running)`, the mita systemd service is operational and will start automatically on boot.

Check the mita application status:

```sh
mita status
```

A fresh installation will report `mita server status is "IDLE"`, indicating the server is not yet listening for client connections.

---

## Configure the Proxy Server

mieru supports two transport protocols: **TCP** and **UDP**. See [mieru Proxy Protocol](./protocol.md) for a comparison.

Configuration is applied via:

```sh
mita apply config <FILE>
```

`<FILE>` is a JSON file. Its contents are merged into the existing server configuration; you do not need to provide a full configuration on every update.

### Minimal Example

```json
{
    "portBindings": [
        {
            "portRange": "2012-2022",
            "protocol": "TCP"
        },
        {
            "port": 2027,
            "protocol": "TCP"
        }
    ],
    "users": [
        {
            "name": "ducaiguozei",
            "password": "xijinping"
        },
        {
            "name": "meiyougongchandang",
            "password": "caiyouxinzhongguo"
        }
    ],
    "loggingLevel": "INFO",
    "mtu": 1400
}
```

**Field reference:**

| Field | Description |
|-------|-------------|
| `portBindings` | List of ports or port ranges to listen on. Each entry needs a `protocol` (`TCP` or `UDP`). Use `port` (integer, 1025–65535) or `portRange` (string, e.g. `"2012-2022"`). |
| `users` | List of credential objects. `name` and `password` are required for each user. |
| `loggingLevel` | Log verbosity: `DEBUG`, `INFO`, `WARN`, or `ERROR`. Default: `INFO`. |
| `mtu` | *(Optional)* Maximum transport-unit payload when using UDP. Default: `1400`. Minimum: `1280`. |

> **Security note:** Ensure your host firewall (e.g., `iptables`, `nftables`, `ufw`, or cloud-provider security groups) allows inbound traffic on the configured ports.

After editing the file, apply it:

```sh
mita apply config server_config.json
```

If validation fails, mita prints the specific error. Fix the file and re-run the command.

Review the current effective configuration with:

```sh
mita describe config
```

---

## Start, Stop, and Reload

### Start the proxy service

```sh
mita start
```

mita will begin listening on the configured ports. Verify with:

```sh
mita status
```

If the output is `mita server status is "RUNNING"`, the server is ready to accept client connections.

### Stop the proxy service

```sh
mita stop
```

### Reload settings without dropping connections

```sh
mita reload
```

`reload` is supported **only** for changes to the `users` or `loggingLevel` fields. For any other configuration change, stop and start the service:

```sh
mita stop
mita start
```

After the server is running, proceed to [Client Installation & Configuration](./client-install.md).

---

## Advanced Settings

### BBR Congestion Control Algorithm

[BBR](https://en.wikipedia.org/wiki/TCP_congestion_control#TCP_BBR) is a congestion-control algorithm that improves throughput on lossy networks. mieru’s UDP protocol already uses BBR; the script below enables it for TCP as well.

```sh
curl -fSsLO https://raw.githubusercontent.com/enfein/mieru/refs/heads/main/tools/enable_tcp_bbr.py
chmod +x enable_tcp_bbr.py
sudo python3 enable_tcp_bbr.py
```

This can be run on both the server and the client.

### Configuring Outbound Proxy

Outbound proxy (proxy chain) lets mita forward traffic through an upstream SOCKS5 proxy.

Example topology:

```
mieru client -> GFW -> mita server -> cloudflare proxy client -> cloudflare CDN -> target website
```

With this setup, the target website sees the Cloudflare CDN IP, not the mita server IP.

Example configuration:

```json
{
    "egress": {
        "proxies": [
            {
                "name": "cloudflare",
                "protocol": "SOCKS5_PROXY_PROTOCOL",
                "host": "127.0.0.1",
                "port": 4000,
                "socks5Authentication": {
                    "user": "shilishanlu",
                    "password": "buhuanjian"
                }
            }
        ],
        "rules": [
            {
                "ipRanges": ["8.8.4.4/32", "8.8.8.8/32"],
                "action": "REJECT"
            },
            {
                "domainNames": ["chatgpt.com", "grok.com"],
                "action": "PROXY",
                "proxyNames": ["cloudflare"]
            },
            {
                "ipRanges": ["*"],
                "domainNames": ["*"],
                "action": "DIRECT"
            }
        ]
    }
}
```

**Field reference:**

| Field | Description |
|-------|-------------|
| `egress.proxies` | Upstream SOCKS5 proxies. `protocol` must be `SOCKS5_PROXY_PROTOCOL`. Omit `socks5Authentication` if the proxy does not require credentials. |
| `egress.rules` | Ordered list of rules. The **first** matching rule is executed. Wildcard `"*"` matches all IPs or domains. Actions: `DIRECT`, `PROXY`, `REJECT`. `PROXY` requires `proxyNames` referencing a defined proxy. Default action: `DIRECT`. |

To disable outbound proxy, set `egress` to `{}`.

> **Proxy chain vs. nested proxy:** Proxy chain routes traffic through an intermediate proxy *after* mita. A nested proxy routes traffic through another proxy *before* mieru (e.g., Tor Browser -> mieru client -> GFW -> mita server -> Tor network). For nested proxy setup, see the [Security Guide](./security.md).

### DNS Policy

When the client requests a destination by domain name, mita resolves it before connecting. You can customize resolution behavior:

```json
{
    "dns": {
        "dualStack": "USE_FIRST_IP",
        "hosts": {
            "example.com": "93.184.216.34",
            "ipv6.example.com": "2606:2800:220:1:248:1893:25c8:1946"
        }
    }
}
```

`dualStack` options:

| Value | Behavior |
|-------|----------|
| `USE_FIRST_IP` | Use the first IP returned by the DNS resolver. (Default) |
| `PREFER_IPv4` | Prefer the first IPv4 address; fall back to IPv6 if absent. |
| `PREFER_IPv6` | Prefer the first IPv6 address; fall back to IPv4 if absent. |
| `ONLY_IPv4` | Force IPv4; fail if none is returned. |
| `ONLY_IPv6` | Force IPv6; fail if none is returned. |

`hosts` defines static name-to-IP mappings, similar to `/etc/hosts`. Matching is exact and case-insensitive; wildcards and suffix matching are not supported. Keys must not begin or end with a dot.

### Allow Users to Access Internal Network

By default, mita rejects requests to private and loopback addresses.

To allow a specific user to reach private IPs (e.g., `192.168.1.100`), set `allowPrivateIP: true`.  
To allow a specific user to reach the server itself (e.g., `127.0.0.1`), set `allowLoopbackIP: true`.

```json
{
    "users": [
        {
            "name": "ducaiguozei",
            "password": "xijinping",
            "allowPrivateIP": true,
            "allowLoopbackIP": true
        },
        {
            "name": "meiyougongchandang",
            "password": "caiyouxinzhongguo"
        }
    ]
}
```

### Limiting User Traffic

Use `quotas` to enforce traffic limits per user.

Example: user `ducaiguozei` may consume at most 1 GB per day and 10 GB per 30 days.

```json
{
    "users": [
        {
            "name": "ducaiguozei",
            "password": "xijinping",
            "quotas": [
                { "days": 1,  "megabytes": 1024 },
                { "days": 30, "megabytes": 10240 }
            ]
        },
        {
            "name": "meiyougongchandang",
            "password": "caiyouxinzhongguo"
        }
    ]
}
```

### User Hint

Since v3.31.0, the mieru client sends a *user hint* that accelerates packet decryption on the server. This is especially beneficial when serving many users.

If you want to block older clients that do not send the hint (to reduce server CPU load), apply:

```json
{
    "advancedSettings": {
        "userHintIsMandatory": true
    }
}
```

---

## [Optional] Install NTP Network Time Synchronization Service

The client and server derive encryption keys from the username, password, and system time. If their clocks diverge significantly, the server cannot decrypt traffic. We strongly recommend enabling NTP on both sides.

If your system uses `systemd-timesyncd`, edit `/etc/systemd/timesyncd.conf`:

```ini
[Time]
NTP=time.google.com
```

Then restart the service:

```sh
sudo systemctl restart systemd-timesyncd
```

Otherwise, install a standalone NTP package:

```sh
# Debian / Ubuntu
sudo apt-get install ntp

# Red Hat / CentOS / Rocky Linux
sudo dnf install ntp
```
