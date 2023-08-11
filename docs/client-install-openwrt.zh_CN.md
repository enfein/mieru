# 客户端安装与配置 - OpenWrt

OpenWrt 是一个极简 Linux 环境，在 OpenWrt 环境运行 mieru 客户端软件需要额外的步骤。请参照[客户端安装与配置](https://github.com/enfein/mieru/blob/main/docs/client-install.zh_CN.md)，同时按照这里的提示安装和配置 mieru 客户端。

## 下载 mieru 客户端

在下载 mieru 客户端软件后，请将解压缩后的 mieru 可执行文件复制到 OpenWrt 系统的 `/usr/bin/` 目录下。

## 修改客户端的设置

为了能让其他设备连接至 OpenWrt 上的代理客户端，必须将 `socks5ListenLAN` 设置成 `true`。如果启动 HTTP / HTTPS 代理，`httpProxyListenLAN` 也必须设置成 `true`。

在 OpenWrt 中，请勿使用 `mieru apply config <FILE>` 和 `mieru describe config` 指令。请将客户端配置直接保存至 `/etc/mieru_client_config.json` 文件。

另外，建议将日志等级 `loggingLevel` 设置成 `ERROR` 防止日志打满硬盘。

## 启动客户端

需要启动和停止客户端时，请勿使用 `mieru start` 和 `mieru stop` 指令。请将项目根目录下的 `configs/examples/etc_initd_mieru` 文件复制并保存至 `/etc/init.d/mieru` 文件，并赋予可执行权限。此后，mieru 客户端将在 OpenWrt 开机后自行启动。

如果需要手动启动或停止 mieru 客户端，可以使用指令 `/etc/init.d/mieru start` 和 `/etc/init.d/mieru stop`。
