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

'''
This program helps user install, update, configure and uninstall
mita proxy server.
'''


import argparse
import json
import os
import platform
import random
import re
import secrets
import subprocess
import sys
import tempfile
import time
import urllib.request

from typing import Any, Callable, List, Tuple


# Language constants.
ZH = 'zh'

_lang = ''


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument('--lang', type=str, default='en', help='language')
    args = parser.parse_args()
    global _lang
    _lang = args.lang

    sys_info = SysInfo()

    if not sys_info.is_mita_installed:
        install_prompt = '''
[install mita]
mita proxy server is not installed.
Type "y" to install mita proxy server.
Type any other character to exit.
(default is "y")
>>> '''
        if _lang == ZH:
            install_prompt = '''
[安装 mita]
尚未安装 mita 代理服务器软件。
输入 "y" 开始安装 mita 代理服务器软件。
输入其他任意字符退出。
(默认值是 "y")
>>> '''
        install, _ = check_input(prompt=install_prompt, validator=any_validator(), default='y')
        if install != 'y':
            return
        installer = Installer()
        mita_package_path = installer.download_mita(sys_info)
        installer.install_mita(mita_package_path)
        time.sleep(1)
        sys_info.is_mita_installed = sys_info.detect_mita_installed()
        sys_info.is_mita_systemd_active = sys_info.detect_mita_systemd_active()
        sys_info.installed_mita_version = sys_info.detect_mita_version()
        if sys_info.is_mita_config_applied:
            return
        # fallthrough

    if sys_info.is_mita_installed and sys_info.installed_mita_version.is_less_than(sys_info.latest_mita_version):
        update_prompt = f'''
[update mita]
mita proxy server {sys_info.installed_mita_version} is installed.
A new version {sys_info.latest_mita_version} is available.
Type "y" to update mita proxy server.
Type any other character to exit.
(default is "y")
>>> '''
        if _lang == ZH:
            update_prompt = f'''
[更新 mita]
已安装的 mita 代理服务器软件版本是 {sys_info.installed_mita_version} 。
最新版本是 {sys_info.latest_mita_version} 。
输入 "y" 开始更新 mita 代理服务器软件。
输入其他任意字符退出。
(默认值是 "y")
>>> '''
        update, _ = check_input(prompt=update_prompt, validator=any_validator(), default='y')
        if update != 'y':
            return
        installer = Installer()
        mita_package_path = installer.download_mita(sys_info)
        installer.install_mita(mita_package_path)
        time.sleep(1)
        sys_info.is_mita_installed = sys_info.detect_mita_installed()
        sys_info.is_mita_systemd_active = sys_info.detect_mita_systemd_active()
        sys_info.installed_mita_version = sys_info.detect_mita_version()
        return # exit after update is successful, assume it is already configured

    if not sys_info.is_mita_config_applied:
        configure_prompt = '''
[configure mita server]
mita proxy server is installed but not configured.
Type "y" to configure mita proxy server.
Type any other character to exit.
(default is "y")
>>> '''
        if _lang == ZH:
            configure_prompt = '''
[配置 mita 代理服务器]
mita 代理服务器已经安装但尚未配置。
输入 "y" 开始配置 mita 代理服务器。
输入其他任意字符退出。
(默认值是 "y")
>>> '''
        configure, _ = check_input(prompt=configure_prompt, validator=any_validator(), default='y')
        if configure != 'y':
            return
        configurer = Configurer()
        add_op_user_prompt = '''
[configure mita server][add Linux operation user]
Type a Linux user name to add the user to "mita" group,
such that the user can invoke mita command.
Otherwise, only root user can invoke mita command.
Press Enter to skip (default).
>>> '''
        if _lang == ZH:
            add_op_user_prompt = '''
[配置 mita 代理服务器][添加 Linux 操作用户]
输入一个 Linux 用户名，将其添加至 "mita" 用户组，
该用户将可以调用 mita 指令。
否则，只有 root 用户可以调用 mita 指令。
输入回车跳过这个步骤(默认)。
>>> '''
        op_user, _ = check_input(prompt=add_op_user_prompt, validator=any_validator())
        if op_user != "":
            if configurer.add_operation_user(op_user):
                if _lang == ZH:
                    print(f'已添加 {op_user} 至 mita 用户组。')
                else:
                    print(f'Added {op_user} to mita group.')
            else:
                if _lang == ZH:
                    print(f'添加 {op_user} 至 mita 用户组失败。')
                else:
                    print(f'Failed to add {op_user} to mita group.')
        configurer.configure_server(sys_info)
        if not configurer.restart_mita():
            if _lang == ZH:
                print_exit('重新启动 mita 代理服务失败。')
            else:
                print_exit('Failed to restart mita proxy server.')
        sys_info.is_mita_config_applied = True
        build_client_prompt = '''
[configure mieru client]
Type "y" to generate mieru proxy client configuration.
Type any other character to exit.
(default is "y")
>>> '''
        if _lang == ZH:
            build_client_prompt = '''
[配置 mieru 客户端]
输入 "y" 生成 mieru 代理客户端的配置。
输入其他任意字符退出。
(默认值是 "y")
>>> '''
        build_client, _ = check_input(prompt=build_client_prompt, validator=any_validator(), default='y')
        if build_client != 'y':
            return
        configurer.build_client_configuration()
        return # exit after configuration is successful

    if sys_info.is_mita_installed:
        uninstall_prompt = '''
[uninstall mita]
mita proxy server is installed.
Type "y" to uninstall mita proxy server and delete configuration.
Type any other character to exit.
(default is "n")
>>> '''
        if _lang == ZH:
            uninstall_prompt = '''
[卸载 mita]
已经安装 mita 代理服务器。
输入 "y" 卸载 mita 代理服务器并删除配置。
输入其他任意字符退出。
(默认值是 "n")
>>> '''
        uninstall, _ = check_input(prompt=uninstall_prompt, validator=any_validator(), default='n')
        if uninstall != 'y':
            return
        uninstaller = Uninstaller()
        uninstaller.uninstall_mita(sys_info)


class SysInfo:

    def __init__(self) -> None:
        self.check_python_version()
        self.check_platform()
        self.check_permission()

        self.package_manager = self.detect_package_manager()
        if self.package_manager == '':
            if _lang == ZH:
                print_exit('检测系统包管理器失败。支持 deb 和 rpm。')
            else:
                print_exit('Failed to detect system package manager. Supported: deb, rpm.')
        self.cpu_arch = self.detect_cpu_arch()
        if self.cpu_arch == '':
            if _lang == ZH:
                print_exit('检测 CPU 架构失败。支持 amd64 和 arm64。')
            else:
                print_exit('Failed to detect CPU architecture. Supported: amd64, arm64.')

        self.is_mita_installed = self.detect_mita_installed()
        self.is_mita_systemd_active = self.detect_mita_systemd_active()
        self.installed_mita_version = None
        if self.is_mita_installed:
            self.installed_mita_version = self.detect_mita_version()
        self.is_mita_config_applied = False
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
            if _lang == ZH:
                print_exit('Python 版本必须为 3.8.0 或更高。')
            else:
                print_exit('Python version must be 3.8.0 or higher.')


    def check_platform(self) -> None:
        if not sys.platform.startswith('linux'):
            if _lang == ZH:
                print_exit('只能在 Linux 系统中运行此程序。')
            else:
                print_exit('You can only run this program on Linux.')


    def check_permission(self) -> None:
        uid = os.getuid()
        if uid != 0:
            if _lang == ZH:
                print_exit('只有 root 用户可以运行此程序。')
            else:
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
        try:
            subprocess.run(['dpkg', '--version'], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        except FileNotFoundError:
            return False
        result = run_command(['dpkg', '-l'])
        return result.returncode == 0 and len(result.stdout.splitlines()) > 1


    def is_rpm(self) -> bool:
        '''
        Return true if system uses rpm package.
        '''
        try:
            subprocess.run(['rpm', '--version'], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        except FileNotFoundError:
            return False
        result = run_command(['rpm', '-qa'])
        return result.returncode == 0 and len(result.stdout.splitlines()) > 1


    def detect_mita_installed(self) -> bool:
        '''
        Return true if mita deb or rpm package is installed.
        '''
        try:
            subprocess.run(['mita', 'version'], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        except FileNotFoundError:
            return False
        if self.is_deb():
            result = run_command(['dpkg', '-l'])
            for l in result.stdout.splitlines():
                if 'mita' in l:
                    return True
        elif self.is_rpm():
            result = run_command(['rpm', '-qa'])
            for l in result.stdout.splitlines():
                if 'mita' in l:
                    return True
        else:
            return False


    def detect_mita_version(self):
        version_out = run_command(['mita', 'version'], check=True)
        version = Version()
        if version.parse(version_out.stdout.strip()):
            return version
        return None


    def detect_mita_systemd_active(self) -> bool:
        result = run_command(['systemctl', 'is-active', 'mita'])
        return result.returncode == 0


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
            resp = urllib.request.urlopen('https://api.github.com/repos/enfein/mieru/releases/latest')
            body = resp.read()
            j = json.loads(body.decode('utf-8'))
            return j['tag_name'].strip('v')
        except Exception as e:
            if _lang == ZH:
                print_exit(f'查询最新的 mita 版本失败：{e}')
            else:
                print_exit(f'Failed to query latest mita version: {e}')


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


class ServerConfig:

    def __init__(self):
        self.config = {
            'portBindings': [],
            'users': [],
            'loggingLevel': 'INFO',
            'mtu': 1400,
        }


    def users(self) -> List:
        return self.config['users']


    def port_bindings(self) -> List:
        return self.config['portBindings']


    def set_user(self, name: str, password: str) -> None:
        self.config['users'].append({
            'name': name,
            'password': password,
        })


    def add_port(self, port: int, protocol: str) -> None:
        self.config['portBindings'].append({
            'port': port,
            'protocol': protocol,
        })


    def add_port_range(self, port_range: str, protocol: str) -> None:
        self.config['portBindings'].append({
            'portRange': port_range,
            'protocol': protocol,
        })


    def to_json(self) -> str:
        return json.dumps(self.config, indent=4)


class ClientConfig:

    def __init__(self):
        self.config = {
            'profiles': [{
                'profileName': 'default',
                'user': {},
                'servers': [{
                    'ipAddress': '',
                    'portBindings': [{}],
                }]
            }],
            'activeProfile': 'default',
            'rpcPort': 0,
            'socks5Port': 0,
            'loggingLevel': 'INFO',
            'httpProxyPort': 0,
        }


    def set_user(self, name: str, password: str) -> None:
        self.config['profiles'][0]['user']['name'] = name
        self.config['profiles'][0]['user']['password'] = password


    def set_ip_port(self, ip: str, port: int, protocol: str) -> None:
        self.config['profiles'][0]['servers'][0]['ipAddress'] = ip
        self.config['profiles'][0]['servers'][0]['portBindings'][0]['port'] = port
        self.config['profiles'][0]['servers'][0]['portBindings'][0]['protocol'] = protocol


    def set_ip_port_range(self, ip: str, port_range: str, protocol: str) -> None:
        self.config['profiles'][0]['servers'][0]['ipAddress'] = ip
        self.config['profiles'][0]['servers'][0]['portBindings'][0]['portRange'] = port_range
        self.config['profiles'][0]['servers'][0]['portBindings'][0]['protocol'] = protocol


    def set_rpc_port(self, port: int) -> None:
        self.config['rpcPort'] = port


    def set_socks5_port(self, port: int) -> None:
        self.config['socks5Port'] = port


    def set_http_proxy_port(self, port: int) -> None:
        self.config['httpProxyPort'] = port


    def to_json(self) -> str:
        return json.dumps(self.config, indent=4)


class Installer:

    def download_mita(self, sys_info: SysInfo) -> str:
        '''
        Download mita deb or rpm installation package.
        Return the path of downloaded file.
        '''
        if sys_info.latest_mita_version == None:
            if _lang == ZH:
                print_exit('获取 mita 的最新版本失败。')
            else:
                print_exit('Latest mita version is unknown.')
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
            if _lang == ZH:
                print_exit(f'从包管理器 {sys_info.package_manager} 和 CPU 架构 {sys_info.cpu_arch} 无法决定下载链接。')
            else:
                print_exit(f'Failed to determine download URL based on package manager {sys_info.package_manager} and CPU architecture {sys_info.cpu_arch}.')
        filename = os.path.join('/tmp', download_url.split('/')[-1])
        try:
            if _lang == ZH:
                print(f'正在下载 {download_url}')
            else:
                print(f'Downloading from {download_url}')
            urllib.request.urlretrieve(download_url, filename)
            if _lang == ZH:
                print(f'下载文件存储在 {filename}')
            else:
                print(f'Downloaded to {filename}')
        except urllib.error.URLError as e:
            if _lang == ZH:
                print_exit(f'下载 {download_url} 失败：{e}')
            else:
                print_exit(f'Failed to download {download_url}: {e}')
        return filename


    def install_mita(self, package_path: str) -> None:
        basename = os.path.basename(package_path)
        ext = basename.split('.')[-1]
        if ext == 'deb':
            run_command(args=['dpkg', '-i', package_path],
                        timeout=60, check=True, print_args=True, print_stdout=True)
            if _lang == ZH:
                print(f'已安装 {package_path}')
            else:
                print(f'Installed {package_path}')
        elif ext == 'rpm':
            run_command(args=['rpm', '-Uvh', '--force', package_path],
                        timeout=60, check=True, print_args=True, print_stdout=True)
            if _lang == ZH:
                print(f'已安装 {package_path}')
            else:
                print(f'Installed {package_path}')
        else:
            if _lang == ZH:
                print_exit(f'无法安装 {basename}：它不是 deb 或 rpm 安装包。')
            else:
                print_exit(f'Unable to install {basename}: it is not a deb or a rpm package.')


class Configurer:

    def __init__(self):
        self._server_config = ServerConfig()
        self._client_config = ClientConfig()


    def add_operation_user(self, user: str) -> bool:
        '''
        Add user to mita group.
        Return true if it is successful.
        '''
        return run_command(['usermod', '-a', '-G', 'mita', user],
                           print_args=True, print_stdout=True).returncode == 0


    def generate_random_str(self, length=8) -> str:
        return secrets.token_urlsafe(length)[:length]


    def configure_server(self, sys_info: SysInfo) -> None:
        '''
        Interactively configure mita server.
        '''
        # Refresh the latest information and check pre-condition.
        sys_info.is_mita_installed = sys_info.detect_mita_installed()
        if not sys_info.is_mita_installed:
            if _lang == ZH:
                print_exit('mita 代理服务器软件尚未安装。')
            else:
                print_exit('mita proxy server is not installed.')
        sys_info.is_mita_systemd_active = sys_info.detect_mita_systemd_active()
        if not sys_info.is_mita_systemd_active:
            if _lang == ZH:
                print_exit('mita systemd 服务尚未运行。')
            else:
                print_exit('mita systemd service is not active.')

        while True:
            # Let user to set server configuration.
            if len(self._server_config.users()) == 0:
                if not self.configure_users():
                    if _lang == ZH:
                        print('配置用户失败。')
                    else:
                        print('Configure user is not successful.')
                    continue
            if len(self._server_config.port_bindings()) == 0:
                if not self.configure_port_bindings():
                    if _lang == ZH:
                        print('配置协议和端口失败。')
                    else:
                        print('Configure protocol and ports is not successful.')
                    continue

            # Let user to confirm the server configuration.
            if _lang == ZH:
                print('即将应用下面的代理服务器配置：')
            else:
                print('The following server configuration will be applied:')
            print('')
            self.describe_server_config()
            print('')
            confirm_prompt = '''Type "y" or "n" to apply or discard the server configuration.
(default is "y")
>>> '''
            if _lang == ZH:
                confirm_prompt = '''输入 "y" 确定，输入 "n" 取消。
(默认值是 "y")
>>> '''
            confirm, valid = check_input(prompt=confirm_prompt, validator=match_preset_validator(['y', 'n']), default='y')
            if not valid:
                self._server_config = ServerConfig()
                if _lang == ZH:
                    print(f'输入 {confirm} 是非法选项。已丢弃代理服务器配置。')
                else:
                    print(f'Invalid input: {confirm} is an invalid option. Discarded the server configuration.')
                continue
            if confirm == 'y':
                config_path = self.apply_server_config()
                if config_path != '':
                    if _lang == ZH:
                        print(f'代理服务器配置文件存储在 {config_path}')
                    else:
                        print(f'Server configuration file is stored at {config_path}')
                    return
                if _lang == ZH:
                    print_exit('应用代理服务器配置失败。')
                else:
                    print_exit('Apply server configuration is not successful.')
            else:
                self._server_config = ServerConfig()
                if _lang == ZH:
                    print('已丢弃代理服务器配置。')
                else:
                    print('Discarded the server configuration.')
                continue


    def build_client_configuration(self) -> None:
        '''
        Assume server configuration is available,
        Interactively build mieru client configuration.
        '''
        while True:
            try:
                external_ip = urllib.request.urlopen('https://checkip.amazonaws.com').read().decode('utf8').strip()
                if _lang == ZH:
                    print(f'服务器的公网 IP 地址是 {external_ip}')
                else:
                    print(f'Server\'s public IP address is: {external_ip}')
            except Exception as e:
                if _lang == ZH:
                    print(f'获取服务器的公网 IP 地址失败：{e}')
                else:
                    print(f'Failed to retrieve server\'s public IP address: {e}')
                time.sleep(1)
                continue
            socks5_port_prompt = '''
[configure mieru client][configure socks5 listening port]
Type a single port number to listen to socks5 requests.
(default is "1080")
>>> '''
            if _lang == ZH:
                socks5_port_prompt = '''
[配置 mieru 客户端][配置 socks5 监听端口]
输入一个端口号用于监听 socks5 请求。
(默认值是 "1080")
>>> '''
            socks5_port, valid = check_input(prompt=socks5_port_prompt, validator=port_validator(), default='1080')
            if not valid:
                if _lang == ZH:
                    print(f'输入 {socks5_port} 是非法的端口号。')
                else:
                    print(f'Invalid input: {socks5_port} is an invalid port number.')
                continue
            http_port_prompt = '''
[configure mieru client][configure HTTP proxy listening port]
Type a single port number to listen to HTTP and HTTPS proxy requests.
(default is "8080")
>>> '''
            if _lang == ZH:
                http_port_prompt = '''
[配置 mieru 客户端][配置 HTTP 代理监听端口]
输入一个端口号用于监听 HTTP 和 HTTPS 代理请求。
(默认值是 "8080")
>>> '''
            http_port, valid = check_input(prompt=http_port_prompt, validator=port_validator(), default='8080')
            if not valid:
                if _lang == ZH:
                    print(f'输入 {http_port} 是非法的端口号。')
                else:
                    print(f'Invalid input: {http_port} is an invalid port number.')
                continue
            rpc_port_prompt = '''
[configure mieru client][configure management listening port]
Type a single port number to listen to management RPC requests.
(default is randonly select a number from 2000 to 8000)
>>> '''
            if _lang == ZH:
                rpc_port_prompt = '''
[配置 mieru 客户端][配置管理监听端口]
输入一个端口号用于监听管理 RPC 请求。
(默认值是从 2000 到 8000 随机选取一个数字)
>>> '''
            rpc_port_default = str(random.randint(2000, 8000))
            rpc_port, valid = check_input(prompt=rpc_port_prompt, validator=port_validator(), default=rpc_port_default)
            if not valid:
                if _lang == ZH:
                    print(f'输入 {rpc_port} 是非法的端口号。')
                else:
                    print(f'Invalid input: {rpc_port} is an invalid port number.')
                continue
            self._client_config.set_user(self._server_config.users()[0]['name'], self._server_config.users()[0]['password'])
            if 'port' in self._server_config.port_bindings()[0]:
                self._client_config.set_ip_port(external_ip,
                                                int(self._server_config.port_bindings()[0]['port']),
                                                self._server_config.port_bindings()[0]['protocol'])
            elif 'portRange' in self._server_config.port_bindings()[0]:
                self._client_config.set_ip_port_range(external_ip,
                                                      self._server_config.port_bindings()[0]['portRange'],
                                                      self._server_config.port_bindings()[0]['protocol'])
            else:
                if _lang == ZH:
                    print_exit('代理服务器的端口绑定设置是非法的。')
                else:
                    print_exit('Found invalid server configuration port bindings.')
            self._client_config.set_rpc_port(int(rpc_port))
            self._client_config.set_socks5_port(int(socks5_port))
            self._client_config.set_http_proxy_port(int(http_port))
            if _lang == ZH:
                print('生成了下面的客户端配置：')
            else:
                print('The following client configuration is generated:')
            print('')
            print(self._client_config.to_json())
            print('')
            ntf = tempfile.NamedTemporaryFile(mode='w+', delete=False, prefix='mieru_', suffix='.json')
            try:
                ntf.write(self._client_config.to_json())
                ntf.flush()
            except Exception as e:
                if _lang == ZH:
                    print(f'存储客户端配置至 {ntf.name} 失败：{e}')
                else:
                    print(f'Failed to save client configuration to {ntf.name}: {e}')
                return ''
            finally:
                ntf.close()
            if _lang == ZH:
                print(f'客户端配置文件存储在 {ntf.name}')
            else:
                print(f'Client configuration file is stored at {ntf.name}')
            return


    def restart_mita(self) -> bool:
        '''
        Restart mita proxy. The process is not restarted.
        '''
        run_command(['mita', 'stop'], print_args=True, print_stdout=True)
        time.sleep(1)
        result = run_command(['mita', 'start'], print_args=True, print_stdout=True)
        return result.returncode == 0


    def configure_users(self) -> bool:
        op_prompt = '''
[configure mita server][configure proxy user]
Type number "1" or "2" to select from the options below.
(1): automatically generate user name and password (default)
(2): manually type user name and password
>>> '''
        if _lang == ZH:
            op_prompt = '''
[配置 mita 代理服务器][配置代理用户]
输入数字 "1" 或 "2" 选择下面的选项。
(1): 自动生成用户名和密码 (默认值)
(2): 手动输入用户名和密码
>>> '''
        op, valid = check_input(prompt=op_prompt, validator=match_preset_validator(['1', '2']), default='1')
        if not valid:
            if _lang == ZH:
                print(f'输入 {op} 是非法的选项。')
            else:
                print(f'Invalid input: {op} is an invalid option.')
            return False
        if op == '1':
            self._server_config.set_user(self.generate_random_str(), self.generate_random_str())
            return True
        elif op == '2':
            user_prompt = '''Type a user name
>>> '''
            if _lang == ZH:
                user_prompt = '''输入用户名
>>> '''
            u, valid = check_input(prompt=user_prompt, validator=not_empty_validator())
            if not valid:
                if _lang == ZH:
                    print('输入的用户名为空值。')
                else:
                    print('Invalid input: user name is empty.')
                return False
            pass_prompt = '''Type a password
>>> '''
            if _lang == ZH:
                pass_prompt = '''输入密码
>>> '''
            p, valid = check_input(prompt=pass_prompt, validator=not_empty_validator())
            if not valid:
                if _lang == ZH:
                    print('输入的密码为空值。')
                else:
                    print('Invalid input: password is empty.')
                return False
            self._server_config.set_user(u, p)
            return True
        else:
            if _lang == ZH:
                print(f'{op} 是非法的选项。')
            else:
                print(f'{op} is an invalid option.')
            return False


    def configure_port_bindings(self) -> bool:
        protocol_prompt = '''
[configure mita server][configure protocol and ports]
Type the proxy protocol to use. Support "TCP" and "UDP".
(default is "TCP")
>>> '''
        if _lang == ZH:
            protocol_prompt = '''
[配置 mita 代理服务器][配置协议和端口]
输入代理协议。支持 "TCP" 和 "UDP"。
(默认值是 "TCP")
>>> '''
        protocol, valid = check_input(prompt=protocol_prompt, validator=match_preset_validator(['TCP', 'UDP']), default='TCP')
        if not valid:
            if _lang == ZH:
                print(f'输入 {protocol} 是非法的协议。')
            else:
                print(f'Invalid input: {protocol} is an invalid protocol.')
            return False
        op_prompt = '''Type number "1" or "2" to select from the options below.
(1): add a single listening port like "9000" (default)
(2): add a listening port range like "9000-9010"
>>> '''
        if _lang == ZH:
            op_prompt = '''输入数字 "1" 或 "2" 选择下面的选项。
(1): 添加一个端口号，例如 "9000" (默认值)
(2): 添加一个端口段，例如 "9000-9010"
>>> '''
        op, valid = check_input(prompt=op_prompt, validator=match_preset_validator(['1', '2']), default='1')
        if not valid:
            if _lang == ZH:
                print(f'输入 {op} 是非法的选项。')
            else:
                print(f'Invalid input: {op} is an invalid option.')
            return False
        if op == '1':
            port_prompt = '''Type a single port number.
Minimum value is 1. Maximum value is 65535.
>>> '''
            if _lang == ZH:
                port_prompt = '''输入一个端口号。
最小值为 1。最大值为 65535。
>>> '''
            port, valid = check_input(prompt=port_prompt, validator=port_validator())
            if not valid:
                if _lang == ZH:
                    print(f'输入 {port} 是非法的端口号。')
                else:
                    print(f'Invalid input: {port} is an invalid port number.')
                return False
            self._server_config.add_port(int(port), protocol)
            return True
        elif op == '2':
            port_range_prompt = '''Type a port range like "9000-9010". No space character.
>>> '''
            if _lang == ZH:
                port_range_prompt = '''输入一个端口段，例如 "9000-9010"。请勿使用空格分隔。
>>> '''
            port_range, valid = check_input(prompt=port_range_prompt, validator=port_range_validator())
            if not valid:
                if _lang == ZH:
                    print(f'输入 {port_range} 是非法的端口段。')
                else:
                    print(f'Invalid input: {port_range} is an invalid port range.')
                return False
            self._server_config.add_port_range(port_range, protocol)
            return True
        else:
            if _lang == ZH:
                print(f'{op} 是非法的选项。')
            else:
                print(f'{op} is an invalid option.')
            return False


    def describe_server_config(self) -> None:
        '''
        Print the server configuration.
        '''
        print(self._server_config.to_json())


    def apply_server_config(self) -> str:
        '''
        Apply the server configuration.
        If successful, return the JSON file path that stores the server configuration.
        '''
        ntf = tempfile.NamedTemporaryFile(mode='w+', delete=False, prefix='mita_', suffix='.json')
        try:
            ntf.write(self._server_config.to_json())
            ntf.flush()
        except Exception as e:
            if _lang == ZH:
                print(f'存储代理服务器配置至 {ntf.name} 失败：{e}')
            else:
                print(f'Failed to save server configuration to {ntf.name}: {e}')
            return ''
        finally:
            ntf.close()

        result = run_command(['mita', 'apply', 'config', ntf.name], print_args=True, print_stdout=True)
        if result.returncode == 0:
            return ntf.name
        return ''


class Uninstaller:

    def uninstall_mita(self, sys_info: SysInfo) -> None:
        if sys_info.package_manager == 'deb':
            run_command(['systemctl', 'stop', 'mita'], print_args=True)
            run_command(['dpkg', '-P', 'mita'], timeout=30, print_args=True, print_stdout=True)
            run_command(['rm', '-rf', '/etc/mita'], print_args=True)
            run_command(['rm', '-rf', '/var/lib/mita'], print_args=True)
            run_command(['rm', '-rf', '/var/run/mita.sock'], print_args=True)
            run_command(['rm', '-rf', '/var/run/mita'], print_args=True)
            run_command(['rm', '-f', '/lib/systemd/system/mita.service'], print_args=True)
            run_command(['rm', '-f', '/etc/sysctl.d/mieru_tcp_bbr.conf'], print_args=True)
            run_command(['systemctl', 'daemon-reload'], timeout=30, print_args=True)
            run_command(['userdel', 'mita'], print_args=True, print_stdout=True)
            run_command(['groupdel', 'mita'], print_args=True, print_stdout=True)
            if _lang == ZH:
                print('成功卸载了 mita 代理服务器软件。')
            else:
                print('mita proxy server is uninstalled.')
        elif sys_info.package_manager == 'rpm':
            run_command(['systemctl', 'stop', 'mita'], print_args=True)
            run_command(['rpm', '-e', 'mita'], timeout=30, print_args=True, print_stdout=True)
            run_command(['rm', '-rf', '/etc/mita'], print_args=True)
            run_command(['rm', '-rf', '/var/spool/mail/mita'], print_args=True)
            run_command(['rm', '-rf', '/var/lib/mita'], print_args=True)
            run_command(['rm', '-rf', '/var/run/mita.sock'], print_args=True)
            run_command(['rm', '-rf', '/var/run/mita'], print_args=True)
            run_command(['rm', '-f', '/lib/systemd/system/mita.service'], print_args=True)
            run_command(['rm', '-f', '/etc/sysctl.d/mieru_tcp_bbr.conf'], print_args=True)
            run_command(['systemctl', 'daemon-reload'], timeout=30, print_args=True)
            run_command(['userdel', 'mita'], print_args=True, print_stdout=True)
            run_command(['groupdel', 'mita'], print_args=True, print_stdout=True)
            if _lang == ZH:
                print('成功卸载了 mita 代理服务器软件。')
            else:
                print('mita proxy server is uninstalled.')
        else:
            if _lang == ZH:
                print_exit('卸载 mita 代理服务器软件失败：未能成功检测系统包管理器。')
            else:
                print_exit('Failed to uninstall mita: failed to detect system package manager.')


def run_command(args: List[str], input=None, timeout=10, check=False, print_args=False, print_stdout=False):
    '''
    Run the command and return a subprocess.CompletedProcess instance.
    '''
    try:
        if print_args:
            if _lang == ZH:
                print(f'运行指令 {args}')
            else:
                print(f'Running command {args}')
        result = subprocess.run(args,
                                input=input,
                                stdout=subprocess.PIPE,
                                stderr=subprocess.STDOUT,
                                timeout=timeout,
                                check=check,
                                text=True)
    except subprocess.TimeoutExpired as te:
        if _lang == ZH:
            print_exit(f'指令 {te.cmd} 运行 {te.timeout} 秒后超时。输出：{te.output}')
        else:
            print_exit(f'Command {te.cmd} timed out after {te.timeout} seconds. Output: {te.output}')
    except subprocess.CalledProcessError as cpe:
        if _lang == ZH:
            print_exit(f'指令 {cpe.cmd} 的返回值为 {cpe.returncode}。输出：{cpe.output}')
        else:
            print_exit(f'Command {cpe.cmd} returned code {cpe.returncode}. Output: {cpe.output}')
    finally:
        if print_stdout and result.stdout:
            print(result.stdout)
    return result


def check_input(prompt: str, validator: Callable[[str], bool], default='') -> Tuple[str, bool]:
    '''
    Collect user input from prompt, then check the input with validator.
    '''
    s = input(prompt)
    if s == '' and default != '':
        s = default
    return (s, validator(s))


def any_validator() -> Callable[[str], bool]:
    '''
    Return a high order function that return true for any input.
    '''
    return lambda _: True


def not_empty_validator() -> Callable[[str], bool]:
    '''
    Return a high order function that return true if the input is not empty.
    '''
    return lambda s: s != ''


def match_preset_validator(preset: List) -> Callable[[str], bool]:
    '''
    Return a high order function that return true if the input parameter is inside the preset list.
    '''
    def validator(s: str) -> bool:
        return s in preset
    return validator


def port_validator() -> Callable[[str], bool]:
    def validator(s: str) -> bool:
        if not s.isdigit():
            return False
        port = int(s)
        return 1 <= port <= 65535
    return validator


def port_range_validator() -> Callable[[str], bool]:
    def validator(s: str) -> bool:
        if not re.match(r'^\d+-\d+$', s):
            return False
        try:
            start, end = map(int, s.split('-'))
            if not (1 <= start <= 65535 and 1 <= end <= 65535):
                return False
            return start <= end
        except ValueError:
            return False
    return validator


def print_exit(*values: Any) -> None:
    '''
    Print and exit with a non-zero value.
    '''
    if _lang == ZH:
        print('[错误]', *values)
    else:
        print('[ERROR]', *values)
    sys.exit(1)


if __name__ == '__main__':
    main()
