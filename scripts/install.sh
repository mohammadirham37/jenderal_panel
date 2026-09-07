#!/usr/bin/env bash
set -euo pipefail

# Jenderal Panel Installer
# Supports: Ubuntu 22.04, Ubuntu 24.04

JENDERAL_VERSION="${JENDERAL_VERSION:-0.1.0}"
JENDERAL_USER="jenderal"
JENDERAL_BIN="/opt/jenderal/jenderal"
JENDERAL_CONFIG="/etc/jenderal/jenderal.yaml"
JENDERAL_SERVICE="/etc/systemd/system/jenderal.service"

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

log()   { echo -e "${GREEN}[+]${NC} $1"; }
warn()  { echo -e "${YELLOW}[!]${NC} $1"; }
error() { echo -e "${RED}[x]${NC} $1"; exit 1; }

# 1. Check root
check_root() {
    if [[ $EUID -ne 0 ]]; then
        error "This script must be run as root"
    fi
    log "Running as root"
}

# 2. Detect OS
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
            log "Detected Ubuntu $VERSION_ID ($PRETTY_NAME)"
            ;;
        *)
            error "Unsupported Ubuntu version: $VERSION_ID. Supported: 22.04, 24.04"
            ;;
    esac
}

# 3. Check architecture
detect_arch() {
    ARCH=$(uname -m)
    case "$ARCH" in
        x86_64)  ARCH="amd64" ;;
        aarch64) ARCH="arm64" ;;
        *)       error "Unsupported architecture: $ARCH" ;;
    esac
    log "Architecture: $ARCH"
}

# 4. Check resources
check_resources() {
    local ram_kb
    ram_kb=$(grep MemTotal /proc/meminfo | awk '{print $2}')
    local ram_mb=$((ram_kb / 1024))

    if [[ $ram_mb -lt 512 ]]; then
        error "Minimum 512MB RAM required. Found: ${ram_mb}MB"
    fi
    log "RAM: ${ram_mb}MB"

    local disk_avail
    disk_avail=$(df -BG / | tail -1 | awk '{print $4}' | tr -d 'G')
    if [[ $disk_avail -lt 5 ]]; then
        error "Minimum 5GB disk space required. Found: ${disk_avail}GB"
    fi
    log "Disk available: ${disk_avail}GB"
}

# 5. Check internet
check_internet() {
    if ! ping -c 1 -W 5 8.8.8.8 &>/dev/null; then
        error "No internet connectivity"
    fi
    log "Internet connectivity: OK"
}

# 6. Install dependencies
install_deps() {
    log "Updating package lists..."
    apt-get update -qq

    log "Installing dependencies..."
    apt-get install -y -qq curl wget sqlite3 nginx ufw openssl > /dev/null

    log "Dependencies installed"
}

# 7. Create system user
create_user() {
    if id "$JENDERAL_USER" &>/dev/null; then
        log "User $JENDERAL_USER already exists"
    else
        useradd --system --no-create-home --shell /usr/sbin/nologin "$JENDERAL_USER"
        log "Created system user: $JENDERAL_USER"
    fi
}

# 8. Create directories
create_dirs() {
    mkdir -p /etc/jenderal/tls
    mkdir -p /var/lib/jenderal
    mkdir -p /var/log/jenderal
    mkdir -p /opt/jenderal

    chown -R "$JENDERAL_USER":"$JENDERAL_USER" /var/lib/jenderal /var/log/jenderal /opt/jenderal
    log "Directories created"
}

# 9. Download binary
download_binary() {
    if [[ -f "$JENDERAL_BIN" ]]; then
        warn "Binary already exists, backing up..."
        cp "$JENDERAL_BIN" "${JENDERAL_BIN}.bak"
    fi

    # TODO: Replace with actual download URL when releases are published
    # curl -fsSL "https://github.com/mohammadirham37/jenderal_panel/releases/download/v${JENDERAL_VERSION}/jenderal-linux-${ARCH}" -o "$JENDERAL_BIN"
    log "Binary download: skipped (install from local build)"

    if [[ -f "$JENDERAL_BIN" ]]; then
        chmod +x "$JENDERAL_BIN"
    fi
}

# 10. Generate self-signed TLS
generate_tls() {
    if [[ -f /etc/jenderal/tls/cert.pem ]]; then
        log "TLS certificate already exists"
        return
    fi

    openssl req -x509 -newkey rsa:4096 -keyout /etc/jenderal/tls/key.pem \
        -out /etc/jenderal/tls/cert.pem -days 365 -nodes \
        -subj "/CN=jenderal-panel" 2>/dev/null

    chmod 600 /etc/jenderal/tls/key.pem
    chown "$JENDERAL_USER":"$JENDERAL_USER" /etc/jenderal/tls/*.pem
    log "Self-signed TLS certificate generated"
}

# 11. Write config
write_config() {
    if [[ -f "$JENDERAL_CONFIG" ]]; then
        log "Config already exists, skipping"
        return
    fi

    cat > "$JENDERAL_CONFIG" << 'CONFIGEOF'
server:
  host: "0.0.0.0"
  port: 8443
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
    log "Config written to $JENDERAL_CONFIG"
}

# 12. Write sudoers
write_sudoers() {
    cat > /etc/sudoers.d/jenderal << 'SUDOEOF'
# Jenderal Panel - privileged operations whitelist
jenderal ALL=(ALL) NOPASSWD: /usr/bin/systemctl start *
jenderal ALL=(ALL) NOPASSWD: /usr/bin/systemctl stop *
jenderal ALL=(ALL) NOPASSWD: /usr/bin/systemctl restart *
jenderal ALL=(ALL) NOPASSWD: /usr/bin/systemctl reload *
jenderal ALL=(ALL) NOPASSWD: /usr/bin/systemctl status *
jenderal ALL=(ALL) NOPASSWD: /usr/sbin/ufw *
jenderal ALL=(ALL) NOPASSWD: /usr/sbin/reboot
jenderal ALL=(ALL) NOPASSWD: /usr/sbin/shutdown *
jenderal ALL=(ALL) NOPASSWD: /usr/bin/hostnamectl set-hostname *
jenderal ALL=(ALL) NOPASSWD: /usr/bin/timedatectl set-timezone *
jenderal ALL=(ALL) NOPASSWD: /usr/bin/apt-get update
jenderal ALL=(ALL) NOPASSWD: /usr/bin/apt-get install -y *
jenderal ALL=(ALL) NOPASSWD: /usr/bin/nginx -t
jenderal ALL=(ALL) NOPASSWD: /usr/bin/tee /etc/nginx/sites-available/*
jenderal ALL=(ALL) NOPASSWD: /usr/bin/ln -sf /etc/nginx/sites-available/* /etc/nginx/sites-enabled/*
jenderal ALL=(ALL) NOPASSWD: /usr/bin/rm /etc/nginx/sites-enabled/*
jenderal ALL=(ALL) NOPASSWD: /usr/sbin/useradd *
jenderal ALL=(ALL) NOPASSWD: /usr/sbin/userdel *
SUDOEOF

    chmod 440 /etc/sudoers.d/jenderal
    log "Sudoers whitelist written"
}

# 13. Run migrations
run_migrations() {
    if [[ ! -f "$JENDERAL_BIN" ]]; then
        warn "Binary not found, skipping migrations"
        return
    fi

    sudo -u "$JENDERAL_USER" "$JENDERAL_BIN" migrate --config "$JENDERAL_CONFIG"
    log "Database migrations completed"
}

# 14-15. Create admin
create_admin() {
    if [[ ! -f "$JENDERAL_BIN" ]]; then
        warn "Binary not found, skipping admin creation"
        return
    fi

    local admin_password
    admin_password=$(openssl rand -base64 16 | tr -d '=/+' | head -c 16)

    echo -e "admin\nadmin@localhost\n${admin_password}" | \
        sudo -u "$JENDERAL_USER" "$JENDERAL_BIN" admin create --config "$JENDERAL_CONFIG"

    ADMIN_PASSWORD="$admin_password"
    log "Admin user created"
}

# 16-17. Install systemd
install_systemd() {
    cat > "$JENDERAL_SERVICE" << 'SERVICEEOF'
[Unit]
Description=Jenderal Panel
After=network.target

[Service]
Type=simple
User=jenderal
Group=jenderal
ExecStart=/opt/jenderal/jenderal serve
WorkingDirectory=/opt/jenderal
Restart=on-failure
RestartSec=5
LimitNOFILE=65535

[Install]
WantedBy=multi-user.target
SERVICEEOF

    systemctl daemon-reload
    systemctl enable jenderal
    systemctl start jenderal
    log "Systemd service installed and started"
}

# 18-19. Configure firewall
configure_firewall() {
    ufw allow 22/tcp comment "SSH" > /dev/null 2>&1 || true
    ufw allow 80/tcp comment "HTTP" > /dev/null 2>&1 || true
    ufw allow 443/tcp comment "HTTPS" > /dev/null 2>&1 || true
    ufw allow 8443/tcp comment "Jenderal Panel" > /dev/null 2>&1 || true

    if ! ufw status | grep -q "Status: active"; then
        echo "y" | ufw enable > /dev/null 2>&1
    fi
    log "Firewall configured"
}

# 20. Print summary
print_summary() {
    local server_ip
    server_ip=$(hostname -I | awk '{print $1}')

    echo ""
    echo -e "${GREEN}================================================${NC}"
    echo -e "${GREEN}  Jenderal Panel installed successfully!${NC}"
    echo -e "${GREEN}================================================${NC}"
    echo ""
    echo -e "  URL:      https://${server_ip}:8443"
    echo -e "  Username: admin"
    echo -e "  Password: ${ADMIN_PASSWORD:-<check install log>}"
    echo ""
    echo -e "  Config:   ${JENDERAL_CONFIG}"
    echo -e "  Logs:     /var/log/jenderal/jenderal.log"
    echo -e "  Service:  systemctl status jenderal"
    echo ""
    echo -e "${YELLOW}  Note: Using self-signed certificate.${NC}"
    echo -e "${YELLOW}  Replace with Let's Encrypt in Phase 4.${NC}"
    echo ""
}

# Main
main() {
    echo ""
    echo "Jenderal Panel Installer v${JENDERAL_VERSION}"
    echo "================================"
    echo ""

    check_root
    detect_os
    detect_arch
    check_resources
    check_internet
    install_deps
    create_user
    create_dirs
    download_binary
    generate_tls
    write_config
    write_sudoers
    run_migrations
    create_admin
    install_systemd
    configure_firewall
    print_summary
}

main "$@"
