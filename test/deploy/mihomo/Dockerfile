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

# Assume this file is executed with `docker build -f` from the root dir of mieru project.

FROM ubuntu:24.04

WORKDIR /test

# Copy binaries, data and test script into the container.
COPY mihomo mieru mita httpserver sockshttpclient socksudpclient udpserver \
    test/deploy/mihomo/mihomo-client-tcp.yaml \
    test/deploy/mihomo/mihomo-client-tcp-no-wait.yaml \
    test/deploy/mihomo/mihomo-server.yaml \
    test/deploy/mihomo/client_tcp_no_wait.json \
    test/deploy/mihomo/client_tcp.json \
    test/deploy/mihomo/client_udp_no_wait.json \
    test/deploy/mihomo/client_udp.json \
    test/deploy/mihomo/server_tcp.json \
    test/deploy/mihomo/libtest.sh \
    test/deploy/mihomo/test_client_tcp.sh \
    test/deploy/mihomo/test_server.sh \
    test/deploy/mihomo/test.sh /test/

# Create mita user and server config directory.
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

CMD ["/test/test.sh"]
