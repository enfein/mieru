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
# - build/package/mieru/debian/DEBIAN/control
# - build/package/mieru/rpm/mieru.spec
# - build/package/mita/debian/DEBIAN/control
# - build/package/mita/rpm/mita.spec
# - docs/client-install.md
# - docs/server-install.md
VERSION="1.4.0"

.PHONY: build bin lib deb rpm test-container fmt vet protobuf src clean clean-cache

# Build binaries and installation packages.
build: bin deb rpm

# Build binaries.
bin: lib client-mac-amd64 client-linux-amd64 client-windows-amd64 server-linux-amd64

# Compile go libraries and run unit tests.
lib: fmt
	CGO_ENABLED=0 go build -v ./...
	CGO_ENABLED=0 go test -test.v -timeout=1m0s ./...

# Build MacOS client.
client-mac-amd64:
	mkdir -p release/darwin
	env GOOS=darwin GOARCH=amd64 CGO_ENABLED=0 go build -ldflags="-s -w" -o release/darwin/mieru cmd/mieru/mieru.go
	cd release/darwin;\
		sha256sum mieru > mieru_${VERSION}_darwin.sha256.txt;\
		tar -zcvf mieru_${VERSION}_darwin_amd64.tar.gz mieru;\
		sha256sum mieru_${VERSION}_darwin_amd64.tar.gz > mieru_${VERSION}_darwin_amd64.tar.gz.sha256.txt
	mv release/darwin/mieru_${VERSION}_darwin_amd64.tar.gz release/
	mv release/darwin/mieru_${VERSION}_darwin_amd64.tar.gz.sha256.txt release/

# Build linux client.
client-linux-amd64:
	mkdir -p release/linux
	env GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -ldflags="-s -w" -o release/linux/mieru cmd/mieru/mieru.go
	cd release/linux;\
		sha256sum mieru > mieru_${VERSION}_linux.sha256.txt;\
		tar -zcvf mieru_${VERSION}_linux_amd64.tar.gz mieru;\
		sha256sum mieru_${VERSION}_linux_amd64.tar.gz > mieru_${VERSION}_linux_amd64.tar.gz.sha256.txt
	mv release/linux/mieru_${VERSION}_linux_amd64.tar.gz release/
	mv release/linux/mieru_${VERSION}_linux_amd64.tar.gz.sha256.txt release/

# Build windows client.
client-windows-amd64:
	mkdir -p release/windows
	env GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -ldflags="-s -w" -o release/windows/mieru.exe cmd/mieru/mieru.go
	cd release/windows;\
		sha256sum mieru.exe > mieru_${VERSION}_windows.exe.sha256.txt;\
		zip -r mieru_${VERSION}_windows_amd64.zip mieru.exe;\
		sha256sum mieru_${VERSION}_windows_amd64.zip > mieru_${VERSION}_windows_amd64.zip.sha256.txt
	mv release/windows/mieru_${VERSION}_windows_amd64.zip release/
	mv release/windows/mieru_${VERSION}_windows_amd64.zip.sha256.txt release/

# Build linux server.
server-linux-amd64:
	mkdir -p release/linux
	env GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -ldflags="-s -w" -o release/linux/mita cmd/mita/mita.go
	cd release/linux;\
		sha256sum mita > mita_${VERSION}_linux.sha256.txt;\
		tar -zcvf mita_${VERSION}_linux_amd64.tar.gz mita;\
		sha256sum mita_${VERSION}_linux_amd64.tar.gz > mita_${VERSION}_linux_amd64.tar.gz.sha256.txt
	mv release/linux/mita_${VERSION}_linux_amd64.tar.gz release/
	mv release/linux/mita_${VERSION}_linux_amd64.tar.gz.sha256.txt release/

# Build debian installation packages.
deb: deb-client deb-server

# Build client debian installation package.
deb-client: client-linux-amd64
	if [ ! -z $$(command -v dpkg-deb) ] && [ ! -z $$(command -v fakeroot) ]; then\
		mkdir -p build/package/mieru/debian/usr/bin;\
		cp release/linux/mieru build/package/mieru/debian/usr/bin/;\
		cd build/package/mieru;\
		fakeroot dpkg-deb --build debian .;\
		cd "${ROOT}";\
		mv build/package/mieru/mieru_${VERSION}_amd64.deb release/;\
		cd release;\
		sha256sum mieru_${VERSION}_amd64.deb > mieru_${VERSION}_amd64.deb.sha256.txt;\
	fi

# Build server debian installation package.
deb-server: server-linux-amd64
	if [ ! -z $$(command -v dpkg-deb) ] && [ ! -z $$(command -v fakeroot) ]; then\
		mkdir -p build/package/mita/debian/usr/bin;\
		cp release/linux/mita build/package/mita/debian/usr/bin/;\
		cd build/package/mita;\
		fakeroot dpkg-deb --build debian .;\
		cd "${ROOT}";\
		mv build/package/mita/mita_${VERSION}_amd64.deb release/;\
		cd release;\
		sha256sum mita_${VERSION}_amd64.deb > mita_${VERSION}_amd64.deb.sha256.txt;\
	fi

# Build RPM installation packages.
rpm: rpm-client rpm-server

# Build client RPM installation package.
rpm-client: client-linux-amd64
	if [ ! -z $$(command -v rpmbuild) ]; then\
		cp release/linux/mieru build/package/mieru/rpm/;\
		cd build/package/mieru/rpm;\
		rpmbuild -ba --build-in-place --define "_topdir $$(pwd)" mieru.spec;\
		cd "${ROOT}";\
		mv build/package/mieru/rpm/RPMS/x86_64/mieru-${VERSION}-1.x86_64.rpm release/;\
		cd release;\
		sha256sum mieru-${VERSION}-1.x86_64.rpm > mieru-${VERSION}-1.x86_64.rpm.sha256.txt;\
	fi

# Build server RPM installation package.
rpm-server: server-linux-amd64
	if [ ! -z $$(command -v rpmbuild) ]; then\
		cp release/linux/mita build/package/mita/rpm/;\
		cd build/package/mita/rpm;\
		rpmbuild -ba --build-in-place --define "_topdir $$(pwd)" mita.spec;\
		cd "${ROOT}";\
		mv build/package/mita/rpm/RPMS/x86_64/mita-${VERSION}-1.x86_64.rpm release/;\
		cd release;\
		sha256sum mita-${VERSION}-1.x86_64.rpm > mita-${VERSION}-1.x86_64.rpm.sha256.txt;\
	fi

# Build a docker image to run integration tests.
test-container:
	if [ ! -z $$(command -v docker) ]; then\
		CGO_ENABLED=0 go build cmd/mieru/mieru.go;\
		CGO_ENABLED=0 go build cmd/mita/mita.go;\
		CGO_ENABLED=0 go build test/cmd/sockshttpclient/sockshttpclient.go;\
		CGO_ENABLED=0 go build test/cmd/httpserver/httpserver.go;\
		docker build -t mieru_httptest:${SHORT_SHA} -f test/deploy/httptest/Dockerfile .;\
		rm mieru mita sockshttpclient httpserver;\
	fi

# Format source code.
fmt:
	CGO_ENABLED=0 go fmt ./...

# Run go vet.
vet:
	CGO_ENABLED=0 go vet ./...

# Generate source code from protobuf.
# Call this after proto files are changed.
protobuf:
	# This protobuf compiler only works for linux amd64 machine.
	if [ ! -x "${ROOT}/tools/build/protoc" ]; then\
		curl -o "${ROOT}/tools/build/protoc" https://raw.githubusercontent.com/enfein/buildtools/main/protoc/3.15.8/linux_amd64/bin/protoc;\
		chmod 755 "${ROOT}/tools/build/protoc";\
	fi
	if [ ! -x "${ROOT}/tools/build/protoc-gen-go" ]; then\
		curl -o "${ROOT}/tools/build/protoc-gen-go" https://raw.githubusercontent.com/enfein/buildtools/main/protoc-gen-go/1.26.0/linux_amd64/protoc-gen-go;\
		chmod 755 "${ROOT}/tools/build/protoc-gen-go";\
	fi
	if [ ! -x "${ROOT}/tools/build/protoc-gen-go-grpc" ]; then\
		curl -o "${ROOT}/tools/build/protoc-gen-go-grpc" https://raw.githubusercontent.com/enfein/buildtools/main/protoc-gen-go-grpc/1.37.1/linux_amd64/protoc-gen-go-grpc;\
		chmod 755 "${ROOT}/tools/build/protoc-gen-go-grpc";\
	fi

	PATH=${PATH}:"${ROOT}/tools/build" ${ROOT}/tools/build/protoc -I="${ROOT}/pkg/appctl/proto" \
		--go_out="${ROOT}/pkg/appctl" --go_opt=module="github.com/enfein/mieru/pkg/appctl" \
		--go-grpc_out="${ROOT}/pkg/appctl" --go-grpc_opt=module="github.com/enfein/mieru/pkg/appctl" \
		--proto_path="${ROOT}/pkg" \
		"${ROOT}/pkg/appctl/proto/clientcfg.proto" \
		"${ROOT}/pkg/appctl/proto/debug.proto" \
		"${ROOT}/pkg/appctl/proto/empty.proto" \
		"${ROOT}/pkg/appctl/proto/endpoint.proto" \
		"${ROOT}/pkg/appctl/proto/lifecycle.proto" \
		"${ROOT}/pkg/appctl/proto/logging.proto" \
		"${ROOT}/pkg/appctl/proto/servercfg.proto" \
		"${ROOT}/pkg/appctl/proto/user.proto"

# Package source code.
src: clean
	cd ..; tar --exclude="${PROJECT_NAME}/.git" -zcvf source.tar.gz "${PROJECT_NAME}"; zip -r source.zip "${PROJECT_NAME}" -x \*.git\*
	mkdir -p release; mv ../source.tar.gz release; mv ../source.zip release

# Clean all the files outside the git repository.
clean:
	git clean -fxd

# Clean go build cache.
clean-cache:
	go clean -cache
	go clean -testcache
