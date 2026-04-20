#!/bin/bash

# Copyright (C) 2026  mieru authors
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

# Binary-level end-to-end test for Noise_XX over UDP.
#
# The server accepts a single UDP port; every new source address goes
# through a Noise handshake before any mieru frame flows. After the
# handshake, all datagrams are wrapped in an outer Noise AEAD (with
# an explicit 64-bit counter for stateless decryption) and the inner
# layer is mieru's existing packet framing with a deterministic
# password derived from the noise config.

set -e
cd "$(dirname "$0")"
source ./libtest.sh

# Start HTTP echo server.
./httpserver &
HTTPSRV=$!

./mita run &
MITA_PID=$!
sleep 2
./mita apply config server_udp.json
./mita describe config
./mita start
sleep 1

./mieru run &
MIERU_PID=$!
sleep 2
./mieru apply config client_udp.json
./mieru describe config
./mieru start
sleep 2

cleanup() {
    echo ">>> cleanup <<<"
    ./mieru stop || true
    ./mita stop || true
    kill "$MITA_PID" "$MIERU_PID" "$HTTPSRV" 2>/dev/null || true
}
trap cleanup EXIT

echo ">>> Noise UDP SOCKS5 new_conn load test <<<"
if ! ./sockshttpclient -dst_host=127.0.0.1 -dst_port=8080 \
      -local_proxy_host=127.0.0.1 -local_proxy_port=1080 \
      -test_case=new_conn -num_request=100; then
    print_mieru_client_log
    echo "Noise UDP: SOCKS5 new_conn load test failed"
    exit 1
fi

echo ">>> Noise UDP SOCKS5 reuse_conn load test <<<"
if ! ./sockshttpclient -dst_host=127.0.0.1 -dst_port=8080 \
      -local_proxy_host=127.0.0.1 -local_proxy_port=1080 \
      -test_case=reuse_conn -num_request=100; then
    print_mieru_client_log
    echo "Noise UDP: SOCKS5 reuse load test failed"
    exit 1
fi

echo ">>> Noise E2E UDP test PASSED <<<"
