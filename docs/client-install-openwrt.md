# Client Installation & Configuration - OpenWrt

OpenWrt is a minimal Linux distribution, and additional steps are required to run the mieru client in an OpenWrt environment. Please refer to the [Client Installation & Configuration](https://github.com/enfein/mieru/blob/main/docs/client-install.md) guide, and follow the instructions here to install and configure the mieru client.

## Download mieru client

After downloading the mieru client software, please copy the extracted mieru executable file to the `/usr/bin/` directory on the OpenWrt system.

## Modify proxy client settings

To allow other devices to connect to the proxy client on OpenWrt, you must set `socks5ListenLAN` to `true`. If you enable HTTP/HTTPS proxy, `httpProxyListenLAN` must also be set to `true`.

In OpenWrt, do not use the `mieru apply config <FILE>` and `mieru describe config` commands. Please save the client configuration directly to the `/etc/mieru_client_config.json` file.

Additionally, it is recommended to set the log level `loggingLevel` to `ERROR` to prevent logs from filling up the disk.

## Start proxy client

To start and stop the client, please do not use the `mieru start` and `mieru stop` commands. Please copy the `configs/examples/etc_initd_mieru` file from the project root directory to the `/etc/init.d/mieru` file, and grant it executable permission. Afterwards, the mieru client will automatically start when OpenWrt boots up.

If you need to manually start or stop the mieru client, you can use the commands `/etc/init.d/mieru start` and `/etc/init.d/mieru stop`.
