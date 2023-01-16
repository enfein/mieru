#!/bin/bash

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

# Make sure this script has executable permission:
# git update-index --chmod=+x <file>

export PATH_PREFIX="test/deploy/packetdrop"

# Load test library.
source ./${PATH_PREFIX}/libtest.sh

# Update mieru server with TCP config.
./mita apply config ${PATH_PREFIX}/server_tcp.json
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

# Update mieru client with TCP config.
./mieru apply config ${PATH_PREFIX}/client_tcp.json
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

# Start testing.
sleep 2
./sockshttpclient -dst_host=127.0.0.1 -dst_port=8080 \
  -local_proxy_host=192.168.234.1 -local_proxy_port=1080 \
  -test_case=reuse_conn -print_speed=1 -test_time_sec=300
if [ "$?" -ne "0" ]; then
    print_mieru_client_log
    print_mieru_client_thread_dump
    print_mieru_server_thread_dump
    echo "Test failed."
    exit 1
fi

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

print_mieru_client_log
delete_mieru_client_log
sleep 1
