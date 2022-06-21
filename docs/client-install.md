# 客户端安装与配置

## 下载 mieru 安装包

mieru 客户端软件支持 Windows，Mac OS 和 Linux 系统。用户可以在 GitHub Releases 页面用浏览器下载。如果 GitHub 没有被墙，也可以使用 Linux / Mac 终端或 Windows PowerShell 下载。

```powershell
# Windows PowerShell
Invoke-WebRequest https://github.com/enfein/mieru/releases/download/v1.5.0/mieru_1.5.0_windows_amd64.zip -OutFile mieru_1.5.0_windows_amd64.zip
```

```sh
# Mac OS (Intel CPU)
curl -LSO https://github.com/enfein/mieru/releases/download/v1.5.0/mieru_1.5.0_darwin_amd64.tar.gz

# Mac OS (ARM CPU)
curl -LSO https://github.com/enfein/mieru/releases/download/v1.5.0/mieru_1.5.0_darwin_arm64.tar.gz
```

解压缩之后，就可以得到可执行文件 `mieru.exe` 或 `mieru`。

如果你的客户端操作系统是 Linux，可以使用下面的指令下载和安装 mieru。如需下载 ARM 架构的安装包，将链接中的 `amd64` 替换成 `arm64`，`x86_64` 替换成 `aarch64` 即可。

```sh
# Debian / Ubuntu
curl -LSO https://github.com/enfein/mieru/releases/download/v1.5.0/mieru_1.5.0_amd64.deb
sudo dpkg -i mieru_1.5.0_amd64.deb

# Fedora / CentOS / Red Hat Enterprise Linux
curl -LSO https://github.com/enfein/mieru/releases/download/v1.5.0/mieru-1.5.0-1.x86_64.rpm
sudo rpm -Uvh --force mieru-1.5.0-1.x86_64.rpm

# Others
curl -LSO https://github.com/enfein/mieru/releases/download/v1.5.0/mieru_1.5.0_linux_amd64.tar.gz
tar -zxvf mieru_1.5.0_linux_amd64.tar.gz
```

接下来，将 mieru 可执行文件移动或添加至系统路径 `PATH`。这样，输入指令时就不需要指定 mieru 可执行文件的位置了。如果使用了 debian 或 RPM 安装包，那么不需要执行这一步。

## 修改客户端的设置

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
                            "protocol": "TCP"
                        }
                    ]
                }
            ],
            "mtu": 1400
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
3. 在 `profiles` -> `servers` -> `ipAddress` 属性中，填写代理服务器的公网地址。支持 IPv4 和 IPv6 地址。
4. 如果你为代理服务器注册了域名，请在 `profiles` -> `servers` -> `domainName` 中填写域名。否则，请勿修改这个属性。
5. 在 `profiles` -> `servers` -> `portBindings` -> `port` 中填写 mita 监听的 UDP 端口号。这个端口号必须与代理服务器中的设置相同。
6. 请为 `rpcPort` 属性指定一个从 1025 到 65535 之间的数值。**请确保防火墙允许使用该端口进行通信。**
7. 请为 `socks5Port` 属性指定一个从 1025 到 65535 之间的数值。该端口不能与 `rpcPort` 相同。**请确保防火墙允许使用该端口进行通信。**

如果你安装了多台代理服务器，或者一台服务器监听多个端口，可以把它们都添加到客户端设置中。每次发起新的连接时，mieru 会随机选取其中的一台服务器和一个端口。**如果使用了多台服务器，请确保每一台服务器都启动了 mita 代理服务。**

上述设置的一个示例如下

```js
            "servers": [
                {
                    "ipAddress": "1.1.1.1",
                    "domainName": "",
                    "portBindings": [
                        {
                            "port": 1110,
                            "protocol": "TCP"
                        },
                        {
                            "port": 1111,
                            "protocol": "TCP"
                        }
                    ]
                },
                {
                    "ipAddress": "2.2.2.2",
                    "domainName": "",
                    "portBindings": [
                        {
                            "port": 2222,
                            "protocol": "TCP"
                        }
                    ]
                }
            ]
```

我们在 `configs/examples/client_config.json` 提供了配置文件的一个例子，仅供参考。

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

如果输出显示 `mieru client is started, listening to localhost:xxxx`，表示 mieru 客户端已经开始运行。

mieru 客户端不会与系统一同启动。在重新启动计算机后，需要手动使用 `mieru start` 指令启动客户端。

**Windows 用户请注意，在命令提示符或 Powershell 中使用 `mieru start` 指令启动客户端之后，请勿关闭命令提示符或 Powershell 窗口。关闭窗口将导致 mieru 客户端停止运行。**

如果需要停止 mieru 客户端，请输入指令

```sh
mieru stop
```

注意，每次使用 `mieru apply config <FILE>` 修改设置后，需要用 `mieru stop` 和 `mieru start` 重启客户端，才能使新设置生效。

## 配置浏览器

Chrome / Firefox 等浏览器可以通过安装插件，使用 socks5 代理访问墙外的网站。关于 socks5 代理的地址，请填写 `localhost:xxxx`，其中 `xxxx` 是客户端设置中 `socks5Port` 的值。这个地址在调用 `mieru start` 指令时也会打印出来。

mieru 不使用 socks5 用户名和密码进行身份验证。

关于在 Tor 浏览器中配置 socks5 代理，参见[翻墙安全指南](https://github.com/enfein/mieru/blob/main/docs/security.md)。
