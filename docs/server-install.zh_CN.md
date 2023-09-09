# 服务器安装与配置

我们建议有经济实力的用户选择亚马逊（Lightsail 除外）、微软、谷歌等国外大型云服务提供商，一般不会被封锁 IP。请勿使用来路不明的云计算服务。代理服务器占用的 CPU 和内存资源很少，最终网络速度主要取决于服务器的网络带宽和线路质量。

代理服务器软件 mita 需要运行在 Linux 系统中。我们提供了 debian 和 RPM 安装包，便于用户在 Debian / Ubuntu 和 Fedora / CentOS / Red Hat Enterprise Linux 系列发行版中安装 mita。

在安装和配置开始之前，先通过 SSH 连接到服务器，再执行下面的指令。

## 下载 mita 安装包

```sh
# Debian / Ubuntu - X86_64
curl -LSO https://github.com/enfein/mieru/releases/download/v1.15.1/mita_1.15.1_amd64.deb

# Debian / Ubuntu - ARM 64
curl -LSO https://github.com/enfein/mieru/releases/download/v1.15.1/mita_1.15.1_arm64.deb

# Fedora / CentOS / Red Hat Enterprise Linux - X86_64
curl -LSO https://github.com/enfein/mieru/releases/download/v1.15.1/mita-1.15.1-1.x86_64.rpm

# Fedora / CentOS / Red Hat Enterprise Linux - ARM 64
curl -LSO https://github.com/enfein/mieru/releases/download/v1.15.1/mita-1.15.1-1.aarch64.rpm
```

如果上述链接被墙，请翻墙后使用浏览器从 GitHub Releases 页面下载安装。

## 安装 mita 软件包

```sh
# Debian / Ubuntu - X86_64
sudo dpkg -i mita_1.15.1_amd64.deb

# Debian / Ubuntu - ARM 64
sudo dpkg -i mita_1.15.1_arm64.deb

# Fedora / CentOS / Red Hat Enterprise Linux - X86_64
sudo rpm -Uvh --force mita-1.15.1-1.x86_64.rpm

# Fedora / CentOS / Red Hat Enterprise Linux - ARM 64
sudo rpm -Uvh --force mita-1.15.1-1.aarch64.rpm
```

## 赋予当前用户操作 mita 的权限，需要重启服务器使此设置生效

```sh
sudo usermod -a -G mita $USER

sudo reboot
```

## 重启后，使用 SSH 重新连接到服务器，检查 mita 守护进程的状态

```sh
systemctl status mita
```

如果输出中包含 `active (running)` ，表示 mita 守护进程已经开始运行。通常情况下，mita 会在服务器启动后自动开始运行。

## 查询 mita 的工作状态

```sh
mita status
```

如果刚刚完成安装，此时的输出为 `mieru server status is "IDLE"`，表示 mita 还没有开始监听来自 mieru 客户端的请求。

## 修改代理服务器设置

mieru 代理支持 TCP 和 UDP 两种不同的传输协议。要了解协议之间的差别，请阅读 [mieru 代理协议](https://github.com/enfein/mieru/blob/main/docs/protocol.zh_CN.md)。本教程以 TCP 协议为例进行讲解。如果要使用 UDP 协议，将所有配置文件中的 `TCP` 更换为 `UDP` 即可。

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
            "protocol": "TCP"
        }
    ],
    "users": [
        {
            "name": "<username@example.com>",
            "password": "<your-password>"
        }
    ],
    "loggingLevel": "INFO",
    "mtu": 1400
}
```

请将这个模板下载或复制到你的服务器中，用文本编辑器打开，并修改如下的内容：

1. `portBindings` -> `port` 属性是 mita 监听的 TCP 或 UDP 端口号，请指定一个从 1025 到 65535 之间的值。**请确保防火墙允许使用该端口进行通信。**
2. 在 `users` -> `name` 属性中填写用户名。
3. 在 `users` -> `password` 属性中填写该用户的密码。
4. `mtu` 属性是使用 UDP 代理协议时，数据链路层最大的载荷大小。默认值是 1400，可以选择 1280 到 1500 之间的值。

除此之外，mita 可以监听多个不同的端口。我们建议在服务器和客户端配置中使用多个端口，以有效对抗封锁。

如果你想把代理线路分享给别人使用，也可以创建多个不同的用户。

下面是服务器配置文件的一个例子。

```js
{
    "portBindings": [
        {
            "port": 2012,
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

查询工作状态，这里如果返回 `mieru server status is "RUNNING"`，说明代理服务正在运行，可以开始相应客户端的请求了。

如果想要停止代理服务，请使用指令

```sh
mita stop
```

注意，每次使用 `mita apply config <FILE>` 修改设置后，需要用 `mita stop` 和 `mita start` 重启代理服务，才能使新设置生效。

启动代理服务后，请继续进行[客户端安装与配置](https://github.com/enfein/mieru/blob/main/docs/client-install.zh_CN.md)。

## 【可选】安装 NTP 网络时间同步服务

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
