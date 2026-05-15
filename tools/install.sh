#!/bin/bash
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

# One-click installation script for mita proxy server.
# Supports: install, update, uninstall, reconfigure, status, start, stop, restart
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/enfein/mieru/main/tools/install.sh | sudo bash -s -- install
#   sudo bash install.sh install

set -euo pipefail

# ---------------------------------------------------------------------------
# Colors & helpers
# ---------------------------------------------------------------------------
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
BOLD='\033[1m'
NC='\033[0m'

info()    { echo -e "${BLUE}[INFO]${NC}  $*"; }
ok()      { echo -e "${GREEN}[OK]${NC}   $*"; }
warn()    { echo -e "${YELLOW}[WARN]${NC}  $*"; }
err()     { echo -e "${RED}[ERR]${NC}   $*" >&2; }
die()     { err "$@"; exit 1; }

# ---------------------------------------------------------------------------
# Constants
# ---------------------------------------------------------------------------
GITHUB_REPO="enfein/mieru"
GITHUB_API="https://api.github.com/repos/${GITHUB_REPO}/releases/latest"
CONFIG_DIR="/etc/mita"
CONFIG_FILE="${CONFIG_DIR}/server_config.json"
META_FILE="${CONFIG_DIR}/.install_meta"

# ---------------------------------------------------------------------------
# Detect environment
# ---------------------------------------------------------------------------
detect_pkg_family() {
    if command -v dpkg &>/dev/null && command -v apt-get &>/dev/null; then
        echo "deb"
    elif command -v rpm &>/dev/null && (command -v dnf &>/dev/null || command -v yum &>/dev/null); then
        echo "rpm"
    else
        die "Unsupported OS: requires Debian/Ubuntu (dpkg/apt) or RHEL/CentOS/Rocky/Fedora (rpm/dnf/yum)"
    fi
}

detect_arch() {
    local family="$1"
    local arch
    arch=$(uname -m)
    case "$arch" in
        x86_64)
            if [ "$family" = "deb" ]; then echo "amd64"; else echo "x86_64"; fi
            ;;
        aarch64|arm64)
            if [ "$family" = "deb" ]; then echo "arm64"; else echo "aarch64"; fi
            ;;
        *) die "Unsupported architecture: $arch" ;;
    esac
}

detect_installed_version() {
    if command -v mita &>/dev/null; then
        mita version 2>/dev/null | head -1 | grep -oE '[0-9]+\.[0-9]+\.[0-9]+' | head -1
    fi
}

# ---------------------------------------------------------------------------
# Fetch latest release
# ---------------------------------------------------------------------------
get_latest_version() {
    local tag
    tag=$(curl -fsSL "$GITHUB_API" 2>/dev/null | grep '"tag_name":' | head -1 | sed -E 's/.*"([^"]+)".*/\1/')
    if [ -z "$tag" ]; then
        # Fallback: try wget
        tag=$(wget -qO- "$GITHUB_API" 2>/dev/null | grep '"tag_name":' | head -1 | sed -E 's/.*"([^"]+)".*/\1/')
    fi
    if [ -z "$tag" ]; then
        die "Failed to fetch latest version from GitHub API"
    fi
    echo "$tag"
}

# ---------------------------------------------------------------------------
# Download helpers
# ---------------------------------------------------------------------------
download() {
    local url="$1"
    local out="$2"
    if command -v curl &>/dev/null; then
        curl -fSL --progress-bar "$url" -o "$out"
    elif command -v wget &>/dev/null; then
        wget -q --show-progress "$url" -O "$out"
    else
        die "Neither curl nor wget is installed"
    fi
}

# ---------------------------------------------------------------------------
# Install / Update package
# ---------------------------------------------------------------------------
install_package() {
    local family
    family=$(detect_pkg_family)
    local arch
    arch=$(detect_arch "$family")
    local tag
    tag=$(get_latest_version)
    local version="${tag#v}"

    info "OS family: $family"
    info "Architecture: $arch"
    info "Latest version: $tag"

    local filename
    if [ "$family" = "deb" ]; then
        filename="mita_${version}_${arch}.deb"
    else
        filename="mita-${version}-1.${arch}.rpm"
    fi

    local url="https://github.com/${GITHUB_REPO}/releases/download/${tag}/${filename}"
    local tmpdir
    tmpdir=$(mktemp -d)
    trap 'rm -rf "$tmpdir"' RETURN

    info "Downloading $filename ..."
    download "$url" "${tmpdir}/${filename}"

    info "Installing package ..."
    if [ "$family" = "deb" ]; then
        if ! dpkg -i "${tmpdir}/${filename}" >/dev/null 2>&1; then
            info "Resolving dependencies ..."
            apt-get update >/dev/null 2>&1
            apt-get install -f -y >/dev/null 2>&1
            dpkg -i "${tmpdir}/${filename}" || die "Package install failed"
        fi
    else
        if command -v dnf &>/dev/null; then
            dnf install -y "${tmpdir}/${filename}" >/dev/null 2>&1 || rpm -Uvh --force "${tmpdir}/${filename}"
        else
            yum install -y "${tmpdir}/${filename}" >/dev/null 2>&1 || rpm -Uvh --force "${tmpdir}/${filename}"
        fi
    fi

    mkdir -p "$CONFIG_DIR"
    echo "version=$tag" > "$META_FILE"
    ok "mita $tag installed"
}

update_package() {
    local current latest_tag latest
    current=$(detect_installed_version)
    latest_tag=$(get_latest_version)
    latest="${latest_tag#v}"

    info "Current version: ${current:-not installed}"
    info "Latest version:  $latest_tag"

    if [ -n "$current" ] && [ "$current" = "$latest" ]; then
        ok "Already running the latest version"
        return 0
    fi

    install_package

    # Re-apply config if one exists
    if [ -f "$CONFIG_FILE" ]; then
        info "Re-applying configuration after upgrade ..."
        systemctl restart mita 2>/dev/null || true
        sleep 1
        mita apply config "$CONFIG_FILE" >/dev/null 2>&1 || true
        mita start >/dev/null 2>&1 || true
    fi
    ok "Update completed"
}

# ---------------------------------------------------------------------------
# Interactive configuration
# ---------------------------------------------------------------------------
read_with_default() {
    local prompt="$1"
    local default="$2"
    read -rp "$(echo -e "${CYAN}${prompt} [${default}]: ${NC}")" value
    echo "${value:-$default}"
}

generate_password() {
    openssl rand -base64 18 2>/dev/null | tr -d '/+=' | cut -c1-24
}

generate_random_string() {
    local len="${1:-8}"
    tr -dc 'a-zA-Z0-9' < /dev/urandom | fold -w "$len" | head -n 1
}

build_config() {
    info "Starting interactive configuration ..."

    # Protocol
    echo ""
    echo -e "${BOLD}Transport protocol:${NC}"
    echo "  1) TCP (default)"
    echo "  2) UDP"
    local proto_choice
    proto_choice=$(read_with_default "Select" "1")
    local protocol="TCP"
    [ "$proto_choice" = "2" ] && protocol="UDP"

    # Port
    echo ""
    echo -e "${BOLD}Port mode:${NC}"
    echo "  1) Single port (default)"
    echo "  2) Port range"
    local port_choice
    port_choice=$(read_with_default "Select" "1")

    local port_binding_json
    local display_port
    if [ "$port_choice" = "2" ]; then
        local range
        range=$(read_with_default "Port range" "2012-2022")
        if ! [[ "$range" =~ ^[0-9]+-[0-9]+$ ]]; then
            die "Invalid port range format"
        fi
        port_binding_json="        {\n            \"portRange\": \"$range\",\n            \"protocol\": \"$protocol\"\n        }"
        display_port="$range"
    else
        local default_port=$(( RANDOM % 20000 + 30000 ))
        local port
        port=$(read_with_default "Server port" "$default_port")
        if ! [[ "$port" =~ ^[0-9]+$ ]] || [ "$port" -lt 1025 ] || [ "$port" -gt 65535 ]; then
            die "Port must be between 1025 and 65535"
        fi
        port_binding_json="        {\n            \"port\": $port,\n            \"protocol\": \"$protocol\"\n        }"
        display_port="$port"
    fi

    # Users
    echo ""
    local user_count
    user_count=$(read_with_default "Number of users" "1")
    if ! [[ "$user_count" =~ ^[0-9]+$ ]] || [ "$user_count" -lt 1 ]; then
        user_count=1
    fi

    local users_json=""
    local first_user=""
    local first_pass=""

    for (( i=1; i<=user_count; i++ )); do
        echo ""
        echo -e "${BOLD}User $i:${NC}"
        local default_name="user$(generate_random_string 4)"
        local uname upass
        uname=$(read_with_default "Username" "$default_name")
        upass=$(read_with_default "Password (empty = random)" "")
        [ -z "$upass" ] && upass=$(generate_password)

        [ "$i" -gt 1 ] && users_json="${users_json},\n"
        users_json="${users_json}        {\n            \"name\": \"$uname\",\n            \"password\": \"$upass\"\n        }"

        if [ "$i" -eq 1 ]; then
            first_user="$uname"
            first_pass="$upass"
        fi
    done

    # Assemble JSON
    cat > "$CONFIG_FILE" <<EOF
{
    "portBindings": [
$(printf '%b\n' "$port_binding_json")
    ],
    "users": [
$(printf '%b\n' "$users_json")
    ],
    "loggingLevel": "INFO",
    "mtu": 1400
}
EOF
    chmod 600 "$CONFIG_FILE"

    {
        echo "protocol=$protocol"
        echo "port=$display_port"
        echo "first_user=$first_user"
        echo "first_pass=$first_pass"
        grep "^version=" "$META_FILE" 2>/dev/null || true
    } > "${META_FILE}.tmp" && mv "${META_FILE}.tmp" "$META_FILE"

    ok "Configuration written to $CONFIG_FILE"
}

apply_config_and_start() {
    info "Applying configuration ..."
    mita apply config "$CONFIG_FILE" || die "Failed to apply configuration"
    ok "Configuration applied"

    info "Starting proxy service ..."
    systemctl start mita 2>/dev/null || true
    sleep 1
    mita start || die "Failed to start proxy"
    ok "Proxy service started"
}

configure_firewall() {
    [ -f "$META_FILE" ] || return 0

    local protocol port
    protocol=$(grep '^protocol=' "$META_FILE" | cut -d= -f2 || true)
    port=$(grep '^port=' "$META_FILE" | cut -d= -f2 || true)
    [ -z "$port" ] && return 0

    local proto_lower
    proto_lower=$(echo "$protocol" | tr '[:upper:]' '[:lower:]')

    if command -v ufw &>/dev/null && ufw status 2>/dev/null | grep -q "Status: active"; then
        info "Configuring UFW firewall ..."
        if [[ "$port" == *-* ]]; then
            ufw allow "${port/-/:}/${proto_lower}" >/dev/null 2>&1 || true
        else
            ufw allow "${port}/${proto_lower}" >/dev/null 2>&1 || true
        fi
        ok "UFW: ${port}/${proto_lower} allowed"
    elif command -v firewall-cmd &>/dev/null && firewall-cmd --state &>/dev/null; then
        info "Configuring firewalld ..."
        if [[ "$port" == *-* ]]; then
            firewall-cmd --permanent --add-port="${port}/${proto_lower}" >/dev/null 2>&1 || true
        else
            firewall-cmd --permanent --add-port="${port}/${proto_lower}" >/dev/null 2>&1 || true
        fi
        firewall-cmd --reload >/dev/null 2>&1 || true
        ok "firewalld: ${port}/${proto_lower} allowed"
    else
        warn "No supported firewall (ufw / firewalld) detected; please open port ${port}/${proto_lower} manually"
    fi
}

# ---------------------------------------------------------------------------
# Show connection info
# ---------------------------------------------------------------------------
get_ipv4() {
    curl -s -4 --connect-timeout 5 ifconfig.me 2>/dev/null || \
    curl -s -4 --connect-timeout 5 ip.sb 2>/dev/null || \
    curl -s -4 --connect-timeout 5 ipinfo.io/ip 2>/dev/null || \
    echo "YOUR_SERVER_IP"
}

show_info() {
    if [ ! -f "$CONFIG_FILE" ]; then
        warn "No configuration found at $CONFIG_FILE"
        return 1
    fi

    local server_ip
    server_ip=$(get_ipv4)
    local protocol port first_user first_pass
    if [ -f "$META_FILE" ]; then
        protocol=$(grep '^protocol=' "$META_FILE" | cut -d= -f2 || true)
        port=$(grep '^port=' "$META_FILE" | cut -d= -f2 || true)
        first_user=$(grep '^first_user=' "$META_FILE" | cut -d= -f2 || true)
        first_pass=$(grep '^first_pass=' "$META_FILE" | cut -d= -f2 || true)
    fi

    echo ""
    echo -e "${BOLD}Connection Information${NC}"
    echo -e "  Server:    $server_ip"
    echo -e "  Port:      ${port:-N/A}"
    echo -e "  Protocol:  ${protocol:-N/A}"
    echo ""
    echo -e "${BOLD}Users:${NC}"
    if command -v python3 &>/dev/null; then
        python3 -c "
import json
with open('$CONFIG_FILE') as f:
    cfg = json.load(f)
for u in cfg.get('users', []):
    print('  - %s / %s' % (u.get('name',''), u.get('password','')))
" 2>/dev/null || true
    fi

    if command -v python3 &>/dev/null && [ -n "$first_user" ]; then
        local link
        link=$(python3 -c "
import json, urllib.parse
with open('$CONFIG_FILE') as f:
    cfg = json.load(f)
qs = [('profile','default')]
for b in cfg.get('portBindings',[]):
    if 'portRange' in b:
        qs.append(('port', b['portRange']))
    elif 'port' in b:
        qs.append(('port', str(b['port'])))
    qs.append(('protocol', b.get('protocol','TCP')))
qs.append(('mtu','1400'))
server = '$server_ip'
for u in cfg.get('users', []):
    n = urllib.parse.quote(u.get('name',''), safe='')
    p = urllib.parse.quote(u.get('password',''), safe='')
    print('mierus://%s:%s@%s?%s' % (n, p, server, urllib.parse.urlencode(qs)))
" 2>/dev/null)
        if [ -n "$link" ]; then
            echo ""
            echo -e "${BOLD}Sharing links (mierus://):${NC}"
            echo -e "${GREEN}${link}${NC}"
        fi
    fi
    echo ""
    info "Use 'mieru import config <URL>' on the client side, or configure manually."
}

# ---------------------------------------------------------------------------
# Service control
# ---------------------------------------------------------------------------
start_proxy() {
    info "Starting mita daemon ..."
    systemctl start mita || die "Failed to start mita daemon"
    sleep 1
    info "Starting proxy ..."
    mita start || die "Failed to start proxy"
    ok "Proxy started"
    mita status
}

stop_proxy() {
    info "Stopping proxy ..."
    mita stop 2>/dev/null || true
    ok "Proxy stopped"
}

restart_proxy() {
    info "Restarting service ..."
    systemctl restart mita || die "Failed to restart mita daemon"
    sleep 1
    mita start >/dev/null 2>&1 || true
    if mita status 2>/dev/null | grep -q "RUNNING"; then
        ok "Proxy restarted"
    else
        warn "Proxy is not in RUNNING state"
    fi
    mita status
}

enable_autostart() {
    systemctl enable mita
    ok "mita auto-start enabled"
}

disable_autostart() {
    systemctl disable mita
    ok "mita auto-start disabled"
}

show_status() {
    local ver
    ver=$(detect_installed_version)
    echo -e "Installed version: ${ver:-unknown}"
    echo ""
    systemctl status mita --no-pager 2>/dev/null | head -8 || true
    echo ""
    info "Proxy status:"
    mita status 2>/dev/null || warn "Cannot query mita (daemon may not be running)"
}

# ---------------------------------------------------------------------------
# BBR
# ---------------------------------------------------------------------------
enable_bbr() {
    local current_cc
    current_cc=$(sysctl -n net.ipv4.tcp_congestion_control 2>/dev/null || true)

    if [ "$current_cc" = "bbr" ]; then
        local current_qdisc
        current_qdisc=$(sysctl -n net.core.default_qdisc 2>/dev/null || true)
        ok "BBR is already enabled (qdisc: ${current_qdisc:-unknown})"
        return 0
    fi

    info "Enabling BBR congestion control (current: ${current_cc:-unknown}) ..."
    local SYSCTL_CONF="/etc/sysctl.conf"

    if ! grep -q "^net.core.default_qdisc" "$SYSCTL_CONF" 2>/dev/null; then
        echo "net.core.default_qdisc = fq" >> "$SYSCTL_CONF"
    fi

    if grep -q "^net.ipv4.tcp_congestion_control" "$SYSCTL_CONF" 2>/dev/null; then
        sed -i 's/^net.ipv4.tcp_congestion_control.*/net.ipv4.tcp_congestion_control = bbr/' "$SYSCTL_CONF"
    else
        echo "net.ipv4.tcp_congestion_control = bbr" >> "$SYSCTL_CONF"
    fi

    sysctl -p >/dev/null 2>&1 || true
    ok "BBR enabled"
}

# ---------------------------------------------------------------------------
# Uninstall
# ---------------------------------------------------------------------------
uninstall_mita() {
    read -rp "Are you sure you want to uninstall mita? [y/N]: " confirm
    if [ "$confirm" != "y" ] && [ "$confirm" != "Y" ]; then
        info "Uninstall cancelled"
        return 0
    fi

    mita stop 2>/dev/null || true
    systemctl stop mita 2>/dev/null || true
    systemctl disable mita 2>/dev/null || true

    local family
    family=$(detect_pkg_family)
    if [ "$family" = "deb" ]; then
        apt-get remove -y mita 2>/dev/null || dpkg -r mita 2>/dev/null || true
    else
        if command -v dnf &>/dev/null; then
            dnf remove -y mita 2>/dev/null || rpm -e mita 2>/dev/null || true
        else
            yum remove -y mita 2>/dev/null || rpm -e mita 2>/dev/null || true
        fi
    fi

    read -rp "Remove configuration directory ${CONFIG_DIR}? [y/N]: " rm_cfg
    if [ "$rm_cfg" = "y" ] || [ "$rm_cfg" = "Y" ]; then
        rm -rf "$CONFIG_DIR"
        info "Configuration removed"
    else
        info "Configuration kept at $CONFIG_DIR"
    fi

    ok "mita uninstalled"
}

# ---------------------------------------------------------------------------
# Menu
# ---------------------------------------------------------------------------
show_menu() {
    echo ""
    echo -e "${BOLD}${BLUE}=====================================================${NC}"
    echo -e "${BOLD}${BLUE}     Mieru (mita) Server Installation Script         ${NC}"
    echo -e "${BOLD}${BLUE}=====================================================${NC}"
    echo ""
    echo -e "${BOLD}Please select an option:${NC}"
    echo "  1) Install mita (with interactive config)"
    echo "  2) Update mita"
    echo "  3) Uninstall mita"
    echo "  4) Reconfigure"
    echo "  5) Start proxy"
    echo "  6) Stop proxy"
    echo "  7) Restart proxy"
    echo "  8) Enable auto-start"
    echo "  9) Disable auto-start"
    echo "  10) Show status"
    echo "  11) Show connection info"
    echo "  12) Enable BBR"
    echo "  0) Exit"
    echo ""
    read -rp "Enter your choice [0-12]: " choice

    case "$choice" in
        1)
            install_package
            build_config
            configure_firewall
            enable_autostart
            apply_config_and_start
            show_info
            ;;
        2) update_package ;;
        3) uninstall_mita ;;
        4)
            build_config
            configure_firewall
            systemctl restart mita 2>/dev/null || true
            sleep 1
            mita start >/dev/null 2>&1 || true
            show_info
            ;;
        5) start_proxy ;;
        6) stop_proxy ;;
        7) restart_proxy ;;
        8) enable_autostart ;;
        9) disable_autostart ;;
        10) show_status ;;
        11) show_info ;;
        12) enable_bbr ;;
        0) info "Exiting ..."; exit 0 ;;
        *) die "Invalid option" ;;
    esac
}

show_help() {
    cat <<EOF
Mieru (mita) Server Installation Script

Usage: $0 [command]

Commands:
  install      Install mita and run interactive configuration
  update       Update mita to the latest version
  uninstall    Uninstall mita
  reconfigure  Rewrite configuration interactively and re-apply
  start        Start the proxy service
  stop         Stop the proxy service
  restart      Restart the proxy service
  enable       Enable auto-start on boot
  disable      Disable auto-start on boot
  status       Show service status
  info         Show connection information
  bbr          Enable BBR congestion control (safe to re-run)
  help         Show this help message

Without arguments, an interactive menu will be displayed.

One-liner install:
  curl -fsSL https://raw.githubusercontent.com/enfein/mieru/main/tools/install.sh | sudo bash -s -- install

EOF
}

# ---------------------------------------------------------------------------
# Main
# ---------------------------------------------------------------------------
main() {
    # Must be root for install / uninstall / firewall / systemd
    if [ "$(id -u)" -ne 0 ]; then
        die "This script must be run as root (use sudo)"
    fi

    case "${1:-}" in
        install)
            install_package
            build_config
            configure_firewall
            enable_autostart
            apply_config_and_start
            show_info
            ;;
        update)       update_package ;;
        uninstall)    uninstall_mita ;;
        reconfigure)
            build_config
            configure_firewall
            systemctl restart mita 2>/dev/null || true
            sleep 1
            mita start >/dev/null 2>&1 || true
            show_info
            ;;
        start)        start_proxy ;;
        stop)         stop_proxy ;;
        restart)      restart_proxy ;;
        enable)       enable_autostart ;;
        disable)      disable_autostart ;;
        status)       show_status ;;
        info)         show_info ;;
        bbr)          enable_bbr ;;
        help|--help|-h)
            show_help
            ;;
        "")
            show_menu
            ;;
        *)
            die "Unknown command: $1. Run '$0 help' for usage."
            ;;
    esac
}

main "$@"
