# 服务器安装与配置

我们建议有经济实力的用户选择亚马逊和微软等国外大型云服务提供商，一般不会被封锁 IP。请勿使用国内公司或来路不明的云计算服务。代理服务器占用的 CPU 和内存资源很少，最终网络速度主要取决于服务器的网络带宽和线路质量。

代理服务器软件 mita 需要运行在 Linux 系统中。我们提供了 debian 和 RPM 安装包，便于用户在 Debian / Ubuntu 和 Fedora / CentOS / Red Hat Enterprise Linux 系列发行版中安装 mita。

在安装和配置开始之前，先通过 SSH 连接到服务器，再执行下面的指令。

**【1】下载 mita 安装包**

```sh
# Debian / Ubuntu
curl -LSO https://github.com/enfein/mieru/releases/download/v1.3.0/mita_1.3.0_amd64.deb

# Fedora / CentOS / Red Hat Enterprise Linux
curl -LSO https://github.com/enfein/mieru/releases/download/v1.3.0/mita-1.3.0-1.x86_64.rpm
```

如果上述链接被墙，请翻墙后使用浏览器从 GitHub Releases 页面下载安装包。

**【2】安装 mita 软件包**

```sh
# Debian / Ubuntu
sudo dpkg -i mita_1.3.0_amd64.deb

# Fedora / CentOS / Red Hat Enterprise Linux
sudo rpm -Uvh --force mita-1.3.0-1.x86_64.rpm
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

除此之外，mita 可以监听多个不同的端口，例如

```js
    "portBindings": [
        {
            "port": 1111,
            "protocol": "UDP"
        },
        {
            "port": 2222,
            "protocol": "UDP"
        }
    ],
```

如果你想把代理线路分享给别人使用，也可以创建多个不同的用户，例如

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

我们在 `configs/examples/server_config.json` 提供了配置文件的一个例子，仅供参考。

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

为了保证服务器系统时间是精确的，我们建议用户安装 NTP 网络时间服务。在许多 Linux 发行版中，安装 NTP 只需要一行指令

```sh
# Debian / Ubuntu
sudo apt-get install ntp

# Fedora
sudo dnf install ntp

# CentOS / Red Hat Enterprise Linux
sudo yum install ntp
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
