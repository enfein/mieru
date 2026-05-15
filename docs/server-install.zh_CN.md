# 服务器安装与配置

本指南介绍 mieru 代理套件中服务器组件 **mita** 的安装、配置与运维。

**mita** 必须在 Linux 系统上运行。本文档提供三种部署方式：

1. **Docker 部署（推荐）** — 部署最快、管理最便捷。
2. **系统软件包安装** — 使用 Debian / RPM 软件包，集成 systemd 服务。
3. **安装脚本** — 针对常见发行版的自动化安装。

- [前置条件](#前置条件)
- [一键安装脚本](#一键安装脚本推荐用于-systemd-系统)
- [Docker 部署](#docker-部署)
  - [使用 Docker 快速启动](#使用-docker-快速启动)
  - [Docker Compose](#docker-compose)
  - [容器运维](#容器运维)
- [软件包安装](#软件包安装)
  - [下载](#下载)
  - [安装](#安装)
  - [权限与守护进程状态](#权限与守护进程状态)
- [配置代理服务器](#配置代理服务器)
- [启动、停止与重载](#启动停止与重载)
- [高级设置](#高级设置)
  - [BBR 拥塞控制算法](#bbr-拥塞控制算法)
  - [出站代理（链式代理）](#配置出站代理)
  - [DNS 策略](#dns-策略)
  - [允许访问内网与本机](#允许用户访问内网)
  - [流量配额](#限制用户流量)
  - [强制用户提示](#用户提示)
- [时间同步服务](#可选安装-ntp-网络时间同步服务)

---

## 前置条件

- 拥有一台具备公网 IPv4 或 IPv6 地址的 Linux 服务器。
- 具备 root 或 `sudo` 权限。
- 防火墙已放行你打算使用的 TCP 和/或 UDP 端口（默认范围 1025–65535）。
- 系统时间准确（请参阅 [NTP 章节](#可选安装-ntp-网络时间同步服务)）。

> **关于时间同步的说明**
> 客户端与服务器端会根据用户名、密码以及当前系统时间分别计算加密密钥。如果两端时钟偏差过大，服务器将无法解密客户端流量。强烈建议在两端均启用 NTP 同步。

---

## 一键安装脚本（推荐用于 systemd 系统）

对于 Debian / Ubuntu 和 RHEL / CentOS / Rocky Linux 系统，最简单的安装方式是使用官方一键脚本。它会自动检测系统架构和发行版，从 GitHub 下载最新版本并安装软件包，引导交互式配置向导，自动配置防火墙，启用开机自启，并输出连接信息。

```sh
curl -fsSL https://raw.githubusercontent.com/enfein/mieru/main/tools/install.sh | sudo bash -s -- install
```

或者先下载脚本，再交互式运行：

```sh
curl -fsSL -o install-mita.sh https://raw.githubusercontent.com/enfein/mieru/main/tools/install.sh
chmod +x install-mita.sh
sudo ./install-mita.sh
```

### 脚本命令

| 命令 | 说明 |
|---------|-------------|
| `install` | 安装最新版 mita 并运行交互式配置 |
| `update` | 更新到最新版本，保留现有配置 |
| `uninstall` | 卸载 mita，可选择是否删除配置 |
| `reconfigure` | 重新交互式生成 `server_config.json` 并重启服务 |
| `start` / `stop` / `restart` | 控制代理服务 |
| `enable` / `disable` | 开关 systemd 开机自启 |
| `status` | 查看守护进程和代理状态 |
| `info` | 显示服务器 IP、端口、用户及 `mierus://` 分享链接 |
| `bbr` | 启用 BBR 拥塞控制（可重复执行，不会重复配置） |

---

## Docker 部署

官方 Docker 镜像托管于 GitHub Container Registry，是启动 mita 的最快方式。

**镜像地址：** `ghcr.io/enfein/mita:latest`  
**构建源：** [deployments/docker/mita/Dockerfile](../deployments/docker/mita/Dockerfile)

### 使用 Docker 快速启动

```sh
# 1. 创建本地配置目录
mkdir -p ~/mita-config

# 2. 创建 server_config.json（配置格式见下文「配置代理服务器」章节）
#    示例：nano ~/mita-config/server_config.json

# 3. 拉取镜像并运行
sudo docker run -d \
  --name mita \
  --restart unless-stopped \
  -p 2012-2022:2012-2022/tcp \
  -p 2027:2027/tcp \
  -v ~/mita-config:/etc/mita:Z \
  ghcr.io/enfein/mita:latest \
  mita run
```

然后在运行中的容器内加载配置并启动服务：

```sh
sudo docker exec -it mita mita apply config /etc/mita/server_config.json
sudo docker exec -it mita mita start
sudo docker exec -it mita mita status
```

### Docker Compose

对于生产环境或长期部署，建议使用 Docker Compose。

创建 `docker-compose.yml`：

```yaml
services:
  mita:
    image: ghcr.io/enfein/mita:latest
    container_name: mita
    restart: unless-stopped
    ports:
      - "2012-2022:2012-2022/tcp"
      - "2027:2027/tcp"
      # 如需使用 UDP，可添加如下端口映射：
      # - "3000-3010:3000-3010/udp"
    volumes:
      - ./mita-config:/etc/mita:Z
    command: mita run
```

创建 `mita-config/server_config.json`（完整配置格式见 [配置代理服务器](#配置代理服务器)）。

启动服务栈：

```sh
docker compose up -d
```

加载配置并启动代理服务：

```sh
docker compose exec mita mita apply config /etc/mita/server_config.json
docker compose exec mita mita start
docker compose exec mita mita status
```

### 容器运维

| 操作 | 命令 |
|------|------|
| 查看日志 | `docker logs --tail 100 -f mita` |
| 重启服务 | `docker restart mita` |
| 停止服务 | `docker exec mita mita stop` |
| 进入容器 Shell | `docker exec -it mita sh` |
| 升级镜像 | `docker pull ghcr.io/enfein/mita:latest && docker compose up -d` |

> **提示：** 容器以 `mita run` 前台模式运行，而非 systemd 服务单元。因此，直接执行 `docker restart` 即可重启守护进程。如需应用必须重启才能生效的配置变更，可先执行 `docker compose exec mita mita stop`，再重启容器。

---

## 软件包安装

适合偏好原生软件包与 systemd 集成的用户。

### 下载与安装

以下命令自动从 GitHub API 获取最新发布版本号，然后下载并安装对应平台的软件包。先执行 `TAG=...` 行，再复制对应平台的命令块运行。

```sh
# 获取最新发布版本号，例如 v3.32.0
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

> **提示：** 如果系统中没有 `curl`，可改用 `TAG=$(wget -qO- https://api.github.com/repos/enfein/mieru/releases/latest | grep '"tag_name":' | head -1 | sed -E 's/.*"([^"]+)".*/\1/')`。

同样的命令也适用于升级已有安装。

### 权限与守护进程状态

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

同样的命令也适用于升级已有安装。

### 权限与守护进程状态

将当前用户加入 `mita` 组，以便无需 `sudo` 即可运行 `mita` 命令：

```sh
sudo usermod -a -G mita $USER
# 重新登录使组变更生效
exit
```

重新通过 SSH 连接后，验证守护进程状态：

```sh
systemctl status mita
```

若输出包含 `active (running)`，说明 mita systemd 服务已正常运行，并将在系统启动时自动启动。

检查 mita 应用状态：

```sh
mita status
```

刚完成安装时，输出为 `mita server status is "IDLE"`，表示服务器尚未开始监听客户端连接。

---

## 配置代理服务器

mieru 支持 **TCP** 和 **UDP** 两种传输协议。两者的区别请参阅 [mieru 代理协议](./protocol.zh_CN.md)。

使用以下命令应用配置：

```sh
mita apply config <FILE>
```

`<FILE>` 为 JSON 格式的配置文件。该命令会将文件内容合并到现有配置中，无需每次提供完整配置。

### 最小可用示例

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

**字段说明：**

| 字段 | 说明 |
|------|------|
| `portBindings` | 监听端口列表。每条记录需指定 `protocol`（`TCP` 或 `UDP`）。使用 `port`（整数，1025–65535）或 `portRange`（字符串，如 `"2012-2022"`）定义端口。 |
| `users` | 认证用户列表。每个用户必须提供 `name` 与 `password`。 |
| `loggingLevel` | 日志级别：`DEBUG`、`INFO`、`WARN` 或 `ERROR`。默认值：`INFO`。 |
| `mtu` | *(可选)* 使用 UDP 协议时的最大传输单元载荷。默认值：`1400`。最小值：`1280`。 |

> **安全提示：** 请确保主机防火墙（如 `iptables`、`nftables`、`ufw` 或云服务商安全组）已放行所配置端口的入站流量。

编辑配置文件后，执行以下命令应用：

```sh
mita apply config server_config.json
```

若校验失败，mita 会输出具体的错误信息。请根据提示修正配置文件后重新运行命令。

使用以下命令查看当前生效配置：

```sh
mita describe config
```

---

## 启动、停止与重载

### 启动代理服务

```sh
mita start
```

mita 将开始监听配置中指定的端口。使用以下命令验证：

```sh
mita status
```

若输出为 `mita server status is "RUNNING"`，表示代理服务已就绪，可以接收客户端请求。

### 停止代理服务

```sh
mita stop
```

### 不重载连接地更新配置

```sh
mita reload
```

`reload` **仅** 支持修改 `users` 或 `loggingLevel` 字段。对于其他配置变更，必须执行完整的重启：

```sh
mita stop
mita start
```

代理服务启动后，请继续进行 [客户端安装与配置](./client-install.zh_CN.md)。

---

## 高级设置

### BBR 拥塞控制算法

[BBR](https://en.wikipedia.org/wiki/TCP_congestion_control#TCP_BBR) 是一种不依赖丢包的拥塞控制算法，在恶劣网络环境下传输速度优于传统算法。mieru 的 UDP 协议已内置 BBR；以下脚本可为 TCP 协议启用 BBR：

```sh
curl -fSsLO https://raw.githubusercontent.com/enfein/mieru/refs/heads/main/tools/enable_tcp_bbr.py
chmod +x enable_tcp_bbr.py
sudo python3 enable_tcp_bbr.py
```

服务器端与客户端均可运行此脚本。

### 配置出站代理

出站代理（链式代理）功能允许 mita 将流量转发至上游 SOCKS5 代理。

示例网络拓扑：

```
mieru 客户端 -> GFW -> mita 服务器 -> cloudflare 代理客户端 -> cloudflare CDN -> 目标网站
```

在此拓扑下，目标网站看到的是 Cloudflare CDN 的 IP 地址，而非 mita 服务器的地址。

示例配置：

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

**字段说明：**

| 字段 | 说明 |
|------|------|
| `egress.proxies` | 上游 SOCKS5 代理列表。`protocol` 必须为 `SOCKS5_PROXY_PROTOCOL`。若代理无需认证，可省略 `socks5Authentication`。 |
| `egress.rules` | 按顺序匹配的规则列表。**第一条**匹配的规则即生效。通配符 `"*"` 匹配所有 IP 或域名。行为包括 `DIRECT`、`PROXY`、`REJECT`。`PROXY` 必须指定 `proxyNames`，引用 `egress.proxies` 中定义的代理。默认行为：`DIRECT`。 |

如需关闭出站代理，将 `egress` 设为空对象 `{}`。

> **链式代理与嵌套代理的区别：** 链式代理是在 mita 之后将流量转发至另一个代理；嵌套代理是在 mieru 客户端之前通过其他代理软件转发流量（例如：Tor 浏览器 -> mieru 客户端 -> GFW -> mita 服务器 -> Tor 网络）。关于嵌套代理的配置，请参阅 [翻墙安全指南](./security.zh_CN.md)。

### DNS 策略

当客户端以域名形式请求目标网站时，mita 需要先解析域名再建立连接。可通过以下配置自定义解析策略：

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

`dualStack` 可选值：

| 值 | 行为 |
|----|------|
| `USE_FIRST_IP` | 始终使用 DNS 返回的第一个 IP 地址。（默认） |
| `PREFER_IPv4` | 优先使用第一个 IPv4 地址；若无 IPv4 则回退到 IPv6。 |
| `PREFER_IPv6` | 优先使用第一个 IPv6 地址；若无 IPv6 则回退到 IPv4。 |
| `ONLY_IPv4` | 强制使用 IPv4；若不存在则连接失败。 |
| `ONLY_IPv6` | 强制使用 IPv6；若不存在则连接失败。 |

`hosts` 定义静态域名到 IP 的映射，作用类似于 `/etc/hosts`。匹配为精确匹配且不区分大小写；不支持通配符或后缀匹配。键不能以点号开头或结尾。

### 允许用户访问内网

默认情况下，mita 拒绝发往私有地址和回环地址的请求。

若允许特定用户访问私有 IP（如 `192.168.1.100`），设置 `allowPrivateIP: true`。  
若允许特定用户访问服务器本机（如 `127.0.0.1`），设置 `allowLoopbackIP: true`。

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

### 限制用户流量

使用 `quotas` 字段为用户设置流量配额。

示例：用户 `ducaiguozei` 每日最多使用 1 GB，每 30 天最多使用 10 GB。

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

### 用户提示

自 v3.31.0 起，mieru 客户端发送「用户提示」，以加速服务器端的数据包解密。当代理用户数量较多时，此功能尤为有效。

如需阻止未发送用户提示的旧版本客户端（以降低服务器 CPU 负载），可应用以下配置：

```json
{
    "advancedSettings": {
        "userHintIsMandatory": true
    }
}
```

---

## 【可选】安装 NTP 网络时间同步服务

客户端与服务器端会根据用户名、密码以及系统时间分别计算加密密钥。若两端时钟偏差过大，服务器将无法解密流量。强烈建议在两端均启用 NTP 服务。

若系统使用 `systemd-timesyncd` 管理时间同步，编辑 `/etc/systemd/timesyncd.conf`：

```ini
[Time]
NTP=time.google.com
```

然后重启服务：

```sh
sudo systemctl restart systemd-timesyncd
```

否则，安装独立的 NTP 软件包：

```sh
# Debian / Ubuntu
sudo apt-get install ntp

# Red Hat / CentOS / Rocky Linux
sudo dnf install ntp
```
