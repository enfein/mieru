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

function print_mieru_client_log() {
    echo "========== BEGIN OF MIERU CLIENT LOG =========="
    cat /root/.cache/mieru/*.log
    echo "==========  END OF MIERU CLIENT LOG  =========="
}

function print_mieru_server_thread_dump() {
    echo "========== BEGIN OF MIERU SERVER THREAD DUMP =========="
    ./mita get thread-dump
    echo "==========  END OF MIERU SERVER THREAD DUMP  =========="
}

function print_mieru_client_thread_dump() {
    echo "========== BEGIN OF MIERU CLIENT THREAD DUMP =========="
    ./mieru get thread-dump
    echo "==========  END OF MIERU CLIENT THREAD DUMP  =========="
}

# Move to working dir.
cd /test

# Start http server.
./httpserver &
sleep 1

# Start mieru server daemon.
./mita run &
sleep 1

# Update mieru server with TCP config.
./mita apply config server_tcp.json
if [[ "$?" -ne 0 ]]; then
    echo "command 'mita apply config server_tcp.json' failed"
    exit 1
fi
echo "mieru server config:"
./mita describe config

# Start mieru server proxy.
./mita start
if [[ "$?" -ne 0 ]]; then
    echo "command 'mita start' failed"
    exit 1
fi
./mita profile cpu start /test/mita.tcp.cpu.gz

# Update mieru client with TCP config.
./mieru apply config client_tcp.json
if [[ "$?" -ne 0 ]]; then
    echo "command 'mieru apply config client_tcp.json' failed"
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
./mieru profile cpu start /test/mieru.tcp.cpu.gz

# Start testing.
sleep 1
./sockshttpclient -dst_host=127.0.0.1 -dst_port=8080 \
  -local_proxy_host=127.0.0.1 -local_proxy_port=1080 \
  -test_case=new_conn -num_request=7200
if [ "$?" -ne "0" ]; then
    print_mieru_client_log
    print_mieru_client_thread_dump
    print_mieru_server_thread_dump
    echo "Test new_conn failed."
    exit 1
fi

sleep 1
./sockshttpclient -dst_host=127.0.0.1 -dst_port=8080 \
  -local_proxy_host=127.0.0.1 -local_proxy_port=1080 \
  -test_case=reuse_conn -num_request=7200
if [ "$?" -ne "0" ]; then
    print_mieru_client_log
    print_mieru_client_thread_dump
    print_mieru_server_thread_dump
    echo "Test reuse_conn failed."
    exit 1
fi

# Collect profile with TCP.
./mieru profile cpu stop
./mita profile cpu stop
./mieru get heap-profile /test/mieru.tcp.heap.gz
./mita get heap-profile /test/mita.tcp.heap.gz

# Stop mieru client.
./mieru stop
if [[ "$?" -ne 0 ]]; then
    echo "command 'mieru stop' failed"
    exit 1
fi
sleep 1

# Stop mieru server proxy.
./mita stop
if [[ "$?" -ne 0 ]]; then
    echo "command 'mita stop' failed"
    exit 1
fi
sleep 1

# Update mieru server with UDP config.
./mita apply config server_udp.json
if [[ "$?" -ne 0 ]]; then
    echo "command 'mita apply config server_udp.json' failed"
    exit 1
fi
echo "mieru server config:"
./mita describe config

# Start mieru server proxy.
./mita start
if [[ "$?" -ne 0 ]]; then
    echo "command 'mita start' failed"
    exit 1
fi
./mita profile cpu start /test/mita.udp.cpu.gz

# Update mieru client with UDP config.
./mieru apply config client_udp.json
if [[ "$?" -ne 0 ]]; then
    echo "command 'mieru apply config client_udp.json' failed"
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
./mieru profile cpu start /test/mieru.udp.cpu.gz

# Start testing.
sleep 1
./sockshttpclient -dst_host=127.0.0.1 -dst_port=8080 \
  -local_proxy_host=127.0.0.1 -local_proxy_port=1080 \
  -test_case=new_conn -num_request=900
if [ "$?" -ne "0" ]; then
    print_mieru_client_log
    print_mieru_client_thread_dump
    print_mieru_server_thread_dump
    echo "Test new_conn failed."
    exit 1
fi

sleep 1
./sockshttpclient -dst_host=127.0.0.1 -dst_port=8080 \
  -local_proxy_host=127.0.0.1 -local_proxy_port=1080 \
  -test_case=reuse_conn -num_request=900
if [ "$?" -ne "0" ]; then
    print_mieru_client_log
    print_mieru_client_thread_dump
    print_mieru_server_thread_dump
    echo "Test reuse_conn failed."
    exit 1
fi

# Collect profile with UDP.
./mieru profile cpu stop
./mita profile cpu stop
./mieru get heap-profile /test/mieru.udp.heap.gz
./mita get heap-profile /test/mita.udp.heap.gz

print_mieru_client_log
echo "Test is successful."
exit 0
