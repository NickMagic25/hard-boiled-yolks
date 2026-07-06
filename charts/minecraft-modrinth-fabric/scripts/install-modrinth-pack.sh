#!/bin/sh
set -eu

server_dir="${SERVER_DIR:-/home/container}"
state_file="${server_dir}/.modrinth_mrpack_state"

require_cmd() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "Error: required command '$1' is not available in the installer image." >&2
    exit 1
  fi
}

for cmd in awk basename chmod cp curl dirname grep jq mkdir mktemp mv rm sha512sum tr unzip wc; do
  require_cmd "$cmd"
done

auth_enabled="${MODRINTH_AUTH_ENABLED:-true}"
auth_scheme="${MODRINTH_AUTH_SCHEME:-raw}"
api_base="${MODRINTH_API_BASE:-https://api.modrinth.com/v2}"
project_id="${MODRINTH_PROJECT_ID:-}"
version_id="${MODRINTH_VERSION_ID:-}"
version_number="${MODRINTH_VERSION_NUMBER:-}"
user_agent="${MODRINTH_USER_AGENT:-minecraft-modrinth-fabric-chart/0.1}"
install_policy="${MODRINTH_INSTALL_POLICY:-IfChanged}"
pack_url="${MODRINTH_PACK_URL:-}"
pack_filename="${MODRINTH_PACK_FILENAME:-modrinth.mrpack}"
pack_sha512="${MODRINTH_PACK_SHA512:-}"
fabric_installer_version="${FABRIC_INSTALLER_VERSION:-1.0.3}"
server_jarfile="${SERVER_JARFILE:-server.jar}"
minecraft_eula="${MINECRAFT_EULA:-false}"

case "$install_policy" in
  IfChanged | Always)
    ;;
  *)
    echo "Error: MODRINTH_INSTALL_POLICY must be 'IfChanged' or 'Always'." >&2
    exit 1
    ;;
esac

if [ "$auth_enabled" = "true" ] && [ -z "${MODRINTH_TOKEN:-}" ]; then
  echo "Error: MODRINTH_TOKEN is required when MODRINTH_AUTH_ENABLED=true." >&2
  exit 1
fi

if [ "$minecraft_eula" = "true" ]; then
  mkdir -p "$server_dir"
  printf 'eula=true\n' > "${server_dir}/eula.txt"
fi

if [ "$install_policy" = "IfChanged" ] \
  && [ -n "$pack_sha512" ] \
  && [ -f "$state_file" ] \
  && [ -f "${server_dir}/${server_jarfile}" ] \
  && grep -q "^pack_sha512=${pack_sha512}$" "$state_file"; then
  echo "Modrinth pack ${pack_sha512} is already installed."
  exit 0
fi

tmpdir="$(mktemp -d "${TMPDIR:-/tmp}/modrinth-pack.XXXXXX")"
trap 'rm -rf "$tmpdir"' EXIT

auth_header() {
  case "$auth_scheme" in
    raw)
      printf 'Authorization: %s' "$MODRINTH_TOKEN"
      ;;
    bearer | Bearer)
      printf 'Authorization: Bearer %s' "$MODRINTH_TOKEN"
      ;;
    *)
      echo "Error: MODRINTH_AUTH_SCHEME must be 'raw' or 'bearer'." >&2
      exit 1
      ;;
  esac
}

is_modrinth_url() {
  case "$1" in
    "$api_base"/* | https://api.modrinth.com/* | https://cdn.modrinth.com/*)
      return 0
      ;;
    *)
      return 1
      ;;
  esac
}

download_url() {
  url="$1"
  output_file="$2"

  if [ "$auth_enabled" = "true" ] && is_modrinth_url "$url"; then
    curl -fsSL --retry 3 --retry-delay 2 \
      -H "$(auth_header)" \
      -H "User-Agent: ${user_agent}" \
      -o "$output_file" \
      "$url"
  else
    curl -fsSL --retry 3 --retry-delay 2 \
      -H "User-Agent: ${user_agent}" \
      -o "$output_file" \
      "$url"
  fi
}

download_json() {
  download_url "$1" "$2"
  jq -e . "$2" >/dev/null
}

sha512_actual() {
  sha512sum "$1" | awk '{print $1}'
}

sha512_matches() {
  file="$1"
  expected="$2"
  actual="$(sha512_actual "$file")"
  [ "$actual" = "$expected" ]
}

verify_sha512() {
  file="$1"
  expected="$2"
  if ! sha512_matches "$file" "$expected"; then
    echo "Error: SHA512 mismatch for $file" >&2
    echo "Expected: $expected" >&2
    echo "Actual:   $(sha512_actual "$file")" >&2
    exit 1
  fi
}

validate_pack_path() {
  case "$1" in
    "" | /* | ./* | ../* | *"/../"* | *"/.." | *"/./"* | *"/.")
      echo "Error: unsafe path in Modrinth pack index: $1" >&2
      exit 1
      ;;
  esac
}

if [ -z "$pack_url" ] || [ -z "$pack_sha512" ]; then
  if [ -z "$project_id" ]; then
    echo "Error: MODRINTH_PROJECT_ID is required when pinned pack.url and pack.sha512 are not set." >&2
    exit 1
  fi

  versions_json="${tmpdir}/versions.json"
  selected_version_json="${tmpdir}/selected-version.json"
  primary_file_json="${tmpdir}/primary-file.json"

  echo "Fetching Modrinth version metadata for project ${project_id}."
  download_json "${api_base}/project/${project_id}/version?include_changelog=false" "$versions_json"

  if [ -n "$version_id" ]; then
    jq -er --arg id "$version_id" '.[] | select(.id == $id)' "$versions_json" > "$selected_version_json"
  elif [ -n "$version_number" ]; then
    jq -er --arg version "$version_number" '.[] | select(.version_number == $version)' "$versions_json" > "$selected_version_json"
  else
    jq -er '.[0]' "$versions_json" > "$selected_version_json"
    version_id="$(jq -r '.id' "$selected_version_json")"
  fi

  if ! jq -er '.files[] | select(.primary == true)' "$selected_version_json" > "$primary_file_json"; then
    jq -er '.files[0]' "$selected_version_json" > "$primary_file_json"
  fi

  pack_url="$(jq -r '.url' "$primary_file_json")"
  pack_sha512="$(jq -r '.hashes.sha512 // empty' "$primary_file_json")"
  pack_filename="$(jq -r '.filename // "modrinth.mrpack"' "$primary_file_json")"
fi

if [ -z "$pack_url" ] || [ -z "$pack_sha512" ]; then
  echo "Error: missing Modrinth pack URL or SHA512 hash." >&2
  exit 1
fi

pack_file="${tmpdir}/$(basename "$pack_filename")"
index_file="${tmpdir}/modrinth.index.json"

echo "Downloading Modrinth pack ${pack_filename}."
download_url "$pack_url" "$pack_file"
verify_sha512 "$pack_file" "$pack_sha512"

unzip -p "$pack_file" modrinth.index.json > "$index_file"
jq -e . "$index_file" >/dev/null

minecraft_version="$(jq -r '.dependencies.minecraft // empty' "$index_file")"
fabric_loader_version="$(jq -r '.dependencies["fabric-loader"] // empty' "$index_file")"

if [ -z "$minecraft_version" ] || [ -z "$fabric_loader_version" ]; then
  echo "Error: modrinth.index.json must include minecraft and fabric-loader dependencies." >&2
  exit 1
fi

mkdir -p "$server_dir"
jq -cr '.files[] | select((.env.server // "required") != "unsupported")' "$index_file" > "${tmpdir}/server-files.jsonl"
file_count="$(wc -l < "${tmpdir}/server-files.jsonl" | awk '{print $1}')"
echo "Installing ${file_count} server-supported files from the Modrinth pack."

while IFS= read -r file_json; do
  pack_path="$(printf '%s' "$file_json" | jq -r '.path')"
  file_sha512="$(printf '%s' "$file_json" | jq -r '.hashes.sha512 // empty')"
  target_file="${server_dir}/${pack_path}"
  target_dir="$(dirname "$target_file")"
  downloaded_file="${tmpdir}/downloaded-file"
  downloads_file="${tmpdir}/downloads"

  validate_pack_path "$pack_path"

  if [ -z "$file_sha512" ]; then
    echo "Error: $pack_path is missing a SHA512 hash." >&2
    exit 1
  fi

  if [ -f "$target_file" ] && sha512_matches "$target_file" "$file_sha512"; then
    echo "Already installed: $pack_path"
    continue
  fi

  printf '%s' "$file_json" | jq -r '.downloads[]?' > "$downloads_file"
  if [ ! -s "$downloads_file" ]; then
    echo "Error: $pack_path does not include any download URLs." >&2
    exit 1
  fi

  downloaded=false
  while IFS= read -r file_url; do
    echo "Downloading $pack_path"
    rm -f "$downloaded_file"
    if download_url "$file_url" "$downloaded_file" && sha512_matches "$downloaded_file" "$file_sha512"; then
      downloaded=true
      break
    fi
  done < "$downloads_file"

  if [ "$downloaded" != true ]; then
    echo "Error: all downloads failed or failed SHA512 verification for $pack_path." >&2
    exit 1
  fi

  mkdir -p "$target_dir"
  mv "$downloaded_file" "$target_file"
done < "${tmpdir}/server-files.jsonl"

overrides_dir="${tmpdir}/overrides"
mkdir -p "$overrides_dir"
if unzip -oq "$pack_file" "overrides/*" -d "$overrides_dir"; then
  if [ -d "${overrides_dir}/overrides" ]; then
    echo "Copying Modrinth overrides into ${server_dir}."
    # Packs can contain restrictive override modes; normalize before copying as UID 1000.
    chmod -R u+rwX "${overrides_dir}/overrides"
    # Do not preserve archive ownership, timestamps, or permissions on the PVC.
    cp -R "${overrides_dir}/overrides/." "$server_dir/"
  fi
else
  echo "No Modrinth overrides directory found."
fi

fabric_server_url="https://meta.fabricmc.net/v2/versions/loader/${minecraft_version}/${fabric_loader_version}/${fabric_installer_version}/server/jar"
echo "Downloading Fabric server jar for Minecraft ${minecraft_version}, loader ${fabric_loader_version}, installer ${fabric_installer_version}."
download_url "$fabric_server_url" "${server_dir}/${server_jarfile}"

manifest_file="${tmpdir}/fabric-server-manifest"
if ! unzip -p "${server_dir}/${server_jarfile}" META-INF/MANIFEST.MF > "$manifest_file"; then
  echo "Error: downloaded Fabric server jar does not contain a manifest." >&2
  exit 1
fi

if ! tr -d '\r' < "$manifest_file" | grep -q '^Main-Class: net.fabricmc.installer.ServerLauncher$'; then
  echo "Error: downloaded Fabric server jar does not advertise the expected ServerLauncher main class." >&2
  exit 1
fi

{
  echo "project_id=${project_id}"
  echo "version_id=${version_id}"
  echo "version_number=${version_number}"
  echo "pack_sha512=${pack_sha512}"
  echo "minecraft_version=${minecraft_version}"
  echo "fabric_loader_version=${fabric_loader_version}"
  echo "fabric_installer_version=${fabric_installer_version}"
} > "$state_file"

echo "Modrinth Fabric server install complete."
