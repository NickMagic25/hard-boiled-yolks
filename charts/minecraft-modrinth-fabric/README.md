# minecraft-modrinth-fabric

Reusable Helm chart for a Fabric Minecraft server whose init container installs a Modrinth `.mrpack`.

The installer:

- reads `MODRINTH_TOKEN` from a Kubernetes Secret when auth is enabled
- fetches Modrinth version JSON, or uses pinned `.mrpack` URL/SHA512 values
- verifies the `.mrpack` SHA512
- extracts `modrinth.index.json`
- downloads every file where `env.server != "unsupported"`
- verifies every downloaded file with SHA512
- writes files using their pack paths, such as `mods/...` and `resourcepacks/...`
- copies `overrides/` into the server root
- downloads the Fabric server jar using the Minecraft and loader versions from `modrinth.index.json`
- starts Minecraft with the installed Fabric server jar

The default runtime image is a Hard Boiled Yolks Java image. The chart sets `STARTUP` and lets the image entrypoint run so `hby-control` can supervise the server when the control package is installed in the image. Set `minecraft.command` and `minecraft.commandArgs` only when using an image that needs an explicit Kubernetes command override.

## Controller Port

Hard Boiled Yolks Java images include `hby-control`, which listens on port 8080 by default. Expose it with an additional container port and Service port:

```yaml
minecraft:
  extraPorts:
    - name: control
      containerPort: 8080
      protocol: TCP
  extraEnv:
    - name: HBY_CONTROL_ADDR
      value: 0.0.0.0:8080
    - name: HBY_CONTROL_ROOT
      value: /home/container

service:
  extraPorts:
    - name: control
      port: 8080
      targetPort: control
      protocol: TCP
```

## Secret

For a private Modrinth project, create a Secret before installing:

```sh
kubectl -n minecraft create secret generic minecraft-modrinth-token \
  --from-literal=MODRINTH_TOKEN="$MODRINTH_TOKEN"
```

Then set:

```yaml
modrinth:
  auth:
    existingSecret: minecraft-modrinth-token
    secretKey: MODRINTH_TOKEN
```

Or render an `ExternalSecret` from the chart:

```yaml
modrinth:
  auth:
    existingSecret: minecraft-modrinth-token

externalSecrets:
  enabled: true
  metadata:
    name: minecraft-modrinth-token
  spec:
    refreshInterval: 1h
    secretStoreRef:
      kind: ClusterSecretStore
      name: openbao
    target:
      name: minecraft-modrinth-token
      creationPolicy: Owner
    data:
      - secretKey: MODRINTH_TOKEN
        remoteRef:
          key: minecraft
          property: MODRINTH_TOKEN
```

Rendered ExternalSecrets are automatically added to the Minecraft container as `envFrom.secretRef` entries. Each key in the ExternalSecret target Secret becomes an environment variable in the runtime image. The target Secret must be in the same namespace as the Minecraft pod because Kubernetes does not allow cross-namespace Secret references from a container.

Use `externalSecrets.items` to render and inject more than one `ExternalSecret`.

## Gateway API

The chart can render Gateway API routes through `gatewayIngress.data`. TCPRoute defaults to `gateway.networking.k8s.io/v1alpha2`; HTTPRoute defaults to `gateway.networking.k8s.io/v1`.

```yaml
gatewayIngress:
  enabled: true
  data:
    - kind: TCPRoute
      name: minecraft
      hostname: minecraft.example.com
      externalDNS: true
      port: 25565
      spec:
        parentRefs:
          - name: game-servers-gateway
            namespace: gateway-system
            sectionName: minecraft
        rules:
          - {}
```

Each route defaults `backendRefs` to this chart's Minecraft Service.

HTTPRoute also supports Gateway API redirect rules without `backendRefs`:

```yaml
gatewayIngress:
  enabled: true
  data:
    - kind: HTTPRoute
      name: minecraft-redirect
      hostnames:
        - minecraft.example.com
      spec:
        parentRefs:
          - name: game-servers-gateway
            namespace: gateway-system
            sectionName: http
        rules:
          - matches:
              - path:
                  type: PathPrefix
                  value: /
            filters:
              - type: RequestRedirect
                requestRedirect:
                  scheme: https
                  statusCode: 301
```

## Example Values

```yaml
minecraft:
  eula: true
  javaOpts: "-Xms128M -Xmx8192M"

image:
  repository: hard-boiled-yolks
  tag: java_25

installerImage:
  repository: hard-boiled-yolks
  tag: installer_modrinth

modrinth:
  projectId: VJ4jg3DP
  versionId: K5CPsQs7
  versionNumber: 1.0.0
  fabricInstallerVersion: "1.0.3"
  pack:
    filename: Essential Sodium and More 1.0.0.mrpack
    url: https://cdn.modrinth.com/data/VJ4jg3DP/versions/K5CPsQs7/Essential%20Sodium%20and%20More%201.0.0.mrpack
    sha512: 44133dd49603de92ee40030644008de9c57945eef190df25e5af8c35066e9aafe6a8d2b55596f907ae1e025b1ac81ac8b7be4e299460483344c199332b7d40fe
  auth:
    existingSecret: minecraft-modrinth-token
```

Install:

```sh
helm upgrade --install minecraft ./charts/minecraft-modrinth-fabric \
  --namespace minecraft \
  --create-namespace \
  -f values.minecraft.yaml
```

If `modrinth.pack.url` and `modrinth.pack.sha512` are empty, the installer fetches project version JSON from `modrinth.apiBase` and selects `versionId`, `versionNumber`, or the first returned version.

The init container records the installed pack in `.modrinth_mrpack_state`. Set `modrinth.installPolicy=Always` to reinstall on every pod start.

## Installer Image

The default installer image is `hard-boiled-yolks:installer_modrinth`, built from the Wolfi/apko config at `installers/wolfi/apko.yaml`.
