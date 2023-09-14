# 見える / mieru

[![Build Status](https://github.com/enfein/mieru/actions/workflows/ci.yaml/badge.svg)](https://github.com/enfein/mieru/actions/workflows/ci.yaml)
[![Releases](https://img.shields.io/github/release/enfein/mieru/all.svg?style=flat)](https://github.com/enfein/mieru/releases)
[![Downloads](https://img.shields.io/github/downloads/enfein/mieru/total.svg?style=flat)](https://github.com/enfein/mieru/releases)
[![LICENSE](https://img.shields.io/github/license/enfein/mieru.svg?style=flat)](https://github.com/enfein/mieru/blob/main/LICENSE)

mieru【見える】是一款安全的、无流量特征、难以主动探测的，基于 TCP 或 UDP 协议的 socks5 / HTTP / HTTPS 网络代理软件。

mieru 代理软件由称为 mieru【見える】的客户端软件和称为 mita【見た】的代理服务器软件这两部分组成。

## 原理和协议

mieru 的翻墙原理与 shadowsocks / v2ray 等软件类似，在客户端和墙外的代理服务器之间建立一个加密的通道。GFW 不能破解加密传输的信息，无法判定你最终访问的网址，因此只能选择放行。

有关 mieru 协议的讲解，请参阅 [mieru 代理协议](https://github.com/enfein/mieru/blob/main/docs/protocol.zh_CN.md)。

## 特性

1. 使用高强度的 AES-256-GCM 加密算法，基于用户名、密码和系统时间生成密钥。以现有计算能力，mieru 传输的数据内容无法被破解。
2. mieru 实现了客户端和代理服务器之间所有传输内容的完整加密，不传输任何明文信息。网络观察者（例如 GFW）仅能获知时间、数据包的发送和接收地址，以及数据包的大小。除此之外，观察者无法得到其它任何流量信息。
3. 当 mieru 发送数据包时，会在尾部填充随机信息。即便是传输相同的内容，数据包大小也不相同。
4. 在使用 UDP 传输协议时，mieru 不需要客户端和服务器进行握手，即可直接发送数据。
5. 当服务器无法解密客户端发送的数据时，不会返回任何内容。GFW 很难通过主动探测发现 mieru 服务。
6. mieru 支持多个用户共享代理服务器。
7. mieru 支持 IPv4 和 IPv6。
8. mieru 提供 socks5, HTTP 和 HTTPS 代理。
9. 客户端软件支持 Windows, Mac OS, Linux 和 Android 系统。Android 用户请使用 0.8.1-rc02 版本或以上 SagerNet 客户端并安装 1.6.1 版本或以上 mieru 插件。
10. 如果需要全局代理或自定义路由规则等高级功能，可以将 mieru 作为 clash 等代理平台的后端。

## 使用教程

1. [服务器安装与配置](https://github.com/enfein/mieru/blob/main/docs/server-install.zh_CN.md)
2. [客户端安装与配置](https://github.com/enfein/mieru/blob/main/docs/client-install.zh_CN.md)
3. [客户端安装与配置 - OpenWrt](https://github.com/enfein/mieru/blob/main/docs/client-install-openwrt.zh_CN.md)
4. [运营维护与故障排查](https://github.com/enfein/mieru/blob/main/docs/operation.zh_CN.md)
5. [翻墙安全指南](https://github.com/enfein/mieru/blob/main/docs/security.zh_CN.md)

## 编译

编译 mieru 的客户端和服务器软件，建议在 Linux 系统中进行。编译过程可能需要翻墙下载依赖的软件包。

编译所需的软件包括：

- curl
- env
- git
- go (version >= 1.20)
- make
- sha256sum
- tar
- zip

编译 debian 安装包需要：

- dpkg-deb
- fakeroot

编译 RPM 安装包需要：

- rpmbuild

编译时，进入项目根目录，调用指令 `make` 即可。编译结果会存放在项目根目录下的 `release` 文件夹。

## 分享

如果你觉得这款软件对你有帮助，请分享给朋友们。谢谢！

## 联系作者

关于本项目，如果你有任何问题，请提交 GitHub Issue 联系我们。

## 许可证

使用本软件需遵从 GPL-3 协议。
