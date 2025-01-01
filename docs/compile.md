# Compilation

It is recommended to compile the `mieru` client and `mita` server software on Linux. The compilation process might require a proxy to download dependency packages.

The following softwares are required for compilation:

- curl
- env
- git
- go (version >= 1.20)
- make
- sha256sum
- tar
- zip

To compile Android executables, you need:

- gcc

To compile Debian packages, you need:

- dpkg-deb
- fakeroot

To compile RPM packages, you need:

- rpmbuild

To compile, navigate to the project's root directory and run the command `make`. The compilation results will be stored in the `release` folder under the project's root directory.

The `make` command will only generate the officially supported executables. If you want to compile executables for a specific CPU instruction set architecture or operating system, you can refer to the following commands:

```sh
# Compile the mita server software, which runs on a Linux system with Loongson processor
env GOOS=linux GOARCH=loong64 CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o mita cmd/mita/mita.go

# Compile the mieru client software, which runs on a FreeBSD system with x86_64 processor
env GOOS=freebsd GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o mieru cmd/mieru/mieru.go

# Compile the mieru client software, which runs on an OpenWRT system with MIPS processor
env GOOS=linux GOARCH=mips CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o mieru cmd/mieru/mieru.go
```

**Note: The `mita` server software may not run on operating systems other than Linux.**
