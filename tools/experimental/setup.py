#!/usr/bin/env python3
# -*- coding: utf-8 -*-
#
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


import os
import platform
import subprocess
import sys

from typing import Any, List


def main() -> None:
    check_platform()
    check_version()
    check_permission()
    print(detect_package_system())
    print(detect_cpu_arch())


def detect_package_system() -> str:
    if is_deb():
        return 'deb'
    if is_rpm():
        return 'rpm'
    else:
        return ''


def is_deb() -> bool:
    '''
    Return true if system uses deb package.
    '''
    result = run_command(['dpkg', '-l'], timeout=15)
    return result.returncode == 0 and len(result.stdout.splitlines()) > 1


def is_rpm() -> bool:
    '''
    Return true if system uses rpm package.
    '''
    result = run_command(['rpm', '-qa'], timeout=15)
    return result.returncode == 0 and len(result.stdout.splitlines()) > 1


def detect_cpu_arch() -> str:
    if is_amd64():
        return 'amd64'
    if is_arm64():
        return 'arm64'
    return ''


def is_amd64() -> bool:
    '''
    Return true if the CPU architecture is amd64.
    '''
    machine = platform.machine()
    return machine == 'x86_64' or machine == 'AMD64'


def is_arm64() -> bool:
    '''
    Return true if the CPU architecture is arm64.
    '''
    machine = platform.machine()
    return machine == 'aarch64' or machine == 'arm64'


def run_command(args: List[str], input=None, timeout=None, check=False):
    '''
    Run the command and return a subprocess.CompletedProcess instance.
    '''
    try:
        result = subprocess.run(args,
                                input=input,
                                stdout=subprocess.PIPE,
                                stderr=subprocess.STDOUT,
                                timeout=timeout,
                                check=check,
                                text=True)
    except subprocess.TimeoutExpired as te:
        print_exit(f'Command {te.cmd} timed out after {te.timeout} seconds. Output: {te.output}')
    except subprocess.CalledProcessError as cpe:
        print_exit(f'Command {cpe.cmd} returned code {cpe.returncode}. Output: {cpe.output}')
    return result


def check_platform() -> None:
    if not sys.platform.startswith('linux'):
        print_exit('You can only run this program on Linux.')


def check_version() -> None:
    if sys.version_info < (3, 8, 0):
        print_exit('Python version must be 3.8.0 or higher.')


def check_permission() -> None:
    uid = os.getuid()
    if uid != 0:
        print_exit('Only root user can run this program.')


def print_exit(*values: Any) -> None:
    print(*values)
    sys.exit(1)


if __name__ == '__main__':
    main()
