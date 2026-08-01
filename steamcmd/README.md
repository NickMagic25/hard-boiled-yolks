# SteamCMD Image

Container image for SteamCMD built with [melange](https://github.com/chainguard-dev/melange) and [apko](https://github.com/chainguard-dev/apko) using [Wolfi](https://wolfi.dev) packages.

This image is `x86_64` only.

## Entrypoint modes

The entrypoint accepts `all` (default), `install`, or `run`, also selectable with `HBY_STEAMCMD_MODE`.

- `all` preserves the update-then-run behavior for direct container users.
- `install` installs an empty data directory and checks for updates when `AUTO_UPDATE=1`, then exits. It writes `.hby_steamcmd_installed` only after SteamCMD succeeds.
- `run` skips SteamCMD and starts `STARTUP` under `hby-control`.

The Helm charts use separate `install` and `run` containers so failed installs block pod initialization and Steam credentials do not need to reach the game container.

`charts/steamcmd-chart` installs native Linux servers. `charts/steamcmd-proton-chart` forces Windows installation and runs Windows servers with the experimental `steamcmd_proton` image.

`HBY_SERVER_DIR` overrides `/home/container` for tests and specialized deployments.

## Prerequisites

- [melange](https://github.com/chainguard-dev/melange)
- [apko](https://github.com/chainguard-dev/apko)

## Build

Run all commands from `/Users/nmajkic/git/hard-boiled-yolks/steamcmd`.

### 1. Generate signing keys (one-time)

```sh
melange keygen
```

### 2. Build local APK packages

```sh
melange build ../control/melange.yaml --source-dir .. --signing-key melange.rsa --out-dir ./packages
melange build runtime/melange.yaml --source-dir . --signing-key melange.rsa
melange build entrypoint/melange.yaml --source-dir . --signing-key melange.rsa
```

### 3. Build the image

```sh
apko build apko.yaml hard-boiled-yolks:steamcmd steamcmd.tar \
  --keyring-append melange.rsa.pub \
  --repository-append ./packages
```

### 4. Load into Docker

```sh
docker load < steamcmd.tar
```
