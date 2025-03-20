# 服务器安装与配置

代理服务器软件 mita 需要运行在 Linux 系统中。我们提供了 debian 和 RPM 安装包，便于用户在 Debian / Ubuntu 和 Fedora / CentOS / Red Hat Enterprise Linux 系列发行版中安装 mita。

在安装和配置开始之前，先通过 SSH 连接到服务器，再执行下面的指令。

## 下载 mita 安装包

```sh
# Debian / Ubuntu - X86_64
curl -LSO https://github.com/enfein/mieru/releases/download/v3.13.0/mita_3.13.0_amd64.deb

# Debian / Ubuntu - ARM 64
curl -LSO https://github.com/enfein/mieru/releases/download/v3.13.0/mita_3.13.0_arm64.deb

# RedHat / CentOS / Rocky Linux - X86_64
curl -LSO https://github.com/enfein/mieru/releases/download/v3.13.0/mita-3.13.0-1.x86_64.rpm

# RedHat / CentOS / Rocky Linux - ARM 64
curl -LSO https://github.com/enfein/mieru/releases/download/v3.13.0/mita-3.13.0-1.aarch64.rpm
```

## 安装 mita 软件包

```sh
# Debian / Ubuntu - X86_64
sudo dpkg -i mita_3.13.0_amd64.deb

# Debian / Ubuntu - ARM 64
sudo dpkg -i mita_3.13.0_arm64.deb

# RedHat / CentOS / Rocky Linux - X86_64
sudo rpm -Uvh --force mita-3.13.0-1.x86_64.rpm

# RedHat / CentOS / Rocky Linux - ARM 64
sudo rpm -Uvh --force mita-3.13.0-1.aarch64.rpm
```

上述指令也可以用来升级 mita 软件包的版本。

## 赋予当前用户操作 mita 的权限，重新登录使此设置生效

```sh
sudo usermod -a -G mita $USER

# logout
exit
```

## 使用 SSH 重新连接到服务器，检查 mita 守护进程的状态

```sh
systemctl status mita
```

如果输出中包含 `active (running)` ，表示 mita 守护进程已经开始运行。通常情况下，mita 会在服务器启动后自动开始运行。

## 查询 mita 的工作状态

```sh
mita status
```

如果刚刚完成安装，此时的输出为 `mita server status is "IDLE"`，表示 mita 还没有开始监听来自 mieru 客户端的请求。

## 修改代理服务器设置

mieru 代理支持 TCP 和 UDP 两种不同的传输协议。要了解协议之间的差别，请阅读 [mieru 代理协议](./protocol.zh_CN.md)。

用户可以通过

```sh
mita apply config <FILE>
```

指令来修改代理服务器的设置，这里的 `<FILE>` 是一个 JSON 格式的配置文件。该配置文件不需要指定完整的代理服务器设置。运行指令 `mita apply config <FILE>` 时，文件内容会合并到任何已有的代理服务器设置。

下面是服务器配置文件的一个例子。

```js
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

1. `portBindings` -> `port` 属性是 mita 监听的 TCP 或 UDP 端口号，请指定一个从 1025 到 65535 之间的值。如果想要监听连续的端口号，也可以改为使用 `portRange` 属性。**请确保防火墙允许使用这些端口进行通信。**
2. `portBindings` -> `protocol` 属性可以使用 `TCP` 或者 `UDP`。
3. 在 `users` -> `name` 属性中填写用户名。
4. 在 `users` -> `password` 属性中填写该用户的密码。
5. 【可选】`mtu` 属性是使用 UDP 代理协议时，传输层最大的载荷大小。默认值是 1400，最小值是 1280。

除此之外，mita 可以监听多个不同的端口。我们建议在服务器和客户端配置中使用多个端口。

如果你想把代理线路分享给别人使用，也可以创建多个不同的用户。

假设在服务器上，这个配置文件的文件名是 `server_config.json`，在文件修改完成之后，请调用指令 `mita apply config server_config.json` 写入该配置。

如果配置有误，mita 会打印出现的问题。请根据提示修改配置文件，重新运行 `mita apply config <FILE>` 指令写入修正后的配置。

写入后，可以用

```sh
mita describe config
```

指令查看当前设置。

## 启动代理服务

使用指令

```sh
mita start
```

启动代理服务。此时 mita 会开始监听设置中指定的端口号。

接下来，调用指令

```sh
mita status
```

查询工作状态，这里如果返回 `mita server status is "RUNNING"`，说明代理服务正在运行，可以开始相应客户端的请求了。

如果想要停止代理服务，请使用指令

```sh
mita stop
```

注意，每次使用 `mita apply config <FILE>` 修改设置后，需要用 `mita stop` 和 `mita start` 重启代理服务，才能使新设置生效。一个例外是，如果只修改了 `users` 或者 `loggingLevel` 设置，你可以使用 `mita reload` 加载新的设置，此时不会影响服务器与客户端的活跃连接。

启动代理服务后，请继续进行[客户端安装与配置](./client-install.zh_CN.md)。

## 高级设置

### BBR 拥塞控制算法

[BBR](https://en.wikipedia.org/wiki/TCP_congestion_control#TCP_BBR) 是一种不依赖于丢包的拥塞控制算法。在恶劣的网络情况下，使用 BBR 的网络传输速度比传统算法更快。

mieru 的 UDP 传输协议已经使用了 BBR 算法。

在项目根目录下，我们提供了脚本 `tools/enable_tcp_bbr.py`，允许用户在 Linux 系统中让 TCP 传输协议使用 BBR 算法。

```sh
sudo ./tools/enable_tcp_bbr.py
```

上述脚本可以在服务器和客户端中使用。

### 配置出站代理

出站代理功能允许 mieru 与其他代理工具结合构成链式代理。链式代理的网络拓扑结构的一个例子如下图所示：

```
mieru 客户端 -> GFW -> mita 服务器 -> cloudflare 代理客户端 -> cloudflare CDN -> 目标网站
```

通过链式代理，目标网站看到的 IP 地址是 cloudflare CDN 的地址，而不是 mita 服务器的地址。

下面是配置链式代理的一个例子。

```js
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
                "ipRanges": ["*"],
                "domainNames": ["*"],
                "action": "PROXY",
                "proxyName": "cloudflare"
            }
        ]
    }
}
```

1. 在 `egress` -> `proxies` 属性中列举出站代理服务器的信息。当前版本只支持 socks5 出站，因此 `protocol` 的值必须设定为 `SOCKS5_PROXY_PROTOCOL`。如果出站代理服务器需要 socks5 用户名和密码验证，请填写 `socks5Authentication` 属性。否则，请删除 `socks5Authentication` 属性。
2. 在 `egress` -> `rules` 属性中列举出站规则。当前版本最多允许用户添加一条规则，且 `ipRanges`, `domainNames` 和 `action` 的值必须与上面的例子相同。`proxyName` 需要指向一个 `egress` -> `proxies` 属性中存在的代理。

如果想要关闭出站代理功能，将 `egress` 属性设置为空 `{}` 即可。

注意，链式代理和嵌套代理不同。嵌套代理的网络拓扑结构的一个例子如下图所示：

```
Tor 浏览器 -> mieru 客户端 -> GFW -> mita 服务器 -> Tor 网络 -> 目标网站
```

关于如何在 Tor 浏览器上配置嵌套代理，请参见[翻墙安全指南](./security.zh_CN.md)。

### IPv4 / IPv6 双栈网络中的 DNS 策略

当代理客户端请求的目标网站是域名，而不是 IP 地址时，代理服务器需要发起 DNS 请求。如果代理服务器处于 IPv4 / IPv6 双栈网络中，可以使用下面的配置调整 DNS 策略：

```js
{
    "dns": {
        "dualStack": "USE_FIRST_IP"
    }
}
```

`dns` -> `dualStack` 属性支持的值包括：

1. `USE_FIRST_IP`：永远使用 DNS 服务器返回的第一个 IP 地址。这是默认策略。
2. `PREFER_IPv4`：优先使用 DNS 服务器返回的第一个 IPv4 地址。如果没有 IPv4 地址，则使用第一个 IPv6 地址。
3. `PREFER_IPv6`：优先使用 DNS 服务器返回的第一个 IPv6 地址。如果没有 IPv6 地址，则使用第一个 IPv4 地址。
4. `ONLY_IPv4`：强制使用 DNS 服务器返回的第一个 IPv4 地址。如果没有 IPv4 地址则连接失败。
5. `ONLY_IPv6`：强制使用 DNS 服务器返回的第一个 IPv6 地址。如果没有 IPv6 地址则连接失败。

### 限制用户流量

我们可以使用 `users` -> `quotas` 属性限制用户可以使用的流量大小。例如，如果想让用户 "ducaiguozei" 在 1 天时间内最多使用 1 GB 流量，并且在 30 天时间内最多使用 10 GB 流量，可以应用下面的设置。

```js
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
            "password": "xijinping",
            "quotas": [
                {
                    "days": 1,
                    "megabytes": 1024
                },
                {
                    "days": 30,
                    "megabytes": 10240
                }
            ]
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

## 【可选】安装 NTP 网络时间同步服务

客户端和代理服务器软件会根据用户名、密码和系统时间，分别计算密钥。只有当客户端和服务器的密钥相同时，服务器才能解密和响应客户端的请求。这要求客户端和服务器的系统时间不能有很大的差别。

为了保证服务器系统时间是精确的，我们建议用户安装 NTP 网络时间服务。在许多 Linux 发行版中，安装 NTP 只需要一行指令

```sh
# Debian / Ubuntu
sudo apt-get install ntp

# RedHat / CentOS / Rocky Linux
sudo dnf install ntp
```
