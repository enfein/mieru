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
    sys_info = SysInfo()


class SysInfo:

    def __init__(self) -> None:
        self.check_python_version()
        self.check_platform()
        self.check_permission()

        self._package_system = self.detect_package_system()
        if self._package_system == '':
            print_exit('Failed to detect system package manager. Supported: deb, rpm.')
        self._cpu_arch = self.detect_cpu_arch()
        if self._cpu_arch == '':
            print_exit('Failed to detect CPU architecture. Supported: amd64, arm64.')

        self._is_mita_installed = self.is_mita_installed()
        self._installed_mita_version = ''
        self._is_mita_config_applied = False
        if self._is_mita_installed:
            result = run_command(['mita', 'version'], check=True)
            self._installed_mita_version = result.stdout.strip()
            if os.path.exists('/etc/mita/server.conf.pb') and \
                    os.stat('/etc/mita/server.conf.pb').st_size > 0:
                self._is_mita_config_applied = True

        self._latest_mita_version = '' # lazy evaluate


    def check_python_version(self) -> None:
        if sys.version_info < (3, 8, 0):
            print_exit('Python version must be 3.8.0 or higher.')


    def check_platform(self) -> None:
        if not sys.platform.startswith('linux'):
            print_exit('You can only run this program on Linux.')


    def check_permission(self) -> None:
        uid = os.getuid()
        if uid != 0:
            print_exit('Only root user can run this program.')


    def detect_package_system(self) -> str:
        if self.is_deb():
            return 'deb'
        elif self.is_rpm():
            return 'rpm'
        else:
            return ''


    def is_deb(self) -> bool:
        '''
        Return true if system uses deb package.
        '''
        result = run_command(['dpkg', '-l'])
        return result.returncode == 0 and len(result.stdout.splitlines()) > 1


    def is_rpm(self) -> bool:
        '''
        Return true if system uses rpm package.
        '''
        result = run_command(['rpm', '-qa'])
        return result.returncode == 0 and len(result.stdout.splitlines()) > 1


    def is_mita_installed(self) -> bool:
        '''
        Return true if mita deb or rpm package is installed.
        '''
        if self.is_deb():
            result = run_command(['dpkg', '-l'])
            for l in result.stdout.splitlines():
                if "mita" in l:
                    return True
        elif self.is_rpm():
            result = run_command(['rpm', '-qa'])
            for l in result.stdout.splitlines():
                if "mita" in l:
                    return True
        else:
            return False


    def detect_cpu_arch(self) -> str:
        machine = platform.machine()
        if machine == 'x86_64' or machine == 'AMD64':
            return 'amd64'
        elif machine == 'aarch64' or machine == 'arm64':
            return 'arm64'
        return ''


def run_command(args: List[str], input=None, timeout=10, check=False):
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


def print_exit(*values: Any) -> None:
    print(*values)
    sys.exit(1)


if __name__ == '__main__':
    main()
