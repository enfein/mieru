#!/bin/bash
#
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

if [ -z "$1" ]; then
    echo "Current version is not provided"
    echo "Usage: ./bump_version.sh <current-version> <next-version>"
    exit 1
fi

if [ -z "$2" ]; then
    echo "Next version is not provided"
    echo "Usage: ./bump_version.sh <current-version> <next-version>"
    exit 1
fi

ROOT=$(git rev-parse --show-toplevel)
cd $ROOT

sed -i "s/$1/$2/g" build/package/mieru/amd64/debian/DEBIAN/control
sed -i "s/$1/$2/g" build/package/mieru/amd64/rpm/mieru.spec
sed -i "s/$1/$2/g" build/package/mieru/arm64/debian/DEBIAN/control
sed -i "s/$1/$2/g" build/package/mieru/arm64/rpm/mieru.spec
sed -i "s/$1/$2/g" build/package/mita/amd64/debian/DEBIAN/control
sed -i "s/$1/$2/g" build/package/mita/amd64/rpm/mita.spec
sed -i "s/$1/$2/g" build/package/mita/arm64/debian/DEBIAN/control
sed -i "s/$1/$2/g" build/package/mita/arm64/rpm/mita.spec
sed -i "s/$1/$2/g" docs/server-install.md
sed -i "s/$1/$2/g" docs/server-install.zh_CN.md
sed -i "s/$1/$2/g" Makefile
sed -i "s/$1/$2/g" pkg/version/current.go
