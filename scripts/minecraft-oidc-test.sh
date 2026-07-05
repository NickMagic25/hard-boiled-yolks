#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
EXAMPLE_DIR="$ROOT/examples/minecraft-oidc"
COMPOSE_FILE="$EXAMPLE_DIR/compose.yaml"
PROJECT="${HBY_COMPOSE_PROJECT:-hby-minecraft-oidc}"
JAVA_VERSION="${HBY_JAVA_VERSION:-25}"
MINECRAFT_IMAGE="${HBY_MINECRAFT_IMAGE:-hard-boiled-yolks:java_${JAVA_VERSION}-minecraft-oidc}"
AUTHELIA_IMAGE="${HBY_AUTHELIA_IMAGE:-docker.io/authelia/authelia:4.39.19}"
BUILD_DIR="${HBY_BUILD_DIR:-$EXAMPLE_DIR/data/build}"
MELANGE_KEY="${HBY_MELANGE_KEY:-$BUILD_DIR/melange.rsa}"
PACKAGES_DIR="${HBY_PACKAGES_DIR:-$BUILD_DIR/packages}"
SOURCE_DIR="${HBY_SOURCE_DIR:-$BUILD_DIR/source}"
IMAGE_TAR="${HBY_IMAGE_TAR:-$BUILD_DIR/java-${JAVA_VERSION}-minecraft-oidc.tar}"
AUTHELIA_USER="${AUTHELIA_USER:-hby}"
AUTHELIA_PASSWORD="${AUTHELIA_PASSWORD:-hby-password}"
AUTHELIA_EMAIL="${AUTHELIA_EMAIL:-hby@example.com}"
HBY_AUTHELIA_URL="${HBY_AUTHELIA_URL:-https://auth.localhost:9091}"
HBY_CONTROL_PUBLIC_URL="${HBY_CONTROL_PUBLIC_URL:-http://hby.localhost:8080}"
AUTHELIA_DOMAIN="${AUTHELIA_DOMAIN:-$(printf '%s' "$HBY_AUTHELIA_URL" | sed -E 's#^[^:/]+://([^/:]+).*#\1#')}"

usage() {
  cat <<EOF
usage: $0 <command>

commands:
  build      build and load the local Java image
  up         prepare Authelia runtime files and start Compose
  deploy     build, prepare, and start Compose
  down       stop Compose containers
  teardown   stop Compose containers and remove Compose volumes
  clean      remove generated Authelia files and local image tarball
  logs       follow Compose logs
  status     show Compose service status
  urls       print local test URLs and credentials

environment:
  HBY_MINECRAFT_IMAGE   local image tag, default: $MINECRAFT_IMAGE
  HBY_JAVA_VERSION      Java image version from java/<version>, default: $JAVA_VERSION
  HBY_AUTHELIA_IMAGE    Authelia image tag, default: $AUTHELIA_IMAGE
  AUTHELIA_USER         login username, default: $AUTHELIA_USER
  AUTHELIA_PASSWORD     login password, default: hby-password
  MC_VERSION            Minecraft version, default: latest
  MC_MEMORY             Minecraft JVM memory, default: 1024M
  HBY_AUTHELIA_URL      public OIDC issuer URL, default: $HBY_AUTHELIA_URL
  HBY_CONTROL_PUBLIC_URL public control UI URL, default: $HBY_CONTROL_PUBLIC_URL
  HBY_SKIP_ARCH_CHECK   set to 1 to skip external manifest checks
EOF
}

die() {
  echo "error: $*" >&2
  exit 1
}

need() {
  command -v "$1" >/dev/null 2>&1 || die "$1 is required"
}

compose() {
  env HBY_MINECRAFT_IMAGE="$MINECRAFT_IMAGE" HBY_AUTHELIA_IMAGE="$AUTHELIA_IMAGE" \
    AUTHELIA_EMAIL="$AUTHELIA_EMAIL" \
    docker compose -p "$PROJECT" -f "$COMPOSE_FILE" "$@"
}

assert_contains() {
  local file="$1"
  local pattern="$2"
  grep -q "$pattern" "$file" || die "$file does not contain required architecture marker: $pattern"
}

check_local_arch_declarations() {
  assert_contains "$ROOT/control/melange.yaml" "x86_64"
  assert_contains "$ROOT/control/melange.yaml" "aarch64"
  assert_contains "$ROOT/java/entrypoint/melange.yaml" "x86_64"
  assert_contains "$ROOT/java/entrypoint/melange.yaml" "aarch64"
  [ -f "$ROOT/java/$JAVA_VERSION/apko.yaml" ] || die "java/$JAVA_VERSION/apko.yaml does not exist"
  assert_contains "$ROOT/java/$JAVA_VERSION/apko.yaml" "x86_64"
  assert_contains "$ROOT/java/$JAVA_VERSION/apko.yaml" "aarch64"
}

check_external_image_arches() {
  [ "${HBY_SKIP_ARCH_CHECK:-0}" = "1" ] && return 0
  need docker

  local out
  if ! out="$(docker buildx imagetools inspect "$AUTHELIA_IMAGE" 2>&1)"; then
    echo "$out" >&2
    die "could not inspect $AUTHELIA_IMAGE; set HBY_SKIP_ARCH_CHECK=1 to bypass"
  fi
  printf '%s\n' "$out" | grep -Eq 'linux/amd64' || die "$AUTHELIA_IMAGE does not advertise linux/amd64"
  printf '%s\n' "$out" | grep -Eq 'linux/arm64|linux/arm64/v8' || die "$AUTHELIA_IMAGE does not advertise linux/arm64"
}

prepare_source_snapshot() {
  need rsync
  rm -rf "$SOURCE_DIR"
  mkdir -p "$SOURCE_DIR"
  rsync -a \
    --exclude .git \
    --exclude examples/minecraft-oidc/data \
    "$ROOT/" "$SOURCE_DIR/"
}

build_melange_package() {
  local config="$1"
  local source_dir="$2"
  local arch

  for arch in aarch64 x86_64; do
    melange build "$config" \
      --arch "$arch" \
      --source-dir "$source_dir" \
      --signing-key "$MELANGE_KEY" \
      --out-dir "$PACKAGES_DIR"
  done
}

tag_native_loaded_image() {
  local suffix
  case "$(uname -m)" in
    arm64|aarch64)
      suffix="arm64"
      ;;
    x86_64|amd64)
      suffix="amd64"
      ;;
    *)
      die "unsupported host architecture for Docker tag selection: $(uname -m)"
      ;;
  esac

  docker tag "$MINECRAFT_IMAGE-$suffix" "$MINECRAFT_IMAGE"
}

ensure_melange_key() {
  need melange
  mkdir -p "$(dirname "$MELANGE_KEY")"
  if [ ! -f "$MELANGE_KEY" ]; then
    echo "Generating $MELANGE_KEY"
    melange keygen "$MELANGE_KEY"
  fi
}

build_image() {
  need apko
  need docker
  ensure_melange_key
  check_local_arch_declarations
  check_external_image_arches
  prepare_source_snapshot

  rm -rf "$PACKAGES_DIR"
  mkdir -p "$PACKAGES_DIR"

  build_melange_package "$SOURCE_DIR/control/melange.yaml" "$SOURCE_DIR"
  build_melange_package "$SOURCE_DIR/java/entrypoint/melange.yaml" "$SOURCE_DIR/java"

  (cd "$ROOT/java" && apko build "$JAVA_VERSION/apko.yaml" "$MINECRAFT_IMAGE" "$IMAGE_TAR" \
    --repository-append "$PACKAGES_DIR" \
    --keyring-append "$MELANGE_KEY.pub")

  docker load -i "$IMAGE_TAR"
  tag_native_loaded_image
}

rand_secret() {
  openssl rand -hex 32
}

escape_sed_replacement() {
  printf '%s' "$1" | sed -e 's/[\/&]/\\&/g'
}

write_authelia_config() {
  local template="$EXAMPLE_DIR/authelia/configuration.yml"
  local target="$EXAMPLE_DIR/data/authelia/configuration.yml"
  sed \
    -e "s#__AUTHELIA_DOMAIN__#$(escape_sed_replacement "$AUTHELIA_DOMAIN")#g" \
    -e "s#__AUTHELIA_URL__#$(escape_sed_replacement "$HBY_AUTHELIA_URL")#g" \
    -e "s#__HBY_CONTROL_PUBLIC_URL__#$(escape_sed_replacement "$HBY_CONTROL_PUBLIC_URL")#g" \
    "$template" > "$target"
}

generate_password_hash() {
  need openssl
  local salt
  salt="$(openssl rand -base64 18 | tr -dc 'A-Za-z0-9' | head -c 16)"
  openssl passwd -6 -salt "$salt" "$AUTHELIA_PASSWORD"
}

write_secret_if_missing() {
  local path="$1"
  if [ ! -f "$path" ]; then
    rand_secret > "$path"
    chmod 600 "$path"
  fi
}

prepare_tls() {
  local tls_dir="$EXAMPLE_DIR/data/authelia/tls"
  mkdir -p "$tls_dir"

  if [ ! -f "$tls_dir/ca.key" ]; then
    openssl genrsa -out "$tls_dir/ca.key" 4096
    openssl req -x509 -new -nodes -key "$tls_dir/ca.key" -sha256 -days 3650 \
      -subj "/CN=HBY Local Test CA" \
      -out "$tls_dir/ca.crt"
  fi

  if [ ! -f "$tls_dir/auth.localhost.crt" ]; then
    local san_conf="$tls_dir/auth.localhost.openssl.cnf"
    cat > "$san_conf" <<EOF
[req]
distinguished_name=req_distinguished_name
req_extensions=v3_req
prompt=no

[req_distinguished_name]
CN=auth.localhost

[v3_req]
keyUsage=keyEncipherment,digitalSignature
extendedKeyUsage=serverAuth
subjectAltName=@alt_names

[alt_names]
DNS.1=auth.localhost
EOF
    openssl genrsa -out "$tls_dir/auth.localhost.key" 2048
    openssl req -new -key "$tls_dir/auth.localhost.key" -out "$tls_dir/auth.localhost.csr" -config "$san_conf"
    openssl x509 -req -in "$tls_dir/auth.localhost.csr" \
      -CA "$tls_dir/ca.crt" -CAkey "$tls_dir/ca.key" -CAcreateserial \
      -out "$tls_dir/auth.localhost.crt" -days 825 -sha256 \
      -extfile "$san_conf" -extensions v3_req
  fi
}

prepare_authelia() {
  need docker
  need openssl
  mkdir -p "$EXAMPLE_DIR/data/authelia/secrets"
  write_authelia_config

  write_secret_if_missing "$EXAMPLE_DIR/data/authelia/secrets/session_secret"
  write_secret_if_missing "$EXAMPLE_DIR/data/authelia/secrets/reset_jwt_secret"
  write_secret_if_missing "$EXAMPLE_DIR/data/authelia/secrets/storage_encryption_key"
  write_secret_if_missing "$EXAMPLE_DIR/data/authelia/secrets/oidc_hmac_secret"

  if [ ! -f "$EXAMPLE_DIR/data/authelia/secrets/oidc-rsa.key" ]; then
    openssl genrsa -out "$EXAMPLE_DIR/data/authelia/secrets/oidc-rsa.key" 2048
    chmod 600 "$EXAMPLE_DIR/data/authelia/secrets/oidc-rsa.key"
  fi

  prepare_tls

  if [ "${AUTHELIA_FORCE_USER:-0}" = "1" ] || [ ! -f "$EXAMPLE_DIR/data/authelia/users_database.yml" ]; then
    echo "Generating Authelia user '$AUTHELIA_USER'"
    local password_hash
    password_hash="$(generate_password_hash)"
    [ -n "$password_hash" ] || die "could not generate Authelia password hash"
    cat > "$EXAMPLE_DIR/data/authelia/users_database.yml" <<EOF
users:
  $AUTHELIA_USER:
    disabled: false
    displayname: HBY Test User
    password: "$password_hash"
    email: $AUTHELIA_EMAIL
    groups:
      - hby
EOF
  fi
}

print_urls() {
  cat <<EOF
OIDC issuer: $HBY_AUTHELIA_URL
Control UI:  $HBY_CONTROL_PUBLIC_URL
Authelia UI: $HBY_AUTHELIA_URL
Minecraft:   localhost:25565

Authelia login:
  username: $AUTHELIA_USER
  password: $AUTHELIA_PASSWORD

The default localhost mode uses a generated Authelia TLS certificate. If your
browser warns about auth.localhost, open it once and trust/accept the certificate
for this test.
EOF
}

cmd="${1:-}"
case "$cmd" in
  build)
    build_image
    ;;
  up)
    prepare_authelia
    compose up -d
    print_urls
    ;;
  deploy)
    build_image
    prepare_authelia
    compose up -d
    print_urls
    ;;
  down)
    compose down
    ;;
  teardown)
    compose down -v --remove-orphans
    ;;
  clean)
    compose down -v --remove-orphans || true
    rm -rf "$EXAMPLE_DIR/data" "$IMAGE_TAR"
    ;;
  logs)
    compose logs -f
    ;;
  status)
    compose ps
    ;;
  urls)
    print_urls
    ;;
  ""|-h|--help|help)
    usage
    ;;
  *)
    usage >&2
    exit 2
    ;;
esac
