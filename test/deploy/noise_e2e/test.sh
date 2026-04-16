#!/bin/bash

# Copyright (C) 2026  mieru authors
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

set -e
cd "$(dirname "$0")"

echo "========== BEGIN OF NOISE TCP TEST =========="
./test_tcp.sh
echo "==========  END OF NOISE TCP TEST  =========="

echo "========== BEGIN OF NOISE UDP TEST =========="
./test_udp.sh
echo "==========  END OF NOISE UDP TEST  =========="

echo ">>> All Noise E2E tests PASSED <<<"
