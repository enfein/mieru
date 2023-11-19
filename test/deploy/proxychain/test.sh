#!/bin/bash

# Copyright (C) 2023  mieru authors
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

function print_mieru_server_thread_dump() {
    echo "========== BEGIN OF MIERU SERVER THREAD DUMP 1 =========="
    MITA_UDS_PATH=/var/run/mita1.sock MITA_CONFIG_JSON_FILE=/test/server1.json ./mita get thread-dump
    echo "==========  END OF MIERU SERVER THREAD DUMP 1  =========="
    echo "========== BEGIN OF MIERU SERVER THREAD DUMP 2 =========="
    MITA_UDS_PATH=/var/run/mita2.sock MITA_CONFIG_JSON_FILE=/test/server2.json ./mita2 get thread-dump
    echo "==========  END OF MIERU SERVER THREAD DUMP 2  =========="
}

function print_mieru_client_thread_dump() {
    echo "========== BEGIN OF MIERU CLIENT THREAD DUMP 1 =========="
    MIERU_CONFIG_JSON_FILE=/test/client1.json ./mieru get thread-dump
    echo "==========  END OF MIERU CLIENT THREAD DUMP 1  =========="
    echo "========== BEGIN OF MIERU CLIENT THREAD DUMP 2 =========="
    MIERU_CONFIG_JSON_FILE=/test/client2.json ./mieru2 get thread-dump
    echo "==========  END OF MIERU CLIENT THREAD DUMP 2  =========="
}

# Start http server.
./httpserver -port=6000 &
sleep 2

# Start server 2.
MITA_UDS_PATH=/var/run/mita2.sock MITA_CONFIG_JSON_FILE=/test/server2.json ./mita2 run &
sleep 1

# Start client 2.
MIERU_CONFIG_JSON_FILE=/test/client2.json ./mieru2 run &
sleep 1

# Start server 1.
MITA_UDS_PATH=/var/run/mita1.sock MITA_CONFIG_JSON_FILE=/test/server1.json ./mita run &
sleep 1

# Start client 1.
MIERU_CONFIG_JSON_FILE=/test/client1.json ./mieru run &
sleep 1

set +e
echo ">>> socks5 - new connections <<<"
./sockshttpclient -dst_host=127.0.0.1 -dst_port=6000 \
  -local_proxy_host=127.0.0.1 -local_proxy_port=2000 \
  -test_case=new_conn -num_request=1000
if [ "$?" -ne "0" ]; then
    print_mieru_client_log
    print_mieru_client_thread_dump
    print_mieru_server_thread_dump
    echo "Test socks5 new_conn failed."
    exit 1
fi

echo "Test is successful."
exit 0
