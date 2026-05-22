#!/bin/bash

# Copyright (C) 2026  mieru authors
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

# Make sure this script has executable permission:
# git update-index --chmod=+x <file>

set -e

TCP_STANDARD_ACCEPTED_COUNT_FILE=/test/socks5_tcp_standard_accepted_count.txt
UDP_STANDARD_ACCEPTED_COUNT_FILE=/test/socks5_udp_standard_accepted_count.txt
TCP_NO_WAIT_ACCEPTED_COUNT_FILE=/test/socks5_tcp_no_wait_accepted_count.txt
UDP_NO_WAIT_ACCEPTED_COUNT_FILE=/test/socks5_udp_no_wait_accepted_count.txt

function print_mieru_client_log() {
    echo "========== BEGIN OF MIERU CLIENT LOG =========="
    cat $HOME/.cache/mieru/*.log 2>/dev/null || true
    echo "==========  END OF MIERU CLIENT LOG  =========="
}

function delete_mieru_client_log() {
    rm -f $HOME/.cache/mieru/*.log
}

function print_mieru_client_thread_dump() {
    echo "========== BEGIN OF MIERU CLIENT THREAD DUMP =========="
    ./mieru get thread-dump || true
    echo "==========  END OF MIERU CLIENT THREAD DUMP  =========="
}

function print_mieru_server_thread_dump() {
    echo "========== BEGIN OF MIERU SERVER THREAD DUMP =========="
    ./mita get thread-dump || true
    echo "==========  END OF MIERU SERVER THREAD DUMP  =========="
}

function fail_test() {
    print_mieru_client_log
    print_mieru_client_thread_dump
    print_mieru_server_thread_dump
    echo "$1"
    exit 1
}

function wait_for_accepted_count_file() {
    local file="$1"

    for _ in {1..50}; do
        if [[ -s "${file}" ]]; then
            return
        fi
        sleep 0.1
    done
    fail_test "socks5 accepted count file ${file} was not created."
}

function read_accepted_count() {
    local file="$1"

    cat "${file}"
}

function assert_accepted_count_increased() {
    local file="$1"
    local before="$2"
    local phase="$3"
    local after
    after=$(read_accepted_count "${file}")
    echo "socks5 accepted count before ${phase}: ${before}, after: ${after}"
    if (( after <= before )); then
        fail_test "socks5 accepted count did not increase during ${phase}."
    fi
}

function apply_server_config() {
    if ! ./mita apply config server_mix.json; then
        fail_test "command 'mita apply config server_mix.json' failed"
    fi
    echo "mieru server config:"
    ./mita describe config
    echo "mieru server effective traffic pattern:"
    ./mita describe effective-traffic-pattern
}

function start_mieru_client() {
    local config="$1"

    if ! ./mieru apply config "${config}"; then
        fail_test "command 'mieru apply config ${config}' failed"
    fi
    echo "mieru client config:"
    ./mieru describe config

    if ! ./mieru start; then
        fail_test "command 'mieru start' failed"
    fi
}

function stop_mieru_client() {
    if ! ./mieru stop; then
        fail_test "command 'mieru stop' failed"
    fi
    sleep 1
    print_mieru_client_log
    delete_mieru_client_log
}

function start_socks5server() {
    local port="$1"
    local count_file="$2"

    ./socks5server -host=127.0.0.1 -port="${port}" -allow_loopback=true -accepted_count_file="${count_file}" &
}

function run_tcp_dialer_test() {
    local config="$1"
    local count_file="$2"
    local phase="$3"

    echo "========== BEGIN OF ${phase} =========="
    local count_before
    count_before=$(read_accepted_count "${count_file}")
    start_mieru_client "${config}"
    sleep 2
    echo ">>> socks5 - new connections - ${phase} <<<"
    if ! ./sockshttpclient -dst_host=127.0.0.1 -dst_port=8080 \
        -local_proxy_host=127.0.0.1 -local_proxy_port=1080 \
        -test_case=new_conn -num_request=3000; then
        fail_test "${phase} failed."
    fi
    assert_accepted_count_increased "${count_file}" "${count_before}" "${phase}"
    stop_mieru_client
    echo "==========  END OF ${phase}  =========="
}

function run_udp_dialer_test() {
    local config="$1"
    local count_file="$2"
    local phase="$3"

    echo "========== BEGIN OF ${phase} =========="
    local count_before
    count_before=$(read_accepted_count "${count_file}")
    start_mieru_client "${config}"
    sleep 2
    echo ">>> socks5 UDP associate - ${phase} <<<"
    if ! ./socksudpclient -dst_host=127.0.0.1 -dst_port=9090 \
        -local_proxy_host=127.0.0.1 -local_proxy_port=1080 \
        -interval_ms=10 -num_request=100 -num_conn=30; then
        fail_test "${phase} failed."
    fi
    assert_accepted_count_increased "${count_file}" "${count_before}" "${phase}"
    stop_mieru_client
    echo "==========  END OF ${phase}  =========="
}

# Start destination servers.
./httpserver -port=8080 &
sleep 2
./udpserver -port=9090 &
sleep 1

# Start standalone socks5 servers used by the client profile dialer.
start_socks5server 2081 "${TCP_STANDARD_ACCEPTED_COUNT_FILE}"
start_socks5server 2082 "${UDP_STANDARD_ACCEPTED_COUNT_FILE}"
start_socks5server 2083 "${TCP_NO_WAIT_ACCEPTED_COUNT_FILE}"
start_socks5server 2084 "${UDP_NO_WAIT_ACCEPTED_COUNT_FILE}"
wait_for_accepted_count_file "${TCP_STANDARD_ACCEPTED_COUNT_FILE}"
wait_for_accepted_count_file "${UDP_STANDARD_ACCEPTED_COUNT_FILE}"
wait_for_accepted_count_file "${TCP_NO_WAIT_ACCEPTED_COUNT_FILE}"
wait_for_accepted_count_file "${UDP_NO_WAIT_ACCEPTED_COUNT_FILE}"

# Start and configure mieru server daemon.
./mita run &
sleep 1
apply_server_config

if ! ./mita start; then
    fail_test "command 'mita start' failed"
fi

run_tcp_dialer_test client_tcp.json "${TCP_STANDARD_ACCEPTED_COUNT_FILE}" "TCP dialer test - HANDSHAKE STANDARD"
run_udp_dialer_test client_udp.json "${UDP_STANDARD_ACCEPTED_COUNT_FILE}" "UDP dialer test - HANDSHAKE STANDARD"
run_tcp_dialer_test client_tcp_no_wait.json "${TCP_NO_WAIT_ACCEPTED_COUNT_FILE}" "TCP dialer test - HANDSHAKE NO WAIT"
run_udp_dialer_test client_udp_no_wait.json "${UDP_NO_WAIT_ACCEPTED_COUNT_FILE}" "UDP dialer test - HANDSHAKE NO WAIT"

if ! ./mita stop; then
    fail_test "command 'mita stop' failed"
fi

echo "Test is successful."
sleep 1
exit 0
