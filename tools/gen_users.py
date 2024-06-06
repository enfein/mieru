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
This program generates users that can be used in mita server configurations.
'''


import sys
sys.dont_write_bytecode = True


from gen_username_passwd import gen_token
import argparse
import json


def gen_users() -> None:
    parser = argparse.ArgumentParser(description='Parse number of users.')
    parser.add_argument('-n', type=int, default=1, required=False, help='Number of users.')
    args = parser.parse_args()
    user_list = []
    for i in range(args.n):
        user_list.append({
            "name": gen_token(8),
            "password": gen_token(8),
        })
    users = {
        "users": user_list
    }
    print(json.dumps(users, indent=4))


if __name__ == '__main__':
    gen_users()
