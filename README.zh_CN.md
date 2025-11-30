# 見える / mieru

[![Build Status](https://github.com/enfein/mieru/actions/workflows/ci.yaml/badge.svg)](https://github.com/enfein/mieru/actions/workflows/ci.yaml)
[![Releases](https://img.shields.io/github/release/enfein/mieru/all.svg?style=flat)](https://github.com/enfein/mieru/releases)
[![Downloads](https://img.shields.io/github/downloads/enfein/mieru/total.svg?style=flat)](https://github.com/enfein/mieru/releases)
[![LICENSE](https://img.shields.io/github/license/enfein/mieru.svg?style=flat)](./LICENSE)

mieru【見える】是一款安全的、无流量特征、难以主动探测的，基于 TCP 或 UDP 协议的 socks5 / HTTP / HTTPS 网络代理软件。

mieru 代理软件由称为 mieru【見える】的客户端软件和称为 mita【見た】的代理服务器软件这两部分组成。

## 特性

1. 提供 socks5, HTTP 和 HTTPS 代理接口。
1. 不使用 TLS 协议，无需注册域名和架设伪装站点。
1. 使用高强度的 XChaCha20-Poly1305 加密算法，基于用户名、密码和系统时间生成密钥。
1. 使用随机填充与重放攻击检测阻止 GFW 探测 mieru 服务。
1. 支持多个用户共享代理服务器。
1. 支持 IPv4 和 IPv6。

## 第三方服务器软件

- [mihomo](https://github.com/MetaCubeX/mihomo)

## 第三方客户端软件

- 桌面 (Windows, MacOS, Linux)
  - [Clash Verge Rev](https://www.clashverge.dev/)
  - [Mihomo Party](https://mihomo.party/)
  - [NekoBox => qr243vbi_Box](https://qr243vbi.github.io/nekobox/#/)
- 安卓
  - [ClashMetaForAndroid](https://github.com/MetaCubeX/ClashMetaForAndroid)
  - [ClashMi](https://clashmi.app/)
  - [Exclave](https://github.com/dyhkwong/Exclave) + [mieru plugin](https://github.com/dyhkwong/Exclave/releases?q=mieru-plugin)
  - [husi](https://github.com/xchacha20-poly1305/husi) + [mieru plugin](https://github.com/xchacha20-poly1305/husi/releases?q=plugin-mieru)
  - [Karing](https://karing.app/)
  - [NekoBoxForAndroid](https://github.com/MatsuriDayo/NekoBoxForAndroid) + [mieru plugin](https://github.com/enfein/NekoBoxPlugins)
- iOS
  - [ClashMi](https://clashmi.app/)
  - [Karing](https://karing.app/)

## 使用教程

1. [服务器安装与配置](./docs/server-install.zh_CN.md)
1. [客户端安装与配置](./docs/client-install.zh_CN.md)
1. [客户端安装与配置 - OpenWrt](./docs/client-install-openwrt.zh_CN.md)
1. [在 Clash Verge Rev 中使用 mieru](./docs/third-party/clash-verge-rev.zh_CN.md)
1. [运营维护与故障排查](./docs/operation.zh_CN.md)
1. [翻墙安全指南](./docs/security.zh_CN.md)
1. [编译](./docs/compile.zh_CN.md)

## 原理和协议

mieru 的翻墙原理与 shadowsocks / v2ray 等软件类似，在客户端和墙外的代理服务器之间建立一个加密的通道。GFW 不能破解加密传输的信息，无法判定你最终访问的网址，因此只能选择放行。

有关 mieru 协议的讲解，请参阅 [mieru 代理协议](./docs/protocol.zh_CN.md)。

## 分享

如果你觉得这款软件对你有帮助，请分享给朋友们。谢谢！

## 联系作者

关于本项目，如果你有任何问题，请提交 GitHub Issue 联系我们。

## 许可证

使用本软件需遵从 GPL-3 协议。

[![Star History Chart](https://api.star-history.com/svg?repos=enfein/mieru&type=Date)](https://www.star-history.com/#enfein/mieru&Date)
