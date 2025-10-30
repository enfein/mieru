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

set -e

function print_mieru_client_log() {
    echo "========== BEGIN OF MIERU CLIENT LOG =========="
    cat $HOME/.cache/mieru/*.log
    echo "==========  END OF MIERU CLIENT LOG  =========="
}

function delete_mieru_client_log() {
    rm -rf $HOME/.cache/mieru/*.log
}

function run_new_conn_test() {
    config="$1"
    sleep 1
    echo ">>> socks5 - new connections with API server - $config <<<"
    ./sockshttpclient -dst_host=127.0.0.1 -dst_port=8080 \
      -local_proxy_host=127.0.0.1 -local_proxy_port=1080 \
      -test_case=new_conn -num_request=3000
    if [ "$?" -ne "0" ]; then
        print_mieru_client_log
        echo "Test socks5 new_conn with API server ($config) failed."
        exit 1
    fi
}

function run_udp_associate_test() {
    config="$1"
    sleep 1
    echo ">>> socks5 UDP associate - with API server - $config <<<"
    ./socksudpclient -dst_host=127.0.0.1 -dst_port=9090 \
      -local_proxy_host=127.0.0.1 -local_proxy_port=1080 \
      -interval_ms=10 -num_request=100 -num_conn=30
    if [ "$?" -ne "0" ]; then
        print_mieru_client_log
        echo "Test UDP associate with API server ($config) failed."
        exit 1
    fi
}

function test_mieru_with_config() {
    config="$1"

    # Update mieru client with TCP config.
    ./mieru apply config $config
    if [[ "$?" -ne 0 ]]; then
        echo "command 'mieru apply config $config' failed"
        exit 1
    fi
    echo "mieru client config:"
    ./mieru describe config

    # Start mieru client.
    ./mieru start
    if [[ "$?" -ne 0 ]]; then
        echo "command 'mieru start' failed"
        exit 1
    fi

    # Start testing.
    run_new_conn_test "$config"
    run_udp_associate_test "$config"

    # Stop mieru client.
    ./mieru stop
    if [[ "$?" -ne 0 ]]; then
        echo "command 'mieru stop' failed"
        exit 1
    fi

    delete_mieru_client_log
    sleep 1
}

# Start http server.
./httpserver &
sleep 2

# Start UDP server.
./udpserver -port=9090 &
sleep 1

# Start mieru API servers.
./exampleapiserver -port=8964 -protocol=TCP -username=baozi -password=manlianpenfen &
sleep 1
./exampleapiserver -port=6489 -protocol=UDP -username=baozi -password=manlianpenfen &
sleep 1

test_mieru_with_config client_tcp.json
test_mieru_with_config client_tcp_no_wait.json
test_mieru_with_config client_udp.json
test_mieru_with_config client_udp_no_wait.json

echo "Test is successful."
sleep 1
exit 0
