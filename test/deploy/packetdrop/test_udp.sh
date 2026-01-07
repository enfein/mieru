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

# Update mieru server with UDP config.
./bin/mita apply config ${PATH_PREFIX}/server_udp.json
if [[ "$?" -ne 0 ]]; then
    echo "command 'mita apply config server_udp.json' failed"
    exit 1
fi
echo "mieru server config:"
./bin/mita describe config

# Start mieru server proxy.
./bin/mita start
if [[ "$?" -ne 0 ]]; then
    echo "command 'mita start' failed"
    exit 1
fi

# Update mieru client with UDP config.
if [[ -z "${MIERU_HANDSHAKE_NO_WAIT}" ]]; then
  CLIENT_CONFIG="${PATH_PREFIX}/client_udp_handshake_standard.json"
else
  CLIENT_CONFIG="${PATH_PREFIX}/client_udp_handshake_no_wait.json"
fi
./bin/mieru apply config ${CLIENT_CONFIG}
if [[ "$?" -ne 0 ]]; then
    echo "command 'mieru apply config ${CLIENT_CONFIG}' failed"
    exit 1
fi
echo "mieru client config:"
./bin/mieru describe config

# Start mieru client.
./bin/mieru start
if [[ "$?" -ne 0 ]]; then
    echo "command 'mieru start' failed"
    exit 1
fi

# Start testing.
sleep 2
./bin/sockshttpclient -dst_host=127.0.0.1 -dst_port=8080 \
  -local_proxy_host=192.168.234.1 -local_proxy_port=1080 \
  -test_case=reuse_conn -interval_ms=1000 -print_speed=1 -test_time_sec=900
if [ "$?" -ne "0" ]; then
    print_mieru_client_log
    print_mieru_client_thread_dump
    print_mieru_server_thread_dump
    echo "Test reuse_conn failed."
    exit 1
fi

# Print metrics and memory statistics.
./bin/mita get users
sleep 1
print_mieru_client_metrics
sleep 1
print_mieru_server_metrics
sleep 1

# Stop mieru client.
./bin/mieru stop
if [[ "$?" -ne 0 ]]; then
    echo "command 'mieru stop' failed"
    exit 1
fi
sleep 1

# Stop mieru server proxy.
./bin/mita stop
if [[ "$?" -ne 0 ]]; then
    echo "command 'mita stop' failed"
    exit 1
fi
sleep 1

print_mieru_client_log
delete_mieru_client_log
sleep 1
