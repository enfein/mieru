# 运营维护与故障排查

## 查看当前客户端与服务器之间的连接

可以在客户端运行 `mieru get connections` 指令查看当前客户端与服务器之间的连接。该指令输出的一个示例如下。

```
SessionID   Protocol  Local       Remote             State        RecvQ+Buf  SendQ+Buf  LastRecv  LastSend
3078661580  UDP       [::]:34453  12.34.123.45:5852  ESTABLISHED  0+0        0+0        0s (31)   0s (28)
3408448183  UDP       [::]:34453  12.34.123.45:5852  ESTABLISHED  0+0        0+0        3s (22)   3s (21)
```

类似的，可以在服务器运行 `mita get connections` 指令查看当前服务器与所有客户端之间的连接。

## 判断客户端与服务器之间的连接是否正常

要确定连接是否正常，可以查看客户端指标。要获取指标，请运行命令 `mieru get metrics`。在下面的例子中，

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
    },
    "HTTP proxy": {
        "ConnErrors": 2,
        "Requests": 130,
        "SchemeErrors": 0
    },
    "replay": {
        "KnownSession": 0,
        "NewSession": 0,
        "NewSessionDecrypted": 0
    },
    "socks5": {
        "ConnectionRefusedErrors": 0,
        "DNSResolveErrors": 0,
        "HandshakeErrors": 1,
        "HostUnreachableErrors": 0,
        "NetworkUnreachableErrors": 0,
        "UDPAssociateErrors": 0,
        "UnsupportedCommandErrors": 0
    },
    "socks5 UDP associate": {
        "DownloadBytes": 0,
        "DownloadPackets": 0,
        "UploadBytes": 0,
        "UploadPackets": 0
    },
    "traffic": {
        "DownloadBytes": 257341785,
        "OutputPaddingBytes": 511223,
        "UploadBytes": 1254325
    },
    "underlay": {
        "ActiveOpens": 16,
        "CurrEstablished": 7,
        "MaxConn": 11,
        "PassiveOpens": 0,
        "UnderlayMalformedUDP": 0,
        "UnsolicitedUDP": 0
    }
}
```

如果 `connections` -> `CurrEstablished` 的值不为 0，说明此刻客户端与服务器之间有活跃的连接。如果 `cipher - client` -> `DirectDecrypt` 的值不为 0，说明客户端曾经成功解密了服务器返回的数据包。

## 故障诊断与排查

mieru 为了防止 GFW 主动探测，增强了服务器端的隐蔽性，但是也增加了调试的难度。如果你的客户端和服务器之间无法建立连接，从以下几个排查方向入手可能会有所帮助。

1. 服务器是否在正常工作？用 `ping` 指令确认客户端是否可以到达服务器。
2. mita 代理服务是否已经启动？用 `mita status` 指令查看。
3. mieru 客户端是否已经启动？用 `mieru status` 指令查看。
4. 客户端和服务器的设置中，端口号是否相同？用户名是否相同？密码是否相同？用 `mieru describe config` 和 `mita describe config` 查看。
5. 打开客户端和服务器的调试日志，查看具体的网络连接情况。

如果未能解决问题，可以提交 GitHub Issue 联系开发者。

## 配置文件存放地址

代理服务器软件 mita 的配置存放在 `/etc/mita/server.conf.pb`。这是一个以 protocol buffer 格式存储的二进制文件。为保护用户信息，mita 不会存储用户密码的明文，只会存储其校验码。

客户端软件 mieru 的配置文件同样是一个二进制文件。它在不同操作系统中存储的位置如下表所示

| 操作系统 | 配置文件位置 | 示例 |
| :----: | :----: | :----: |
| Linux | $HOME/.config/mieru/client.conf.pb | /home/enfein/.config/mieru/client.conf.pb |
| Mac OS | $HOME/Library/Application Support/mieru/client.conf.pb | /Users/enfein/Library/Application Support/mieru/client.conf.pb |
| Windows | %USERPROFILE%\AppData\Roaming\mieru\client.conf.pb | C:\Users\enfein\AppData\Roaming\mieru\client.conf.pb |

## 查看代理服务器 mita 的日志

用户可以使用下面的指令打印 mita 的近期日志

```sh
sudo journalctl -u mita -xe --no-pager
```

## 查看客户端 mieru 的日志

客户端 mieru 的日志存放位置如下表所示

| 操作系统 | 日志文件位置 | 示例 |
| :----: | :----: | :----: |
| Linux | $HOME/.cache/mieru/ or $XDG_CACHE_HOME/mieru/ | /home/enfein/.cache/mieru/ |
| Mac OS | $HOME/Library/Caches/mieru/ | /Users/enfein/Library/Caches/mieru/ |
| Windows | %USERPROFILE%\AppData\Local\mieru\ | C:\Users\enfein\AppData\Local\mieru\ |

每个日志文件的格式为 `yyyyMMdd_HHmm_PID.log`，其中 `yyyyMMdd_HHmm` 是 mieru 进程启动的时间，`PID` 是进程号码。每次重启 mieru 会生成一个新的日志文件。当日志文件的数量太多时，旧的文件会被自动删除。

## 打开和关闭调试日志

mieru / mita 在默认的日志等级下，打印的信息非常少，不包含 IP 地址、端口号等敏感信息。如果需要诊断单个网络连接，则需要打开调试日志（debug log）。

本项目在 `configs/templates` 目录下提供了快速打开和关闭调试日志的配置文件。请将它们下载到服务器或本地计算机上，并输入以下指令。注意，改变 mieru 的设置后，需要重新启动服务才能生效。

```sh
# enable debug logging
mita apply config server_enable_debug_logging.json

# OR disable debug logging
mita apply config server_disable_debug_logging.json

# This will not interrupt traffic
mita reload
```

```sh
# enable debug logging
mieru apply config client_enable_debug_logging.json

# OR disable debug logging
mieru apply config client_disable_debug_logging.json

mieru stop

mieru start
```

所有支持的日志等级包括 `FATAL`, `ERROR`, `WARN`, `INFO`, `DEBUG` 和 `TRACE`。

## 修改指标日志输出间隔

mieru / mita 每隔 10 分钟通过日志打印指标信息。如果想要调整这个输出的间隔，请使用下面的设置：

```js
{
    "advancedSettings": {
        "metricsLoggingInterval": "1h30m"
    }
}
```

其中可以使用单位

- 秒 `s`
- 分钟 `m`
- 小时 `h`

以及他们的组合。

## 查看用户网络流量

可以在服务器运行 `mita get users` 和 `mita get quotas` 指令，查看每个用户最近的活跃时间和消耗的网络流量。

```
$ mita get users
User  LastActive            1DayDownload  1DayUpload  30DaysDownload  30DaysUpload
abcd  2025-04-23T01:02:03Z  938.1MiB      12.9MiB     4.0GiB          31.8MiB

$ mita get quotas
User  Days  Limit    Usage
abcd  1     10.0GiB  951.1MiB
abcd  7     40.0GiB  4.0GiB
```

## 环境变量

如有必要，用户可以使用环境变量控制服务器和客户端的行为。

- `MITA_CONFIG_JSON_FILE` 从这个路径加载 JSON 格式的服务器配置文件。
- `MITA_CONFIG_FILE` 从这个路径加载 protocol buffer 格式的服务器配置文件。
- `MIERU_CONFIG_JSON_FILE` 从这个路径加载 JSON 格式的客户端配置文件。
- `MIERU_CONFIG_FILE` 从这个路径加载 protocol buffer 格式的客户端配置文件。
- `MITA_LOG_NO_TIMESTAMP` 这个值非空时，服务器日志不打印时间戳。因为 journald 已经提供了时间戳，我们默认开启这项设置，以避免打印重复的时间戳。
- `MITA_UDS_PATH` 使用这个路径创建服务器 UNIX domain socket 文件。默认的路径是 `/var/run/mita/mita.sock`。
- `MITA_INSECURE_UDS` 这个值非空时，不强制修改服务器 UNIX domain socket 文件 `/var/run/mita/mita.sock` 的用户和访问权限。这个设置可以用于某些非常受限（例如不能创建新用户）的系统中。

## 在前台运行客户端

在下面几种情况下，用户可能希望在前台运行 mieru 客户端：

- 同时运行多个 mieru 客户端进程。
- 让 mieru 客户端随系统一同启动。
- 快速调试 mieru 客户端配置。

在 Linux 系统下，假设 mieru 客户端配置文件的路径是 `/etc/mieru_client_config.json`，用户可以使用下面的指令在前台运行客户端：

```sh
MIERU_CONFIG_JSON_FILE=/etc/mieru_client_config.json mieru run
```

此时，mieru 客户端的日志会直接打印至终端。按下 Ctrl+C 退出。

## 关闭客户端自动检查更新

启动 mieru 客户端时，每隔几天，会自动检查更新。如果想关闭自动检查更新，请添加如下的客户端配置：

```json
{
    "advancedSettings": {
        "noCheckUpdate": true
    }
}
```

## 重置服务器指标

服务器指标存储在文件 `/var/lib/mita/metrics.pb` 中。即便服务器重启，也可以通过 `mita get metrics`，`mita get users` 和 `mita get quotas` 指令读取累积的指标。如果想重置指标，可以运行下面的命令：

```sh
sudo systemctl stop mita && sudo rm -f /var/lib/mita/metrics.pb && sudo systemctl start mita
```
