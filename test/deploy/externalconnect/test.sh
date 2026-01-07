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

# Make sure the current user has root privilege.
uid=$(id -u "$USER")
if [ $uid -ne 0 ]; then
    echo "Error: need root to run this test"
    exit 1
fi

# Start mieru server daemon.
mkdir -p /etc/mita
mkdir -p /var/lib/mita
mkdir -p /var/run/mita
export MITA_INSECURE_UDS=1
./bin/mita run &
sleep 1

# Run UDP test.
echo "========== BEGIN OF UDP TEST =========="
./test/deploy/externalconnect/test_udp.sh
echo "==========  END OF UDP TEST  =========="

# Run TCP test.
echo "========== BEGIN OF TCP TEST =========="
./test/deploy/externalconnect/test_tcp.sh
echo "==========  END OF TCP TEST  =========="

echo "Test is successful."
sleep 1
exit 0
