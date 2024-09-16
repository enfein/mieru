# 見える / mieru

[![Build Status](https://github.com/enfein/mieru/actions/workflows/ci.yaml/badge.svg)](https://github.com/enfein/mieru/actions/workflows/ci.yaml)
[![Releases](https://img.shields.io/github/release/enfein/mieru/all.svg?style=flat)](https://github.com/enfein/mieru/releases)
[![Downloads](https://img.shields.io/github/downloads/enfein/mieru/total.svg?style=flat)](https://github.com/enfein/mieru/releases)
[![LICENSE](https://img.shields.io/github/license/enfein/mieru.svg?style=flat)](./LICENSE)

[中文文档](./README.zh_CN.md)

mieru is a secure, hard to classify, hard to probe, TCP or UDP protocol-based socks5 / HTTP / HTTPS network proxy software.

The mieru proxy software suite consists of two parts, a client software called mieru, and a proxy server software called mita.

## Protocol

The principle of mieru is similar to shadowsocks / v2ray etc. It creates an encrypted channel between the client and the proxy server outside the firewall. GFW cannot decrypt the encrypted transmission and cannot determine the destination you end up visiting, so it has no choice but to let you go.

For an explanation of the mieru protocol, see [mieru Proxy Protocol](./docs/protocol.md).

## Features

1. mieru uses a high-strength XChaCha20-Poly1305 encryption algorithm that generates encryption keys based on username, password and system time.
1. mieru implements complete encryption of all transmitted content between the client and the proxy server, without transmitting any plaintext information.
1. When mieru sends a packet, it is padded with random bytes at the end. Even when the same content is transmitted, the packet size varies.
1. When using the UDP transport protocol, mieru does not require a handshake between client and server.
1. When the server can not decrypt the data sent by the client, no content is returned. it is difficult for GFW to discover the mieru service through active probing.
1. mieru supports multiple users sharing a single proxy server.
1. mieru supports IPv4 and IPv6.
1. mieru provides socks5, HTTP and HTTPS proxy.
1. The server software supports socks5 outbound (proxy chain).
1. The client software supports Windows, Mac OS, Linux and Android. Android clients include
   - [NekoBox](https://github.com/MatsuriDayo/NekoBoxForAndroid) version 1.3.1 or above, with [mieru plugin](https://github.com/enfein/NekoBoxPlugins).
   - [Exclave](https://github.com/dyhkwong/Exclave), with [mieru plugin](https://github.com/dyhkwong/Exclave/releases?q=mieru-plugin).
1. If you need advanced features like global proxy or customized routing rules, you can use mieru as the backend of a proxy platform such as [Xray](https://github.com/XTLS/Xray-core) and [sing-box](https://github.com/SagerNet/sing-box).

## User Guide

1. [Server Installation & Configuration](./docs/server-install.md)
1. [Client Installation & Configuration](./docs/client-install.md)
1. [Client Installation & Configuration - OpenWrt](./docs/client-install-openwrt.md)
1. [Maintenance & Troubleshooting](./docs/operation.md)
1. [Security Guide](./docs/security.md)
1. [Compilation](./docs/compile.md)

## Share

If you think this software is helpful, please share to your friends. Thanks!

## Contact Us

Use GitHub issue.

## License

Use of this software is subject to the GPL-3 license.
