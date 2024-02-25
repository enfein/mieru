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
This program generate secure strings that can be used as username or password.
'''


import argparse
import secrets


def gen() -> None:
    parser = argparse.ArgumentParser(description='Parse string length.')
    parser.add_argument('--length', type=int, default=12, required=False, help='Length of string to generate.')
    args = parser.parse_args()
    s = secrets.token_urlsafe(args.length)
    print(s[:args.length])


if __name__ == '__main__':
    gen()
