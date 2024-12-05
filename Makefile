# Copyright (C) 2022  mieru authors
#
# This program is free software: you can redistribute it and/or modify
# it under the terms of the GNU General Public License as published by
# the Free Software Foundation, either version 3 of the License, or
# (at your option) any later version.
#
# This program is distributed in the hope that it will be useful,
# but WITHOUT ANY WARRANTY; without even the implied warranty of
# MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
# GNU General Public License for more details.
#
# You should have received a copy of the GNU General Public License
# along with this program.  If not, see <https://www.gnu.org/licenses/>.

ROOT=$(shell git rev-parse --show-toplevel)
SHORT_SHA=$(shell git rev-parse --short HEAD)
PROJECT_NAME=$(shell basename "${ROOT}")

# If this version is changed, also change the version in
#
# - build/package/mieru/amd64/debian/DEBIAN/control
# - build/package/mieru/amd64/rpm/mieru.spec
# - build/package/mieru/arm64/debian/DEBIAN/control
# - build/package/mieru/arm64/rpm/mieru.spec
# - build/package/mita/amd64/debian/DEBIAN/control
# - build/package/mita/amd64/rpm/mita.spec
# - build/package/mita/arm64/debian/DEBIAN/control
# - build/package/mita/arm64/rpm/mita.spec
# - docs/server-install.md
# - docs/server-install.zh_CN.md
# - pkg/version/current.go
#
# Use `tools/bump_version.sh` script to change all those files at one shot.
VERSION="3.8.1"

# Build binaries and installation packages.
.PHONY: build
build: bin deb rpm

# Build binaries.
.PHONY: bin
bin: lib client-android client-linux client-mac client-windows server-linux

# Compile go libraries and run unit tests.
.PHONY: lib
lib: fmt vet
	CGO_ENABLED=0 go build -v ./...
	CGO_ENABLED=0 go test -timeout=1m0s -coverprofile coverage.out ./...
	go tool cover -html coverage.out -o coverage.html

# Format source code.
.PHONY: fmt
fmt:
	CGO_ENABLED=0 go fmt ./...

# Run go vet.
.PHONY: vet
vet:
	CGO_ENABLED=0 go vet ./...

# Run benchmark.
.PHONY: bench
bench:
	CGO_ENABLED=0 go test -bench=. -benchtime=5s ./pkg/cipher

# Generate vendor directory.
.PHONY: vendor
vendor:
	go mod tidy
	go mod vendor

# Build Android clients.
.PHONY: client-android
client-android: client-android-amd64 client-android-arm64

# Build Android amd64 client.
.PHONY: client-android-amd64
client-android-amd64:
	if [ ! -z $$(command -v gcc) ]; then\
		mkdir -p release/android/amd64;\
		env GOOS=android GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o release/android/amd64/mieru cmd/mieru/mieru.go;\
		cd release/android/amd64;\
		sha256sum mieru > mieru_${VERSION}_android_amd64.sha256.txt;\
		tar -zcvf mieru_${VERSION}_android_amd64.tar.gz mieru;\
		sha256sum mieru_${VERSION}_android_amd64.tar.gz > mieru_${VERSION}_android_amd64.tar.gz.sha256.txt;\
		cd "${ROOT}";\
		mv release/android/amd64/mieru_${VERSION}_android_amd64.tar.gz release/;\
		mv release/android/amd64/mieru_${VERSION}_android_amd64.tar.gz.sha256.txt release/;\
	fi

# Build Android arm64 client.
.PHONY: client-android-arm64
client-android-arm64:
	if [ ! -z $$(command -v gcc) ]; then\
		mkdir -p release/android/arm64;\
		env GOOS=android GOARCH=arm64 CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o release/android/arm64/mieru cmd/mieru/mieru.go;\
		cd release/android/arm64;\
		sha256sum mieru > mieru_${VERSION}_android_arm64.sha256.txt;\
		tar -zcvf mieru_${VERSION}_android_arm64.tar.gz mieru;\
		sha256sum mieru_${VERSION}_android_arm64.tar.gz > mieru_${VERSION}_android_arm64.tar.gz.sha256.txt;\
		cd "${ROOT}";\
		mv release/android/arm64/mieru_${VERSION}_android_arm64.tar.gz release/;\
		mv release/android/arm64/mieru_${VERSION}_android_arm64.tar.gz.sha256.txt release/;\
	fi

# Build linux clients.
.PHONY: client-linux
client-linux: client-linux-amd64 client-linux-arm64 client-linux-armv7 client-linux-riscv64

# Build linux amd64 client.
.PHONY: client-linux-amd64
client-linux-amd64:
	mkdir -p release/linux/amd64
	env GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o release/linux/amd64/mieru cmd/mieru/mieru.go
	cd release/linux/amd64;\
		sha256sum mieru > mieru_${VERSION}_linux_amd64.sha256.txt;\
		tar -zcvf mieru_${VERSION}_linux_amd64.tar.gz mieru;\
		sha256sum mieru_${VERSION}_linux_amd64.tar.gz > mieru_${VERSION}_linux_amd64.tar.gz.sha256.txt
	mv release/linux/amd64/mieru_${VERSION}_linux_amd64.tar.gz release/
	mv release/linux/amd64/mieru_${VERSION}_linux_amd64.tar.gz.sha256.txt release/

# Build linux arm64 client.
.PHONY: client-linux-arm64
client-linux-arm64:
	mkdir -p release/linux/arm64
	env GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o release/linux/arm64/mieru cmd/mieru/mieru.go
	cd release/linux/arm64;\
		sha256sum mieru > mieru_${VERSION}_linux_arm64.sha256.txt;\
		tar -zcvf mieru_${VERSION}_linux_arm64.tar.gz mieru;\
		sha256sum mieru_${VERSION}_linux_arm64.tar.gz > mieru_${VERSION}_linux_arm64.tar.gz.sha256.txt
	mv release/linux/arm64/mieru_${VERSION}_linux_arm64.tar.gz release/
	mv release/linux/arm64/mieru_${VERSION}_linux_arm64.tar.gz.sha256.txt release/

# Build linux armv7 client.
.PHONY: client-linux-armv7
client-linux-armv7:
	mkdir -p release/linux/armv7
	env GOOS=linux GOARCH=arm GOARM=7 CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o release/linux/armv7/mieru cmd/mieru/mieru.go
	cd release/linux/armv7;\
		sha256sum mieru > mieru_${VERSION}_linux_armv7.sha256.txt;\
		tar -zcvf mieru_${VERSION}_linux_armv7.tar.gz mieru;\
		sha256sum mieru_${VERSION}_linux_armv7.tar.gz > mieru_${VERSION}_linux_armv7.tar.gz.sha256.txt
	mv release/linux/armv7/mieru_${VERSION}_linux_armv7.tar.gz release/
	mv release/linux/armv7/mieru_${VERSION}_linux_armv7.tar.gz.sha256.txt release/

# Build linux riscv64 client.
.PHONY: client-linux-riscv64
client-linux-riscv64:
	mkdir -p release/linux/riscv64
	env GOOS=linux GOARCH=riscv64 CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o release/linux/riscv64/mieru cmd/mieru/mieru.go
	cd release/linux/riscv64;\
		sha256sum mieru > mieru_${VERSION}_linux_riscv64.sha256.txt;\
		tar -zcvf mieru_${VERSION}_linux_riscv64.tar.gz mieru;\
		sha256sum mieru_${VERSION}_linux_riscv64.tar.gz > mieru_${VERSION}_linux_riscv64.tar.gz.sha256.txt
	mv release/linux/riscv64/mieru_${VERSION}_linux_riscv64.tar.gz release/
	mv release/linux/riscv64/mieru_${VERSION}_linux_riscv64.tar.gz.sha256.txt release/

# Build MacOS clients.
.PHONY: client-mac
client-mac: client-mac-amd64 client-mac-arm64

# Build MacOS amd64 client.
.PHONY: client-mac-amd64
client-mac-amd64:
	mkdir -p release/macos/amd64
	env GOOS=darwin GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o release/macos/amd64/mieru cmd/mieru/mieru.go
	cd release/macos/amd64;\
		sha256sum mieru > mieru_${VERSION}_macos_amd64.sha256.txt;\
		tar -zcvf mieru_${VERSION}_macos_amd64.tar.gz mieru;\
		sha256sum mieru_${VERSION}_macos_amd64.tar.gz > mieru_${VERSION}_macos_amd64.tar.gz.sha256.txt
	mv release/macos/amd64/mieru_${VERSION}_macos_amd64.tar.gz release/
	mv release/macos/amd64/mieru_${VERSION}_macos_amd64.tar.gz.sha256.txt release/

# Build MacOS arm64 client.
.PHONY: client-mac-arm64
client-mac-arm64:
	mkdir -p release/macos/arm64
	env GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o release/macos/arm64/mieru cmd/mieru/mieru.go
	cd release/macos/arm64;\
		sha256sum mieru > mieru_${VERSION}_macos_arm64.sha256.txt;\
		tar -zcvf mieru_${VERSION}_macos_arm64.tar.gz mieru;\
		sha256sum mieru_${VERSION}_macos_arm64.tar.gz > mieru_${VERSION}_macos_arm64.tar.gz.sha256.txt
	mv release/macos/arm64/mieru_${VERSION}_macos_arm64.tar.gz release/
	mv release/macos/arm64/mieru_${VERSION}_macos_arm64.tar.gz.sha256.txt release/

# Build windows clients.
.PHONY: client-windows
client-windows: client-windows-x86 client-windows-amd64

# Build windows x86 client.
.PHONY: client-windows-x86
client-windows-x86:
	mkdir -p release/windows/386
	env GOOS=windows GOARCH=386 CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o release/windows/386/mieru.exe cmd/mieru/mieru.go
	cd release/windows/386;\
		sha256sum mieru.exe > mieru_${VERSION}_windows_x86.exe.sha256.txt;\
		zip -r mieru_${VERSION}_windows_x86.zip mieru.exe;\
		sha256sum mieru_${VERSION}_windows_x86.zip > mieru_${VERSION}_windows_x86.zip.sha256.txt
	mv release/windows/386/mieru_${VERSION}_windows_x86.zip release/
	mv release/windows/386/mieru_${VERSION}_windows_x86.zip.sha256.txt release/

# Build windows amd64 client.
.PHONY: client-windows-amd64
client-windows-amd64:
	mkdir -p release/windows/amd64
	env GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o release/windows/amd64/mieru.exe cmd/mieru/mieru.go
	cd release/windows/amd64;\
		sha256sum mieru.exe > mieru_${VERSION}_windows_amd64.exe.sha256.txt;\
		zip -r mieru_${VERSION}_windows_amd64.zip mieru.exe;\
		sha256sum mieru_${VERSION}_windows_amd64.zip > mieru_${VERSION}_windows_amd64.zip.sha256.txt
	mv release/windows/amd64/mieru_${VERSION}_windows_amd64.zip release/
	mv release/windows/amd64/mieru_${VERSION}_windows_amd64.zip.sha256.txt release/

# Build linux servers.
.PHONY: server-linux
server-linux: server-linux-amd64 server-linux-arm64

# Build linux amd64 server.
.PHONY: server-linux-amd64
server-linux-amd64:
	mkdir -p release/linux/amd64
	env GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o release/linux/amd64/mita cmd/mita/mita.go
	cd release/linux/amd64;\
		sha256sum mita > mita_${VERSION}_linux_amd64.sha256.txt

# Build linux arm64 server.
.PHONY: server-linux-arm64
server-linux-arm64:
	mkdir -p release/linux/arm64
	env GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o release/linux/arm64/mita cmd/mita/mita.go
	cd release/linux/arm64;\
		sha256sum mita > mita_${VERSION}_linux_arm64.sha256.txt

# Build debian installation packages.
.PHONY: deb
deb: deb-client-amd64 deb-client-arm64 deb-server-amd64 deb-server-arm64

# Build debian client amd64 installation package.
.PHONY: deb-client-amd64
deb-client-amd64: client-linux-amd64
	if [ ! -z $$(command -v dpkg-deb) ] && [ ! -z $$(command -v fakeroot) ]; then\
		rm -rf build/package/mieru/amd64/debian/usr/bin;\
		mkdir -p build/package/mieru/amd64/debian/usr/bin;\
		cp release/linux/amd64/mieru build/package/mieru/amd64/debian/usr/bin/;\
		cd build/package/mieru/amd64;\
		fakeroot dpkg-deb -Zxz --build debian .;\
		cd "${ROOT}";\
		mv build/package/mieru/amd64/mieru_${VERSION}_amd64.deb release/;\
		cd release;\
		sha256sum mieru_${VERSION}_amd64.deb > mieru_${VERSION}_amd64.deb.sha256.txt;\
	fi

# Build debian client arm64 installation package.
.PHONY: deb-client-arm64
deb-client-arm64: client-linux-arm64
	if [ ! -z $$(command -v dpkg-deb) ] && [ ! -z $$(command -v fakeroot) ]; then\
		rm -rf build/package/mieru/arm64/debian/usr/bin;\
		mkdir -p build/package/mieru/arm64/debian/usr/bin;\
		cp release/linux/arm64/mieru build/package/mieru/arm64/debian/usr/bin/;\
		cd build/package/mieru/arm64;\
		fakeroot dpkg-deb -Zxz --build debian .;\
		cd "${ROOT}";\
		mv build/package/mieru/arm64/mieru_${VERSION}_arm64.deb release/;\
		cd release;\
		sha256sum mieru_${VERSION}_arm64.deb > mieru_${VERSION}_arm64.deb.sha256.txt;\
	fi

# Build debian server amd64 installation package.
.PHONY: deb-server-amd64
deb-server-amd64: server-linux-amd64
	if [ ! -z $$(command -v dpkg-deb) ] && [ ! -z $$(command -v fakeroot) ]; then\
		rm -rf build/package/mita/amd64/debian/usr/bin;\
		mkdir -p build/package/mita/amd64/debian/usr/bin;\
		cp release/linux/amd64/mita build/package/mita/amd64/debian/usr/bin/;\
		cd build/package/mita/amd64;\
		fakeroot dpkg-deb -Zxz --build debian .;\
		cd "${ROOT}";\
		mv build/package/mita/amd64/mita_${VERSION}_amd64.deb release/;\
		cd release;\
		sha256sum mita_${VERSION}_amd64.deb > mita_${VERSION}_amd64.deb.sha256.txt;\
	fi

# Build debian server arm64 installation package.
.PHONY: deb-server-arm64
deb-server-arm64: server-linux-arm64
	if [ ! -z $$(command -v dpkg-deb) ] && [ ! -z $$(command -v fakeroot) ]; then\
		rm -rf build/package/mita/arm64/debian/usr/bin;\
		mkdir -p build/package/mita/arm64/debian/usr/bin;\
		cp release/linux/arm64/mita build/package/mita/arm64/debian/usr/bin/;\
		cd build/package/mita/arm64;\
		fakeroot dpkg-deb -Zxz --build debian .;\
		cd "${ROOT}";\
		mv build/package/mita/arm64/mita_${VERSION}_arm64.deb release/;\
		cd release;\
		sha256sum mita_${VERSION}_arm64.deb > mita_${VERSION}_arm64.deb.sha256.txt;\
	fi

# Build RPM installation packages.
.PHONY: rpm
rpm: rpm-client-amd64 rpm-client-arm64 rpm-server-amd64 rpm-server-arm64

# Build RPM client amd64 installation package.
.PHONY: rpm-client-amd64
rpm-client-amd64: client-linux-amd64
	if [ ! -z $$(command -v rpmbuild) ]; then\
		rm -rf build/package/mieru/amd64/rpm/mieru;\
		cp release/linux/amd64/mieru build/package/mieru/amd64/rpm/;\
		cd build/package/mieru/amd64/rpm;\
		rpmbuild -bb --target x86_64 --build-in-place --define "_topdir $$(pwd)" mieru.spec;\
		cd "${ROOT}";\
		mv build/package/mieru/amd64/rpm/RPMS/x86_64/mieru-${VERSION}-1.x86_64.rpm release/;\
		cd release;\
		sha256sum mieru-${VERSION}-1.x86_64.rpm > mieru-${VERSION}-1.x86_64.rpm.sha256.txt;\
	fi

# Build RPM client arm64 installation package.
.PHONY: rpm-client-arm64
rpm-client-arm64: client-linux-arm64
	if [ ! -z $$(command -v rpmbuild) ]; then\
		rm -rf build/package/mieru/arm64/rpm/mieru;\
		cp release/linux/arm64/mieru build/package/mieru/arm64/rpm/;\
		cd build/package/mieru/arm64/rpm;\
		rpmbuild -bb --target aarch64 --build-in-place --define "_topdir $$(pwd)" mieru.spec;\
		cd "${ROOT}";\
		mv build/package/mieru/arm64/rpm/RPMS/aarch64/mieru-${VERSION}-1.aarch64.rpm release/;\
		cd release;\
		sha256sum mieru-${VERSION}-1.aarch64.rpm > mieru-${VERSION}-1.aarch64.rpm.sha256.txt;\
	fi

# Build RPM server amd64 installation package.
.PHONY: rpm-server-amd64
rpm-server-amd64: server-linux-amd64
	if [ ! -z $$(command -v rpmbuild) ]; then\
		rm -rf build/package/mita/amd64/rpm/mita;\
		cp release/linux/amd64/mita build/package/mita/amd64/rpm/;\
		cd build/package/mita/amd64/rpm;\
		rpmbuild -bb --target x86_64 --build-in-place --define "_topdir $$(pwd)" mita.spec;\
		cd "${ROOT}";\
		mv build/package/mita/amd64/rpm/RPMS/x86_64/mita-${VERSION}-1.x86_64.rpm release/;\
		cd release;\
		sha256sum mita-${VERSION}-1.x86_64.rpm > mita-${VERSION}-1.x86_64.rpm.sha256.txt;\
	fi

# Build RPM server arm64 installation package.
.PHONY: rpm-server-arm64
rpm-server-arm64: server-linux-arm64
	if [ ! -z $$(command -v rpmbuild) ]; then\
		rm -rf build/package/mita/arm64/rpm/mita;\
		cp release/linux/arm64/mita build/package/mita/arm64/rpm/;\
		cd build/package/mita/arm64/rpm;\
		rpmbuild -bb --target aarch64 --build-in-place --define "_topdir $$(pwd)" mita.spec;\
		cd "${ROOT}";\
		mv build/package/mita/arm64/rpm/RPMS/aarch64/mita-${VERSION}-1.aarch64.rpm release/;\
		cd release;\
		sha256sum mita-${VERSION}-1.aarch64.rpm > mita-${VERSION}-1.aarch64.rpm.sha256.txt;\
	fi

# Build binaries used in integration tests.
.PHONY: test-binary
test-binary:
	CGO_ENABLED=0 go build -ldflags="-X 'github.com/enfein/mieru/pkg/log.LogPrefix=C'" -o mieru cmd/mieru/mieru.go
	CGO_ENABLED=0 go build -ldflags="-X 'github.com/enfein/mieru/pkg/log.LogPrefix=S'" -o mita cmd/mita/mita.go
	CGO_ENABLED=1 go build -race -ldflags="-X 'github.com/enfein/mieru/pkg/log.LogPrefix=C2'" -o mieru2 cmd/mieru/mieru.go
	CGO_ENABLED=1 go build -race -ldflags="-X 'github.com/enfein/mieru/pkg/log.LogPrefix=S2'" -o mita2 cmd/mita/mita.go
	CGO_ENABLED=0 go build test/cmd/exampleapiclient/exampleapiclient.go
	CGO_ENABLED=0 go build test/cmd/httpserver/httpserver.go
	CGO_ENABLED=0 go build test/cmd/sockshttpclient/sockshttpclient.go
	CGO_ENABLED=0 go build test/cmd/socksudpclient/socksudpclient.go
	CGO_ENABLED=0 go build test/cmd/udpserver/udpserver.go

# Build a docker image to run integration tests.
.PHONY: test-container
test-container: test-binary
	if [ ! -z $$(command -v docker) ]; then\
		docker build -t mieru_httptest:${SHORT_SHA} -f test/deploy/httptest/Dockerfile .;\
		docker build -t mieru_proxychain:${SHORT_SHA} -f test/deploy/proxychain/Dockerfile .;\
	fi
	rm -f exampleapiclient mieru mieru2 mita mita2 httpserver sockshttpclient socksudpclient udpserver

# Run docker integration tests.
.PHONY: run-container-test
run-container-test: test-container
	if [ ! -z $$(command -v docker) ]; then\
		docker run mieru_httptest:${SHORT_SHA};\
		docker run mieru_proxychain:${SHORT_SHA};\
	fi

# Generate source code from protobuf.
# Call this after proto files are changed.
PROTOC_ARCH =
ifeq ($(shell uname -m), x86_64)
PROTOC_ARCH = amd64
endif
ifeq ($(shell uname -m), arm64)
PROTOC_ARCH = arm64
endif
.PHONY: protobuf
protobuf:
	# Download location: https://github.com/protocolbuffers/protobuf/releases
	if [ ! -x "${ROOT}/tools/build/protoc" ]; then\
		curl -o "${ROOT}/tools/build/protoc" https://raw.githubusercontent.com/enfein/buildtools/main/protoc/22.3/linux_${PROTOC_ARCH}/bin/protoc;\
		chmod 755 "${ROOT}/tools/build/protoc";\
	fi
	# Download location: https://github.com/protocolbuffers/protobuf-go/releases
	if [ ! -x "${ROOT}/tools/build/protoc-gen-go" ]; then\
		curl -o "${ROOT}/tools/build/protoc-gen-go" https://raw.githubusercontent.com/enfein/buildtools/main/protoc-gen-go/1.30.0/linux_${PROTOC_ARCH}/protoc-gen-go;\
		chmod 755 "${ROOT}/tools/build/protoc-gen-go";\
	fi
	# Download location: https://github.com/grpc/grpc-go/releases
	if [ ! -x "${ROOT}/tools/build/protoc-gen-go-grpc" ]; then\
		curl -o "${ROOT}/tools/build/protoc-gen-go-grpc" https://raw.githubusercontent.com/enfein/buildtools/main/protoc-gen-go-grpc/1.3.0/linux_${PROTOC_ARCH}/protoc-gen-go-grpc;\
		chmod 755 "${ROOT}/tools/build/protoc-gen-go-grpc";\
	fi

	PATH=${PATH}:"${ROOT}/tools/build" ${ROOT}/tools/build/protoc -I="${ROOT}/pkg/appctl/proto" \
		--go_out="${ROOT}/pkg/appctl" --go_opt=module="github.com/enfein/mieru/v3/pkg/appctl" \
		--go-grpc_out="${ROOT}/pkg/appctl" --go-grpc_opt=module="github.com/enfein/mieru/v3/pkg/appctl" \
		--proto_path="${ROOT}/pkg" \
		"${ROOT}/pkg/appctl/proto/base.proto" \
		"${ROOT}/pkg/appctl/proto/clientcfg.proto" \
		"${ROOT}/pkg/appctl/proto/misc.proto" \
		"${ROOT}/pkg/appctl/proto/rpc.proto" \
		"${ROOT}/pkg/appctl/proto/servercfg.proto"

	PATH=${PATH}:"${ROOT}/tools/build" ${ROOT}/tools/build/protoc -I="${ROOT}/pkg/metrics/proto" \
		--go_out="${ROOT}/pkg/metrics" --go_opt=module="github.com/enfein/mieru/v3/pkg/metrics" \
		--proto_path="${ROOT}/pkg" \
		"${ROOT}/pkg/metrics/proto/metrics.proto"

	PATH=${PATH}:"${ROOT}/tools/build" ${ROOT}/tools/build/protoc -I="${ROOT}/pkg/version/updater/proto" \
		--go_out="${ROOT}/pkg/version/updater" --go_opt=module="github.com/enfein/mieru/v3/pkg/version/updater" \
		--proto_path="${ROOT}/pkg" \
		"${ROOT}/pkg/version/updater/proto/history.proto"

# Package source code.
.PHONY: src
src: clean
	cd ..; tar --exclude="${PROJECT_NAME}/.git" -zcvf source.tar.gz "${PROJECT_NAME}"; zip -r source.zip "${PROJECT_NAME}" -x \*.git\*
	mkdir -p release; mv ../source.tar.gz release; mv ../source.zip release

# Clean all the files outside the git repository.
.PHONY: clean
clean:
	git clean -fxd

# Clean go build cache.
.PHONY: clean-cache
clean-cache:
	go clean -cache
	go clean -testcache
