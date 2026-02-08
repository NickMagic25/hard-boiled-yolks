# Python Entrypoint

Melange package that bundles `entrypoint.sh` into an APK for use in apko-built Python images.

## Prerequisites

- [melange](https://github.com/chainguard-dev/melange)
- A signing key pair (generate with `melange keygen`)

## Build

From the `python/` directory:

```bash
melange build entrypoint/melange.yaml \
  --source-dir . \
  --signing-key melange.rsa
```

This produces an APK under `packages/` that can be referenced in an apko image config:

```yaml
contents:
  packages:
    - python-hard-boiled-yolks-entrypoint@local
```

With the apko build flag `--repository-append ./packages --keyring-append melange.rsa.pub`.
