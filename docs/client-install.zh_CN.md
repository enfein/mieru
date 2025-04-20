# 客户端安装与配置

## 下载 mieru 客户端

mieru 客户端软件支持 Windows，Mac OS 和 Linux 系统。用户可以在 GitHub Releases 页面下载。解压缩后请将 mieru 可执行文件放置于系统路径 `PATH` 中。

如果你的客户端操作系统是 Linux，也可以使用 debian 和 RPM 安装包安装 mieru。

## 修改客户端的设置

用户可以通过

```sh
mieru apply config <FILE>
```

指令来修改客户端的设置，这里的 `<FILE>` 是一个 JSON 格式的配置文件。该配置文件不需要指定完整的客户端设置。运行指令 `mieru apply config <FILE>` 时，文件内容会合并到任何已有的客户端设置。

客户端配置的一个示例如下。

```js
{
    "profiles": [
        {
            "profileName": "default",
            "user": {
                "name": "ducaiguozei",
                "password": "xijinping"
            },
            "servers": [
                {
                    "ipAddress": "12.34.56.78",
                    "domainName": "",
                    "portBindings": [
                        {
                            "portRange": "2012-2022",
                            "protocol": "TCP"
                        },
                        {
                            "port": 2027,
                            "protocol": "TCP"
                        }
                    ]
                }
            ],
            "mtu": 1400,
            "multiplexing": {
                "level": "MULTIPLEXING_HIGH"
            }
        }
    ],
    "activeProfile": "default",
    "rpcPort": 8964,
    "socks5Port": 1080,
    "loggingLevel": "INFO",
    "socks5ListenLAN": false,
    "httpProxyPort": 8080,
    "httpProxyListenLAN": false
}
```

请用文本编辑器修改如下的内容：

1. 在 `profiles` -> `user` -> `name` 属性中，填写用户名。此处必须与代理服务器中的设置相同。
2. 在 `profiles` -> `user` -> `password` 属性中，填写密码。此处必须与代理服务器中的设置相同。
3. 在 `profiles` -> `servers` -> `ipAddress` 属性中，填写代理服务器的公网地址。支持 IPv4 和 IPv6 地址。
4. 【可选】如果你为代理服务器注册了域名，请在 `profiles` -> `servers` -> `domainName` 中填写域名。否则，请勿修改这个属性。
5. 在 `profiles` -> `servers` -> `portBindings` -> `port` 中填写 mita 监听的 TCP 或 UDP 端口号。这个端口号必须与代理服务器中的设置相同。如果想要监听连续的端口号，也可以改为使用 `portRange` 属性。
6. 【可选】请为 `profiles` -> `mtu` 属性中指定一个从 1280 到 1400 之间的值。默认值为 1400。这个值可以与代理服务器中的设置不同。
7. 【可选】如果想要调整多路复用的频率，是更多地创建新连接，还是更多地重用旧连接，可以为 `profiles` -> `multiplexing` -> `level` 属性设定一个值。这里可以使用的值包括 `MULTIPLEXING_OFF`, `MULTIPLEXING_LOW`, `MULTIPLEXING_MIDDLE`, `MULTIPLEXING_HIGH`。其中 `MULTIPLEXING_OFF` 会关闭多路复用功能。默认值为 `MULTIPLEXING_LOW`。
8. 请为 `rpcPort` 属性指定一个从 1025 到 65535 之间的数值。
9. 请为 `socks5Port` 属性指定一个从 1025 到 65535 之间的数值。该端口不能与 `rpcPort` 相同。
10. 【可选】如果客户端需要为局域网中的其他设备提供代理服务，请将 `socks5ListenLAN` 属性设置为 `true`。
11. 【可选】如果要启动 HTTP / HTTPS 代理，请为 `httpProxyPort` 属性指定一个从 1025 到 65535 之间的数值。该端口不能与 `rpcPort` 和 `socks5Port` 相同。如果需要为局域网中的其他设备提供 HTTP / HTTPS 代理，请将 `httpProxyListenLAN` 属性设置为 `true`。如果不需要 HTTP / HTTPS 代理，请删除 `httpProxyPort` 和 `httpProxyListenLAN` 属性。

如果你安装了多台代理服务器，或者一台服务器监听多个端口，可以把它们都添加到客户端设置中。每次发起新的连接时，mieru 会随机选取其中的一台服务器和一个端口。**如果使用了多台服务器，请确保每一台服务器都启动了 mita 代理服务。**

假设这个配置文件的文件名是 `client_config.json`，在修改完成之后，调用指令 `mieru apply config client_config.json` 写入该配置。

如果配置有误，mieru 会打印出现的问题。请根据提示修改配置文件，重新运行 `mieru apply config <FILE>` 指令写入修正后的配置。

写入后，可以用

```sh
mieru describe config
```

指令查看当前设置。

## 启动客户端

```sh
mieru start
```

如果输出显示 `mieru client is started, listening to xxxxx`，表示 mieru 客户端已经在后台开始运行。

mieru 客户端不会与系统一同启动。在重新启动计算机后，需要手动使用 `mieru start` 指令启动客户端。

**Windows 用户请注意，在命令提示符或 Powershell 中使用 `mieru start` 指令启动客户端之后，请勿关闭命令提示符或 Powershell 窗口。关闭窗口将导致 mieru 客户端停止运行。** 一些新版本的 Windows 允许用户把命令提示符或 Powershell 最小化到托盘。

如果需要停止 mieru 客户端，请输入指令

```sh
mieru stop
```

注意，每次使用 `mieru apply config <FILE>` 修改设置后，需要用 `mieru stop` 和 `mieru start` 重启客户端，才能使新设置生效。

## 测试客户端与服务器的连接

```sh
mieru test

或者

mieru test https://<website.you.want.to.connect>
```

如果输出显示 `Connected to ...`，表示 mieru 客户端成功连接了代理服务器。

## 配置浏览器

Chrome / Firefox 等浏览器可以通过安装插件，使用 socks5 代理访问墙外的网站。关于 socks5 代理的地址，请填写 `127.0.0.1:xxxx`，其中 `xxxx` 是客户端设置中 `socks5Port` 的值。这个地址在调用 `mieru start` 指令时也会打印出来。

mieru 不使用 socks5 用户名和密码进行身份验证。

关于在 Tor 浏览器中配置 socks5 代理，参见[翻墙安全指南](./security.zh_CN.md)。

## 高级设置

### socks5 用户名和密码验证

如果你想要让应用程序必须通过用户名和密码验证才能访问 socks5 代理，可以在客户端设置中添加 `socks5Authentication` 属性。一个示例如下：

```js
{
    "socks5Authentication": [
        {
            "user": "yitukai",
            "password": "manlianpenfen"
        },
        {
            "user": "shilishanlu",
            "password": "buhuanjian"
        }
    ]
}
```

应用程序可以选择 `socks5Authentication` 列表中的任意一组用户名和密码访问 socks5 代理。

**socks5 用户名和密码验证与 HTTP / HTTPS 代理不兼容。** 因为 HTTP / HTTPS 代理不需要用户名和密码验证，根据威胁模型，mieru 禁止在使用 socks5 用户名和密码验证的同时使用 HTTP / HTTPS 代理。

如果需要删除已有的 HTTP / HTTPS 代理配置，请运行 `mieru delete http proxy` 指令。如果想要删除 socks5 用户名和密码验证的设置，请运行 `mieru delete socks5 authentication` 指令。

## 分享客户端的设置

用户可以使用 `mieru export config` 或者 `mieru export config simple` 指令生成 URL 链接，来分享客户端的配置。这些 URL 链接可以使用 `mieru import config <URL>` 指令导入至其他客户端。

### 标准分享链接

使用指令 `mieru export config` 生成一个标准分享链接。例如：

```
mieru://CpsBCgdkZWZhdWx0ElgKBWJhb3ppEg1tYW5saWFucGVuZmVuGkA0MGFiYWM0MGY1OWRhNTVkYWQ2YTk5ODMxYTUxMTY1MjJmYmM4MGUzODViYjFhYjE0ZGM1MmRiMzY4ZjczOGE0Gi8SCWxvY2FsaG9zdBoFCIo0EAIaDRACGgk5OTk5LTk5OTkaBQjZMhABGgUIoCYQASD4CioCCAQSB2RlZmF1bHQYnUYguAgwBTgA
```

标准分享链接以 `mieru://` 开始，使用 base64 编码完整的客户端配置。用户可以使用标准分享链接在一台全新的设备上复刻客户端配置。

### 简单分享链接

使用指令 `mieru export config simple` 生成人类可读的简单分享链接。例如：

```
mierus://baozi:manlianpenfen@1.2.3.4?mtu=1400&multiplexing=MULTIPLEXING_HIGH&port=6666&port=9998-9999&port=6489&port=4896&profile=default&protocol=TCP&protocol=TCP&protocol=UDP&protocol=UDP
```

简单分享链接的格式如下：

`mierus://用户名:密码@服务器地址?参数列表`

简单分享链接以 `mierus://` 开始，其中 `s` 表示 `simple`。

简单分享链接中只有一个服务器地址。如果客户端的设置含有多台服务器，则会生成多个链接。

链接中支持的参数列表如下：

- `profile`
- `mtu`
- `multiplexing`
- `port`
- `protocol`

其中 `profile` 必须出现一次，`mtu` 以及 `multiplexing` 最多出现一次，`port` 和 `protocol` 可以出现多次，且他们出现的次数必须相同，以便将同一位置上的 `port` 和 `protocol` 联系起来。另外 `port` 也可以用来指定一段连续的端口。

上面的简单分享链接等同于如下的客户端配置片段：

```json
{
    "profileName":  "default",
    "user":  {
        "name":  "baozi",
        "password":  "manlianpenfen"
    },
    "servers":  [
        {
            "ipAddress":  "1.2.3.4",
            "portBindings":  [
                {
                    "port":  6666,
                    "protocol":  "TCP"
                },
                {
                    "protocol":  "TCP",
                    "portRange":  "9998-9999"
                },
                {
                    "port":  6489,
                    "protocol":  "UDP"
                },
                {
                    "port":  4896,
                    "protocol":  "UDP"
                }
            ]
        }
    ],
    "mtu":  1400,
    "multiplexing":  {
        "level":  "MULTIPLEXING_HIGH"
    }
}
```

注意，简单分享链接不含有 `socks5Port` 等必要的客户端配置。因此，在全新的设备上导入简单分享链接会失败。
