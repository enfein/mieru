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

# Assume this file is executed with `docker build -f` from the root dir of mieru project.

# This image runs the Noise Protocol Framework unit tests inside a
# clean golang container. It exists so that `make run-container-test-noise`
# can validate the noise package on any host that has OrbStack / Docker
# without requiring a local Go install.
FROM golang:1.22

WORKDIR /src

# Copy the entire source tree. We deliberately build inside the image
# instead of relying on host-built binaries so a broken local toolchain
# cannot mask a real regression.
COPY . /src/

# Pre-fetch module cache so `go test` in CMD doesn't need the network.
RUN go mod download

# The CMD keeps the test output visible and returns a non-zero exit
# code on any failure.
CMD ["go", "test", "-race", "-count=1", "-timeout=180s", \
     "./pkg/cipher", \
     "./pkg/cipher/noise", \
     "./pkg/protocol", \
     "-run=TestNoise|TestHandshake|TestConfig|TestParse|TestIPv4TCP|TestIPv6TCP"]
