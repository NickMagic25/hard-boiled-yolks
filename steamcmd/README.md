# SteamCMD Image

Container image for SteamCMD built with [melange](https://github.com/chainguard-dev/melange) and [apko](https://github.com/chainguard-dev/apko) using [Wolfi](https://wolfi.dev) packages.

This image is `x86_64` only.

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
