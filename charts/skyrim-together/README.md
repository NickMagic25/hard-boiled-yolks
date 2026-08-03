# skyrim-together

Reusable Helm chart for a Skyrim Together Reborn dedicated server.

The default runtime image is `hard-boiled-yolks:skyrim-together`. It starts `hby-control` as the container entrypoint and supervises `/st-server/SkyrimTogetherServer`, so the web control UI can manage the server process and files under `/home/container`.

The chart defaults the game Service to UDP port `10578` and exposes the Hard Boiled Yolks control UI on TCP port `8080`. Readiness and liveness probes check the control UI instead of the UDP game socket.

## Controller

Default controller settings are configured through `skyrim.extraEnv`:

```yaml
skyrim:
  extraEnv:
    - name: HBY_CONTROL_ADDR
      value: 0.0.0.0:8080
    - name: HBY_CONTROL_ROOT
      value: /home/container
    - name: HBY_CONTROL_SECURE_COOKIES
      value: "true"
    - name: HBY_CONTROL_OIDC_SCOPES
      value: "openid profile email groups"
```

Authentication is disabled unless you set password or OIDC environment variables. Configure authentication before exposing the control Service outside the cluster. Use `externalSecrets` to inject sensitive values such as `HBY_CONTROL_OIDC_CLIENT_SECRET`, `HBY_CONTROL_USERNAME`, or `HBY_CONTROL_PASSWORD`.

## External Secrets

Rendered ExternalSecrets are automatically added to the Skyrim Together container as `envFrom.secretRef` entries. Each key in the ExternalSecret target Secret becomes an environment variable in the runtime image. The target Secret must be in the same namespace as the pod because Kubernetes does not allow cross-namespace Secret references from a container.

```yaml
externalSecrets:
  enabled: true
  metadata:
    name: skyrim-together-control
  spec:
    refreshInterval: 1h
    secretStoreRef:
      kind: ClusterSecretStore
      name: openbao
    target:
      name: skyrim-together-control
      creationPolicy: Owner
    data:
      - secretKey: HBY_CONTROL_OIDC_CLIENT_SECRET
        remoteRef:
          key: skyrim-together
          property: HBY_CONTROL_OIDC_CLIENT_SECRET
```

Use `externalSecrets.items` to render and inject more than one `ExternalSecret`.

## Gateway API

The chart can render Gateway API routes through `gatewayIngress.data`. UDPRoute and TCPRoute default to `gateway.networking.k8s.io/v1alpha2`; HTTPRoute defaults to `gateway.networking.k8s.io/v1`.

```yaml
gatewayIngress:
  enabled: true
  data:
    - kind: UDPRoute
      name: skyrim-together
      hostname: skyrim.example.com
      externalDNS: true
      port: 10578
      spec:
        parentRefs:
          - name: game-servers-gateway
            namespace: gateway-system
            sectionName: skyrim
        rules:
          - {}
```

Each route defaults `backendRefs` to this chart's Service.

## Example Values

```yaml
image:
  repository: hard-boiled-yolks
  tag: skyrim-together

skyrim:
  serverArgs: []

service:
  type: ClusterIP
  port: 10578
```

Install:

```sh
helm upgrade --install skyrim-together ./charts/skyrim-together \
  --namespace skyrim \
  --create-namespace \
  -f values.skyrim.yaml
```
