# 运营维护与故障排查

本指南涵盖 **mita** 服务器和 **mieru** 客户端的监控、诊断和常见问题处理。

- [查看活跃连接](#查看活跃连接)
- [检查连通性](#检查连通性)
- [查看日志](#查看日志)
- [打开和关闭调试日志](#打开和关闭调试日志)
- [常见问题速查](#常见问题速查)
  - [完全无法连接](#症状完全无法连接)
  - [连接一段时间后断开](#症状连接一段时间后断开)
  - [速度非常慢](#症状速度非常慢)
  - [客户端无法启动](#症状客户端无法启动)
- [配置文件存放位置](#配置文件存放位置)
- [环境变量](#环境变量)
- [用户流量与配额](#用户流量与配额)
- [重置服务器指标](#重置服务器指标)
- [其他设置](#其他设置)

---

## 查看活跃连接

### 客户端

```sh
mieru get connections
```

示例输出：

```
SessionID    Protocol  Local        Remote              State        RecvQ+Buf  SendQ+Buf  LastRecv  LastSend
3078661580   UDP       [::]:34453   12.34.123.45:5852   ESTABLISHED  0+0        0+0        0s (31)   0s (28)
3408448183   UDP       [::]:34453   12.34.123.45:5852   ESTABLISHED  0+0        0+0        3s (22)   3s (21)
```

### 服务器

```sh
mita get connections
```

显示所有客户端的当前连接。

---

## 检查连通性

最可靠的端到端验证方式是查看客户端指标。

```sh
mieru get metrics
```

示例（节选）：

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

**关键指标：**

| 字段 | 健康值 | 含义 |
|------|--------|------|
| `connections` → `CurrEstablished` | > 0 | 当前客户端与服务器之间至少有一条活跃连接。 |
| `cipher - client` → `DirectDecrypt` | > 0 | 客户端已成功解密服务器返回的数据包。 |
| `cipher - client` → `FailedDirectDecrypt` | 0 | 无解密失败（时间同步正常，密码匹配）。 |
| `underlay` → `UnsolicitedUDP` | 0 | 无异常 UDP 包（无重放/探测攻击）。 |

如果 `CurrEstablished` 为 0 但 `DirectDecrypt` > 0，说明连接曾建立但当前处于空闲状态。

如果 `FailedDirectDecrypt` > 0，最可能的原因：

1. **时钟偏差** — 客户端与服务器系统时间相差超过几分钟。请在两端均启用 NTP。
2. **凭据错误** — 用户名或密码在客户端与服务器之间不匹配。
3. **端口/协议错误** — 客户端连接的端口或协议并非服务器正在监听的。

---

## 查看日志

### 服务器 (mita)

```sh
sudo journalctl -u mita -xe --no-pager
```

### 客户端 (mieru)

| 操作系统 | 日志目录 | 示例 |
|----------|----------|------|
| Linux | `$HOME/.cache/mieru/` 或 `$XDG_CACHE_HOME/mieru/` | `/home/enfein/.cache/mieru/` |
| macOS | `$HOME/Library/Caches/mieru/` | `/Users/enfein/Library/Caches/mieru/` |
| Windows | `%USERPROFILE%\AppData\Local\mieru\` | `C:\Users\enfein\AppData\Local\mieru\` |

日志文件命名格式：`yyyyMMdd_HHmm_PID.log`。每次重启 mieru 会生成新文件。日志数量过多时，旧文件会被自动清理。

---

## 打开和关闭调试日志

默认情况下，mieru / mita 打印的日志非常少，且不包含 IP 地址、端口号等敏感信息。如需诊断单个网络连接，请打开调试日志。

仓库在 `configs/templates/` 目录下提供了模板文件。

**服务器：**

```sh
# 启用
mita apply config server_enable_debug_logging.json
mita reload   # 不会中断流量

# 关闭
mita apply config server_disable_debug_logging.json
mita reload
```

**客户端：**

```sh
# 启用
mieru apply config client_enable_debug_logging.json
mieru stop
mieru start

# 关闭
mieru apply config client_disable_debug_logging.json
mieru stop
mieru start
```

所有支持的日志级别：`FATAL`、`ERROR`、`WARN`、`INFO`、`DEBUG`、`TRACE`。

---

## 常见问题速查

### 症状：完全无法连接

| 步骤 | 命令/操作 | 检查内容 |
|------|-----------|----------|
| 1 | `ping <server_ip>` | 确认基础网络可达性。 |
| 2 | `mita status`（服务器端） | 确认代理服务处于 `RUNNING` 状态。 |
| 3 | `mieru status`（客户端） | 确认客户端已启动。 |
| 4 | `mieru describe config` 与 `mita describe config` | 对比**端口**、**协议**、**用户名**和**密码**，必须完全一致。 |
| 5 | `date`（两端均执行） | 确认系统时间偏差在几分钟以内。 |
| 6 | `mieru get metrics` | 检查 `FailedDirectDecrypt`。若 > 0，请参阅[检查连通性](#检查连通性)。 |
| 7 | 在两端启用调试日志 | 查找 `handshake`、`cipher` 或 `underlay` 相关错误。 |

### 症状：连接一段时间后断开

| 可能原因 | 如何检查 | 解决方法 |
|----------|----------|----------|
| 服务器防火墙关闭了空闲端口 | `mita get connections` → 连接数降为 0 | 使用更大的端口范围，或在应用层启用 keepalive。 |
| 客户端或服务器重启 | 检查日志时间戳 | 服务器端确保执行 `systemctl enable mita`；客户端配置[开机自启](./client-install.zh_CN.md#开机自启)。 |
| GFW 限速/QoS | 仅在高峰时段降速 | 改用 UDP 协议或启用流量混淆。 |

### 症状：速度非常慢

| 可能原因 | 如何检查 | 解决方法 |
|----------|----------|----------|
| TCP 拥塞控制未使用 BBR | `sysctl net.ipv4.tcp_congestion_control` | 运行 [BBR 脚本](./server-install.zh_CN.md#bbr-拥塞控制算法)。 |
| MTU 不匹配导致分片 | `ping -M do -s 1372 <server>`（Linux） | 在两端将 `mtu` 设为 1280 后再次测试。 |
| 多路复用过于激进 | `mieru get metrics` → `MaxConn` 非常高 | 将 multiplexing 降为 `MULTIPLEXING_LOW` 或 `MULTIPLEXING_MIDDLE`。 |
| 服务器过载（用户过多） | `mita get users` | 增加服务器数量，或为单个用户设置流量配额。 |

### 症状：客户端无法启动

| 可能原因 | 如何检查 | 解决方法 |
|----------|----------|----------|
| 端口已被占用 | `lsof -i :1080`（macOS/Linux）或 `netstat -ano | findstr 1080`（Windows） | 更换 `socks5Port` 或 `rpcPort` 为未占用的端口。 |
| 配置文件 JSON 语法错误 | `mieru apply config` 打印错误 | 修正 JSON 语法后重新应用。 |
| 权限不足（Linux） | 客户端二进制文件无执行权限 | `chmod +x mieru` 或通过软件包安装。 |

---

## 配置文件存放位置

### 服务器

| 文件 | 路径 | 格式 |
|------|------|------|
| 配置 | `/etc/mita/server.conf.pb` | Protocol buffer（二进制） |

服务器**不**存储密码明文，仅保存校验码。

### 客户端

| 操作系统 | 配置路径 | 示例 |
|----------|----------|------|
| Linux | `$HOME/.config/mieru/client.conf.pb` | `/home/enfein/.config/mieru/client.conf.pb` |
| macOS | `$HOME/Library/Application Support/mieru/client.conf.pb` | `/Users/enfein/Library/Application Support/mieru/client.conf.pb` |
| Windows | `%USERPROFILE%\AppData\Roaming\mieru\client.conf.pb` | `C:\Users\enfein\AppData\Roaming\mieru\client.conf.pb` |

---

## 环境变量

| 变量 | 用途 |
|------|------|
| `MITA_CONFIG_JSON_FILE` | 从该路径加载 JSON 格式服务器配置文件。 |
| `MITA_CONFIG_FILE` | 从该路径加载 protobuf 格式服务器配置文件。 |
| `MIERU_CONFIG_JSON_FILE` | 从该路径加载 JSON 格式客户端配置文件。 |
| `MIERU_CONFIG_FILE` | 从该路径加载 protobuf 格式客户端配置文件。 |
| `MITA_LOG_NO_TIMESTAMP` | 非空时，服务器日志不打印时间戳（配合 journald 使用）。 |
| `MITA_UDS_PATH` | UNIX domain socket 路径。默认值：`/var/run/mita/mita.sock`。 |
| `MITA_INSECURE_UDS` | 非空时，跳过 UDS 文件权限强制检查。适用于受限系统。 |

**示例：使用自定义配置文件在前台运行客户端**

```sh
MIERU_CONFIG_JSON_FILE=/etc/mieru_client_config.json mieru run
```

日志直接输出到终端。按 `Ctrl+C` 退出。

---

## 用户流量与配额

### 查看各用户使用情况

```sh
mita get users
```

```
User  LastActive            1DayDownload  1DayUpload  30DaysDownload  30DaysUpload
abcd  2025-04-23T01:02:03Z  938.1MiB      12.9MiB     4.0GiB          31.8MiB
```

### 查看配额状态

```sh
mita get quotas
```

```
User  Days  Limit    Usage
abcd  1     10.0GiB  951.1MiB
abcd  7     40.0GiB  4.0GiB
```

---

## 重置服务器指标

服务器指标存储在 `/var/lib/mita/metrics.pb` 中，重启后依然保留。如需清零：

```sh
sudo systemctl stop mita
sudo rm -f /var/lib/mita/metrics.pb
sudo systemctl start mita
```

---

## 其他设置

### 修改指标日志输出间隔

默认每隔 10 分钟在日志中打印一次指标。如需调整间隔：

```json
{
    "advancedSettings": {
        "metricsLoggingInterval": "1h30m"
    }
}
```

有效单位：`s`（秒）、`m`（分钟）、`h`（小时），以及它们的组合。

### 关闭客户端自动检查更新

```json
{
    "advancedSettings": {
        "noCheckUpdate": true
    }
}
```
