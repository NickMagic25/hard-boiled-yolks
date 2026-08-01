# steamcmd-chart

Generic native-Linux SteamCMD game-server chart using the Hard Boiled Yolks image and controller.

The chart installs or updates `steamcmd.appId` in an init container, optionally runs a post-install bootstrap, and then starts `steamcmd.startup` under `hby-control`. Egg variables belong in `hby.container.env`; sensitive values should use `envFrom` and an existing Secret or a rendered ExternalSecret.

Pterodactyl `config.files` parsers and arbitrary egg installer scripts are not interpreted. Reproduce exceptional post-install work with `steamcmd.bootstrap` and pin downloaded artifacts and checksums.

```sh
helm dependency build charts/steamcmd-chart
helm upgrade --install palworld charts/steamcmd-chart -f charts/steamcmd-chart/examples/palworld.values.yaml
```
