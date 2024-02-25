#!/usr/bin/env python3
#
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

'''
This program generates portBindings that can be used in mieru
client and server configurations.
'''


import argparse
import json
import random
import sys


def gen() -> None:
    random.seed()
    args = parse_args()
    bindings = []
    if args.num_tcp_port > 0:
        tcp = PortBindings(protocol='TCP', n=args.num_tcp_port, min_port=args.tcp_min_port, max_port=args.tcp_max_port)
        tcp.select_ports()
        bindings = bindings + tcp.to_bindings()
    if args.num_udp_port > 0:
        udp = PortBindings(protocol='UDP', n=args.num_udp_port, min_port=args.udp_min_port, max_port=args.udp_max_port)
        udp.select_ports()
        bindings = bindings + udp.to_bindings()
    out = {'portBindings': bindings}
    print(json.dumps(out, indent=4))


def parse_args():
    parser = argparse.ArgumentParser(description='Parse port requirements.')
    parser.add_argument('--num_tcp_port', type=int, default=0, required=False, help='Number of TCP ports.')
    parser.add_argument('--num_udp_port', type=int, default=0, required=False, help='Number of UDP ports.')
    parser.add_argument('--tcp_min_port', type=int, default=-1, required=False, help='Minimum TCP port number.')
    parser.add_argument('--tcp_max_port', type=int, default=-1, required=False, help='Maximum TCP port number.')
    parser.add_argument('--udp_min_port', type=int, default=-1, required=False, help='Minimum UDP port number.')
    parser.add_argument('--udp_max_port', type=int, default=-1, required=False, help='Maximum UDP port number.')
    args = parser.parse_args()

    # Verify inputs.
    if args.num_tcp_port < 0:
        print_exit('--num_tcp_port number', args.num_tcp_port, 'is invalid.')
    if args.num_udp_port < 0:
        print_exit('--num_udp_port number', args.num_udp_port, 'is invalid.')
    if args.num_tcp_port == 0 and args.num_udp_port == 0:
        print_exit('Either --num_tcp_port or --num_udp_port should be bigger than 0.')
    if args.num_tcp_port > 0:
        if args.tcp_min_port == -1 or args.tcp_max_port == -1:
            print_exit('--tcp_min_port and --tcp_max_port are required if --num_tcp_port is not 0.')
        elif args.tcp_min_port > args.tcp_max_port:
            print_exit('--tcp_min_port is bigger than --tcp_max_port.')
    if args.num_udp_port > 0:
        if args.udp_min_port == -1 or args.udp_max_port == -1:
            print_exit('--udp_min_port and --udp_max_port are required if --num_udp_port is not 0.')
        elif args.udp_min_port > args.udp_max_port:
            print_exit('--udp_min_port is bigger than --udp_max_port.')
    if args.tcp_min_port != -1 and (args.tcp_min_port < 1 or args.tcp_min_port > 65535):
        print_exit('--tcp_min_port number', args.tcp_min_port, 'is invalid.')
    if args.tcp_max_port != -1 and (args.tcp_max_port < 1 or args.tcp_max_port > 65535):
        print_exit('--tcp_max_port number', args.tcp_max_port, 'is invalid.')
    if args.udp_min_port != -1 and (args.udp_min_port < 1 or args.udp_min_port > 65535):
        print_exit('--udp_min_port number', args.udp_min_port, 'is invalid.')
    if args.udp_max_port != -1 and (args.udp_max_port < 1 or args.udp_max_port > 65535):
        print_exit('--udp_max_port number', args.udp_max_port, 'is invalid.')
    if args.tcp_max_port - args.tcp_min_port + 1 < args.num_tcp_port:
        print_exit("Can't allocate", args.num_tcp_port, 'TCP ports.')
    if args.udp_max_port - args.udp_min_port + 1 < args.num_udp_port:
        print_exit("Can't allocate", args.num_udp_port, 'UDP ports.')

    return args


class PortBindings:

    def __init__(self, protocol: str, n: int, min_port: int, max_port: int):
        self.protocol = protocol
        self.n = n
        self.min_port = min_port
        self.max_port = max_port
        self.selected = []


    def select_ports(self) -> None:
        l = [port for port in range(self.min_port, self.max_port+1)]
        random.shuffle(l)
        self.selected = l[:self.n]
        self.selected = sorted(self.selected)


    def to_bindings(self) -> list[dict]:
        res = []
        begin = None
        prev = None
        self.selected.append(1000000) # a value to teminate the loop
        for port in self.selected:
            if prev != None:
                if port == prev + 1:
                    # This port is adjacent to the previous one.
                    # It should be part of the port range.
                    prev += 1
                    continue
                if prev == begin:
                    # The previous port is a single port.
                    res.append({'port': prev, 'protocol': self.protocol})
                else:
                    # The previous port is a inside a port range.
                    res.append({
                        'portRange': '{}-{}'.format(begin, prev),
                        'protocol': self.protocol,
                    })
            begin = prev = port
        self.selected.pop()
        return res


def print_exit(*values: object) -> None:
    print(*values)
    sys.exit(1)


if __name__ == '__main__':
    gen()
