# Installer Images

Installer images contain the small toolsets used by chart init containers and egg install scripts.

## Modrinth Installer

`installers/wolfi/apko.yaml` builds a Wolfi image with the tools required by the Modrinth `.mrpack` installer script: `curl`, `jq`, `unzip`, BusyBox utilities, and CA certificates.

Build from the repository root:

```sh
apko build installers/wolfi/apko.yaml hard-boiled-yolks:installer_modrinth installer-modrinth.tar
```

Load locally:

```sh
docker load < installer-modrinth.tar
```
