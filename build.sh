#!/bin/bash

# Copyright (C) 2021  mieru authors
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

# Make sure this script has executable permission:
# git update-index --chmod=+x <file>

check_command() {
    rc=$(command -v $1 2>&1 > /dev/null; echo $?)
    if [[ ${rc} -ne 0 ]]; then
        echo "command \"$1\" not found in the system"
        exit 1
    fi
}

has_command() {
    rc=$(command -v $1 2>&1 > /dev/null; echo $?)
    return ${rc}
}

# If this version is changed, also change the version in
# - build/package/mieru/debian/DEBIAN/control
# - build/package/mita/debian/DEBIAN/control
# - all references in README.md
version="1.0.1"

set -e

check_command "curl"
check_command "env"
check_command "git"
check_command "go"
check_command "sha256sum"
check_command "tar"
check_command "zip"

SHORT_SHA=$(git rev-parse --short HEAD)
ROOT=$(git rev-parse --show-toplevel)

cd "$ROOT"

# mieru uses protobuf to generate source code. Download protobuf compiler if necessary.
# This only works for linux amd64 machine.
if [[ ! -x "$ROOT/tools/build/protoc" ]]; then
    echo "downloading protoc"
    curl -o "$ROOT/tools/build/protoc" \
        https://raw.githubusercontent.com/enfein/buildtools/main/protoc/3.15.8/linux_amd64/bin/protoc
    chmod 755 "$ROOT/tools/build/protoc"
fi
if [[ ! -x "$ROOT/tools/build/protoc-gen-go" ]]; then
    echo "downloading protoc-gen-go"
    curl -o "$ROOT/tools/build/protoc-gen-go" \
        https://raw.githubusercontent.com/enfein/buildtools/main/protoc-gen-go/1.26.0/linux_amd64/protoc-gen-go
    chmod 755 "$ROOT/tools/build/protoc-gen-go"
fi
if [[ ! -x "$ROOT/tools/build/protoc-gen-go-grpc" ]]; then
    echo "downloading protoc-gen-go-grpc"
    curl -o "$ROOT/tools/build/protoc-gen-go-grpc" \
        https://raw.githubusercontent.com/enfein/buildtools/main/protoc-gen-go-grpc/1.37.1/linux_amd64/protoc-gen-go-grpc
    chmod 755 "$ROOT/tools/build/protoc-gen-go-grpc"
fi

# If the system already have protoc or protoc-gen-go in PATH, use the original one.
export PATH=$PATH:"$ROOT/tools/build"

protoc -I="$ROOT/pkg/appctl" \
    --go_out="$ROOT/pkg/appctl" --go_opt=module="github.com/enfein/mieru/pkg/appctl" \
    --go-grpc_out="$ROOT/pkg/appctl" --go-grpc_opt=module="github.com/enfein/mieru/pkg/appctl" \
    --proto_path="$ROOT/pkg" \
    "$ROOT/pkg/appctl/clientcfg.proto" \
    "$ROOT/pkg/appctl/debug.proto" \
    "$ROOT/pkg/appctl/empty.proto" \
    "$ROOT/pkg/appctl/endpoint.proto" \
    "$ROOT/pkg/appctl/lifecycle.proto" \
    "$ROOT/pkg/appctl/logging.proto" \
    "$ROOT/pkg/appctl/servercfg.proto" \
    "$ROOT/pkg/appctl/user.proto"

go build -v ./...
CGO_ENABLED=0 go test -test.v -timeout=2m0s ./...
CGO_ENABLED=0 go vet ./...

# Build the client binary for mac, linux and windows.
SUPPORTED_OS=(darwin linux windows)
for os in ${SUPPORTED_OS[@]}; do
    ext=""
    if [[ "${os}" == "windows" ]]; then
        ext=".exe"
    fi
    mkdir -p release/${os}
    env GOOS=${os} GOARCH=amd64 go build -ldflags="-s -w" -o release/${os}/mieru${ext} cmd/mieru/mieru.go
    cd release/${os}
    sha256sum mieru${ext} > mieru_${version}_${os}${ext}.sha256.txt
    if [[ "${os}" == "windows" ]]; then
        zip -r mieru_${version}_${os}_amd64.zip mieru${ext}
        mv mieru_${version}_${os}_amd64.zip ..
    else
        tar -zcvf mieru_${version}_${os}_amd64.tar.gz mieru${ext}
        mv mieru_${version}_${os}_amd64.tar.gz ..
    fi
    cd "$ROOT"
done

# Build the server binary for linux.
env GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o release/linux/mita cmd/mita/mita.go
cd release/linux
sha256sum mita > mita_${version}_linux.sha256.txt
tar -zcvf mita_${version}_linux_amd64.tar.gz mita
mv mita_${version}_linux_amd64.tar.gz ..
cd "$ROOT"

# Build debian packages if possible.
has_command "dpkg-deb" && has_command "fakeroot"
if [[ $? -eq 0 ]]; then
    # Build client debian package.
    mkdir -p build/package/mieru/debian/usr/bin
    cp release/linux/mieru build/package/mieru/debian/usr/bin/
    cd build/package/mieru
    fakeroot dpkg-deb --build debian .
    cd "$ROOT"
    mv build/package/mieru/*.deb release/
    cd release && sha256sum mieru_${version}_amd64.deb > mieru_${version}_amd64.deb.sha256.txt && cd ..

    # Build server debian package.
    mkdir -p build/package/mita/debian/usr/bin
    cp release/linux/mita build/package/mita/debian/usr/bin/
    cd build/package/mita
    fakeroot dpkg-deb --build debian .
    cd "$ROOT"
    mv build/package/mita/*.deb release/
    cd release && sha256sum mita_${version}_amd64.deb > mita_${version}_amd64.deb.sha256.txt && cd ..
fi

# Build the test container if docker is available in the system.
has_command "docker"
if [[ $? -eq 0 ]]; then
    docker build -t mieru_httptest:$SHORT_SHA \
        -f test/deploy/httptest/Dockerfile .
fi
