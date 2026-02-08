# Java Images

Container images for Java 8, 11, 17, 18, 19, 21, and 25 built with [melange](https://github.com/chainguard-dev/melange) and [apko](https://github.com/chainguard-dev/apko) using [Wolfi](https://wolfi.dev) packages.

## Prerequisites

- [melange](https://github.com/chainguard-dev/melange)
- [apko](https://github.com/chainguard-dev/apko)
- [Docker](https://www.docker.com/) (used as the melange build runner)

## Building

All commands should be run from the `java/` directory.

### 1. Generate signing keys (one-time)

```sh
melange keygen
```

This creates `melange.rsa` (private) and `melange.rsa.pub` (public) in the current directory.

### 2. Build the entrypoint package

```sh
melange build entrypoint/melange.yaml --source-dir . --signing-key melange.rsa
```

This produces APK packages under `packages/x86_64/` and `packages/aarch64/`.

### 3. Build a Java image

```sh
apko build <version>/apko.yaml java-<version>:latest java-<version>.tar --keyring-append melange.rsa.pub --repository-append ./packages
```

For example, to build the Java 21 image:

```sh
apko build 21/apko.yaml java-21:latest java-21.tar --keyring-append melange.rsa.pub --repository-append ./packages
```

### 4. Build all Java images

```sh
for v in 8 11 17 18 19 21 25; do
  apko build "$v/apko.yaml" "java-$v:latest" "java-$v.tar" --keyring-append melange.rsa.pub --repository-append ./packages
done
```

### 5. Load into Docker

```sh
docker load < java-21.tar
```

### 6. Scan with Trivy

```sh
trivy image java-21:latest-arm64
```

## Project Structure

```
java/
  entrypoint/
    melange.yaml    # melange package definition for entrypoint.sh
  entrypoint.sh     # shared entrypoint script
  8/apko.yaml       # Java 8 image config
  11/apko.yaml      # Java 11 image config
  17/apko.yaml      # Java 17 image config
  18/apko.yaml      # Java 18 image config
  19/apko.yaml      # Java 19 image config
  21/apko.yaml      # Java 21 image config
  25/apko.yaml      # Java 25 image config
  melange.rsa       # signing key (not committed)
  melange.rsa.pub   # public key (not committed)
  packages/         # melange build output (not committed)
```
