#!/bin/bash

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

# Make sure this script has executable permission:
# git update-index --chmod=+x <file>

set -e

# Make sure the current user has root privilege.
uid=$(id -u "$USER")
if [ $uid -ne 0 ]; then
    echo "Error: need root to run this test"
    exit 1
fi

# Create a separate network namespace "sim" for testing.
ip netns add sim

# Create a veth pair, attach server part to the namespace, and assign IP addresses.
ip link add veth-client type veth peer name veth-server
ip link set veth-server netns sim
ip addr add 192.168.234.1/24 dev veth-client
ip netns exec sim ip addr add 192.168.234.2/24 dev veth-server
ip link set veth-client up
ip netns exec sim ip link set lo up
ip netns exec sim ip link set veth-server up

echo "========== BEGIN OF HOST NETWORK CONFIGURATION =========="
ip addr show
echo "==========  END OF HOST NETWORK CONFIGURATION  =========="

echo "========== BEGIN OF NAMESPACED NETWORK CONFIGURATION =========="
ip netns exec sim ip addr show
echo "==========  END OF NAMESPACED NETWORK CONFIGURATION  =========="

# Enable IP forwarding in the host.
echo 1 > /proc/sys/net/ipv4/ip_forward

# Go to the root directory of the project.
cd "$(git rev-parse --show-toplevel)"

# Start http server.
ip netns exec sim ./httpserver -huge &
sleep 2

# Start mieru server daemon.
mkdir -p /etc/mita
export MITA_INSECURE_UDS=1
ip netns exec sim ./mita run &
sleep 1

# Add 100ms delay.
tc qdisc add dev veth-client root netem delay 50ms 10ms distribution normal
ip netns exec sim tc qdisc add dev veth-server root netem delay 50ms 10ms distribution normal

# Randomly drop 0.1% of packets in both directions.
ip netns exec sim iptables -F INPUT
ip netns exec sim iptables -F OUTPUT
ip netns exec sim iptables -A INPUT -p tcp --dport 8964 -m statistic --mode random --probability 0.001 -j DROP
ip netns exec sim iptables -A INPUT -p udp --dport 8964 -m statistic --mode random --probability 0.001 -j DROP
iptables -A INPUT -p tcp --sport 8964 -m statistic --mode random --probability 0.001 -j DROP
iptables -A INPUT -p udp --sport 8964 -m statistic --mode random --probability 0.001 -j DROP

# Run TCP test.
echo "========== BEGIN OF TCP TEST =========="
./test/deploy/packetdrop/test_tcp.sh
echo "==========  END OF TCP TEST  =========="

# Run UDP test.
echo "========== BEGIN OF UDP TEST =========="
./test/deploy/packetdrop/test_udp.sh
echo "==========  END OF UDP TEST  =========="

# Remove root network namespace iptables rules.
iptables -D INPUT -p tcp --sport 8964 -m statistic --mode random --probability 0.001 -j DROP
iptables -D INPUT -p udp --sport 8964 -m statistic --mode random --probability 0.001 -j DROP

echo "Test is successful."
exit 0
