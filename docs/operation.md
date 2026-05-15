# Maintenance & Troubleshooting

This guide covers monitoring, diagnostics, and common issues for both the **mita** server and **mieru** client.

- [View Active Connections](#view-active-connections)
- [Check Connectivity](#check-connectivity)
- [View Logs](#view-logs)
- [Enable / Disable Debug Logging](#enable--disable-debug-logging)
- [Common Issues — Quick Reference](#common-issues--quick-reference)
  - [Cannot connect at all](#symptom-cannot-connect-at-all)
  - [Connection drops after a while](#symptom-connection-drops-after-a-while)
  - [Very slow speed](#symptom-very-slow-speed)
  - [Client fails to start](#symptom-client-fails-to-start)
- [Configuration File Locations](#configuration-file-locations)
- [Environment Variables](#environment-variables)
- [User Traffic & Quotas](#user-traffic--quotas)
- [Reset Server Metrics](#reset-server-metrics)
- [Other Settings](#other-settings)

---

## View Active Connections

### On the client

```sh
mieru get connections
```

Example output:

```
SessionID    Protocol  Local        Remote              State        RecvQ+Buf  SendQ+Buf  LastRecv  LastSend
3078661580   UDP       [::]:34453   12.34.123.45:5852   ESTABLISHED  0+0        0+0        0s (31)   0s (28)
3408448183   UDP       [::]:34453   12.34.123.45:5852   ESTABLISHED  0+0        0+0        3s (22)   3s (21)
```

### On the server

```sh
mita get connections
```

Shows all current connections from every client.

---

## Check Connectivity

The most reliable way to verify end-to-end connectivity is to check client metrics.

```sh
mieru get metrics
```

Example (trimmed):

```json
{
    "cipher - client": {
        "DirectDecrypt": 64540,
        "FailedDirectDecrypt": 0
    },
    "connections": {
        "ActiveOpens": 130,
        "CurrEstablished": 2,
        "MaxConn": 35,
        "PassiveOpens": 0
    }
}
```

**Key indicators:**

| Field | Healthy value | Meaning |
|-------|---------------|---------|
| `connections` → `CurrEstablished` | > 0 | There is at least one active connection between client and server right now. |
| `cipher - client` → `DirectDecrypt` | > 0 | The client has successfully decrypted server response packets. |
| `cipher - client` → `FailedDirectDecrypt` | 0 | No decryption failures (time sync or password mismatch). |
| `underlay` → `UnsolicitedUDP` | 0 | No unexpected UDP packets (possible replay / probe). |

If `CurrEstablished` is 0 but `DirectDecrypt` is > 0, the connection was established earlier but may have gone idle.

If `FailedDirectDecrypt` is > 0, the most likely causes are:

1. **Clock skew** — Client and server system times differ by more than a few minutes. Enable NTP on both sides.
2. **Wrong credentials** — Username or password does not match between client and server.
3. **Wrong port / protocol** — Client is connecting to a port or protocol the server is not listening on.

---

## View Logs

### Server (mita)

```sh
sudo journalctl -u mita -xe --no-pager
```

### Client (mieru)

| OS | Log directory | Example |
|----|---------------|---------|
| Linux | `$HOME/.cache/mieru/` or `$XDG_CACHE_HOME/mieru/` | `/home/enfein/.cache/mieru/` |
| macOS | `$HOME/Library/Caches/mieru/` | `/Users/enfein/Library/Caches/mieru/` |
| Windows | `%USERPROFILE%\AppData\Local\mieru\` | `C:\Users\enfein\AppData\Local\mieru\` |

Log file naming: `yyyyMMdd_HHmm_PID.log`. A new file is created every time mieru restarts. Old files are deleted automatically when the directory grows too large.

---

## Enable / Disable Debug Logging

By default, mieru / mita log very little and never include IP addresses or port numbers. To diagnose a single network connection, enable debug logging.

The repository provides template files in `configs/templates/`.

**Server:**

```sh
# Enable
mita apply config server_enable_debug_logging.json
mita reload   # does NOT interrupt traffic

# Disable
mita apply config server_disable_debug_logging.json
mita reload
```

**Client:**

```sh
# Enable
mieru apply config client_enable_debug_logging.json
mieru stop
mieru start

# Disable
mieru apply config client_disable_debug_logging.json
mieru stop
mieru start
```

All supported levels: `FATAL`, `ERROR`, `WARN`, `INFO`, `DEBUG`, `TRACE`.

---

## Common Issues — Quick Reference

### Symptom: Cannot connect at all

| Step | Command / Action | What to check |
|------|------------------|---------------|
| 1 | `ping <server_ip>` | Verify basic network reachability. |
| 2 | `mita status` (on server) | Ensure the proxy service is `RUNNING`. |
| 3 | `mieru status` (on client) | Ensure the client is started. |
| 4 | `mieru describe config` vs `mita describe config` | Compare **port**, **protocol**, **username**, and **password**. They must match exactly. |
| 5 | `date` (on both sides) | Verify system times are within a few minutes of each other. |
| 6 | `mieru get metrics` | Check `FailedDirectDecrypt`. If > 0, see [Check Connectivity](#check-connectivity). |
| 7 | Enable debug logs on both sides | Look for `handshake`, `cipher`, or `underlay` errors. |

### Symptom: Connection drops after a while

| Possible cause | How to check | Fix |
|----------------|--------------|-----|
| Server firewall closed the idle port | `mita get connections` → count drops to 0 | Use a larger port range or enable keepalive at the application level. |
| Client or server restarted | Check log timestamps | Ensure `systemctl enable mita` on the server; configure [client auto-start](./client-install.md#auto-start-on-boot). |
| GFW throttling / QoS | Speed drops during peak hours only | Switch to UDP protocol or enable traffic pattern. |

### Symptom: Very slow speed

| Possible cause | How to check | Fix |
|----------------|--------------|-----|
| TCP congestion control not using BBR | `sysctl net.ipv4.tcp_congestion_control` | Run the [BBR script](./server-install.md#bbr-congestion-control-algorithm). |
| MTU mismatch causing fragmentation | `ping -M do -s 1372 <server>` (Linux) | Set `mtu` to 1280 on both sides and test again. |
| Multiplexing too aggressive | `mieru get metrics` → `MaxConn` is very high | Lower multiplexing to `MULTIPLEXING_LOW` or `MULTIPLEXING_MIDDLE`. |
| Server overloaded (many users) | `mita get users` | Consider adding more servers or limiting per-user quotas. |

### Symptom: Client fails to start

| Possible cause | How to check | Fix |
|----------------|--------------|-----|
| Port already in use | `lsof -i :1080` (macOS/Linux) or `netstat -ano \| findstr 1080` (Windows) | Change `socks5Port` or `rpcPort` to an unused port. |
| Invalid JSON in config | `mieru apply config` prints an error | Fix the JSON syntax and re-apply. |
| Permission denied (Linux) | Client binary not executable | `chmod +x mieru` or install via package. |

---

## Configuration File Locations

### Server

| File | Path | Format |
|------|------|--------|
| Config | `/etc/mita/server.conf.pb` | Protocol buffer (binary) |

The server does **not** store plaintext passwords; only a checksum is kept.

### Client

| OS | Config path | Example |
|----|-------------|---------|
| Linux | `$HOME/.config/mieru/client.conf.pb` | `/home/enfein/.config/mieru/client.conf.pb` |
| macOS | `$HOME/Library/Application Support/mieru/client.conf.pb` | `/Users/enfein/Library/Application Support/mieru/client.conf.pb` |
| Windows | `%USERPROFILE%\AppData\Roaming\mieru\client.conf.pb` | `C:\Users\enfein\AppData\Roaming\mieru\client.conf.pb` |

---

## Environment Variables

| Variable | Purpose |
|----------|---------|
| `MITA_CONFIG_JSON_FILE` | Load JSON server config from this path. |
| `MITA_CONFIG_FILE` | Load protobuf server config from this path. |
| `MIERU_CONFIG_JSON_FILE` | Load JSON client config from this path. |
| `MIERU_CONFIG_FILE` | Load protobuf client config from this path. |
| `MITA_LOG_NO_TIMESTAMP` | If non-empty, server log omits timestamps (useful with journald). |
| `MITA_UDS_PATH` | UNIX domain socket path. Default: `/var/run/mita/mita.sock`. |
| `MITA_INSECURE_UDS` | If non-empty, skip permission enforcement on the UDS file. For restricted systems. |

**Example: run client in foreground with a custom config file**

```sh
MIERU_CONFIG_JSON_FILE=/etc/mieru_client_config.json mieru run
```

Logs go to the terminal. Press `Ctrl+C` to exit.

---

## User Traffic & Quotas

### View per-user usage

```sh
mita get users
```

```
User  LastActive            1DayDownload  1DayUpload  30DaysDownload  30DaysUpload
abcd  2025-04-23T01:02:03Z  938.1MiB      12.9MiB     4.0GiB          31.8MiB
```

### View quota status

```sh
mita get quotas
```

```
User  Days  Limit    Usage
abcd  1     10.0GiB  951.1MiB
abcd  7     40.0GiB  4.0GiB
```

---

## Reset Server Metrics

Server metrics are persisted in `/var/lib/mita/metrics.pb` and survive restarts. To clear them:

```sh
sudo systemctl stop mita
sudo rm -f /var/lib/mita/metrics.pb
sudo systemctl start mita
```

---

## Other Settings

### Metrics logging interval

By default, metrics are printed in logs every 10 minutes. To change the interval:

```json
{
    "advancedSettings": {
        "metricsLoggingInterval": "1h30m"
    }
}
```

Valid units: `s` (seconds), `m` (minutes), `h` (hours), and combinations.

### Disable client auto-update check

```json
{
    "advancedSettings": {
        "noCheckUpdate": true
    }
}
```
