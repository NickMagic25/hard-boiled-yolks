# Hard Boiled Yolks

A curated collection of minimal, secure container images built with [apko](https://github.com/chainguard-dev/apko) and [Wolfi](https://wolfi.dev) for use with [Kubeegg](https://github.com/NickMagic25/kubeegg) — running [Pterodactyl](https://pterodactyl.io) and [Pelican](https://pelican.dev) eggs on Kubernetes.

Images are built declaratively using apko YAML configs and produce reproducible, distroless OCI images with auto-generated SBOMs. Custom scripts are packaged as APKs using [melange](https://github.com/chainguard-dev/melange).

All images support `linux/amd64` and `linux/arm64` unless otherwise noted.

## Repository Structure

* `oses/` — base OS images (Wolfi, Alpine, Debian)
* `java/` — Java runtime images (OpenJDK 8, 11, 17, 18, 19, 21, 25)
* `games/` — game-specific images
* `installers/` — images used by egg install scripts
* `nodejs/` — Node.js runtime images
* `python/` — Python runtime images
* `go/` — Go runtime images

## Building

### Prerequisites

- [apko](https://github.com/chainguard-dev/apko) (or use the `cgr.dev/chainguard/apko` container image)
- [melange](https://github.com/chainguard-dev/melange) (for packaging entrypoint scripts)

### Build entrypoint package (melange)

```bash
cd java
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

## Available Images

### Java

* [`java8`](java/8) — `hard-boiled-yolks:java_8`
* [`java11`](java/11) — `hard-boiled-yolks:java_11`
* [`java17`](java/17) — `hard-boiled-yolks:java_17`
* [`java18`](java/18) — `hard-boiled-yolks:java_18`
* [`java19`](java/19) — `hard-boiled-yolks:java_19`
* [`java21`](java/21) — `hard-boiled-yolks:java_21`
* [`java25`](java/25) — `hard-boiled-yolks:java_25`

## Contributing

Each image is defined by an `apko.yaml` in its version folder (e.g. `java/21/apko.yaml`). To add a new version, create a new folder with its apko config and update the corresponding GitHub Actions workflow.

Custom entrypoint scripts should be packaged as APKs via melange. See [`java/entrypoint/README.md`](java/entrypoint/README.md) for details.
