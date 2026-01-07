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

# Assume this file is executed with `docker build -f` from the root dir of mieru project.

FROM ubuntu:24.04

WORKDIR /test

# Copy binaries, data and test script into the container.
COPY bin/sing-box bin/mita bin/httpserver bin/sockshttpclient bin/socksudpclient bin/udpserver \
    test/deploy/singbox/singbox-client-config-tcp.json \
    test/deploy/singbox/singbox-client-config-udp.json \
    test/deploy/singbox/server_tcp.json \
    test/deploy/singbox/server_udp.json \
    test/deploy/singbox/libtest.sh \
    test/deploy/singbox/test_client.sh \
    test/deploy/singbox/test.sh /test/

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
