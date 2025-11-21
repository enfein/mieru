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

# This script scans all source code files and checks for GPL v3 license headers.

# Change to project root directory.
cd "$(dirname "$0")/.."

# Colors for output.
RED='\033[0;31m'
GREEN='\033[0;32m'
NC='\033[0m' # No Color

missing_files=()

LICENSE_PATTERN="GNU General Public License"
EXCLUDE_DIRS=(
    ".git"
    "vendor"
)
EXCEPTION_PATHS=(
    "./deployments"
    "./pkg/appctl/proto/google/protobuf"
    "./pkg/congestion/rtt.go"
    "./pkg/log"
    "./pkg/deque"
    "./pkg/socks5"
)

# Build find exclude arguments.
EXCLUDE_ARGS=""
for dir in "${EXCLUDE_DIRS[@]}"; do
    EXCLUDE_ARGS="$EXCLUDE_ARGS -path ./$dir -prune -o"
done

# Check if a file should be excluded from license checking.
is_exception() {
    local file="$1"

    for exception in "${EXCEPTION_PATHS[@]}"; do
        # Check if file matches exception exactly or starts with exception path (for directories).
        if [[ "$file" == "$exception" ]] || [[ "$file" == "$exception/"* ]]; then
            return 0
        fi
    done
    return 1
}

# Check if a file has the license header.
check_license() {
    local file="$1"

    if is_exception "$file"; then
        return 0
    fi

    if ! head -n 20 "$file" | grep -q "$LICENSE_PATTERN"; then
        missing_files+=("$file")
        return 1
    fi
    return 0
}

# Check all files matching a given pattern.
check_files_by_pattern() {
    local pattern="$1"
    while IFS= read -r -d '' file; do
        check_license "$file"
    done < <(find . $EXCLUDE_ARGS -type f -name "$pattern" -print0)
}

echo "Scanning for license headers..."

# Check different file types
check_files_by_pattern "*.go"
check_files_by_pattern "*.py"
check_files_by_pattern "*.sh"
check_files_by_pattern "*.proto"

echo ""
if [ ${#missing_files[@]} -eq 0 ]; then
    echo -e "${GREEN}✓ All source files have license headers${NC}"
    exit 0
else
    echo -e "${RED}✗ Found ${#missing_files[@]} file(s) missing license headers:${NC}"
    echo ""
    for file in "${missing_files[@]}"; do
        echo "  $file"
    done
    exit 1
fi
