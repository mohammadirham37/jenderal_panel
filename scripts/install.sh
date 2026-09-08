#!/usr/bin/env bash
set -euo pipefail

#----------------------------------------------------------#
#                   Jenderal Panel Installer                #
#                                                          #
#  Supports: Ubuntu 22.04 LTS, Ubuntu 24.04 LTS           #
#                                                          #
#  Usage:                                                  #
#    wget https://raw.githubusercontent.com/               #
#      mohammadirham37/jenderal_panel/main/                #
#      scripts/install.sh                                  #
#    bash install.sh                                       #
#                                                          #
#  Options:                                                #
#    --hostname <fqdn>    Set server hostname              #
#    --email <email>      Admin email (for SSL)            #
#    --password <pass>    Admin password (auto-gen if not)  #
#    --port <port>        Panel port (default: 8443)       #
#    --version <ver>      Version to install (latest)      #
#    --force              Skip confirmation prompts        #
#    --help               Show this help                   #
#----------------------------------------------------------#

JENDERAL_REPO="mohammadirham37/jenderal_panel"
JENDERAL_VERSION="${JENDERAL_VERSION:-latest}"
JENDERAL_USER="jenderal"
JENDERAL_BIN="/opt/jenderal/jenderal"
JENDERAL_CONFIG="/etc/jenderal/jenderal.yaml"
JENDERAL_SERVICE="/etc/systemd/system/jenderal.service"
PANEL_PORT=8443
ADMIN_EMAIL=""
ADMIN_PASSWORD=""
ADMIN_HOSTNAME=""
FORCE=false

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
BOLD='\033[1m'
NC='\033[0m'

log()   { echo -e "  ${GREEN}*${NC} $1"; }
warn()  { echo -e "  ${YELLOW}!${NC} $1"; }
error() { echo -e "  ${RED}x${NC} $1"; exit 1; }

#----------------------------------------------------------#
#                    Parse Arguments                        #
#----------------------------------------------------------#
parse_args() {
    while [[ $# -gt 0 ]]; do
        case $1 in
            --hostname)  ADMIN_HOSTNAME="$2"; shift 2 ;;
            --email)     ADMIN_EMAIL="$2"; shift 2 ;;
            --password)  ADMIN_PASSWORD="$2"; shift 2 ;;
            --port)      PANEL_PORT="$2"; shift 2 ;;
            --version)   JENDERAL_VERSION="$2"; shift 2 ;;
            --force)     FORCE=true; shift ;;
            --help)      show_help; exit 0 ;;
            *)           echo "Unknown option: $1"; show_help; exit 1 ;;
        esac
    done
}

show_help() {
    echo ""
    echo "Jenderal Panel Installer"
    echo ""
    echo "Usage: bash install.sh [OPTIONS]"
    echo ""
    echo "Options:"
    echo "  --hostname <fqdn>     Server hostname"
    echo "  --email <email>       Admin email address"
    echo "  --password <pass>     Admin password (auto-generated if omitted)"
    echo "  --port <port>         Panel port (default: 8443)"
    echo "  --version <ver>       Version to install (default: latest)"
    echo "  --force               Skip confirmation prompts"
    echo "  --help                Show this help message"
    echo ""
    echo "Example:"
    echo "  bash install.sh --hostname panel.example.com --email admin@example.com"
    echo ""
}

#----------------------------------------------------------#
#                    Pre-flight Checks                      #
#----------------------------------------------------------#
check_root() {
    if [[ $EUID -ne 0 ]]; then
        error "This installer must be run as root. Use: sudo bash install.sh"
    fi
}

detect_os() {
    if [[ ! -f /etc/os-release ]]; then
        error "Cannot detect OS: /etc/os-release not found"
    fi

    . /etc/os-release

    if [[ "$ID" != "ubuntu" ]]; then
        error "Unsupported OS: $ID. Only Ubuntu is supported."
    fi

    case "$VERSION_ID" in
        "22.04"|"24.04")
            log "OS: Ubuntu $VERSION_ID ($PRETTY_NAME)"
            ;;
        *)
            error "Unsupported Ubuntu version: $VERSION_ID. Supported: 22.04, 24.04"
            ;;
    esac
}

detect_arch() {
    ARCH=$(uname -m)
    case "$ARCH" in
        x86_64)  ARCH="amd64" ;;
        aarch64) ARCH="arm64" ;;
        *)       error "Unsupported architecture: $ARCH" ;;
    esac
    log "Architecture: $ARCH"
}

check_resources() {
    local ram_kb
    ram_kb=$(grep MemTotal /proc/meminfo | awk '{print $2}')
    local ram_mb=$((ram_kb / 1024))

    if [[ $ram_mb -lt 512 ]]; then
        error "Minimum 512MB RAM required. Detected: ${ram_mb}MB"
    fi
    log "RAM: ${ram_mb}MB"

    local disk_avail
    disk_avail=$(df -BG / | tail -1 | awk '{print $4}' | tr -d 'G')
    if [[ $disk_avail -lt 5 ]]; then
        error "Minimum 5GB free disk space required. Available: ${disk_avail}GB"
    fi
    log "Disk: ${disk_avail}GB available"
}

check_internet() {
    if ! curl -fsS --max-time 10 https://github.com > /dev/null 2>&1; then
        error "No internet connectivity. Cannot reach github.com"
    fi
    log "Internet: OK"
}

check_existing() {
    if [[ -f "$JENDERAL_BIN" ]]; then
        local current_ver
        current_ver=$("$JENDERAL_BIN" version 2>/dev/null | awk '{print $NF}' || echo "unknown")
        warn "Jenderal Panel already installed (version: $current_ver)"
        if [[ "$FORCE" != true ]]; then
            echo ""
            read -rp "  Reinstall/upgrade? [y/N]: " confirm
            if [[ "$confirm" != "y" && "$confirm" != "Y" ]]; then
                echo "  Aborted."
                exit 0
            fi
        fi
    fi
}

#----------------------------------------------------------#
#                    Interactive Setup                      #
#----------------------------------------------------------#
interactive_setup() {
    if [[ "$FORCE" == true ]]; then
        # Generate defaults for non-interactive mode
        if [[ -z "$ADMIN_EMAIL" ]]; then
            ADMIN_EMAIL="admin@localhost"
        fi
        if [[ -z "$ADMIN_PASSWORD" ]]; then
            ADMIN_PASSWORD=$(openssl rand -base64 16 | tr -d '=/+' | head -c 16)
        fi
        return
    fi

    echo ""
    echo -e "${CYAN}${BOLD}  Jenderal Panel Installation${NC}"
    echo -e "  ─────────────────────────────"
    echo ""

    # Hostname
    if [[ -z "$ADMIN_HOSTNAME" ]]; then
        local default_hostname
        default_hostname=$(hostname -f 2>/dev/null || hostname)
        read -rp "  Hostname [$default_hostname]: " ADMIN_HOSTNAME
        ADMIN_HOSTNAME="${ADMIN_HOSTNAME:-$default_hostname}"
    fi

    # Email
    if [[ -z "$ADMIN_EMAIL" ]]; then
        read -rp "  Admin email [admin@$ADMIN_HOSTNAME]: " ADMIN_EMAIL
        ADMIN_EMAIL="${ADMIN_EMAIL:-admin@$ADMIN_HOSTNAME}"
    fi

    # Password
    if [[ -z "$ADMIN_PASSWORD" ]]; then
        ADMIN_PASSWORD=$(openssl rand -base64 16 | tr -d '=/+' | head -c 16)
        log "Generated admin password"
    fi

    # Port
    read -rp "  Panel port [$PANEL_PORT]: " input_port
    PANEL_PORT="${input_port:-$PANEL_PORT}"

    echo ""
    echo -e "  ${BOLD}Installation Summary:${NC}"
    echo "  ─────────────────────────────"
    echo "  Hostname:  $ADMIN_HOSTNAME"
    echo "  Email:     $ADMIN_EMAIL"
    echo "  Port:      $PANEL_PORT"
    echo "  Version:   $JENDERAL_VERSION"
    echo ""

    read -rp "  Continue with installation? [Y/n]: " confirm
    if [[ "$confirm" == "n" || "$confirm" == "N" ]]; then
        echo "  Aborted."
        exit 0
    fi
    echo ""
}

#----------------------------------------------------------#
#                    Installation Steps                     #
#----------------------------------------------------------#
install_dependencies() {
    log "Updating package lists..."
    apt-get update -qq > /dev/null 2>&1

    # Fix any broken dpkg state first
    dpkg --configure -a > /dev/null 2>&1 || true
    apt-get install -f -y > /dev/null 2>&1 || true

    # Pre-fix nginx IPv6 issue before install
    # On systems without IPv6, nginx default config fails on [::]:80
    if [[ ! -f /proc/net/if_inet6 ]]; then
        log "IPv6 not available, pre-configuring nginx..."
        mkdir -p /etc/nginx/sites-available /etc/nginx/sites-enabled
        # If default config exists with IPv6, fix it
        for f in /etc/nginx/sites-available/default /etc/nginx/sites-enabled/default; do
            if [[ -f "$f" ]]; then
                sed -i 's/listen \[::\]:80/#listen [::]:80/g' "$f"
                sed -i 's/listen \[::\]:443/#listen [::]:443/g' "$f"
            fi
        done
    fi

    log "Installing dependencies..."
    DEBIAN_FRONTEND=noninteractive apt-get install -y \
        curl wget sqlite3 ufw openssl gcc make git \
        software-properties-common apt-transport-https \
        2>&1 | tail -5 || {
        warn "Some base packages may have failed, continuing..."
    }

    # Install nginx
    log "Installing nginx..."
    DEBIAN_FRONTEND=noninteractive apt-get install -y nginx 2>&1 | tail -5 || {
        warn "Nginx install had issues, attempting fix..."
        # Fix IPv6 after nginx installs its config
        for f in /etc/nginx/sites-available/default /etc/nginx/sites-enabled/default; do
            if [[ -f "$f" ]]; then
                sed -i 's/listen \[::\]:80/#listen [::]:80/g' "$f"
                sed -i 's/listen \[::\]:443/#listen [::]:443/g' "$f"
            fi
        done
        dpkg --configure -a 2>&1 | tail -3 || true
        systemctl start nginx 2>/dev/null || true
    }

    # Verify nginx is working
    if nginx -t > /dev/null 2>&1; then
        systemctl start nginx 2>/dev/null || true
        log "Nginx: OK"
    else
        warn "Nginx config test failed, but continuing installation"
    fi

    log "Dependencies installed"
}

create_user() {
    if id "$JENDERAL_USER" &>/dev/null; then
        log "System user '$JENDERAL_USER' already exists"
    else
        useradd --system --no-create-home --shell /usr/sbin/nologin "$JENDERAL_USER"
        log "Created system user: $JENDERAL_USER"
    fi
}

create_directories() {
    mkdir -p /etc/jenderal/tls
    mkdir -p /var/lib/jenderal/backups
    mkdir -p /var/log/jenderal
    mkdir -p /opt/jenderal

    chown -R "$JENDERAL_USER":"$JENDERAL_USER" \
        /var/lib/jenderal \
        /var/log/jenderal \
        /opt/jenderal

    log "Directories created"
}

resolve_version() {
    if [[ "$JENDERAL_VERSION" == "latest" ]]; then
        log "Resolving latest version..."
        JENDERAL_VERSION=$(curl -fsSL \
            "https://api.github.com/repos/${JENDERAL_REPO}/releases/latest" \
            2>/dev/null | grep '"tag_name"' | sed -E 's/.*"v?([^"]+)".*/\1/' || echo "0.1.0")

        if [[ -z "$JENDERAL_VERSION" || "$JENDERAL_VERSION" == "null" ]]; then
            JENDERAL_VERSION="0.1.0"
            warn "Could not resolve latest version, using $JENDERAL_VERSION"
        fi
    fi
    log "Version: $JENDERAL_VERSION"
}

download_binary() {
    local download_url="https://github.com/${JENDERAL_REPO}/releases/download/v${JENDERAL_VERSION}/jenderal-linux-${ARCH}"
    local tmp_bin="/tmp/jenderal-$$"

    if [[ -f "$JENDERAL_BIN" ]]; then
        cp "$JENDERAL_BIN" "${JENDERAL_BIN}.bak"
        log "Backed up existing binary"
    fi

    log "Downloading Jenderal Panel v${JENDERAL_VERSION}..."
    if curl -fsSL --progress-bar "$download_url" -o "$tmp_bin" 2>/dev/null; then
        mv "$tmp_bin" "$JENDERAL_BIN"
        chmod +x "$JENDERAL_BIN"
        chown "$JENDERAL_USER":"$JENDERAL_USER" "$JENDERAL_BIN"
        log "Binary downloaded successfully"
    else
        warn "Download failed from releases. Trying to build from source..."
        build_from_source
    fi
}

build_from_source() {
    # Install Go if not available
    export PATH=$PATH:/usr/local/go/bin
    if ! command -v go &>/dev/null; then
        log "Installing Go..."
        local go_ver="1.23.4"
        if ! curl -fsSL "https://go.dev/dl/go${go_ver}.linux-${ARCH}.tar.gz" -o /tmp/go.tar.gz; then
            error "Failed to download Go ${go_ver}"
        fi
        rm -rf /usr/local/go
        tar -C /usr/local -xzf /tmp/go.tar.gz
        rm -f /tmp/go.tar.gz

        if ! /usr/local/go/bin/go version > /dev/null 2>&1; then
            error "Go installation failed"
        fi
        log "Go $(/usr/local/go/bin/go version | awk '{print $3}') installed"
    else
        log "Go $(go version | awk '{print $3}') already installed"
    fi

    # Install Node.js if not available
    if ! command -v node &>/dev/null; then
        log "Installing Node.js..."
        # Try NodeSource first, fallback to apt
        if curl -fsSL https://deb.nodesource.com/setup_20.x 2>/dev/null | bash - > /dev/null 2>&1; then
            apt-get install -y nodejs > /dev/null 2>&1
        else
            warn "NodeSource failed, trying apt..."
            apt-get install -y nodejs npm > /dev/null 2>&1
        fi

        if ! command -v node &>/dev/null; then
            error "Node.js installation failed"
        fi
        log "Node.js $(node --version) installed"
    else
        log "Node.js $(node --version) already installed"
    fi

    # Install build dependencies
    apt-get install -y -qq gcc g++ make git > /dev/null 2>&1 || true

    log "Building from source (this may take a few minutes)..."
    local build_dir="/tmp/jenderal-build-$$"

    if ! git clone --depth 1 "https://github.com/${JENDERAL_REPO}.git" "$build_dir" 2>&1; then
        error "Failed to clone repository"
    fi

    # Build frontend
    log "Building frontend..."
    cd "$build_dir/web"
    if ! npm install 2>&1 | tail -3; then
        error "npm install failed"
    fi
    if ! npm run build 2>&1 | tail -3; then
        error "Frontend build failed"
    fi

    # Copy frontend build for embedding
    cd "$build_dir"
    rm -rf cmd/jenderal/web_build
    cp -r web/build cmd/jenderal/web_build

    # Build Go binary
    log "Compiling Go binary..."
    if ! CGO_ENABLED=1 /usr/local/go/bin/go build \
        -ldflags "-X main.version=${JENDERAL_VERSION}" \
        -o "$JENDERAL_BIN" \
        ./cmd/jenderal 2>&1 | tail -5; then
        error "Go build failed"
    fi

    chmod +x "$JENDERAL_BIN"
    chown "$JENDERAL_USER":"$JENDERAL_USER" "$JENDERAL_BIN"

    cd /
    rm -rf "$build_dir"
    log "Built from source successfully"
}

generate_tls() {
    if [[ -f /etc/jenderal/tls/cert.pem ]]; then
        log "TLS certificate already exists"
        return
    fi

    openssl req -x509 -newkey rsa:4096 \
        -keyout /etc/jenderal/tls/key.pem \
        -out /etc/jenderal/tls/cert.pem \
        -days 365 -nodes \
        -subj "/CN=${ADMIN_HOSTNAME:-jenderal-panel}" 2>/dev/null

    chmod 600 /etc/jenderal/tls/key.pem
    chown "$JENDERAL_USER":"$JENDERAL_USER" /etc/jenderal/tls/*.pem
    log "Self-signed TLS certificate generated"
}

write_config() {
    if [[ -f "$JENDERAL_CONFIG" && "$FORCE" != true ]]; then
        log "Config already exists, keeping existing"
        return
    fi

    cat > "$JENDERAL_CONFIG" << CONFIGEOF
server:
  host: "0.0.0.0"
  port: ${PANEL_PORT}
  tls:
    enabled: true
    cert: "/etc/jenderal/tls/cert.pem"
    key: "/etc/jenderal/tls/key.pem"

database:
  path: "/var/lib/jenderal/jenderal.db"

auth:
  session_ttl: "24h"
  rate_limit_login: 5
  rate_limit_api: 100
  argon2:
    memory: 65536
    iterations: 3
    parallelism: 2

metrics:
  collect_interval: "5s"
  store_interval: "60s"
  retention_days: 7

logging:
  level: "info"
  file: "/var/log/jenderal/jenderal.log"
  max_size_mb: 100
  max_backups: 3

services:
  allowed:
    - nginx
    - "php*-fpm"
    - mysql
    - postgresql
    - redis-server
CONFIGEOF

    chown "$JENDERAL_USER":"$JENDERAL_USER" "$JENDERAL_CONFIG"
    log "Configuration written"
}

write_sudoers() {
    cat > /etc/sudoers.d/jenderal << 'SUDOEOF'
# Jenderal Panel - Privileged Operations Whitelist
# DO NOT EDIT - managed by Jenderal Panel installer

jenderal ALL=(ALL) NOPASSWD: /usr/bin/systemctl start *
jenderal ALL=(ALL) NOPASSWD: /usr/bin/systemctl stop *
jenderal ALL=(ALL) NOPASSWD: /usr/bin/systemctl restart *
jenderal ALL=(ALL) NOPASSWD: /usr/bin/systemctl reload *
jenderal ALL=(ALL) NOPASSWD: /usr/bin/systemctl status *
jenderal ALL=(ALL) NOPASSWD: /usr/bin/systemctl show *
jenderal ALL=(ALL) NOPASSWD: /usr/bin/systemctl daemon-reload
jenderal ALL=(ALL) NOPASSWD: /usr/bin/systemctl enable *
jenderal ALL=(ALL) NOPASSWD: /usr/bin/systemctl disable *
jenderal ALL=(ALL) NOPASSWD: /usr/bin/systemctl is-active *
jenderal ALL=(ALL) NOPASSWD: /usr/sbin/ufw *
jenderal ALL=(ALL) NOPASSWD: /usr/sbin/reboot
jenderal ALL=(ALL) NOPASSWD: /usr/sbin/shutdown *
jenderal ALL=(ALL) NOPASSWD: /usr/bin/hostnamectl set-hostname *
jenderal ALL=(ALL) NOPASSWD: /usr/bin/timedatectl set-timezone *
jenderal ALL=(ALL) NOPASSWD: /usr/bin/apt-get update
jenderal ALL=(ALL) NOPASSWD: /usr/bin/apt-get install -y *
jenderal ALL=(ALL) NOPASSWD: /usr/bin/apt-get remove -y *
jenderal ALL=(ALL) NOPASSWD: /usr/bin/nginx -t
jenderal ALL=(ALL) NOPASSWD: /usr/bin/nginx -v
jenderal ALL=(ALL) NOPASSWD: /usr/bin/tee *
jenderal ALL=(ALL) NOPASSWD: /usr/bin/cp *
jenderal ALL=(ALL) NOPASSWD: /usr/bin/mv *
jenderal ALL=(ALL) NOPASSWD: /usr/bin/rm *
jenderal ALL=(ALL) NOPASSWD: /usr/bin/mkdir *
jenderal ALL=(ALL) NOPASSWD: /usr/bin/chown *
jenderal ALL=(ALL) NOPASSWD: /usr/bin/chmod *
jenderal ALL=(ALL) NOPASSWD: /usr/bin/ln *
jenderal ALL=(ALL) NOPASSWD: /usr/bin/ls *
jenderal ALL=(ALL) NOPASSWD: /usr/bin/cat *
jenderal ALL=(ALL) NOPASSWD: /usr/bin/tail *
jenderal ALL=(ALL) NOPASSWD: /usr/bin/stat *
jenderal ALL=(ALL) NOPASSWD: /usr/bin/test *
jenderal ALL=(ALL) NOPASSWD: /usr/bin/kill *
jenderal ALL=(ALL) NOPASSWD: /usr/bin/tar *
jenderal ALL=(ALL) NOPASSWD: /usr/bin/git *
jenderal ALL=(ALL) NOPASSWD: /usr/bin/docker *
jenderal ALL=(ALL) NOPASSWD: /usr/bin/docker-compose *
jenderal ALL=(ALL) NOPASSWD: /usr/bin/crontab *
jenderal ALL=(ALL) NOPASSWD: /usr/bin/mysql *
jenderal ALL=(ALL) NOPASSWD: /usr/bin/mysqldump *
jenderal ALL=(ALL) NOPASSWD: /usr/bin/psql *
jenderal ALL=(ALL) NOPASSWD: /usr/bin/createdb *
jenderal ALL=(ALL) NOPASSWD: /usr/bin/dropdb *
jenderal ALL=(ALL) NOPASSWD: /usr/bin/createuser *
jenderal ALL=(ALL) NOPASSWD: /usr/bin/dropuser *
jenderal ALL=(ALL) NOPASSWD: /usr/bin/pg_dump *
jenderal ALL=(ALL) NOPASSWD: /usr/bin/redis-cli *
jenderal ALL=(ALL) NOPASSWD: /usr/sbin/useradd *
jenderal ALL=(ALL) NOPASSWD: /usr/sbin/userdel *
jenderal ALL=(ALL) NOPASSWD: /usr/bin/certbot *
jenderal ALL=(ALL) NOPASSWD: /usr/bin/node *
jenderal ALL=(ALL) NOPASSWD: /usr/bin/npm *
jenderal ALL=(ALL) NOPASSWD: /usr/bin/composer *
jenderal ALL=(ALL) NOPASSWD: /usr/bin/php *
jenderal ALL=(ALL) NOPASSWD: /usr/bin/sh -c *
SUDOEOF

    chmod 440 /etc/sudoers.d/jenderal
    log "Sudoers whitelist configured"
}

run_migrations() {
    log "Running database migrations..."
    sudo -u "$JENDERAL_USER" "$JENDERAL_BIN" migrate --config "$JENDERAL_CONFIG"
    log "Migrations completed"
}

create_admin() {
    log "Creating admin user..."
    echo -e "admin\n${ADMIN_EMAIL}\n${ADMIN_PASSWORD}" | \
        sudo -u "$JENDERAL_USER" "$JENDERAL_BIN" admin create --config "$JENDERAL_CONFIG" \
        > /dev/null 2>&1
    log "Admin user created"
}

install_systemd() {
    cat > "$JENDERAL_SERVICE" << 'SERVICEEOF'
[Unit]
Description=Jenderal Panel - VPS Control Panel
Documentation=https://github.com/mohammadirham37/jenderal_panel
After=network.target
Wants=network-online.target

[Service]
Type=simple
User=jenderal
Group=jenderal
ExecStart=/opt/jenderal/jenderal serve --config /etc/jenderal/jenderal.yaml
WorkingDirectory=/opt/jenderal
Restart=on-failure
RestartSec=5
LimitNOFILE=65535

# Security hardening
NoNewPrivileges=false
ProtectSystem=full
ProtectHome=false
ReadWritePaths=/var/lib/jenderal /var/log/jenderal /etc/jenderal

# Environment
Environment=JENDERAL_LOGGING_LEVEL=info

[Install]
WantedBy=multi-user.target
SERVICEEOF

    systemctl daemon-reload
    systemctl enable jenderal > /dev/null 2>&1
    systemctl start jenderal
    log "Systemd service installed and started"
}

configure_firewall() {
    ufw allow 22/tcp comment "SSH" > /dev/null 2>&1 || true
    ufw allow 80/tcp comment "HTTP" > /dev/null 2>&1 || true
    ufw allow 443/tcp comment "HTTPS" > /dev/null 2>&1 || true
    ufw allow "${PANEL_PORT}/tcp" comment "Jenderal Panel" > /dev/null 2>&1 || true

    if ! ufw status | grep -q "Status: active"; then
        echo "y" | ufw enable > /dev/null 2>&1
    fi
    log "Firewall configured (ports: 22, 80, 443, ${PANEL_PORT})"
}

set_hostname() {
    if [[ -n "$ADMIN_HOSTNAME" ]]; then
        hostnamectl set-hostname "$ADMIN_HOSTNAME" 2>/dev/null || true
        log "Hostname set to: $ADMIN_HOSTNAME"
    fi
}

#----------------------------------------------------------#
#                    Post-Installation                      #
#----------------------------------------------------------#
print_summary() {
    local server_ip
    server_ip=$(curl -fsSL https://api.ipify.org 2>/dev/null || hostname -I | awk '{print $1}')

    # Wait a moment for service to start
    sleep 2

    local status
    if systemctl is-active --quiet jenderal; then
        status="${GREEN}Running${NC}"
    else
        status="${RED}Not running${NC}"
        warn "Service failed to start. Check: journalctl -u jenderal"
    fi

    echo ""
    echo -e "${GREEN}${BOLD}"
    echo "  ╔═══════════════════════════════════════════════════╗"
    echo "  ║                                                   ║"
    echo "  ║        Jenderal Panel v${JENDERAL_VERSION} Installed!        ║"
    echo "  ║                                                   ║"
    echo "  ╚═══════════════════════════════════════════════════╝"
    echo -e "${NC}"
    echo -e "  ${BOLD}Panel URL:${NC}    https://${server_ip}:${PANEL_PORT}"
    if [[ -n "$ADMIN_HOSTNAME" ]]; then
    echo -e "  ${BOLD}Hostname:${NC}     https://${ADMIN_HOSTNAME}:${PANEL_PORT}"
    fi
    echo -e "  ${BOLD}Username:${NC}     admin"
    echo -e "  ${BOLD}Password:${NC}     ${ADMIN_PASSWORD}"
    echo -e "  ${BOLD}Status:${NC}       ${status}"
    echo ""
    echo -e "  ${BOLD}Useful Commands:${NC}"
    echo "  ─────────────────────────────────────────────"
    echo "  systemctl status jenderal     Check status"
    echo "  systemctl restart jenderal    Restart panel"
    echo "  journalctl -u jenderal -f     View logs"
    echo "  cat /var/log/jenderal/jenderal.log"
    echo ""
    echo -e "  ${BOLD}Config:${NC}  ${JENDERAL_CONFIG}"
    echo -e "  ${BOLD}Binary:${NC}  ${JENDERAL_BIN}"
    echo -e "  ${BOLD}Data:${NC}    /var/lib/jenderal/"
    echo -e "  ${BOLD}Logs:${NC}    /var/log/jenderal/"
    echo ""
    echo -e "  ${YELLOW}Note: Using self-signed certificate.${NC}"
    echo -e "  ${YELLOW}Your browser will show a security warning.${NC}"
    echo -e "  ${YELLOW}Use Let's Encrypt via the panel for production.${NC}"
    echo ""
}

save_install_log() {
    local log_file="/var/log/jenderal/install.log"
    cat > "$log_file" << LOGEOF
Jenderal Panel Installation Log
================================
Date:       $(date -u +"%Y-%m-%d %H:%M:%S UTC")
Version:    ${JENDERAL_VERSION}
OS:         $(. /etc/os-release && echo "$PRETTY_NAME")
Arch:       ${ARCH}
Hostname:   ${ADMIN_HOSTNAME}
Email:      ${ADMIN_EMAIL}
Port:       ${PANEL_PORT}
Admin User: admin
LOGEOF
    chmod 600 "$log_file"
    chown "$JENDERAL_USER":"$JENDERAL_USER" "$log_file"
}

#----------------------------------------------------------#
#                         Main                              #
#----------------------------------------------------------#
main() {
    echo ""
    echo -e "${CYAN}${BOLD}"
    echo "       _ _____ _   _ ____  _____ ____      _    _     "
    echo "      | | ____| \ | |  _ \| ____|  _ \    / \  | |    "
    echo "   _  | |  _| |  \| | | | |  _| | |_) |  / _ \ | |    "
    echo "  | |_| | |___| |\  | |_| | |___|  _ <  / ___ \| |___ "
    echo "   \___/|_____|_| \_|____/|_____|_| \_\/_/   \_\_____|"
    echo ""
    echo "              VPS Control Panel Installer"
    echo -e "${NC}"

    parse_args "$@"

    # Pre-flight checks
    check_root
    detect_os
    detect_arch
    check_resources
    check_internet
    check_existing

    # Interactive setup
    interactive_setup

    # Installation
    echo -e "  ${BOLD}Installing Jenderal Panel...${NC}"
    echo "  ─────────────────────────────────────────────"

    install_dependencies
    create_user
    create_directories
    resolve_version
    download_binary
    generate_tls
    write_config
    write_sudoers
    run_migrations
    create_admin
    install_systemd
    configure_firewall
    set_hostname
    save_install_log

    # Done
    print_summary
}

main "$@"
