# Hard Boiled Yolks

A curated collection of minimal, secure container images built with [apko](https://github.com/chainguard-dev/apko) and [Wolfi](https://wolfi.dev) for use with [Kubeegg](https://github.com/NickMagic25/kubeegg) — running [Pterodactyl](https://pterodactyl.io) and [Pelican](https://pelican.dev) eggs on Kubernetes.

Images are built declaratively using apko YAML configs and produce reproducible, distroless OCI images with auto-generated SBOMs. Custom scripts are packaged as APKs using [melange](https://github.com/chainguard-dev/melange).

All images support `linux/amd64` and `linux/arm64` unless otherwise noted.

## Repository Structure

* `java/` — Java runtime images (OpenJDK 8, 11, 17, 18, 19, 21, 25)
* `steamcmd/` — SteamCMD image for game server hosting
* `games/` — game-specific images
* `control/` — shared web UI and process supervisor package
* `installers/` — images used by egg install scripts
* `nodejs/` — Node.js runtime images
* `python/` — Python runtime images
* `go/` — Go runtime images

## Building

### Prerequisites

- [apko](https://github.com/chainguard-dev/apko) (or use the `cgr.dev/chainguard/apko` container image)
- [melange](https://github.com/chainguard-dev/melange) (for packaging entrypoint scripts)

### Build local packages (melange)

```bash
cd java
melange build ../control/melange.yaml --source-dir .. --signing-key melange.rsa --out-dir ./packages
melange build entrypoint/melange.yaml --source-dir . --signing-key melange.rsa
```

### Build an image (apko)

```bash
cd java
apko build 21/apko.yaml hard-boiled-yolks:java_21 java-21.tar \
  --repository-append ./packages \
  --keyring-append melange.rsa.pub
```

Or using Docker:

```bash
docker run --rm -v "${PWD}/java":/work -w /work cgr.dev/chainguard/apko build \
  21/apko.yaml hard-boiled-yolks:java_21 java-21.tar \
  --repository-append ./packages \
  --keyring-append melange.rsa.pub
```

### Load and run

```bash
docker load < java/java-21.tar
docker run --rm hard-boiled-yolks:java_21
```

## Web Control UI

Images include `hby-control`, which exposes a web UI on `:8080` by default. It provides:

- browsing, creating, editing, uploading, downloading, and deleting files under `/home/container`
- a PTY-backed console connected to the supervised server process
- start, stop, and restart controls for the main server command
- optional HTTPS with `HBY_CONTROL_TLS_CERT_FILE` and `HBY_CONTROL_TLS_KEY_FILE`
- optional password login with `HBY_CONTROL_USERNAME` and `HBY_CONTROL_PASSWORD`
- optional OIDC login with `HBY_CONTROL_OIDC_ISSUER_URL`, `HBY_CONTROL_OIDC_CLIENT_ID`, and `HBY_CONTROL_OIDC_CLIENT_SECRET`

Authentication is disabled unless password or OIDC environment variables are set. See [`control/README.md`](control/README.md) for the complete environment reference.

## Available Images

### Java

* [`java8`](java/8) — `hard-boiled-yolks:java_8`
* [`java11`](java/11) — `hard-boiled-yolks:java_11`
* [`java17`](java/17) — `hard-boiled-yolks:java_17`
* [`java18`](java/18) — `hard-boiled-yolks:java_18`
* [`java19`](java/19) — `hard-boiled-yolks:java_19`
* [`java21`](java/21) — `hard-boiled-yolks:java_21`
* [`java25`](java/25) — `hard-boiled-yolks:java_25`

### Python

* [`python3.10`](python/3.10) — `hard-boiled-yolks:python_3.10`
* [`python3.11`](python/3.11) — `hard-boiled-yolks:python_3.11`
* [`python3.12`](python/3.12) — `hard-boiled-yolks:python_3.12`
* [`python3.13`](python/3.13) — `hard-boiled-yolks:python_3.13`

### SteamCMD

> `x86_64` only

* [`steamcmd`](steamcmd) — `hard-boiled-yolks:steamcmd`

SteamCMD image with Valve's Steam console client, rcon-cli, and 32-bit runtime libraries. Includes an entrypoint that handles auto-updating game servers on start via `SRCDS_APPID` and `STARTUP` environment variables. See [`steamcmd/README.md`](steamcmd/README.md) for build instructions and full environment variable reference.

### Installers

* [`installer_modrinth`](installers/modrinth) — `hard-boiled-yolks:installer_modrinth`

Wolfi-based installer image with `curl`, `jq`, `unzip`, CA certificates, and BusyBox utilities for installing Modrinth `.mrpack` files in init containers. See [`installers/README.md`](installers/README.md) for build instructions.

## Contributing

Each image is defined by an `apko.yaml` in its version folder (e.g. `java/21/apko.yaml`). To add a new version, create a new folder with its apko config and update the corresponding GitHub Actions workflow.

Custom entrypoint scripts should be packaged as APKs via melange. Shared runtime functionality belongs in [`control/`](control). See [`java/entrypoint/README.md`](java/entrypoint/README.md) for details.
