# SteamCMD Runtime Package

Melange package that installs:

- SteamCMD launcher and binaries from Valve (`steamcmd_linux.tar.gz`)
- Required i386 runtime libraries extracted from Debian packages

## Build

From `/Users/nmajkic/git/hard-boiled-yolks/steamcmd`:

```sh
melange build runtime/melange.yaml \
  --source-dir . \
  --signing-key melange.rsa
```

The package is referenced in `apko.yaml` as:

```yaml
contents:
  packages:
    - steamcmd-hard-boiled-yolks-runtime@local
```
