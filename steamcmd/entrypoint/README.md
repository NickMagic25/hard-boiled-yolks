# SteamCMD Entrypoint

Melange package that bundles `entrypoint.sh` into an APK for use in apko-built SteamCMD images.

## Build

From `/Users/nmajkic/git/hard-boiled-yolks/steamcmd`:

```sh
melange build entrypoint/melange.yaml \
  --source-dir . \
  --signing-key melange.rsa
```

This produces an APK under `packages/` referenced in `apko.yaml` as:

```yaml
contents:
  packages:
    - steamcmd-hard-boiled-yolks-entrypoint@local
```
