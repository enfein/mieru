#!/bin/bash

# Copyright (C) 2025  mieru authors
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

    sleep 1
    echo ">>> socks5 - new connections - TCP <<<"
    ./sockshttpclient -dst_host=127.0.0.1 -dst_port=8080 \
      -local_proxy_host=127.0.0.1 -local_proxy_port=${port} \
      -test_case=new_conn -num_request=3000
    if [ "$?" -ne "0" ]; then
        print_mieru_server_thread_dump
        echo "TCP - test socks5 new_conn failed."
        exit 1
    fi

    sleep 1
    echo ">>> http - new connections - TCP <<<"
    ./sockshttpclient -proxy_mode=http -dst_host=127.0.0.1 -dst_port=8080 \
      -local_http_host=127.0.0.1 -local_http_port=${port} \
      -test_case=new_conn -num_request=1000
    if [ "$?" -ne "0" ]; then
        print_mieru_server_thread_dump
        echo "TCP - test HTTP new_conn failed."
        exit 1
    fi

    sleep 1
    echo ">>> socks5 - reuse one connection - TCP <<<"
    ./sockshttpclient -dst_host=127.0.0.1 -dst_port=8080 \
      -local_proxy_host=127.0.0.1 -local_proxy_port=${port} \
      -test_case=reuse_conn -test_time_sec=30
    if [ "$?" -ne "0" ]; then
        print_mieru_server_thread_dump
        echo "TCP - test socks5 reuse_conn failed."
        exit 1
    fi

    sleep 1
    echo ">>> socks5 UDP associate - TCP <<<"
    ./socksudpclient -dst_host=127.0.0.1 -dst_port=9090 \
      -local_proxy_host=127.0.0.1 -local_proxy_port=${port} \
      -interval_ms=10 -num_request=100 -num_conn=60
    if [ "$?" -ne "0" ]; then
        print_mieru_server_thread_dump
        echo "TCP - test socks5 udp_associate failed."
        exit 1
    fi
}

function run_udp_tests() {
    local port="$1"

    sleep 1
    echo ">>> socks5 - new connections - UDP <<<"
    ./sockshttpclient -dst_host=127.0.0.1 -dst_port=8080 \
      -local_proxy_host=127.0.0.1 -local_proxy_port=${port} \
      -test_case=new_conn -num_request=3000
    if [ "$?" -ne "0" ]; then
        print_mieru_server_thread_dump
        echo "UDP - test socks5 new_conn failed."
        exit 1
    fi

    sleep 1
    echo ">>> http - new connections - UDP <<<"
    ./sockshttpclient -proxy_mode=http -dst_host=127.0.0.1 -dst_port=8080 \
      -local_http_host=127.0.0.1 -local_http_port=${port} \
      -test_case=new_conn -num_request=1000
    if [ "$?" -ne "0" ]; then
        print_mieru_server_thread_dump
        echo "UDP - test HTTP new_conn failed."
        exit 1
    fi

    sleep 1
    echo ">>> socks5 - reuse one connection - UDP <<<"
    ./sockshttpclient -dst_host=127.0.0.1 -dst_port=8080 \
      -local_proxy_host=127.0.0.1 -local_proxy_port=${port} \
      -test_case=reuse_conn -test_time_sec=30
    if [ "$?" -ne "0" ]; then
        print_mieru_server_thread_dump
        echo "UDP - test socks5 reuse_conn failed."
        exit 1
    fi

    sleep 1
    echo ">>> socks5 UDP associate - UDP <<<"
    ./socksudpclient -dst_host=127.0.0.1 -dst_port=9090 \
      -local_proxy_host=127.0.0.1 -local_proxy_port=${port} \
      -interval_ms=10 -num_request=100 -num_conn=60
    if [ "$?" -ne "0" ]; then
        print_mieru_server_thread_dump
        echo "UDP - test socks5 udp_associate failed."
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
./sing-box run -c singbox-config-tcp.json &
sleep 1
run_tcp_tests 1080
print_mieru_server_metrics
sleep 1
./mita stop
if [[ "$?" -ne 0 ]]; then
    echo "command 'mita stop' failed"
    exit 1
fi
echo "==========  END OF CLIENT TCP TEST  =========="

echo "========== BEGIN OF CLIENT UDP TEST =========="
./mita apply config server_udp.json
if [[ "$?" -ne 0 ]]; then
    echo "command 'mita apply config server_udp.json' failed"
    exit 1
fi
echo "mieru server config:"
./mita describe config
./mita start
if [[ "$?" -ne 0 ]]; then
    echo "command 'mita start' failed"
    exit 1
fi
./sing-box run -c singbox-config-udp.json &
sleep 1
run_udp_tests 1083
print_mieru_server_metrics
sleep 1
./mita stop
if [[ "$?" -ne 0 ]]; then
    echo "command 'mita stop' failed"
    exit 1
fi
echo "==========  END OF CLIENT UDP TEST  =========="
