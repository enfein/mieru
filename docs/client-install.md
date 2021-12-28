# 客户端安装与配置

**【1】下载 mieru 安装包**

mieru 客户端软件支持 Windows，Mac OS 和 Linux 系统。用户可以在 GitHub Releases 页面用浏览器下载，如果 GitHub 没有被墙，也可以使用 Linux / Mac 终端或 Windows PowerShell 下载。

```powershell
# Windows PowerShell
Invoke-WebRequest https://github.com/enfein/mieru/releases/download/v1.0.1/mieru_1.0.1_windows_amd64.zip -OutFile mieru_1.0.1_windows_amd64.zip
```

```sh
# Mac OS
curl -LSO https://github.com/enfein/mieru/releases/download/v1.0.1/mieru_1.0.1_darwin_amd64.tar.gz
```

```sh
# Linux
curl -LSO https://github.com/enfein/mieru/releases/download/v1.0.1/mieru_1.0.1_linux_amd64.tar.gz
```

解压缩之后，就可以得到可执行文件 `mieru` 或 `mieru.exe`。

**【可选】下载和安装 mieru debian 安装包**

如果你的客户端操作系统是 Debian/Ubuntu 系列 Linux 发行版，可以使用下面的指令下载和安装 mieru。

```sh
curl -LSO https://github.com/enfein/mieru/releases/download/v1.0.1/mieru_1.0.1_amd64.deb

sudo dpkg -i mieru_1.0.1_amd64.deb
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

**【5】配置浏览器**

Chrome / Firefox 等浏览器可以通过安装插件，使用 socks5 代理访问墙外的网站。关于 socks5 代理的地址，请填写 `localhost:xxxx`，其中 `xxxx` 是客户端设置中 `socks5Port` 的值。这个地址在调用 `mieru start` 指令时也会打印出来。

mieru 不使用 socks5 用户名和密码进行身份验证。
