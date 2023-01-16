# 运营维护与故障排查

## 配置文件存放地址

代理服务器软件 mita 的配置存放在 `/etc/mita/server.conf.pb`。这是一个以 protocol buffer 格式存储的二进制文件。为保护用户信息，mita 不会存储用户密码的明文，只会存储其校验码。

客户端软件 mieru 的配置文件同样是一个二进制文件。它在不同操作系统中存储的位置如下表所示

| Operating System | Configuration File Path |
| :----: | :----: |
| Linux | $HOME/.config/mieru/client.conf.pb |
| Mac OS | /Users/USERNAME/Library/Application Support/mieru/client.conf.pb |
| Windows | C:\Users\USERNAME\AppData\Roaming\mieru\client.conf.pb |

## 查看代理服务器 mita 的日志

用户可以使用下面的指令打印 mita 的全部日志

```sh
sudo journalctl -u mita -xe --no-pager
```

## 查看客户端 mieru 的日志

客户端 mieru 的日志存放位置如下表所示

| Operating System | Configuration File Path |
| :----: | :----: |
| Linux | $HOME/.cache/mieru/ or $XDG_CACHE_HOME/mieru/ |
| Mac OS | /Users/USERNAME/Library/Caches/mieru/ |
| Windows | C:\Users\USERNAME\AppData\Local\mieru\ |

每个日志文件的格式为 `yyyyMMdd_HHmm_PID.log`，其中 `yyyyMMdd_HHmm` 是 mieru 进程启动的时间，`PID` 是进程号码。每次重启 mieru 会生成一个新的日志文件。当日志文件的数量太多时，旧的文件会被自动删除。

## 打开和关闭调试日志

mieru / mita 在默认的日志等级下，打印的信息非常少，不包含 IP 地址、端口号等敏感信息。如果需要诊断单个网络连接，则需要打开调试日志（debug log）。

本项目在 `configs/templates` 目录下提供了快速打开和关闭调试日志的配置文件。请将它们下载到服务器或本地计算机上，并输入以下指令。注意，改变 mieru / mita 的设置后，需要重新启动服务才能生效。

```sh
# enable debug logging
mita apply config server_enable_debug_logging.json

# OR disable debug logging
mita apply config server_disable_debug_logging.json

mita stop

mita start
```

```sh
# enable debug logging
mieru apply config client_enable_debug_logging.json

# OR disable debug logging
mieru apply config client_disable_debug_logging.json

mieru stop

mieru start
```

## 判断客户端与服务器之间的连接是否正常

判断连接是否正常，只需要查看客户端日志。例如，在下面的日志中

```
INFO [metrics]
INFO [metrics - cipher - client] DirectDecrypt=7241 FailedDirectDecrypt=0
INFO [metrics - connections] ActiveOpens=42 CurrEstablished=20 MaxConn=26 PassiveOpens=0
INFO [metrics - errors] KCPInErrors=0 KCPReceiveErrors=0 KCPSendErrors=0 TCPReceiveErrors=0 TCPSendErrors=0 UDPInErrors=0
INFO [metrics - KCP] BytesReceived=8115713 BytesSent=113291 FastRetransSegs=0 InSegs=7168 LostSegs=0 OutOfWindowSegs=166 OutSegs=7168 RepeatSegs=0 RetransSegs=0
INFO [metrics - replay] KnownSession=0 NewSession=0
INFO [metrics - socks5] ConnectionRefusedErrors=0 DNSResolveErrors=0 HandshakeErrors=0 HostUnreachableErrors=0 NetworkUnreachableErrors=0 UDPAssociateErrors=0 UDPAssociateInBytes=0 UDPAssociateInPkts=0 UDPAssociateOutBytes=0 UDPAssociateOutPkts=0 UnsupportedCommandErrors=0
INFO [metrics - traffic] InBytes=8645909 OutBytes=941739 OutPaddingBytes=444693
```

如果 `CurrEstablished` 的值不为 0，说明此刻客户端与服务器之间有活跃的连接。如果 `DirectDecrypt` 的值不为 0，说明客户端曾经成功解密了服务器返回的数据包。

## 故障诊断与排查

mieru 为了防止 GFW 主动探测，增强了服务器端的隐蔽性，但是也增加了调试的难度。如果你的客户端和服务器之间无法建立连接，从以下几个排查方向入手可能会有所帮助。

1. 服务器是否在正常工作？用 `ping` 指令确认客户端是否可以到达服务器。
2. mita 代理服务是否已经启动？用 `mita status` 指令查看。
3. mieru 客户端是否已经启动？用 `mieru status` 指令查看。
4. 客户端和服务器的设置中，端口号是否相同？用户名是否相同？密码是否相同？用 `mieru describe config` 和 `mita describe config` 查看。
5. 打开客户端和服务器的调试日志，查看具体的网络连接情况。

如果未能解决问题，可以提交 GitHub Issue 联系开发者。
