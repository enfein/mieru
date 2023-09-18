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

# Run TCP test.
echo "========== BEGIN OF TCP TEST =========="
./test_tcp.sh
echo "==========  END OF TCP TEST  =========="

# Run UDP test.
echo "========== BEGIN OF UDP TEST =========="
./test_udp.sh
echo "==========  END OF UDP TEST  =========="

# Run UDP associate test.
echo "========== BEGIN OF UDP ASSOCIATE TEST =========="
./test_mix_udp_associate.sh
echo "==========  END OF UDP ASSOCIATE TEST  =========="

echo "Test is successful."
exit 0
