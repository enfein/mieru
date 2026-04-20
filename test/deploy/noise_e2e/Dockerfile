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

# Assume this file is executed with `docker build -f` from the root dir
# of the mieru project. Relies on binaries produced by `make test-binary`.

FROM ubuntu:24.04

WORKDIR /test

# Copy binaries, configs, and test scripts into the container.
COPY bin/mieru bin/mita bin/httpserver bin/sockshttpclient \
    test/deploy/noise_e2e/client_tcp.json \
    test/deploy/noise_e2e/server_tcp.json \
    test/deploy/noise_e2e/client_udp.json \
    test/deploy/noise_e2e/server_udp.json \
    test/deploy/noise_e2e/libtest.sh \
    test/deploy/noise_e2e/test_tcp.sh \
    test/deploy/noise_e2e/test_udp.sh \
    test/deploy/noise_e2e/test.sh \
    /test/

# Create mita user + server config directories (mirrors basic/).
RUN /usr/sbin/useradd --no-create-home --user-group mita && \
    mkdir -p /etc/mita && \
    chown -R mita:mita /etc/mita && \
    chmod 775 /etc/mita && \
    mkdir -p /var/lib/mita && \
    chown -R mita:mita /var/lib/mita && \
    chmod 775 /var/lib/mita && \
    mkdir -p /var/run/mita && \
    chown -R mita:mita /var/run/mita && \
    chmod 775 /var/run/mita

RUN chmod +x /test/test.sh /test/test_tcp.sh /test/test_udp.sh /test/libtest.sh

CMD ["/test/test.sh"]
