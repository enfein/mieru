#!/bin/sh

set -e

mita run &
sleep 2

mita apply config ./conf/config.json

mita start

mita describe config

wait -n
