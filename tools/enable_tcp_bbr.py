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
This program enables TCP BBR congestion control.
'''


import os
import subprocess
import sys


def enable_tcp_bbr() -> None:
    if not sys.platform.startswith('linux'):
        print_exit('You can only run this program on Linux.')
        return

    if is_bbr_enabled():
        print('BBR is already enabled.')
        return

    uid = os.getuid()
    if uid != 0:
        print_exit('Only root user can run this program.')

    must_run_command(['modprobe', 'tcp_bbr'])
    mods = must_run_command(['lsmod'])
    if 'tcp_bbr' not in mods:
        print_exit('Fail to load tcp_bbr kernel module.')
    must_write_sysctl_file(['net.core.default_qdisc=fq', 'net.ipv4.tcp_congestion_control=bbr'])
    must_run_command(['sysctl', '--system', '--pattern', '^net'])

    if is_bbr_enabled():
        print('BBR is enabled.')
    else:
        print_exit('BBR is not enabled. This program doesn\'t support your operating system.')


def is_bbr_enabled() -> bool:
    try:
        with open('/proc/sys/net/ipv4/tcp_congestion_control', 'r') as f:
            return f.read().strip() == 'bbr'
    except Exception as e:
        print_exit(e)


def must_write_sysctl_file(content: list[str]) -> None:
    try:
        with open('/etc/sysctl.d/mieru_tcp_bbr.conf', 'w') as f:
            for line in content:
                f.write(line)
                f.write('\n')
    except Exception as e:
        print_exit(e)


def must_run_command(command: list[str]) -> str:
    try:
        result = subprocess.run(command, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, check=True, text=True)
        return result.stdout
    except Exception as e:
        print_exit(e)


def print_exit(*values: object) -> None:
    print(*values)
    sys.exit(1)


if __name__ == "__main__":
    enable_tcp_bbr()
