#!/usr/bin/env bash
# Veilon installer
#
#   bash <(curl -Ls https://raw.githubusercontent.com/veilonhq/veilon/main/installer/install.sh) [version]
#
# Installs (or upgrades) Veilon on Ubuntu, Debian, CentOS Stream, AlmaLinux,
# Rocky Linux, Fedora or Arch Linux (amd64, arm64). Run it again to upgrade:
# data and settings are kept. After installing, run "veilon" for the menu.
#
# Environment: VEILON_REPO (default veilonhq/veilon).
set -euo pipefail

REPO="${VEILON_REPO:-veilonhq/veilon}"
BIN_PATH="/usr/local/bin/veilon"
CONF_DIR="/etc/veilon"
CONF_FILE="${CONF_DIR}/veilon.env"
DATA_DIR="/var/lib/veilon"
UNIT_FILE="/etc/systemd/system/veilon.service"
SVC_USER="veilon"
CRED_FILE="/root/veilon-credentials.txt"

ACTION="install"
USE_SQLITE=0
PANEL_LANG="fa"   # default panel language: fa | en
PORT=""
PANEL_PATH=""
ADMIN_USER=""
ADMIN_PASS=""
REL_VERSION="latest"
LOCAL_BINARY=""
PURGE=0
ASSUME_YES=0
CERT_MODE=""   # domain | ip | none
CERT_DOMAIN=""
CERT_IP=""
CERT_IPV6=""
CERT_EMAIL=""
NODE_TOKEN=""   # node install: enrollment token from the panel
ARCH=""
OS_ID=""
PKG=""         # apt | dnf | yum | pacman
INTERACTIVE=0

if [[ -t 1 && -z "${NO_COLOR:-}" ]]; then
	C_RED=$'\e[31m'; C_GRN=$'\e[32m'; C_YEL=$'\e[33m'; C_CYN=$'\e[36m'; C_BLD=$'\e[1m'; C_DIM=$'\e[2m'; C_RST=$'\e[0m'
else
	C_RED=""; C_GRN=""; C_YEL=""; C_CYN=""; C_BLD=""; C_DIM=""; C_RST=""
fi
info() { printf '%s›%s %s\n' "$C_CYN" "$C_RST" "$*"; }
ok() { printf '%s✔%s %s\n' "$C_GRN" "$C_RST" "$*"; }
warn() { printf '%s!%s %s\n' "$C_YEL" "$C_RST" "$*" >&2; }
die() { printf '%serror:%s %s\n' "$C_RED" "$C_RST" "$*" >&2; exit 1; }

usage() {
	cat <<'USAGE'
Veilon installer

Usage:
  bash <(curl -Ls https://raw.githubusercontent.com/veilonhq/veilon/main/installer/install.sh) [version] [options]
  install.sh [install|update|uninstall|info] [version] [options]

Install options (asked interactively when omitted; defaults with -y or without a terminal):
  --port <n>          panel port (default: random free port)
  --path <p>          panel path (default: random)
  --admin <name>      admin username (default: admin)
  --password <pw>     admin password (default: random)
  --cert <mode>       panel certificate: domain | ip | none (default: none without a terminal)
  --domain <name>     domain for --cert domain (Let's Encrypt, 90 days)
  --ip <addr|auto>    address for --cert ip (Let's Encrypt short-lived, renewed automatically)
  --ipv6 <addr|auto>  also include this IPv6 address in an IP certificate
  --email <addr>      contact email for Let's Encrypt (optional)
  --sqlite            use SQLite instead of PostgreSQL
  --lang fa|en        default panel language (default: fa)
  --version <tag>     release to install (default: latest; same as the positional [version])
  --binary <file>     install a local binary instead of downloading
  -y, --yes           no questions: use defaults and given options

Node (a remote server that serves inbounds for a panel):
  node --token <token>  set this server up as a node; copy the full command
                        from the panel (Nodes > Add node)

Other:
  --purge             with uninstall: also remove data, configuration, database and user
  -h, --help          this help

Running the installer on a server where Veilon is installed upgrades it in
place (data and settings are kept).
USAGE
	exit 0
}

parse_args() {
	while [[ $# -gt 0 ]]; do
		case "$1" in
			install|update|upgrade|uninstall|info|node) ACTION="$1"; [[ "$ACTION" == upgrade ]] && ACTION=update ;;
			--token) NODE_TOKEN="${2:?--token needs a value}"; shift ;;
			--sqlite) USE_SQLITE=1 ;;
			--lang|--language) PANEL_LANG="${2:?--lang needs a value}"; shift ;;
			--port) PORT="${2:?--port needs a value}"; shift ;;
			--path) PANEL_PATH="${2:?--path needs a value}"; shift ;;
			--admin|--username) ADMIN_USER="${2:?--admin needs a value}"; shift ;;
			--password) ADMIN_PASS="${2:?--password needs a value}"; shift ;;
			--cert) CERT_MODE="${2:?--cert needs a value}"; shift ;;
			--domain) CERT_DOMAIN="${2:?--domain needs a value}"; shift ;;
			--ip) CERT_IP="${2:?--ip needs a value}"; shift ;;
			--ipv6) CERT_IPV6="${2:?--ipv6 needs a value}"; shift ;;
			--email) CERT_EMAIL="${2:?--email needs a value}"; shift ;;
			--version) REL_VERSION="${2:?--version needs a value}"; shift ;;
			--binary) LOCAL_BINARY="${2:?--binary needs a value}"; shift ;;
			--purge) PURGE=1 ;;
			-y|--yes) ASSUME_YES=1 ;;
			-h|--help) usage ;;
			latest|v[0-9]*|[0-9]*) REL_VERSION="$1" ;;
			*) die "unknown argument: $1 (see --help)" ;;
		esac
		shift
	done
	if [[ "$REL_VERSION" != latest && "$REL_VERSION" != v* ]]; then
		REL_VERSION="v${REL_VERSION}"
	fi
	if [[ -n "$CERT_DOMAIN" && -z "$CERT_MODE" ]]; then CERT_MODE="domain"; fi
	if [[ -n "$CERT_IP$CERT_IPV6" && -z "$CERT_MODE" ]]; then CERT_MODE="ip"; fi
	case "$CERT_MODE" in
		""|domain|ip|none) ;;
		skip) CERT_MODE="none" ;;
		*) die "--cert must be domain, ip or none" ;;
	esac
	if [[ "$CERT_MODE" == domain && -z "$CERT_DOMAIN" && $ASSUME_YES -eq 1 ]]; then
		die "--cert domain needs --domain"
	fi
	case "$PANEL_LANG" in fa|en) ;; *) die "--lang must be fa or en" ;; esac
	[[ -z "$PORT" ]] || valid_port "$PORT" || die "--port must be 1-65535"
	[[ -z "$PANEL_PATH" ]] || valid_path "$PANEL_PATH" || die "--path must be 2-64 characters of letters, digits, _ or -"
	[[ -z "$ADMIN_USER" ]] || valid_user "$ADMIN_USER" || die "--admin must be 3-32 characters of letters, digits, _ . -"
	[[ -z "$ADMIN_PASS" ]] || valid_pass "$ADMIN_PASS" || die "--password must be at least 8 characters"
	if [[ $ASSUME_YES -eq 0 && -t 1 && -r /dev/tty ]] && { : </dev/tty; } 2>/dev/null; then
		INTERACTIVE=1
	fi
}

valid_port() { [[ "$1" =~ ^[0-9]+$ ]] && (( $1 >= 1 && $1 <= 65535 )); }
valid_path() { [[ "$1" =~ ^[A-Za-z0-9_-]{2,64}$ ]]; }
valid_user() { [[ "$1" =~ ^[A-Za-z0-9_.-]{3,32}$ ]]; }
valid_pass() { (( ${#1} >= 8 )); }

# ask <question> <default>: prints the answer (default on Enter).
ask() {
	local q="$1" def="$2" ans=""
	if [[ -n "$def" ]]; then
		printf '%s %s[%s]%s: ' "$q" "$C_DIM" "$def" "$C_RST" >/dev/tty
	else
		printf '%s: ' "$q" >/dev/tty
	fi
	IFS= read -r ans </dev/tty || true
	ans="${ans#"${ans%%[![:space:]]*}"}"
	ans="${ans%"${ans##*[![:space:]]}"}"
	printf '%s' "${ans:-$def}"
}

# ask_yn <question> <default y|n>: succeeds on yes.
ask_yn() {
	local def="$2" hint="y/N" ans
	[[ "$def" == y ]] && hint="Y/n"
	ans=$(ask "$1 ($hint)" "")
	ans="${ans:-$def}"
	[[ "$ans" =~ ^[Yy]([Ee][Ss])?$ ]]
}

require_root() { [[ $EUID -eq 0 ]] || die "run as root (sudo bash install.sh ...)"; }

detect_os() {
	[[ -r /etc/os-release ]] || die "cannot detect the OS (/etc/os-release missing)"
	# read only ID and ID_LIKE: sourcing os-release into this shell would
	# overwrite our variables (it defines VERSION, NAME, ...)
	local os_id os_like
	# shellcheck source=/dev/null
	os_id=$(. /etc/os-release && printf '%s' "${ID:-}")
	# shellcheck source=/dev/null
	os_like=$(. /etc/os-release && printf '%s' "${ID_LIKE:-}")
	OS_ID="${os_id:-unknown}"
	case "${os_id} ${os_like}" in
		*ubuntu*|*debian*) PKG="apt" ;;
		*arch*) PKG="pacman" ;;
		*rhel*|*centos*|*fedora*|*almalinux*|*rocky*)
			if command -v dnf >/dev/null; then PKG="dnf"; else PKG="yum"; fi ;;
		*) die "unsupported OS '${OS_ID}' (Ubuntu, Debian, CentOS, AlmaLinux, Rocky Linux, Fedora or Arch Linux required)" ;;
	esac
	command -v systemctl >/dev/null || die "systemd is required"
}

detect_arch() {
	case "$(uname -m)" in
		x86_64|amd64) ARCH="amd64" ;;
		aarch64|arm64) ARCH="arm64" ;;
		*) die "unsupported architecture $(uname -m) (amd64 or arm64 required)" ;;
	esac
}

rand_str() { tr -dc 'A-Za-z0-9' </dev/urandom | head -c "$1" || true; }
rand_lower() { tr -dc 'a-z0-9' </dev/urandom | head -c "$1" || true; }

port_in_use() { ss -Hltnu "sport = :$1" 2>/dev/null | grep -q .; }

random_port() {
	local p
	for _ in $(seq 1 50); do
		p=$(( ((RANDOM << 15) | RANDOM) % 40000 + 20000 ))
		port_in_use "$p" || { echo "$p"; return; }
	done
	die "could not find a free port"
}

# public_ip 4|6: the address external checkers see for this server.
public_ip() {
	local fam="$1" ip=""
	for url in https://api.ipify.org https://icanhazip.com https://ifconfig.me/ip; do
		ip=$(curl "-${fam}" -fsS --max-time 5 "$url" 2>/dev/null | tr -d '[:space:]') || true
		if [[ "$fam" == 4 && "$ip" =~ ^[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+$ ]] || [[ "$fam" == 6 && "$ip" == *:* ]]; then
			echo "$ip"
			return
		fi
	done
	if [[ "$fam" == 4 ]]; then
		hostname -I 2>/dev/null | tr ' ' '\n' | grep -E '^[0-9.]+$' | head -n1 || true
	fi
}

pkg_install() {
	case "$PKG" in
		apt) DEBIAN_FRONTEND=noninteractive apt-get install -y -qq "$@" >/dev/null ;;
		dnf) dnf install -y -q "$@" >/dev/null ;;
		yum) yum install -y -q "$@" >/dev/null ;;
		pacman) pacman -S --noconfirm --needed "$@" >/dev/null ;;
	esac
}

install_deps() {
	info "installing dependencies (${OS_ID}, ${PKG})"
	case "$PKG" in
		apt)
			apt-get update -qq >/dev/null
			pkg_install curl ca-certificates tar iproute2 iptables nftables coreutils util-linux ;;
		dnf)
			# tc lives in iproute-tc on Fedora / RHEL 8+.
			pkg_install curl ca-certificates tar iproute iproute-tc iptables nftables coreutils util-linux ;;
		yum)
			pkg_install curl ca-certificates tar iproute iptables coreutils util-linux
			pkg_install nftables || warn "nftables is not available; per-user IP limits will not be enforced" ;;
		pacman)
			pacman -Sy --noconfirm >/dev/null
			pkg_install curl ca-certificates tar iproute2 iptables nftables coreutils util-linux ;;
	esac
	# The connection guard enforces IP limits with nft and speed limits with tc.
	command -v nft >/dev/null || warn "nft not found; per-user IP limits will not be enforced"
	command -v tc >/dev/null || warn "tc not found; per-user speed limits will not be enforced"
}

pg_ready() { runuser -u postgres -- psql -tAc 'SELECT 1' >/dev/null 2>&1; }

install_postgres() {
	if ! command -v psql >/dev/null || ! id -u postgres >/dev/null 2>&1; then
		info "installing PostgreSQL"
		case "$PKG" in
			apt) pkg_install postgresql ;;
			dnf|yum) pkg_install postgresql-server postgresql ;;
			pacman) pkg_install postgresql ;;
		esac
	fi
	case "$PKG" in
		dnf|yum)
			if [[ ! -f /var/lib/pgsql/data/PG_VERSION ]]; then
				postgresql-setup --initdb >/dev/null 2>&1 || postgresql-setup initdb >/dev/null
			fi
			# Default RHEL rules use "ident" for TCP connections; Veilon logs in
			# with a password over 127.0.0.1.
			if grep -qE '^host.*(ident)\s*$' /var/lib/pgsql/data/pg_hba.conf 2>/dev/null; then
				sed -i -E 's/^(host.*)ident\s*$/\1scram-sha-256/' /var/lib/pgsql/data/pg_hba.conf
				systemctl restart postgresql >/dev/null 2>&1 || true
			fi ;;
		pacman)
			if [[ ! -f /var/lib/postgres/data/PG_VERSION ]]; then
				runuser -u postgres -- initdb -D /var/lib/postgres/data >/dev/null
			fi ;;
	esac
	systemctl enable --now postgresql >/dev/null 2>&1 || true
	for _ in $(seq 1 30); do
		pg_ready && return
		sleep 1
	done
	die "PostgreSQL did not become ready"
}

# setup_postgres_db <password>: create role and database if missing, (re)set password.
setup_postgres_db() {
	local pw="$1"
	if runuser -u postgres -- psql -tAc "SELECT 1 FROM pg_roles WHERE rolname='veilon'" | grep -q 1; then
		runuser -u postgres -- psql -q -c "ALTER ROLE veilon WITH LOGIN PASSWORD '${pw}'"
	else
		runuser -u postgres -- psql -q -c "CREATE ROLE veilon LOGIN PASSWORD '${pw}'"
	fi
	if ! runuser -u postgres -- psql -tAc "SELECT 1 FROM pg_database WHERE datname='veilon'" | grep -q 1; then
		runuser -u postgres -- psql -q -c "CREATE DATABASE veilon OWNER veilon"
	fi
}

# fetch_binary <dest>: install the release (or local) binary at dest.
fetch_binary() {
	local dest="$1" tmp asset base
	if [[ -n "$LOCAL_BINARY" ]]; then
		[[ -f "$LOCAL_BINARY" ]] || die "binary $LOCAL_BINARY not found"
		install -m 0755 "$LOCAL_BINARY" "$dest"
		return
	fi
	asset="veilon-linux-${ARCH}"
	if [[ "$REL_VERSION" == "latest" ]]; then
		base="https://github.com/${REPO}/releases/latest/download"
	else
		base="https://github.com/${REPO}/releases/download/${REL_VERSION}"
	fi
	tmp=$(mktemp -d)
	info "downloading ${asset} (${REL_VERSION})"
	if ! curl -fL --retry 3 --progress-bar -o "${tmp}/${asset}" "${base}/${asset}"; then
		rm -rf "$tmp"
		die "download failed: ${base}/${asset}"
	fi
	if ! curl -fsSL --retry 3 -o "${tmp}/checksums.txt" "${base}/checksums.txt"; then
		rm -rf "$tmp"
		die "checksums.txt is not available for ${REL_VERSION}; refusing to install an unverified binary"
	fi
	if ! (cd "$tmp" && grep -E "[[:space:]]\*?${asset}\$" checksums.txt | sha256sum -c --status); then
		rm -rf "$tmp"
		die "checksum verification failed"
	fi
	ok "checksum verified"
	install -m 0755 "${tmp}/${asset}" "$dest"
	rm -rf "$tmp"
}

ensure_user() {
	if ! id -u "$SVC_USER" >/dev/null 2>&1; then
		local nologin=/usr/sbin/nologin
		[[ -x "$nologin" ]] || nologin=/sbin/nologin
		useradd --system --home-dir "$DATA_DIR" --shell "$nologin" "$SVC_USER"
	fi
	install -d -m 0750 -o "$SVC_USER" -g "$SVC_USER" "$DATA_DIR"
	install -d -m 0750 -o root -g "$SVC_USER" "$CONF_DIR"
}

write_config() {
	if [[ -f "$CONF_FILE" ]]; then
		info "keeping existing ${CONF_FILE}"
		return
	fi
	local driver dsn pw
	if [[ $USE_SQLITE -eq 1 ]]; then
		driver="sqlite"
		dsn="${DATA_DIR}/veilon.db"
	else
		pw=$(rand_str 32)
		setup_postgres_db "$pw"
		driver="postgres"
		dsn="host=127.0.0.1 port=5432 user=veilon password=${pw} dbname=veilon sslmode=disable"
	fi
	(
		umask 027
		cat >"$CONF_FILE" <<EOF
# Veilon configuration (generated by install.sh)
VEILON_DB_DRIVER=${driver}
VEILON_DB_DSN=${dsn}
VEILON_DATA_DIR=${DATA_DIR}
VEILON_LISTEN=0.0.0.0
VEILON_XRAY_AUTO_DOWNLOAD=true
EOF
	)
	chown root:"$SVC_USER" "$CONF_FILE"
	chmod 0640 "$CONF_FILE"
}

write_unit() {
	local after="network-online.target"
	if grep -q '^VEILON_DB_DRIVER=postgres' "$CONF_FILE" 2>/dev/null; then
		after="network-online.target postgresql.service"
	fi
	cat >"$UNIT_FILE" <<EOF
[Unit]
Description=Veilon proxy panel
Documentation=https://github.com/${REPO}
After=${after}
Wants=network-online.target

[Service]
Type=simple
User=${SVC_USER}
Group=${SVC_USER}
Environment=VEILON_CONFIG=${CONF_FILE}
WorkingDirectory=${DATA_DIR}
ExecStart=${BIN_PATH} serve
Restart=on-failure
RestartSec=5
LimitNOFILE=1048576
# NET_BIND_SERVICE: ports below 1024 (panel, inbounds, ACME port 80).
# NET_ADMIN/NET_RAW: TUN inbounds and the AmneziaWG sidecar (interface, NAT).
AmbientCapabilities=CAP_NET_BIND_SERVICE CAP_NET_ADMIN CAP_NET_RAW
CapabilityBoundingSet=CAP_NET_BIND_SERVICE CAP_NET_ADMIN CAP_NET_RAW
NoNewPrivileges=true
ProtectSystem=full
ProtectHome=true
PrivateTmp=true
ReadWritePaths=${DATA_DIR}

[Install]
WantedBy=multi-user.target
EOF
	systemctl daemon-reload
}

# enable_updater installs veilon-updater.path/.service so the panel can
# offer updates itself (download, checksum, backup, swap, restart, rollback).
enable_updater() {
	VEILON_CONFIG="$CONF_FILE" "$BIN_PATH" self-update --install-units >/dev/null 2>&1 ||
		warn "in-panel updates are not available with this version"
}

# vctl runs the veilon CLI as the service user with the service config.
vctl() { runuser -u "$SVC_USER" -- env VEILON_CONFIG="$CONF_FILE" VEILON_NO_REEXEC=1 "$BIN_PATH" "$@"; }

ufw_active() { command -v ufw >/dev/null && ufw status 2>/dev/null | grep -q 'Status: active'; }
firewalld_active() { command -v firewall-cmd >/dev/null && [[ "$(firewall-cmd --state 2>/dev/null)" == running ]]; }

# open_port <port>: allow <port>/tcp in an active ufw or firewalld.
open_port() {
	local port="$1"
	if ufw_active; then
		ufw allow "${port}/tcp" >/dev/null && info "opened ${port}/tcp in ufw"
	elif firewalld_active; then
		firewall-cmd -q --permanent --add-port="${port}/tcp" && firewall-cmd -q --reload && info "opened ${port}/tcp in firewalld"
	fi
	return 0
}

wait_healthy() {
	local port path
	port=$(vctl info --json 2>/dev/null | sed -n 's/.*"port": *\([0-9]*\).*/\1/p' | head -n1)
	path=$(vctl info --json 2>/dev/null | sed -n 's/.*"path": *"\([^"]*\)".*/\1/p' | head -n1)
	for _ in $(seq 1 40); do
		if [[ -S "${DATA_DIR}/veilon.sock" ]]; then
			for scheme in http https; do
				if curl -fsk --noproxy '*' --max-time 2 "${scheme}://127.0.0.1:${port}${path}api/v1/health" >/dev/null 2>&1; then
					return 0
				fi
			done
		fi
		sleep 1
	done
	return 1
}

# ask_settings fills PORT, ADMIN_USER, ADMIN_PASS, PANEL_PATH and CERT_* (prompts on a terminal).
ask_settings() {
	local def_port def_pass def_path v
	def_port="${PORT:-$(random_port)}"
	def_pass="${ADMIN_PASS:-$(rand_str 16)}"
	def_path="${PANEL_PATH:-$(rand_lower 16)}"
	ADMIN_USER="${ADMIN_USER:-admin}"
	if [[ $INTERACTIVE -eq 0 ]]; then
		PORT="$def_port"; ADMIN_PASS="$def_pass"; PANEL_PATH="$def_path"
		CERT_MODE="${CERT_MODE:-none}"
		return
	fi
	echo
	printf '%sPanel settings%s %s(press Enter to accept the default)%s\n' "$C_BLD" "$C_RST" "$C_DIM" "$C_RST"
	while :; do
		v=$(ask "  Panel port" "$def_port")
		if ! valid_port "$v"; then warn "enter a number from 1 to 65535"; continue; fi
		if port_in_use "$v"; then warn "port $v is already in use"; continue; fi
		PORT="$v"; break
	done
	while :; do
		v=$(ask "  Admin username" "$ADMIN_USER")
		valid_user "$v" && { ADMIN_USER="$v"; break; }
		warn "3-32 characters of letters, digits, _ . -"
	done
	while :; do
		v=$(ask "  Admin password" "$def_pass")
		valid_pass "$v" && { ADMIN_PASS="$v"; break; }
		warn "at least 8 characters"
	done
	while :; do
		v=$(ask "  Panel path" "$def_path")
		v="${v#/}"; v="${v%/}"
		valid_path "$v" && { PANEL_PATH="$v"; break; }
		warn "2-64 characters of letters, digits, _ or -"
	done
	[[ -n "$CERT_MODE" ]] && return
	echo
	printf '%sPanel certificate (HTTPS)%s\n' "$C_BLD" "$C_RST"
	echo "  1) Domain     Let's Encrypt, 90 days, renewed automatically (the domain must point to this server)"
	echo "  2) Server IP  Let's Encrypt, 6-day certificate renewed automatically every ~3 days"
	echo "  3) Skip       plain HTTP for now (run 'veilon' later to add one)"
	printf '  %sValidation uses port 80/tcp; it must be reachable from the internet.%s\n' "$C_DIM" "$C_RST"
	while :; do
		v=$(ask "  Choose" "2")
		case "$v" in
			1) CERT_MODE="domain"; break ;;
			2) CERT_MODE="ip"; break ;;
			3) CERT_MODE="none"; break ;;
		esac
	done
	if [[ "$CERT_MODE" == domain && -z "$CERT_DOMAIN" ]]; then
		while :; do
			CERT_DOMAIN=$(ask "  Domain" "")
			[[ "$CERT_DOMAIN" =~ ^[A-Za-z0-9.-]+\.[A-Za-z]{2,}$ ]] && break
			warn "enter a domain name such as panel.example.com"
		done
	fi
	if [[ "$CERT_MODE" == ip && ( -z "$CERT_IP" || "$CERT_IP" == auto ) ]]; then
		local ip4 ip6
		info "detecting the server's public IP"
		ip4=$(public_ip 4); ip6=$(public_ip 6)
		[[ -n "$ip4" ]] && printf '  detected IPv4: %s%s%s\n' "$C_BLD" "$ip4" "$C_RST"
		[[ -n "$ip6" ]] && printf '  detected IPv6: %s%s%s\n' "$C_BLD" "$ip6" "$C_RST"
		while :; do
			CERT_IP=$(ask "  IP address for the certificate (Enter to confirm, or type another)" "${ip4:-$ip6}")
			[[ "$CERT_IP" =~ ^[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+$ || "$CERT_IP" == *:* ]] && break
			warn "enter an IPv4 or IPv6 address"
		done
		if [[ -n "$ip6" && -z "$CERT_IPV6" && "$CERT_IP" != *:* ]] && ask_yn "  Also include IPv6 ${ip6}?" n; then
			CERT_IPV6="$ip6"
		fi
	fi
}

# issue_cert: request the panel certificate through the running service.
issue_cert() {
	local args=()
	case "$CERT_MODE" in
		domain) [[ -n "$CERT_DOMAIN" ]] || { warn "no domain given; skipping the certificate"; return 1; }
			args=(--domain "$CERT_DOMAIN") ;;
		ip) args=(--ip "${CERT_IP:-auto}")
			[[ -n "$CERT_IPV6" ]] && args+=(--ipv6 "$CERT_IPV6") ;;
		*) return 1 ;;
	esac
	[[ -n "$CERT_EMAIL" ]] && args+=(--email "$CERT_EMAIL")
	open_port 80
	info "requesting the certificate (this can take a minute)"
	if VEILON_CONFIG="$CONF_FILE" "$BIN_PATH" cert issue "${args[@]}" --panel -y; then
		return 0
	fi
	warn "the certificate could not be issued; the panel stays on plain HTTP."
	warn "check that port 80/tcp is open (also in your provider's firewall), then run: veilon  (menu 14)"
	return 1
}

# box_line <label> <value> <width>: one row of the summary box.
box_line() {
	local label="$1" value="$2" w="$3" fill
	fill=$(( w - ${#label} - ${#value} ))
	(( fill < 0 )) && fill=0
	printf '%s│%s  %s%s%s%s%*s  %s│%s\n' "$C_GRN" "$C_RST" "$label" "$C_BLD" "$value" "$C_RST" "$fill" "" "$C_GRN" "$C_RST"
}

# summary <password or empty>: print the final box.
summary() {
	local pass="$1" url user path w=40 bar row
	url=$(vctl info --url 2>/dev/null || true)
	user=$(vctl info --json 2>/dev/null | sed -n 's/.*"admin": *"\([^"]*\)".*/\1/p' | head -n1)
	path=$(vctl info --json 2>/dev/null | sed -n 's/.*"path": *"\([^"]*\)".*/\1/p' | head -n1)
	pass="${pass:-(unchanged)}"
	for row in "$url" "$user" "$pass" "$path"; do
		(( ${#row} + 10 > w )) && w=$(( ${#row} + 10 ))
	done
	bar=$(printf '─%.0s' $(seq 1 $((w + 4))))
	echo
	printf '%s╭%s╮%s\n' "$C_GRN" "$bar" "$C_RST"
	box_line "" "Veilon is ready" "$w"
	printf '%s├%s┤%s\n' "$C_GRN" "$bar" "$C_RST"
	box_line "URL       " "$url" "$w"
	box_line "Username  " "$user" "$w"
	box_line "Password  " "$pass" "$w"
	box_line "Path      " "$path" "$w"
	box_line "" "" "$w"
	box_line "Menu      " "run: veilon" "$w"
	printf '%s╰%s╯%s\n' "$C_GRN" "$bar" "$C_RST"
	if [[ "$pass" != "(unchanged)" ]]; then
		echo "  Saved to ${CRED_FILE} (root only). Open inbound ports later with: veilon firewall"
	fi
	echo
}

do_install() {
	require_root
	detect_os
	detect_arch
	if [[ -x "$BIN_PATH" && -f "$UNIT_FILE" ]]; then
		info "Veilon is already installed: upgrading (data and settings are kept)"
		do_update
		summary ""
		return
	fi
	printf '%sInstalling Veilon%s %s(%s, %s)%s\n' "$C_BLD" "$C_RST" "$C_DIM" "$OS_ID" "$ARCH" "$C_RST"
	ask_settings
	install_deps
	if [[ $USE_SQLITE -eq 0 ]]; then
		install_postgres
	fi
	ensure_user
	fetch_binary "$BIN_PATH"
	write_config

	info "initialising the database and settings"
	vctl set-port "$PORT" >/dev/null
	vctl set-path "$PANEL_PATH" >/dev/null
	vctl set-language "$PANEL_LANG" >/dev/null
	local ip
	ip=$(public_ip 4)
	[[ -n "$ip" ]] && vctl set-address "$ip" >/dev/null
	vctl admin reset-credentials --username "$ADMIN_USER" --password "$ADMIN_PASS" >/dev/null

	write_unit
	enable_updater
	systemctl enable --now veilon >/dev/null 2>&1
	open_port "$PORT"
	if ! wait_healthy; then
		warn "the panel did not answer yet; check: veilon logs"
	elif [[ "$CERT_MODE" == domain || "$CERT_MODE" == ip ]]; then
		issue_cert || true
	fi
	(
		umask 077
		printf 'Veilon panel\nURL:      %s\nUsername: %s\nPassword: %s\n' "$(vctl info --url)" "$ADMIN_USER" "$ADMIN_PASS" >"$CRED_FILE"
	)
	summary "$ADMIN_PASS"
}

do_update() {
	require_root
	detect_arch
	[[ -x "$BIN_PATH" ]] || die "Veilon is not installed"
	local tmp
	tmp=$(mktemp)
	fetch_binary "$tmp"
	if cmp -s "$tmp" "$BIN_PATH"; then
		ok "already up to date ($("$BIN_PATH" version))"
		rm -f "$tmp"
	else
		install -m 0755 "$tmp" "$BIN_PATH"
		rm -f "$tmp"
		ok "installed $("$BIN_PATH" version)"
	fi
	if [[ -f "$UNIT_FILE" ]]; then
		write_unit
		enable_updater
		systemctl restart veilon
		# wait for the new process to finish its migrations before any other
		# command opens the database
		if wait_healthy; then
			ok "service restarted"
		else
			warn "the service did not answer yet; check: veilon logs"
		fi
	fi
}

do_uninstall() {
	require_root
	if [[ $ASSUME_YES -eq 0 && $INTERACTIVE -eq 0 ]]; then
		die "refusing to uninstall non-interactively without --yes"
	fi
	if [[ $ASSUME_YES -eq 0 ]]; then
		local what="the service and binary (data is kept)"
		[[ $PURGE -eq 1 ]] && what="the service, binary, configuration, ALL DATA and the database"
		ask_yn "This removes ${what}. Continue?" n || die "aborted"
	fi
	systemctl disable --now veilon veilon-updater.path >/dev/null 2>&1 || true
	rm -f "$UNIT_FILE" /etc/systemd/system/veilon-updater.path /etc/systemd/system/veilon-updater.service
	systemctl daemon-reload || true
	rm -f "$BIN_PATH"
	ok "service and binary removed"
	if [[ $PURGE -eq 1 ]]; then
		if grep -qs '^VEILON_DB_DRIVER=postgres' "$CONF_FILE" && command -v psql >/dev/null; then
			runuser -u postgres -- psql -q -c "DROP DATABASE IF EXISTS veilon" || true
			runuser -u postgres -- psql -q -c "DROP ROLE IF EXISTS veilon" || true
		fi
		local was_pg=0
		grep -qs '^VEILON_DB_DRIVER=postgres' "$CONF_FILE" && was_pg=1
		rm -rf "$CONF_DIR" "$DATA_DIR" "$CRED_FILE"
		if id -u "$SVC_USER" >/dev/null 2>&1; then
			userdel "$SVC_USER" 2>/dev/null || true
		fi
		if [[ -f /etc/sysctl.d/99-veilon-bbr.conf ]]; then
			rm -f /etc/sysctl.d/99-veilon-bbr.conf
			info "removed /etc/sysctl.d/99-veilon-bbr.conf (BBR stays on until the next reboot)"
		fi
		ok "configuration, data and database removed"
		[[ $was_pg -eq 1 ]] && info "PostgreSQL itself is left installed; remove it with your package manager if nothing else uses it."
	else
		echo "  Data kept in ${DATA_DIR} and ${CONF_DIR} (use --purge to remove)."
	fi
}

# do_node installs only the binary and the node agent (no panel, no
# database); "veilon node install" writes the unit, gets Xray and starts it.
do_node() {
	require_root
	[[ -n "$NODE_TOKEN" ]] || die "node needs --token (copy the command from the panel: Nodes > Add node)"
	detect_os
	detect_arch
	install_deps
	if [[ -x "$BIN_PATH" ]]; then
		local tmp
		tmp=$(mktemp)
		fetch_binary "$tmp"
		install -m 0755 "$tmp" "$BIN_PATH"
		rm -f "$tmp"
	else
		fetch_binary "$BIN_PATH"
	fi
	"$BIN_PATH" node install --token "$NODE_TOKEN"
}

main() {
	parse_args "$@"
	case "$ACTION" in
		install) do_install ;;
		update) detect_os; do_update; summary "" ;;
		uninstall) do_uninstall ;;
		node) do_node ;;
		info) require_root; [[ -x "$BIN_PATH" ]] || die "Veilon is not installed"; vctl info ;;
	esac
}

main "$@"
