# 見える / mieru

[![Build Status](https://github.com/enfein/mieru/actions/workflows/presubmit-postsubmit.yaml/badge.svg)](https://github.com/enfein/mieru/actions/workflows/presubmit-postsubmit.yaml)
[![Releases](https://img.shields.io/github/release/enfein/mieru/all.svg?style=flat)](https://github.com/enfein/mieru/releases)
[![LICENSE](https://img.shields.io/github/license/enfein/mieru.svg?style=flat)](https://github.com/enfein/mieru/blob/main/LICENSE)
[![Go Reference](https://pkg.go.dev/badge/github.com/enfein/mieru.svg)](https://pkg.go.dev/github.com/enfein/mieru)
[![Go Report Card](https://goreportcard.com/badge/github.com/enfein/mieru)](https://goreportcard.com/report/github.com/enfein/mieru)

[中文文档](https://github.com/enfein/mieru/blob/main/README.zh_CN.md)

mieru is a secure, hard to classify, hard to probe, TCP or UDP protocol-based socks5 network proxy software.

The mieru proxy software suite consists of two parts, a client software called mieru, and a proxy server software called mita.

## Protocol

The principle of mieru is similar to shadowsocks / v2ray etc. It creates an encrypted channel between the client and the proxy server outside the firewall. GFW cannot decrypt the encrypted transmission and cannot determine the destination you end up visiting, so it has no choice but to let you go.

For an explanation of the mieru protocol, see [mieru Proxy Protocol](https://github.com/enfein/mieru/blob/main/docs/protocol.md).

## Features

1. mieru uses a high-strength AES-256-GCM encryption algorithm that generates encryption keys based on username, password and system time. With the current computing power, the data content transmitted by mieru cannot be cracked.
2. mieru implements complete encryption of all transmitted content between the client and the proxy server, without transmitting any plaintext information. A network observer (e.g. GFW) only knows the time, the sending and receiving addresses of the packets, and the size of the packets. Other than that, the observer cannot get any other traffic information.
3. When mieru sends a packet, it is padded with random bytes at the end. Even when the same content is transmitted, the packet size varies.
4. When using the UDP transport protocol, mieru does not require a handshake between client and server.
5. When the server can not decrypt the data sent by the client, no content is returned. it is difficult for GFW to discover the mieru service through active probing.
6. mieru supports multiple users sharing a single proxy server.
7. mieru supports IPv4 and IPv6.
8. The client software supports Windows, Mac OS, Linux and Android systems. Android users should use SagerNet client version 0.8.1-rc02 or above, and install mieru plugin version 1.6.1 or above.

## User Guide

1. [Server Installation & Configuration](https://github.com/enfein/mieru/blob/main/docs/server-install.md)
2. [Client Installation & Configuration](https://github.com/enfein/mieru/blob/main/docs/client-install.md)
3. [Maintenance & Troubleshooting](https://github.com/enfein/mieru/blob/main/docs/operation.md)
4. [Security Guide](https://github.com/enfein/mieru/blob/main/docs/security.md)

## Compile

Compiling should be done in Linux. The compilation process requires downloading dependent packages, which may be blocked by the firewall.

The following softwares are required for compilation.

- curl
- env
- git
- go (version >= 1.19)
- make
- sha256sum
- tar
- zip

To build debian packages:

- dpkg-deb
- fakeroot

To build RPM packages:

- rpmbuild

To compile, go to the root directory of the project and invoke `make`. The compilation result will be stored in the `release` directory.

## Contact Us

Use GitHub issue.

## License

Use of this software is subject to the GPL-3 license.
