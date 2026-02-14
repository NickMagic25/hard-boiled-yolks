#!/bin/sh

set -eu

DESTDIR="${1:?destination directory is required}"

WORKDIR="$(mktemp -d)"
trap 'rm -rf "${WORKDIR}"' EXIT

DEBIAN_MIRROR="${DEBIAN_MIRROR:-https://deb.debian.org/debian}"
STEAMCMD_URL="${STEAMCMD_URL:-https://media.steampowered.com/installer/steamcmd_linux.tar.gz}"
RCON_VERSION="${RCON_VERSION:-0.10.3}"
RCON_URL="https://github.com/gorcon/rcon-cli/releases/download/v${RCON_VERSION}/rcon-${RCON_VERSION}-amd64_linux.tar.gz"

mkdir -p "${WORKDIR}/index" "${WORKDIR}/debs" "${WORKDIR}/steamcmd" "${WORKDIR}/rootfs" "${WORKDIR}/rcon"

curl -fsSL "${DEBIAN_MIRROR}/dists/bookworm/main/binary-i386/Packages.gz" -o "${WORKDIR}/index/bookworm.gz"
if ! curl -fsSL "${DEBIAN_MIRROR}/dists/bookworm-updates/main/binary-i386/Packages.gz" -o "${WORKDIR}/index/bookworm-updates.gz" 2>/dev/null; then
  echo "warning: unable to fetch bookworm-updates i386 index, continuing with bookworm main" >&2
fi

gzip -dc "${WORKDIR}/index/bookworm.gz" > "${WORKDIR}/index/all-packages"
if [ -s "${WORKDIR}/index/bookworm-updates.gz" ]; then
  printf "\n" >> "${WORKDIR}/index/all-packages"
  gzip -dc "${WORKDIR}/index/bookworm-updates.gz" >> "${WORKDIR}/index/all-packages"
fi

resolve_filename() {
  pkg="${1:?package name required}"
  awk -v pkg="${pkg}" '
    BEGIN { RS=""; FS="\n" }
    $1 == "Package: " pkg {
      for (i = 1; i <= NF; i++) {
        if ($i ~ /^Filename: /) {
          sub(/^Filename: /, "", $i)
          print $i
          exit
        }
      }
    }
  ' "${WORKDIR}/index/all-packages"
}

download_deb() {
  pkg="${1:?package name required}"
  filename="$(resolve_filename "${pkg}")"
  if [ -z "${filename}" ]; then
    echo "unable to resolve Debian package: ${pkg}" >&2
    exit 1
  fi

  curl -fsSL "${DEBIAN_MIRROR}/${filename}" -o "${WORKDIR}/debs/${pkg}.deb"
}

# 32-bit runtime requirements for linux32/steamcmd.
for pkg in libc6 libgcc-s1 libstdc++6 zlib1g; do
  download_deb "${pkg}"
done

for deb in "${WORKDIR}"/debs/*.deb; do
  dpkg-deb -x "${deb}" "${WORKDIR}/rootfs"
done

curl -fsSL "${STEAMCMD_URL}" -o "${WORKDIR}/steamcmd/steamcmd_linux.tar.gz"
tar -xzf "${WORKDIR}/steamcmd/steamcmd_linux.tar.gz" -C "${WORKDIR}/steamcmd"
curl -fsSL "${RCON_URL}" -o "${WORKDIR}/rcon/rcon.tar.gz"
tar -xzf "${WORKDIR}/rcon/rcon.tar.gz" -C "${WORKDIR}/rcon"

mkdir -p \
  "${DESTDIR}/usr/bin" \
  "${DESTDIR}/usr/lib/games/steam/linux32" \
  "${DESTDIR}/usr/lib/i386-linux-gnu"

cp "${WORKDIR}/steamcmd/steamcmd.sh" "${DESTDIR}/usr/lib/games/steam/steamcmd.sh"
cp "${WORKDIR}/steamcmd/steam.sh" "${DESTDIR}/usr/lib/games/steam/steam.sh"
cp "${WORKDIR}/steamcmd/linux32/steamcmd" "${DESTDIR}/usr/lib/games/steam/linux32/steamcmd"
cp "${WORKDIR}/steamcmd/linux32/libstdc++.so.6" "${DESTDIR}/usr/lib/games/steam/linux32/libstdc++.so.6"
chmod 0755 "${DESTDIR}/usr/lib/games/steam/steamcmd.sh"
chmod 0755 "${DESTDIR}/usr/lib/games/steam/steam.sh"
chmod 0755 "${DESTDIR}/usr/lib/games/steam/linux32/steamcmd"

cat > "${DESTDIR}/usr/bin/steamcmd" <<'EOF'
#!/bin/sh

STEAMROOT=/usr/lib/games/steam
PLATFORM=linux32

export LD_LIBRARY_PATH="${STEAMROOT}/${PLATFORM}:${LD_LIBRARY_PATH:-}"
ulimit -n 2048

exec /usr/lib/i386-linux-gnu/ld-linux.so.2 \
  --library-path "${STEAMROOT}/${PLATFORM}:/usr/lib/i386-linux-gnu" \
  "${STEAMROOT}/${PLATFORM}/steamcmd" "$@"
EOF
chmod 0755 "${DESTDIR}/usr/bin/steamcmd"

cp "${WORKDIR}/rcon/rcon-${RCON_VERSION}-amd64_linux/rcon" "${DESTDIR}/usr/bin/rcon"
chmod 0755 "${DESTDIR}/usr/bin/rcon"

if [ -d "${WORKDIR}/rootfs/lib/i386-linux-gnu" ]; then
  cp -a "${WORKDIR}/rootfs/lib/i386-linux-gnu/." "${DESTDIR}/usr/lib/i386-linux-gnu/"
fi

if [ -d "${WORKDIR}/rootfs/usr/lib/i386-linux-gnu" ]; then
  cp -a "${WORKDIR}/rootfs/usr/lib/i386-linux-gnu/." "${DESTDIR}/usr/lib/i386-linux-gnu/"
fi
