# 运营维护与故障排查

## 关于配置文件存放地址

代理服务器软件 mita 的配置存放在 `/etc/mita/server.conf.pb`。这是一个以 protocol buffer 格式存储的二进制文件。为保护用户信息，mita 不会存储用户密码的明文，只会存储其校验码。

客户端软件 mieru 的配置文件同样是一个二进制文件。它在不同操作系统中存储的位置如下表所示

| Operating System | Configuration File Path |
| :----: | :----: |
| Linux | /home/USERNAME/.config/mieru/client.conf.pb |
| Mac OS | /Users/USERNAME/Library/Application Support/mieru/client.conf.pb |
| Windows | C:\Users\USERNAME\AppData\Roaming\mieru\client.conf.pb |

## 查看代理服务器 mita 的日志

用户可以使用下面的指令打印 mita 的全部日志

```sh
journalctl -u mita --no-pager
```

只有当你登录服务器的用户属于 `adm` 或 `systemd-journal` 用户组时才可以查看日志。必要时可以使用 `sudo` 提升权限。

```sh
sudo journalctl -u mita --no-pager
```

## 查看客户端 mieru 的日志

客户端 mieru 的日志存放位置如下表所示

| Operating System | Configuration File Path |
| :----: | :----: |
| Linux | /home/USERNAME/.cache/mieru/ |
| Mac OS | /Users/USERNAME/Library/Caches/mieru/ |
| Windows | C:\Users\USERNAME\AppData\Local\mieru\ |

每个日志文件的格式为 `yyyyMMdd_HHmm_PID.log`，其中 `yyyyMMdd_HHmm` 是 mieru 进程启动的时间。每次重启 mieru 会生成一个新的日志文件。当日志文件的数量太多时，旧的文件会被自动删除。

## 打开和关闭调试日志

mieru / mita 在默认的日志等级下，打印的信息非常少，不包含 IP 地址、端口号等敏感信息。如果需要诊断单个网络连接，则需要打开调试日志（debug log）。

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

## 判断客户端与服务器之间的连接是否正常

如果服务器仅有一个客户端，判断连接是否正常，只需要查看服务器日志。例如，在下面的日志示例中

```
INFO [metrics - connections] ActiveOpens=0 CurrEstablished=0 MaxConn=0 PassiveOpens=0
INFO [metrics - KCP segments] EarlyRetransSegs=0 FastRetransSegs=0 InSegs=0 LostSegs=0 OutOfWindowSegs=0 OutSegs=0 RepeatSegs=0 RetransSegs=0
```

如果 `InSegs` 的值不为 0，说明服务器成功解密了客户端发送的数据包。如果 `OutSegs` 的值不为 0，说明服务器向客户端返回了数据包。

## 故障诊断与排查

mieru 为了防止 GFW 主动探测，增强了服务器端的隐蔽性，但是也增加了调试的难度。如果你的客户端和服务器之间无法建立连接，从以下几个排查方向入手可能会有所帮助。

1. 服务器是否在正常工作？用 `ping` 指令确认客户端是否可以到达服务器。
2. mita 代理服务是否已经启动？用 `mita status` 指令查看。
3. mieru 客户端是否已经启动？用 `mieru status` 指令查看。
4. 客户端和服务器的设置中，端口号是否相同？用户名是否相同？密码是否相同？用 `mieru describe config` 和 `mita describe config` 查看。
