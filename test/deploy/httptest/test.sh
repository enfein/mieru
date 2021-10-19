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

# Update mieru server config.
./mita apply config test/deploy/httptest/server.json
if [[ "$?" -ne 0 ]]; then
    echo "command 'mita apply config test/deploy/httptest/server.json' failed"
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

# Update mieru client config.
./mieru apply config test/deploy/httptest/client.json
if [[ "$?" -ne 0 ]]; then
    echo "command 'mieru apply config test/deploy/httptest/client.json' failed"
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
sleep 1
./sockshttpclient -dst_host=127.0.0.1 -dst_port=8080 \
  -local_proxy_host=127.0.0.1 -local_proxy_port=1080 \
  -test_case=new_conn -interval=1000 \
  -num_request=1000
if [ "$?" -ne "0" ]; then
    print_mieru_client_log
    print_mieru_client_thread_dump
    print_mieru_server_thread_dump
    echo "Test failed."
    exit 1
fi

print_mieru_client_log
echo "Test is successful."
exit 0
