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

function run_new_conn_test() {
    local config="$1"
    local port="$2"

    sleep 1
    echo ">>> socks5 - new connections with singbox server - $config <<<"
    ./sockshttpclient -dst_host=127.0.0.1 -dst_port=8080 \
      -local_proxy_host=127.0.0.1 -local_proxy_port=${port} \
      -test_case=new_conn -num_request=3000
    if [ "$?" -ne "0" ]; then
        print_mieru_client_log
        echo "Test socks5 new_conn with singbox server ($config) failed."
        exit 1
    fi
}

function run_udp_associate_test() {
    local config="$1"
    local port="$2"

    sleep 1
    echo ">>> socks5 UDP associate - with singbox server - $config <<<"
    ./socksudpclient -dst_host=127.0.0.1 -dst_port=9090 \
      -local_proxy_host=127.0.0.1 -local_proxy_port=${port} \
      -interval_ms=10 -num_request=100 -num_conn=30
    if [ "$?" -ne "0" ]; then
        print_mieru_client_log
        echo "Test UDP associate with singbox server ($config) failed."
        exit 1
    fi
}

function test_mieru_with_config() {
    local config="$1"
    local port="$2"

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
    run_new_conn_test "${config}" "${port}"
    run_udp_associate_test "${config}" "${port}"

    # Stop mieru client.
    ./mieru stop
    if [[ "$?" -ne 0 ]]; then
        echo "command 'mieru stop' failed"
        exit 1
    fi

    delete_mieru_client_log
    sleep 1
}

# Start singbox.
./sing-box run -c singbox-server-config.json &
sleep 1

echo "========== BEGIN OF SERVER TCP TEST =========="
test_mieru_with_config client_tcp.json 1082
test_mieru_with_config client_tcp_no_wait.json 1082
echo "==========  END OF SERVER TCP TEST  =========="

echo "========== BEGIN OF SERVER UDP TEST =========="
test_mieru_with_config client_udp.json 1085
test_mieru_with_config client_udp_no_wait.json 1085
echo "==========  END OF SERVER UDP TEST  =========="
