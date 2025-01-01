# 编译

编译 mieru 客户端和 mita 服务器软件，建议在 Linux 系统中进行。编译过程可能需要翻墙下载依赖的软件包。

编译所需的软件包括：

- curl
- env
- git
- go (version >= 1.20)
- make
- sha256sum
- tar
- zip

编译 Android 可执行文件需要：

- gcc

编译 Debian 安装包需要：

- dpkg-deb
- fakeroot

编译 RPM 安装包需要：

- rpmbuild

编译时，进入项目根目录，运行指令 `make` 即可。编译结果会存放在项目根目录下的 `release` 文件夹。

`make` 指令只会生成官方支持的可执行文件。如果你想要编译特定 CPU 指令集架构或操作系统的可执行文件，可以参考下面的几个指令：

```sh
# 编译可以在龙芯处理器 Linux 系统上运行的 mita 服务器软件
env GOOS=linux GOARCH=loong64 CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o mita cmd/mita/mita.go

# 编译可以在 x86_64 处理器 FreeBSD 系统上运行的 mieru 客户端软件
env GOOS=freebsd GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o mieru cmd/mieru/mieru.go

# 编译可以在 MIPS 处理器 OpenWRT 系统上运行的 mieru 客户端软件
env GOOS=linux GOARCH=mips CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o mieru cmd/mieru/mieru.go
```

**注意，`mita` 服务器软件可能无法在 Linux 之外的操作系统中运行。**
