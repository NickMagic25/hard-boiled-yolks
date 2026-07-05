# Minecraft OIDC Compose Test

This is a local test harness for a Java image, `hby-control`, and Authelia OIDC.

It uses:

- `hard-boiled-yolks:java_25-minecraft-oidc`, built locally from `java/25/apko.yaml`
- `docker.io/authelia/authelia:4.39.19`
- `auth.localhost` for Authelia
- `hby.localhost` for the control UI callback

The local Java image, `hard-boiled-yolks-control` package, and Java entrypoint package all declare `x86_64` and `aarch64`. The script also checks the Authelia image manifest for `linux/amd64` and `linux/arm64` unless `HBY_SKIP_ARCH_CHECK=1` is set.

The script defaults to Java 25 because current Minecraft releases require it. Set `HBY_JAVA_VERSION=21` with an older `MC_VERSION` if you want to test the Java 21 image instead.

Run from the repository root:

```sh
./scripts/minecraft-oidc-test.sh deploy
```

Then open:

- Control UI: `http://hby.localhost:8080`
- Authelia: `https://auth.localhost:9091`
- Minecraft: `localhost:25565`

Authelia uses a generated local TLS certificate. Your browser may ask you to trust the certificate the first time you open `https://auth.localhost:9091`.

Default Authelia login:

- Username: `hby`
- Password: `hby-password`

Useful commands:

```sh
./scripts/minecraft-oidc-test.sh build
./scripts/minecraft-oidc-test.sh up
./scripts/minecraft-oidc-test.sh logs
./scripts/minecraft-oidc-test.sh down
./scripts/minecraft-oidc-test.sh teardown
```

`teardown` removes Compose containers, networks, and volumes. `clean` also removes generated local Authelia runtime files and image tarballs.
