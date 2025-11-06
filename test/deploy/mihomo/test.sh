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

set -e

# Load test library.
source ./libtest.sh

# Start http server.
./httpserver &
sleep 2

# Start UDP server.
./udpserver -port=9090 &
sleep 1

# Start mieru server daemon.
./mita run &
sleep 1

# Run server test.
# echo "========== BEGIN OF SERVER TEST =========="
# ./test_server.sh
# echo "==========  END OF SERVER TEST  =========="

# Run client TCP test.
echo "========== BEGIN OF CLIENT TCP TEST =========="
./test_client_tcp.sh
echo "==========  END OF CLIENT TCP TEST  =========="

echo "Test is successful."
sleep 1
exit 0
