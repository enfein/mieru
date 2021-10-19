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

check_command() {
    rc=$(command -v $1 2>&1 > /dev/null; echo $?)
    if [[ ${rc} -ne 0 ]]; then
        echo "command \"$1\" not found in the system"
        exit 1
    fi
}

set -e

check_command "git"
check_command "tar"
check_command "zip"

ROOT=$(git rev-parse --show-toplevel)
PROJECT_NAME=$(basename "$ROOT")

cd "$ROOT"
git clean -fxd

cd ..
tar --exclude="$PROJECT_NAME/.git" -zcvf source.tar.gz "$PROJECT_NAME"
zip -r source.zip "$PROJECT_NAME" -x \*.git\*

cd "$ROOT"
mkdir -p release
mv ../source.tar.gz release
mv ../source.zip release
