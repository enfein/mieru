# mieru / 見える

mieru【見える】是一款安全的、无流量特征、无法被主动探测的，基于 UDP 和 KCP 协议的 socks5 网络代理软件。

## 原理

mieru 的翻墙原理与 shadowsocks / v2ray 等软件类似，在客户端和墙外的代理服务器之间建立一个加密的通道。GFW 不能破解加密传输的信息，无法判定你最终访问的网址，因此只能选择放行。

## 特性

1. 使用高强度的 AES-256-GCM 加密算法，基于用户名、密码和系统时间生成密钥。以现有计算能力，mieru 传输的数据内容无法被破解。
2. mieru 实现了客户端和代理服务器之间所有传输内容的完整加密，不传输任何明文信息。网络观察者（例如 GFW）仅能获知时间、数据包的发送和接收地址，以及数据包的大小。除此之外，观察者无法得到其它任何流量信息。
3. 当 mieru 发送数据包时，会在尾部填充随机信息。即便是传输相同的内容，数据包大小也不相同。这从根本上解决了 KCP 协议的 ACK 包容易被识别的问题。
4. mieru 不需要客户端和服务器进行握手，即可直接发送数据。当服务器无法解密客户端发送的数据时，不会返回任何内容。因此 GFW 不能通过主动探测发现 mieru 服务。
5. mieru 支持多个用户共享代理服务器。
6. 客户端软件支持 Windows，Mac OS 和 Linux 系统。

## 快速入门

mieru 代理软件由称为 mieru【見える】的客户端软件和称为 mita【見た】的代理服务器软件这两部分组成。

### 代理服务器软件的安装和配置

我们建议有经济实力的用户选择亚马逊和微软等国外大型云服务提供商，一般不会被封锁 IP。请勿使用国内公司或来路不明的云计算服务。代理服务器占用的 CPU 和内存资源很少，最终网络速度主要取决于服务器的网速和线路质量。

代理服务器软件 mita 需要运行在 Linux 系统中。我们提供了 debian 安装包，便于用户在 Debian/Ubuntu 系列发行版中安装 mita。

在安装和配置开始之前，先用 SSH 连接到服务器，再执行下面的指令。

**【1】下载 mita 安装包**

```sh
curl -L -O https://github.com/enfein/mieru/releases/download/v1.0.0/mieru_1.0.0_amd64.deb
```

**【2】安装 mita 软件包**

```sh
sudo dpkg -i mita_1.0.0_amd64.deb
```

**【3】赋予当前用户操作 mita 的权限，需要重启服务器使此设置生效**

```sh
sudo usermod -a -G mita $USER

sudo reboot
```

**【4】重启后，使用 SSH 重新连接到服务器，检查 mita 守护进程的状态**

```sh
systemctl status mita
```

如果输出中包含 `active (running)` ，表示 mita 守护进程已经开始运行。通常情况下，mita 会在服务器启动后自动开始运行。

**【5】查询 mita 的工作状态**

```sh
mita status
```

如果刚刚完成安装，此时的输出为 `mieru server status is "IDLE"`，表示 mita 还没有开始监听来自 mieru 客户端的请求。

**【6】查询代理服务器设置**

```sh
mita describe config
```

如果刚刚完成安装，设置应该是空的，此时会返回 `{}`。

**【7】修改代理服务器设置**

用户可以通过

```sh
mita apply config <FILE>
```

指令来修改代理服务器的设置，这里的 `<FILE>` 是一个 JSON 格式的文件。我们在项目根目录下的 `configs/templates/server_config.json` 文件中提供了一个配置模板。该模板的内容如下所示：

```js
{
    "portBindings": [
        {
            "port": -1,
            "protocol": "UDP"
        }
    ],
    "users": [
        {
            "name": "<username@example.com>",
            "password": "<your-password>"
        }
    ],
    "loggingLevel": "INFO"
}
```

请将这个模板下载或复制到你的服务器中，用文本编辑器打开，并修改如下的内容：

1. `portBindings` -> `port` 属性是 mita 监听的 UDP 端口号，请指定一个从 1025 到 65535 之间的值。**请确保防火墙允许使用该端口进行通信。**
2. 在 `users` -> `name` 属性中填写用户名。
3. 在 `users` -> `password` 属性中填写该用户的密码。

也可以创建多个不同的用户，例如

```js
    "users": [
        {
            "name": "<user1@example.com>",
            "password": "<user1-password>"
        },
        {
            "name": "<user2@example.com>",
            "password": "<user2-password>"
        }
    ],
```

假设在服务器上，这个配置文件的文件名是 `server_config.json`，在文件修改完成之后，请调用指令 `mita apply config server_config.json` 写入该配置。

如果配置有误，mita 会打印出现的问题。请根据提示修改配置文件，重新运行 `mita apply config <FILE>` 指令写入修正后的配置。

写入后，可以用

```sh
mita describe config
```

指令查看当前设置。

**【8】启动代理服务**

使用指令

```sh
mita start
```

启动代理服务。此时 mita 会开始监听设置中指定的端口号。

接下来，调用指令

```sh
mita status
```

查询工作状态，这里如果返回 `mieru server status is "RUNNING"`，说明代理服务正在运行，可以开始相应客户端的请求了。

如果想要停止代理服务，请使用指令

```sh
mita stop
```

注意，在使用 `mita apply config <FILE>` 修改设置后，需要用 `mita stop` 和 `mita start` 重启代理服务，才能使新设置生效。

**【可选】安装 NTP 服务**

客户端和代理服务器软件会根据用户名、密码和系统时间，分别计算密钥。只有当客户端和服务器的密钥相同时，服务器才能解密和响应客户端的请求。这要求客户端和服务器的系统时间不能有很大的差别。

为了保证服务器系统时间是精确的，我们建议用户安装 NTP 网络时间服务。在 Debian/Ubuntu 系列发行版中，安装 NTP 只需要一行指令

```sh
sudo apt-get install ntp
```

**【可选】关闭 ICMP Destination Unreachable 消息**

我们在前面讲到，当服务器无法解密客户端发送的数据时，不会返回任何内容。不过，如果外界向服务器发送 UDP 包进行主动探测，依旧可以区分下面两种情况：

1. 没有任何服务在监听这个 UDP 端口
2. 某个服务在监听这个 UDP 端口，但没有返回任何内容

这是因为，当没有任何服务在监听这个 UDP 端口时，Linxu 内核会返回一个 ICMP Destination Unreachable - Port Unreachable 消息。

如果不想让外界探测服务器在监听哪些 UDP 端口，可以设置 Linux 系统防火墙丢弃 ICMP Destination Unreachable 消息。

```sh
sudo iptables -A OUTPUT -p icmp --icmp-type 3 -j DROP
```

注意，启用这条防火墙规则会使故障排查变得复杂。如果需要删除这条防火墙规则，请使用

```sh
sudo iptables -D OUTPUT -p icmp --icmp-type 3 -j DROP
```

### 客户端软件的安装和配置

**【1】下载 mieru 安装包**

mieru 客户端软件支持 Windows，Mac OS 和 Linux 系统。用户可以在 GitHub Releases 页面用浏览器下载，也可以使用 Linux / Mac 终端或 Windows PowerShell 下载。

```powershell
# Windows PowerShell
Invoke-WebRequest https://github.com/enfein/mieru/releases/download/v1.0.0/mieru_1.0.0_windows_amd64.zip -OutFile mieru_1.0.0_windows_amd64.zip
```

```sh
# Mac OS
curl -L -O https://github.com/enfein/mieru/releases/download/v1.0.0/mieru_1.0.0_darwin_amd64.tar.gz
```

```sh
# Linux
curl -L -O https://github.com/enfein/mieru/releases/download/v1.0.0/mieru_1.0.0_linux_amd64.tar.gz
```

解压缩之后，就可以得到可执行文件 `mieru` 或 `mieru.exe`。

**【可选】下载和安装 mieru debian 安装包**

如果你的客户端操作系统是 Debian/Ubuntu 系列 Linux 发行版，可以使用下面的指令下载和安装 mieru。

```sh
# TODO: 更换下载链接的地址
curl -O https://github.com/enfein/mieru/mieru_1.0.0_amd64.deb

sudo dpkg -i mieru_1.0.0_amd64.deb
```

**【可选】将 mieru 可执行文件移动或添加至系统路径 PATH**

这样，输入指令时就不需要指定 mieru 可执行文件的位置了。

如果使用了 debian 安装包，那么不需要执行这一步。

**【2】查询客户端的设置**

```sh
mieru describe config
```

如果是第一次完成 mieru 的安装，设置应该是空的，此时会返回 `{}`。

**【3】修改客户端的设置**

用户可以通过

```sh
mieru apply config <FILE>
```

指令来修改客户端的设置，这里的 `<FILE>` 是一个 JSON 格式的文件。我们在项目根目录下的 `configs/templates/client_config.json` 文件中提供了一个配置模板。该模板的内容如下所示：

```js
{
    "profiles": [
        {
            "profileName": "default",
            "user": {
                "name": "<username@example.com>",
                "password": "<your-password>"
            },
            "servers": [
                {
                    "ipAddress": "<1.1.1.1>",
                    "domainName": "",
                    "portBindings": [
                        {
                            "port": -1,
                            "protocol": "UDP"
                        }
                    ]
                }
            ]
        }
    ],
    "activeProfile": "default",
    "rpcPort": -1,
    "socks5Port": -1,
    "loggingLevel": "INFO"
}
```

请下载或复制这个模板，然后用文本编辑器修改如下的内容：

1. 在 `profiles` -> `user` -> `name` 属性中，填写用户名。此处必须与代理服务器中的设置相同。
2. 在 `profiles` -> `user` -> `password` 属性中，填写密码。此处必须与代理服务器中的设置相同。
3. 在 `profiles` -> `servers` -> `ipAddress` 属性中，填写代理服务器的公网地址。目前只支持 IPv4 地址。
4. 如果你为代理服务器注册了域名，请在 `profiles` -> `servers` -> `domainName` 中填写域名。否则，请勿修改这个属性。
5. 在 `profiles` -> `servers` -> `portBindings` -> `port` 中填写 mita 监听的 UDP 端口号。这个端口号必须与代理服务器中的设置相同。
6. 请为 `rpcPort` 属性指定一个从 1025 到 65535 之间的数值。**请确保防火墙允许使用该端口进行通信。**
7. 请为 `socks5Port` 属性指定一个从 1025 到 65535 之间的数值。该端口不能与 `rpcPort` 相同。**请确保防火墙允许使用该端口进行通信。**

假设这个配置文件的文件名是 `client_config.json`，在修改完成之后，调用指令 `mieru apply config client_config.json` 写入该配置。

如果配置有误，mieru 会打印出现的问题。请根据提示修改配置文件，重新运行 `mieru apply config <FILE>` 指令写入修正后的配置。

写入后，可以用

```sh
mieru describe config
```

指令查看当前设置。

**【4】启动客户端**

```sh
mieru start
```

如果输出显示 `mieru client is started, listening to localhost:xxxx`，表示 mieru 客户端已经开始运行。

mieru 客户端不会与系统一同启动。在重新启动计算机后，需要手动使用 `mieru start` 指令启动客户端。

如果需要停止 mieru 客户端，请输入指令

```sh
mieru stop
```

注意，在使用 `mieru apply config <FILE>` 修改设置后，需要用 `mieru stop` 和 `mieru start` 重启客户端，才能使新设置生效。

### 浏览器配置

Chrome / Firefox 等浏览器可以通过安装插件，使用 socks5 代理访问墙外的网站。关于 socks5 代理的地址，请填写 `localhost:xxxx`，其中 `xxxx` 是客户端设置中 `socks5Port` 的值。这个地址在调用 `mieru start` 指令时也会打印出来。

mieru 不使用 socks5 用户名和密码进行身份验证。

## 运营与维护

### 配置文件存放地址

代理服务器软件 mita 的配置存放在 `/etc/mita/server.conf.pb`。这是一个以 protocol buffer 格式存储的二进制文件。为保护用户信息，mita 不会存储用户密码的明文，只会存储其校验码。

客户端软件 mieru 的配置文件同样是一个二进制文件。它在不同操作系统中存储的位置如下表所示

| Operating System | Configuration File Path |
| Linux | /home/USERNAME/.config/mieru/client.conf.pb |
| Mac OS | /Users/USERNAME/Library/Application Support/mieru/client.conf.pb |
| Windows | C:\Users\USERNAME\AppData\Roaming\mieru\client.conf.pb |

### 查看代理服务器 mita 的日志

用户可以使用下面的指令打印 mita 的全部日志

```sh
journalctl -u mita --no-pager
```

只有当你登录服务器的用户属于 `adm` 或 `systemd-journal` 用户组时才可以查看日志。必要时可以使用 `sudo` 提升权限。

```sh
sudo journalctl -u mita --no-pager
```

### 查看客户端 mieru 的日志

客户端 mieru 的日志存放位置如下表所示

| Operating System | Configuration File Path |
| Linux | /home/USERNAME/.cache/mieru/ |
| Mac OS | /Users/USERNAME/Library/Caches/mieru/ |
| Windows | C:\Users\USERNAME\AppData\Local\mieru\ |

每个日志文件的格式为 `yyyyMMdd_HHmm_PID.log`，其中 `yyyyMMdd_HHmm` 是 mieru 进程启动的时间。每次重启 mieru 会生成一个新的日志文件。当日志文件的数量太多时，旧的文件会被自动删除。

### 打开和关闭调试日志

mieru / mita 在默认的日志等级下，提供的内容非常少，不包含 IP 地址、端口号等敏感信息。如果需要诊断单个网络连接，则需要打开调试日志（debug log）。

本项目在 `configs/templates` 目录下提供了快速打开和关闭调试日志的配置文件。请将它们下载到服务器或本地计算机上，并输入以下指令。注意，改变 mieru / mita 的设置后，需要重新启动服务才能生效。

打开 mita 代理服务器调试日志

```sh
mita apply config server_enable_debug_logging.json
```

关闭 mita 代理服务器调试日志

```sh
mita apply config server_disable_debug_logging.json
```

重新启动 mita 代理服务器

```sh
mita stop

mita start
```

打开 mieru 客户端调试日志

```sh
mieru apply config client_enable_debug_logging.json
```

关闭 mieru 客户端调试日志

```sh
mieru apply config client_disable_debug_logging.json
```

重新启动 mieru 客户端

```sh
mieru stop

mieru start
```

### 判断客户端与服务器之间的连接是否正常

如果服务器仅有一个客户端，判断连接是否正常，只需要查看服务器日志。例如，在下面的日志示例中

```
INFO [metrics - connections] ActiveOpens=0 CurrEstablished=0 MaxConn=0 PassiveOpens=0
INFO [metrics - KCP segments] EarlyRetransSegs=0 FastRetransSegs=0 InSegs=0 LostSegs=0 OutOfWindowSegs=0 OutSegs=0 RepeatSegs=0 RetransSegs=0
```

如果 `InSegs` 的值不为 0，说明服务器成功解密了客户端发送的数据包。如果 `OutSegs` 的值不为 0，说明服务器向客户端返回了数据包。

### 故障诊断与排查

mieru 为了防止 GFW 主动探测，增强了服务器端的隐蔽性，但是也增加了调试的难度。如果你的客户端和服务器之间无法建立连接，从以下几个排查方向入手可能会有所帮助。

1. 服务器是否在正常工作？用 `ping` 指令确认客户端是否可以到达服务器。
2. mita 代理服务是否已经启动？用 `mita status` 指令查看。
3. mieru 客户端是否已经启动？用 `mieru status` 指令查看。
4. 客户端和服务器的设置中，端口号是否相同？用户名是否相同？密码是否相同？用 `mieru describe config` 和 `mita describe config` 查看。

## 编译

编译 mieru 的客户端和服务器软件，建议在 Debian/Ubuntu 系列发行版的 Linux 系统中进行。编译过程可能需要翻墙下载依赖的软件包。

编译所需的软件包括：

- curl
- env
- git
- go (version >= 1.15)
- sha256sum
- tar
- zip

编译服务器 debian 安装包还需要：

- dpkg-deb
- fakeroot

编译时，进入项目根目录，调用指令 `./build.sh` 即可。编译结果会存放在项目根目录下的 `release` 文件夹。

## 贡献

出于安全考虑，mieru 实际开发时使用的 git 仓库是一个私有仓库。我们在发布新版本前，会把私有仓库内的改动合并起来移动到这里。由于在两个仓库之间双向同步代码比较困难，我们暂时不接受 pull request。如果有新需求或报告 bug 请提交 GitHub Issue。

## 联系作者

关于本项目，如果你有任何问题，请提交 GitHub Issue 联系我们。

## 许可证

使用本软件需遵从 GPL 第三版协议。
