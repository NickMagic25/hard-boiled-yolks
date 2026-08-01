# minecraft-modrinth-fabric

Fabric Minecraft server chart built on the shared Hard Boiled Yolks Helm library. Version 1.0 has no compatibility layer for the former 0.x values.

Set `minecraft.eula: true`, select a Modrinth project (or a pinned pack URL and SHA512), and configure shared workload behavior under `hby:`. When Modrinth authentication is enabled, `modrinth.auth.existingSecret` must name a Secret containing `modrinth.auth.secretKey`.

The installer verifies pack and file hashes, installs Fabric, downloads checksum-pinned extra JARs, and records install state on the data volume.
