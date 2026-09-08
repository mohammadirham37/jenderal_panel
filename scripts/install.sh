#!/usr/bin/env bash
#----------------------------------------------------------#
#                   Jenderal Panel Installer                #
#                                                          #
#  Supports: Ubuntu 22.04 LTS, Ubuntu 24.04 LTS           #
#                                                          #
#  Usage:                                                  #
#    wget https://raw.githubusercontent.com/               #
#      mohammadirham37/jenderal_panel/main/                #
#      scripts/install.sh                                  #
#    sudo bash install.sh                                  #
#                                                          #
#  Options:                                                #
#    --hostname <fqdn>    Set server hostname              #
#    --email <email>      Admin email (for SSL)            #
#    --password <pass>    Admin password (auto-gen if not)  #
#    --port <port>        Panel port (default: 8443)       #
#    --force              Skip confirmation prompts        #
#    --help               Show this help                   #
#----------------------------------------------------------#

# Don't use set -e — we handle errors manually for better UX
set -uo pipefail

JENDERAL_REPO="mohammadirham37/jenderal_panel"
JENDERAL_VERSION="0.1.0"
JENDERAL_USER="jenderal"
JENDERAL_BIN="/opt/jenderal/jenderal"
JENDERAL_CONFIG="/etc/jenderal/jenderal.yaml"
PANEL_PORT=8443
ADMIN_EMAIL=""
ADMIN_PASSWORD=""
ADMIN_HOSTNAME=""
FORCE=false
ARCH=""

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
BOLD='\033[1m'
NC='\033[0m'

log()   { echo -e "  ${GREEN}*${NC} $1"; }
warn()  { echo -e "  ${YELLOW}!${NC} $1"; }
fail()  { echo -e "  ${RED}x${NC} $1"; exit 1; }

parse_args() {
    while [[ $# -gt 0 ]]; do
        case $1 in
            --hostname)  ADMIN_HOSTNAME="$2"; shift 2 ;;
            --email)     ADMIN_EMAIL="$2"; shift 2 ;;
            --password)  ADMIN_PASSWORD="$2"; shift 2 ;;
            --port)      PANEL_PORT="$2"; shift 2 ;;
            --force)     FORCE=true; shift ;;
            --help)
                echo "Usage: sudo bash install.sh [--hostname fqdn] [--email email] [--password pass] [--port 8443] [--force]"
                exit 0 ;;
            *)  echo "Unknown: $1"; exit 1 ;;
        esac
    done
}

#----------------------------------------------------------#
#                      Pre-checks                           #
#----------------------------------------------------------#
preflight() {
    # Root check
    [[ $EUID -eq 0 ]] || fail "Run as root: sudo bash install.sh"

    # OS check
    [[ -f /etc/os-release ]] || fail "/etc/os-release not found"
    . /etc/os-release
    [[ "$ID" == "ubuntu" ]] || fail "Only Ubuntu supported (detected: $ID)"
    case "$VERSION_ID" in
        22.04|24.04) log "OS: Ubuntu $VERSION_ID" ;;
        *) fail "Ubuntu $VERSION_ID not supported (need 22.04 or 24.04)" ;;
    esac

    # Arch
    case "$(uname -m)" in
        x86_64)  ARCH="amd64" ;;
        aarch64) ARCH="arm64" ;;
        *) fail "Unsupported arch: $(uname -m)" ;;
    esac
    log "Arch: $ARCH"

    # Resources
    local ram_mb=$(( $(grep MemTotal /proc/meminfo | awk '{print $2}') / 1024 ))
    [[ $ram_mb -ge 512 ]] || fail "Need 512MB+ RAM (have ${ram_mb}MB)"
    log "RAM: ${ram_mb}MB"

    local disk_gb=$(df -BG / | tail -1 | awk '{print $4}' | tr -d 'G')
    [[ $disk_gb -ge 5 ]] || fail "Need 5GB+ disk (have ${disk_gb}GB)"
    log "Disk: ${disk_gb}GB free"

    # Internet
    curl -fsS --max-time 10 https://github.com > /dev/null 2>&1 || fail "No internet"
    log "Internet: OK"
}

#----------------------------------------------------------#
#                    Interactive Setup                       #
#----------------------------------------------------------#
setup_interactive() {
    if [[ "$FORCE" == true ]]; then
        [[ -z "$ADMIN_EMAIL" ]] && ADMIN_EMAIL="admin@localhost"
        [[ -z "$ADMIN_PASSWORD" ]] && ADMIN_PASSWORD=$(openssl rand -base64 16 | tr -d '=/+' | head -c 16)
        [[ -z "$ADMIN_HOSTNAME" ]] && ADMIN_HOSTNAME=$(hostname -f 2>/dev/null || hostname)
        return
    fi

    echo ""
    echo -e "  ${CYAN}${BOLD}Setup${NC}"
    echo ""

    if [[ -z "$ADMIN_HOSTNAME" ]]; then
        local dh=$(hostname -f 2>/dev/null || hostname)
        read -rp "  Hostname [$dh]: " ADMIN_HOSTNAME
        ADMIN_HOSTNAME="${ADMIN_HOSTNAME:-$dh}"
    fi

    if [[ -z "$ADMIN_EMAIL" ]]; then
        read -rp "  Admin email [admin@${ADMIN_HOSTNAME}]: " ADMIN_EMAIL
        ADMIN_EMAIL="${ADMIN_EMAIL:-admin@${ADMIN_HOSTNAME}}"
    fi

    if [[ -z "$ADMIN_PASSWORD" ]]; then
        ADMIN_PASSWORD=$(openssl rand -base64 16 | tr -d '=/+' | head -c 16)
    fi

    read -rp "  Panel port [$PANEL_PORT]: " p
    PANEL_PORT="${p:-$PANEL_PORT}"

    echo ""
    echo "  Hostname: $ADMIN_HOSTNAME | Email: $ADMIN_EMAIL | Port: $PANEL_PORT"
    echo ""
    read -rp "  Install? [Y/n]: " c
    [[ "$c" == "n" || "$c" == "N" ]] && { echo "  Aborted."; exit 0; }
    echo ""
}

#----------------------------------------------------------#
#                    Install Functions                       #
#----------------------------------------------------------#
step_fix_dpkg() {
    # Fix any broken dpkg/apt state from prior failed installs
    dpkg --configure -a > /dev/null 2>&1 || true
    apt-get install -f -y > /dev/null 2>&1 || true
}

step_install_deps() {
    log "Installing system packages..."
    apt-get update -qq > /dev/null 2>&1

    # Fix nginx IPv6 issue BEFORE installing nginx
    # Systems without IPv6 fail on [::]:80 listen directive
    for f in /etc/nginx/sites-available/default /etc/nginx/sites-enabled/default; do
        [[ -f "$f" ]] && sed -i 's/listen \[::\]/#listen [::]/' "$f" 2>/dev/null || true
    done

    DEBIAN_FRONTEND=noninteractive apt-get install -y \
        curl wget git gcc g++ make sqlite3 openssl ufw \
        software-properties-common 2>&1 | grep -E "^E:|upgraded|newly" || true

    # Install nginx — handle IPv6 failure on systems without IPv6
    log "Installing nginx..."
    DEBIAN_FRONTEND=noninteractive apt-get install -y nginx 2>&1 || true

    # Fix IPv6 listen directive if nginx failed to start
    if ! systemctl is-active --quiet nginx 2>/dev/null; then
        log "Fixing nginx IPv6 configuration..."
        for f in /etc/nginx/sites-available/default /etc/nginx/sites-enabled/default; do
            if [[ -f "$f" ]]; then
                sed -i 's/listen \[::\]:80 default_server;/#listen [::]:80 default_server;/' "$f"
                sed -i 's/listen \[::\]:443 ssl default_server;/#listen [::]:443 ssl default_server;/' "$f"
                sed -i 's/listen \[::\]:80;/#listen [::]:80;/' "$f"
                sed -i 's/listen \[::\]:443;/#listen [::]:443;/' "$f"
            fi
        done
        dpkg --configure -a 2>&1 || true
        apt-get install -f -y 2>&1 || true
        systemctl start nginx 2>/dev/null || true
    fi

    if systemctl is-active --quiet nginx 2>/dev/null; then
        log "Nginx: running"
    else
        warn "Nginx not running, but continuing..."
    fi

    log "System packages: OK"
}

step_install_go() {
    export PATH="/usr/local/go/bin:$PATH"
    if command -v go &>/dev/null; then
        log "Go: $(go version | awk '{print $3}') (exists)"
        return
    fi

    log "Installing Go 1.23.4..."
    curl -fsSL "https://go.dev/dl/go1.23.4.linux-${ARCH}.tar.gz" -o /tmp/go.tar.gz \
        || fail "Failed to download Go"
    rm -rf /usr/local/go
    tar -C /usr/local -xzf /tmp/go.tar.gz
    rm -f /tmp/go.tar.gz
    /usr/local/go/bin/go version > /dev/null 2>&1 || fail "Go install failed"
    log "Go: $(/usr/local/go/bin/go version | awk '{print $3}')"
}

step_install_node() {
    if command -v node &>/dev/null; then
        log "Node.js: $(node --version) (exists)"
        return
    fi

    log "Installing Node.js 20..."
    # Try NodeSource
    if curl -fsSL https://deb.nodesource.com/setup_20.x 2>/dev/null | bash - > /dev/null 2>&1; then
        apt-get install -y nodejs > /dev/null 2>&1
    fi

    # Fallback to apt
    if ! command -v node &>/dev/null; then
        apt-get install -y nodejs npm > /dev/null 2>&1
    fi

    command -v node &>/dev/null || fail "Node.js install failed"
    log "Node.js: $(node --version)"
}

step_create_user() {
    if id "$JENDERAL_USER" &>/dev/null; then
        log "User '$JENDERAL_USER': exists"
    else
        useradd --system --no-create-home --shell /usr/sbin/nologin "$JENDERAL_USER"
        log "User '$JENDERAL_USER': created"
    fi
}

step_create_dirs() {
    mkdir -p /etc/jenderal/tls /var/lib/jenderal/backups /var/lib/jenderal/acme /var/lib/jenderal/acme-challenges /var/log/jenderal /opt/jenderal
    chown -R "$JENDERAL_USER":"$JENDERAL_USER" /var/lib/jenderal /var/log/jenderal /opt/jenderal
    log "Directories: OK"
}

step_build() {
    log "Building Jenderal Panel from source..."
    local bd="/tmp/jenderal-build-$$"

    git clone --depth 1 "https://github.com/${JENDERAL_REPO}.git" "$bd" 2>&1 | tail -1

    # Frontend
    log "  Building frontend..."
    cd "$bd/web"
    npm install --loglevel=error 2>&1 | tail -3
    npm run build 2>&1 | tail -1
    [[ -d build ]] || fail "Frontend build failed — web/build not found"

    # Prepare embed
    cd "$bd"
    rm -rf cmd/jenderal/web_build
    cp -r web/build cmd/jenderal/web_build

    # Go build
    log "  Compiling binary..."
    export PATH="/usr/local/go/bin:$PATH"
    CGO_ENABLED=1 go build -ldflags "-X main.version=${JENDERAL_VERSION}" \
        -o "$JENDERAL_BIN" ./cmd/jenderal 2>&1
    [[ -f "$JENDERAL_BIN" ]] || fail "Go build failed"

    chmod +x "$JENDERAL_BIN"
    chown "$JENDERAL_USER":"$JENDERAL_USER" "$JENDERAL_BIN"

    cd /
    rm -rf "$bd"

    # Verify
    "$JENDERAL_BIN" version 2>/dev/null || fail "Binary verification failed"
    log "Binary: $($JENDERAL_BIN version)"
}

step_tls() {
    if [[ -f /etc/jenderal/tls/cert.pem ]]; then
        log "TLS cert: exists"
        return
    fi

    openssl req -x509 -newkey rsa:2048 \
        -keyout /etc/jenderal/tls/key.pem \
        -out /etc/jenderal/tls/cert.pem \
        -days 365 -nodes \
        -subj "/CN=${ADMIN_HOSTNAME}" 2>/dev/null
    chmod 600 /etc/jenderal/tls/key.pem
    chown "$JENDERAL_USER":"$JENDERAL_USER" /etc/jenderal/tls/*.pem
    log "TLS cert: generated (self-signed)"
}

step_config() {
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
    log "Config: written"
}

step_sudoers() {
    cat > /etc/sudoers.d/jenderal << 'EOF'
jenderal ALL=(ALL) NOPASSWD: ALL
EOF
    chmod 440 /etc/sudoers.d/jenderal
    log "Sudoers: configured"
}

step_migrate() {
    log "Running migrations..."
    sudo -u "$JENDERAL_USER" "$JENDERAL_BIN" migrate --config "$JENDERAL_CONFIG" 2>&1
    log "Database: migrated"
}

step_admin() {
    log "Creating admin user..."
    echo -e "admin\n${ADMIN_EMAIL}\n${ADMIN_PASSWORD}" | \
        sudo -u "$JENDERAL_USER" "$JENDERAL_BIN" admin create --config "$JENDERAL_CONFIG" \
        > /dev/null 2>&1 || warn "Admin may already exist"
    log "Admin: created"
}

step_systemd() {
    cat > /etc/systemd/system/jenderal.service << 'EOF'
[Unit]
Description=Jenderal Panel
After=network.target

[Service]
Type=simple
User=jenderal
Group=jenderal
ExecStart=/opt/jenderal/jenderal serve --config /etc/jenderal/jenderal.yaml
WorkingDirectory=/opt/jenderal
Restart=on-failure
RestartSec=5
LimitNOFILE=65535

[Install]
WantedBy=multi-user.target
EOF
    systemctl daemon-reload
    systemctl enable jenderal > /dev/null 2>&1
    systemctl restart jenderal
    log "Service: installed"

    # Wait and verify
    sleep 3
    if ss -tlnp | grep -q ":${PANEL_PORT}"; then
        log "Service: listening on port ${PANEL_PORT}"
    else
        warn "Service started but port ${PANEL_PORT} not listening yet"
        warn "Check: sudo journalctl -u jenderal -n 20"
    fi
}

step_firewall() {
    ufw allow 22/tcp > /dev/null 2>&1 || true
    ufw allow 80/tcp > /dev/null 2>&1 || true
    ufw allow 443/tcp > /dev/null 2>&1 || true
    ufw allow "${PANEL_PORT}/tcp" > /dev/null 2>&1 || true
    echo "y" | ufw enable > /dev/null 2>&1 || true
    log "Firewall: OK"
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
    preflight
    setup_interactive

    echo -e "  ${BOLD}Installing...${NC}"
    echo "  ─────────────────────────────────────────"

    step_fix_dpkg
    step_install_deps
    step_install_go
    step_install_node
    step_create_user
    step_create_dirs
    step_build
    step_tls
    step_config
    step_sudoers
    step_migrate
    step_admin
    step_systemd
    step_firewall

    # Summary
    local ip=$(curl -fsSL https://api.ipify.org 2>/dev/null || hostname -I | awk '{print $1}')
    echo ""
    echo -e "${GREEN}${BOLD}"
    echo "  ╔═══════════════════════════════════════════════╗"
    echo "  ║       Jenderal Panel v${JENDERAL_VERSION} Installed!       ║"
    echo "  ╚═══════════════════════════════════════════════╝"
    echo -e "${NC}"
    echo -e "  URL:       ${BOLD}https://${ip}:${PANEL_PORT}${NC}"
    echo -e "  Username:  ${BOLD}admin${NC}"
    echo -e "  Password:  ${BOLD}${ADMIN_PASSWORD}${NC}"
    echo ""
    echo -e "  ${YELLOW}Browser will show certificate warning (self-signed).${NC}"
    echo -e "  ${YELLOW}Use Let's Encrypt via panel for production SSL.${NC}"
    echo ""
}

main "$@"
