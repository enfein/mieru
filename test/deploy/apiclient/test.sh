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

# Start http server.
./httpserver &
sleep 2

# Start UDP server.
./udpserver -port=9090 &
sleep 1

# Start mieru server daemon.
./mita run &
sleep 1

# Update mieru server config.
./mita apply config server_mix.json
if [[ "$?" -ne 0 ]]; then
    echo "command 'mita apply config server_mix.json' failed"
    exit 1
fi
echo "mieru server config:"
./mita describe config
sleep 1

# Start mieru server proxy.
./mita start
if [[ "$?" -ne 0 ]]; then
    echo "command 'mita start' failed"
    exit 1
fi
sleep 1

# Start mieru API clients.
./exampleapiclient -port=1081 -username=baozi -password=manlianpenfen \
  -server_ip=127.0.0.1 -server_port=8964 -server_protocol=TCP &
sleep 1
./exampleapiclient -port=1082 -username=baozi -password=manlianpenfen \
  -server_ip=127.0.0.1 -server_port=6489 -server_protocol=UDP &
sleep 1
./exampleapiclient -port=1083 -username=baozi -password=manlianpenfen \
  -server_ip=127.0.0.1 -server_port=8964 -server_protocol=TCP -handshake_mode=HANDSHAKE_NO_WAIT -bypass_dns &
sleep 1
./exampleapiclient -port=1084 -username=baozi -password=manlianpenfen \
  -server_ip=127.0.0.1 -server_port=6489 -server_protocol=UDP -handshake_mode=HANDSHAKE_NO_WAIT -bypass_dns &
sleep 1

echo "========== BEGIN OF TCP TEST =========="
sleep 1
echo ">>> socks5 - new connections with API client - TCP <<<"
./sockshttpclient -dst_host=127.0.0.1 -dst_port=8080 \
  -local_proxy_host=127.0.0.1 -local_proxy_port=1081 \
  -test_case=new_conn -num_request=3000
if [ "$?" -ne "0" ]; then
    echo "TCP - test socks5 new_conn with API client failed."
    exit 1
fi

sleep 1
echo ">>> socks5 - new connections with API client - TCP - handshake no wait <<<"
./sockshttpclient -dst_host=127.0.0.1 -dst_port=8080 \
  -local_proxy_host=127.0.0.1 -local_proxy_port=1083 \
  -test_case=new_conn -num_request=3000
if [ "$?" -ne "0" ]; then
    echo "TCP - test socks5 new_conn (handshake no wait) with API client failed."
    exit 1
fi

sleep 1
echo ">>> socks5 UDP associate - TCP with API client <<<"
./socksudpclient -dst_host=127.0.0.1 -dst_port=9090 \
  -local_proxy_host=127.0.0.1 -local_proxy_port=1081 \
  -interval_ms=10 -num_request=100 -num_conn=30
if [ "$?" -ne "0" ]; then
    echo "Test UDP associate - TCP with API client failed."
    exit 1
fi

sleep 1
echo ">>> socks5 UDP associate - TCP with API client - handshake no wait <<<"
./socksudpclient -dst_host=127.0.0.1 -dst_port=9090 \
  -local_proxy_host=127.0.0.1 -local_proxy_port=1083 \
  -interval_ms=10 -num_request=100 -num_conn=30
if [ "$?" -ne "0" ]; then
    echo "Test UDP associate - TCP with API client (handshake no wait) failed."
    exit 1
fi
echo "==========  END OF TCP TEST  =========="

echo "========== BEGIN OF UDP TEST =========="
sleep 1
echo ">>> socks5 - new connections with API client - UDP <<<"
./sockshttpclient -dst_host=127.0.0.1 -dst_port=8080 \
  -local_proxy_host=127.0.0.1 -local_proxy_port=1082 \
  -test_case=new_conn -num_request=3000
if [ "$?" -ne "0" ]; then
    echo "UDP - test socks5 new_conn with API client failed."
    exit 1
fi

sleep 1
echo ">>> socks5 - new connections with API client - UDP - handshake no wait <<<"
./sockshttpclient -dst_host=127.0.0.1 -dst_port=8080 \
  -local_proxy_host=127.0.0.1 -local_proxy_port=1084 \
  -test_case=new_conn -num_request=3000
if [ "$?" -ne "0" ]; then
    echo "UDP - test socks5 new_conn (handshake no wait) with API client failed."
    exit 1
fi

sleep 1
echo ">>> socks5 UDP associate - UDP with API client <<<"
./socksudpclient -dst_host=127.0.0.1 -dst_port=9090 \
  -local_proxy_host=127.0.0.1 -local_proxy_port=1082 \
  -interval_ms=10 -num_request=100 -num_conn=30
if [ "$?" -ne "0" ]; then
    echo "Test UDP associate - UDP with API client failed."
    exit 1
fi

sleep 1
echo ">>> socks5 UDP associate - UDP with API client - handshake no wait <<<"
./socksudpclient -dst_host=127.0.0.1 -dst_port=9090 \
  -local_proxy_host=127.0.0.1 -local_proxy_port=1084 \
  -interval_ms=10 -num_request=100 -num_conn=30
if [ "$?" -ne "0" ]; then
    echo "Test UDP associate - UDP with API client (handshake no wait) failed."
    exit 1
fi
echo "==========  END OF UDP TEST  =========="

# Stop mieru server proxy.
./mita stop
if [[ "$?" -ne 0 ]]; then
    echo "command 'mita stop' failed"
    exit 1
fi

echo "Test is successful."
sleep 1
exit 0
