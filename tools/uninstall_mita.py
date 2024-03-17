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
This program removes all the files created by mita proxy server.

WARNING: this program is NOT safe against forensic collection, because
mita runtime information may be stored in system journal log.
'''


import os
import subprocess
import sys


def uninstall_mita() -> None:
    print('[check permission]')
    uid = os.getuid()
    if uid != 0:
        print_exit('ERROR: only root user can run this program.')
    else:
        print('OK')

    print('[check package manager]')
    if is_deb():
        print('Package manager is dpkg.')
        deb_uninstall()
    elif is_rpm():
        print('Package manager is rpm.')
        rpm_uninstall()
    else:
        print_exit('ERROR: unable to determine package manager.')


def is_deb() -> bool:
    try:
        result = subprocess.run(['dpkg', '-l'], capture_output=True, text=True)
        if result.returncode == 0 and len(result.stdout.splitlines()) > 1:
            return True
    except Exception:
        pass
    return False


def is_rpm() -> bool:
    try:
        result = subprocess.run(['rpm', '-q', '-a'], capture_output=True, text=True)
        if result.returncode == 0 and len(result.stdout.splitlines()) > 1:
            return True
    except Exception:
        pass
    return False


def run_command(description: str, command: list[str], ignore_exception=False) -> None:
    print(description)
    try:
        result = subprocess.run(command, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True)
        if result.returncode == 0:
            print('OK')
        else:
            print(result.stdout)
    except Exception as e:
        if not ignore_exception:
            print_exit(e)


def deb_uninstall() -> None:
    run_command('[stop mita service]', ['systemctl', 'stop', 'mita'])
    run_command('[uninstall deb package]', ['dpkg', '-P', 'mita'])
    run_command('[remove mita configuration]', ['rm', '-rf', '/etc/mita'])
    run_command('[remove mita metrics]', ['rm', '-rf', '/var/lib/mita'])
    run_command('[remove mita runtime]', ['rm', '-rf', '/var/run/mita'])
    run_command('[remove mita systemd unit]', ['rm', '-f', '/lib/systemd/system/mita.service'])
    run_command('[reload systemd]', ['systemctl', 'daemon-reload'])
    run_command('[delete mita user]', ['userdel', 'mita'])
    run_command('[delete mita group]', ['groupdel', 'mita'])


def rpm_uninstall() -> None:
    run_command('[stop mita service]', ['systemctl', 'stop', 'mita'])
    run_command('[uninstall rpm package]', ['rpm', '-e', 'mita'])
    run_command('[remove mita configuration]', ['rm', '-rf', '/etc/mita'])
    run_command('[remove mita mailbox]', ['rm', '-rf', '/var/spool/mail/mita'])
    run_command('[remove mita metrics]', ['rm', '-rf', '/var/lib/mita'])
    run_command('[remove mita runtime]', ['rm', '-rf', '/var/run/mita'])
    run_command('[remove mita systemd unit]', ['rm', '-f', '/lib/systemd/system/mita.service'])
    run_command('[reload systemd]', ['systemctl', 'daemon-reload'])
    run_command('[delete mita user]', ['userdel', 'mita'])
    run_command('[delete mita group]', ['groupdel', 'mita'])


def print_exit(*values: object) -> None:
    print(*values)
    sys.exit(1)


if __name__ == '__main__':
    uninstall_mita()
