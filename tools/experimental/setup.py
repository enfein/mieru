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


import json
import os
import platform
import re
import subprocess
import sys
import urllib.request

from typing import Any, List


def main() -> None:
    sys_info = SysInfo()
    installer = Installer()
    print(installer.download_mita(sys_info))


class SysInfo:

    def __init__(self) -> None:
        self.check_python_version()
        self.check_platform()
        self.check_permission()

        self.package_manager = self.detect_package_manager()
        if self.package_manager == '':
            print_exit('Failed to detect system package manager. Supported: deb, rpm.')
        self.cpu_arch = self.detect_cpu_arch()
        if self.cpu_arch == '':
            print_exit('Failed to detect CPU architecture. Supported: amd64, arm64.')

        self.is_mita_installed = self.detect_mita_installed()
        self.installed_mita_version = None
        self.is_mita_config_applied = False
        if self.is_mita_installed:
            version_out = run_command(['mita', 'version'], check=True)
            version = Version()
            if version.parse(version_out.stdout.strip()):
                self.installed_mita_version = version
            if os.path.exists('/etc/mita/server.conf.pb') and \
                    os.stat('/etc/mita/server.conf.pb').st_size > 0:
                self.is_mita_config_applied = True
        self.latest_mita_version = None
        latest_mita_version_str = self.query_latest_mita_version()
        version2 = Version()
        if version2.parse(latest_mita_version_str):
            self.latest_mita_version = version2


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


    def detect_package_manager(self) -> str:
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


    def detect_mita_installed(self) -> bool:
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


    def query_latest_mita_version(self) -> str:
        '''
        Use GitHub API to fetch the latest mita version.
        '''
        try:
            resp = urllib.request.urlopen("https://api.github.com/repos/enfein/mieru/releases/latest")
            body = resp.read()
            j = json.loads(body.decode('utf-8'))
            return j["tag_name"].strip('v')
        except Exception as e:
            print_exit(f"Failed to query latest mita version: {e}")


class Version:

    def __init__(self, major=None, minor=None, patch=None):
        self.major = major
        self.minor = minor
        self.patch = patch


    def __str__(self):
        return f'{self.major}.{self.minor}.{self.patch}'


    def parse(self, v: str) -> bool:
        match = re.match(r'(\d+)\.(\d+)\.(\d+)', v)
        if match:
            self.major = int(match.group(1))
            self.minor = int(match.group(2))
            self.patch = int(match.group(3))
            return True
        return False


    def is_less_than(self, another) -> bool:
        '''
        Return true if this version is less than another version.
        '''
        if self.major is None or self.minor is None or self.patch is None or \
                another.major is None or another.minor is None or another.patch is None:
            return False  # Handle uninitialized versions

        if self.major < another.major:
            return True
        elif self.major > another.major:
            return False
        if self.minor < another.minor:
            return True
        elif self.minor > another.minor:
            return False
        if self.patch < another.patch:
            return True
        else:
            return False


class Installer:

    def download_mita(self, sys_info: SysInfo) -> str:
        '''
        Download mita deb or rpm installation package.
        Return the path of downloaded file.
        '''
        if sys_info.latest_mita_version == None:
            print_exit('Latest mita version is unknown')
        ver = sys_info.latest_mita_version
        download_url = ''
        if sys_info.package_manager == 'deb' and sys_info.cpu_arch == 'amd64':
            download_url = f'https://github.com/enfein/mieru/releases/download/v{ver}/mita_{ver}_amd64.deb'
        elif sys_info.package_manager == 'deb' and sys_info.cpu_arch == 'arm64':
            download_url = f'https://github.com/enfein/mieru/releases/download/v{ver}/mita_{ver}_arm64.deb'
        elif sys_info.package_manager == 'rpm' and sys_info.cpu_arch == 'amd64':
            download_url = f'https://github.com/enfein/mieru/releases/download/v{ver}/mita-{ver}-1.x86_64.rpm'
        elif sys_info.package_manager == 'rpm' and sys_info.cpu_arch == 'arm64':
            download_url = f'https://github.com/enfein/mieru/releases/download/v{ver}/mita-{ver}-1.aarch64.rpm'
        else:
            print_exit(f'Failed to determine download URL based on package manager {sys_info.package_manager} and CPU architecture {sys_info.cpu_arch}')
        filename = os.path.join('/tmp', download_url.split('/')[-1])
        try:
            urllib.request.urlretrieve(download_url, filename)
        except urllib.error.URLError as e:
            print_exit(f'Failed to download {download_url}: {e}')
        return filename


class Configurer:
    pass


class Uninstaller:
    pass


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
