# steamcmd-proton-chart

Generic Windows SteamCMD game-server chart using GE-Proton, the Hard Boiled Yolks image, and `hby-control`. The chart always passes `+@sSteamCmdForcePlatformType windows` to SteamCMD and initializes the App-ID-specific Proton prefix before running `steamcmd.startup`.

The image and chart are x86_64-only. Schedule the workload onto an amd64 node and publish `hard-boiled-yolks:steamcmd_proton` only after the Proton image gate passes.

```sh
helm dependency build charts/steamcmd-proton-chart
helm upgrade --install enshrouded charts/steamcmd-proton-chart \
  -f charts/steamcmd-proton-chart/examples/enshrouded.values.yaml
```

`steamcmd.appId` and `steamcmd.startup` are required. Game variables belong in `hby.container.env`; credentials required only for installation belong in `steamcmd.credentials.envFrom`. Use `steamcmd.bootstrap` for controlled post-install configuration and explicitly inject only the Secrets that script needs.

The Enshrouded example expects an existing Secret named `enshrouded-secrets` with `ADMIN_PASSWORD`, `FRIEND_PASSWORD`, `GUEST_PASSWORD`, and `VISITOR_PASSWORD` keys. Its bootstrap updates the managed fields in `enshrouded_server.json` while retaining game-generated settings.
