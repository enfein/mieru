#!/bin/bash

# Copyright (C) 2024  mieru authors
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

# Load test library.
source ./libtest.sh

function run_tcp_tests() {
    local port="$1"
    local suffix="${2:-}"

    sleep 1
    echo ">>> socks5 - new connections - TCP ${suffix} <<<"
    ./sockshttpclient -dst_host=127.0.0.1 -dst_port=8080 \
      -local_proxy_host=127.0.0.1 -local_proxy_port=${port} \
      -test_case=new_conn -num_request=3000
    if [ "$?" -ne "0" ]; then
        print_mieru_server_thread_dump
        echo "TCP - test socks5 new_conn ${suffix} failed."
        exit 1
    fi

    sleep 1
    echo ">>> socks5 - reuse one connection - TCP ${suffix} <<<"
    ./sockshttpclient -dst_host=127.0.0.1 -dst_port=8080 \
      -local_proxy_host=127.0.0.1 -local_proxy_port=${port} \
      -test_case=reuse_conn -test_time_sec=30
    if [ "$?" -ne "0" ]; then
        print_mieru_server_thread_dump
        echo "TCP - test socks5 reuse_conn ${suffix} failed."
        exit 1
    fi

    sleep 1
    echo ">>> socks5 UDP associate - TCP ${suffix} <<<"
    ./socksudpclient -dst_host=127.0.0.1 -dst_port=9090 \
      -local_proxy_host=127.0.0.1 -local_proxy_port=${port} \
      -interval_ms=10 -num_request=100 -num_conn=30
    if [ "$?" -ne "0" ]; then
        print_mieru_server_thread_dump
        echo "TCP - test socks5 udp_associate ${suffix} failed."
        exit 1
    fi
}

function run_udp_tests() {
    local port="$1"
    local suffix="${2:-}"

    sleep 1
    echo ">>> socks5 - new connections - UDP ${suffix} <<<"
    ./sockshttpclient -dst_host=127.0.0.1 -dst_port=8080 \
      -local_proxy_host=127.0.0.1 -local_proxy_port=${port} \
      -test_case=new_conn -num_request=3000
    if [ "$?" -ne "0" ]; then
        print_mieru_server_thread_dump
        echo "UDP - test socks5 new_conn ${suffix} failed."
        exit 1
    fi

    sleep 1
    echo ">>> socks5 - reuse one connection - UDP ${suffix} <<<"
    ./sockshttpclient -dst_host=127.0.0.1 -dst_port=8080 \
      -local_proxy_host=127.0.0.1 -local_proxy_port=${port} \
      -test_case=reuse_conn -test_time_sec=30
    if [ "$?" -ne "0" ]; then
        print_mieru_server_thread_dump
        echo "UDP - test socks5 reuse_conn ${suffix} failed."
        exit 1
    fi

    sleep 1
    echo ">>> socks5 UDP associate - UDP ${suffix} <<<"
    ./socksudpclient -dst_host=127.0.0.1 -dst_port=9090 \
      -local_proxy_host=127.0.0.1 -local_proxy_port=${port} \
      -interval_ms=10 -num_request=100 -num_conn=30
    if [ "$?" -ne "0" ]; then
        print_mieru_server_thread_dump
        echo "UDP - test socks5 udp_associate ${suffix} failed."
        exit 1
    fi
}

echo "========== BEGIN OF CLIENT TCP TEST =========="
./mita apply config server_tcp.json
if [[ "$?" -ne 0 ]]; then
    echo "command 'mita apply config server_tcp.json' failed"
    exit 1
fi
echo "mieru server config:"
./mita describe config
./mita start
if [[ "$?" -ne 0 ]]; then
    echo "command 'mita start' failed"
    exit 1
fi
./mihomo -f mihomo-client-tcp.yaml &
sleep 1
./mihomo -f mihomo-client-tcp-no-wait.yaml &
sleep 1
run_tcp_tests 1080
run_tcp_tests 1081 "(handshake no wait)"
print_mieru_server_metrics
sleep 1
./mita stop
if [[ "$?" -ne 0 ]]; then
    echo "command 'mita stop' failed"
    exit 1
fi
echo "==========  END OF CLIENT TCP TEST  =========="

# echo "========== BEGIN OF CLIENT UDP TEST =========="
# ./mita apply config server_udp.json
# if [[ "$?" -ne 0 ]]; then
#     echo "command 'mita apply config server_udp.json' failed"
#     exit 1
# fi
# echo "mieru server config:"
# ./mita describe config
# ./mita start
# if [[ "$?" -ne 0 ]]; then
#     echo "command 'mita start' failed"
#     exit 1
# fi
# ./mihomo -f mihomo-client-udp.yaml &
# sleep 1
# ./mihomo -f mihomo-client-udp-no-wait.yaml &
# sleep 1
# run_udp_tests 1083
# run_udp_tests 1084 "(handshake no wait)"
# print_mieru_server_metrics
# sleep 1
# ./mita stop
# if [[ "$?" -ne 0 ]]; then
#     echo "command 'mita stop' failed"
#     exit 1
# fi
# echo "==========  END OF CLIENT UDP TEST  =========="
