# Modrinth Installer Image

Wolfi-based installer image for Modrinth `.mrpack` init containers.

It includes:

- `curl`
- `jq`
- `unzip`
- CA certificates
- BusyBox utilities such as `sha512sum`, `awk`, `grep`, and `mktemp`

Build from the repository root:

```sh
apko build installers/modrinth/apko.yaml hard-boiled-yolks:installer_modrinth installer-modrinth.tar
```

Load locally:

```sh
docker load < installer-modrinth.tar
```

