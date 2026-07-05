# Hard Boiled Yolks Control

`hard-boiled-yolks-control` packages `hby-control`, a small web UI and process supervisor for game server images. On Linux it attaches the supervised process to a pseudo-terminal when `/dev/ptmx` is available, falling back to stdin/stdout/stderr pipes otherwise.

## Build

Run from the repository root:

```sh
melange build control/melange.yaml --source-dir . --signing-key melange.rsa
```

Then include the package in an apko image:

```yaml
contents:
  packages:
    - hard-boiled-yolks-control@local
```

## Web UI tests

Run the Playwright tests from the `control` directory:

```sh
npm install
npm run test:webui:install
npm run test:webui
```

The tests build `hby-control`, launch it against a temporary fake game server, and exercise login, file management, console I/O, and process controls.

## Runtime

Entrypoints should launch the server command through:

```sh
hby-control run -- /bin/sh -lc "$STARTUP_COMMAND"
```

The UI listens on `:8080` by default and manages `/home/container` by default.

## Environment

| Variable | Default | Description |
| --- | --- | --- |
| `HBY_CONTROL_ENABLED` | `true` | Set to `false` to bypass the supervisor and run the command directly. |
| `HBY_CONTROL_ADDR` | `:8080` | HTTP bind address. |
| `HBY_CONTROL_ROOT` | `/home/container` | File manager root and supervised process working directory. |
| `HBY_CONTROL_TLS_CERT_FILE` | empty | Enables HTTPS when set with `HBY_CONTROL_TLS_KEY_FILE`. |
| `HBY_CONTROL_TLS_KEY_FILE` | empty | TLS key path. |
| `HBY_CONTROL_USERNAME` | empty | Enables password login when set with `HBY_CONTROL_PASSWORD`. |
| `HBY_CONTROL_PASSWORD` | empty | Password for the configured username. |
| `HBY_CONTROL_SESSION_KEY` | random | Optional persistent session signing key. |
| `HBY_CONTROL_OIDC_ISSUER_URL` | empty | Enables OIDC when set with `HBY_CONTROL_OIDC_CLIENT_ID`. |
| `HBY_CONTROL_OIDC_CLIENT_ID` | empty | OIDC client ID. |
| `HBY_CONTROL_OIDC_CLIENT_SECRET` | empty | OIDC client secret. |
| `HBY_CONTROL_OIDC_REDIRECT_URL` | inferred | External callback URL, usually `https://host/auth/oidc/callback`. |
| `HBY_CONTROL_OIDC_SCOPES` | `openid profile email` | Space-separated OIDC scopes. |
| `HBY_CONTROL_OIDC_ALLOWED_EMAILS` | empty | Optional comma-separated allow list. |
| `HBY_CONTROL_OIDC_ALLOWED_DOMAINS` | empty | Optional comma-separated email domain allow list. |
| `HBY_CONTROL_OIDC_ALLOWED_GROUPS` | empty | Optional comma-separated OIDC group allow list. |
| `HBY_CONTROL_STOP_TIMEOUT` | `30s` | Grace period before killing a stopped server process. |
| `HBY_CONTROL_AUTO_START` | `true` | Set to `false` to boot the UI with the game server stopped. |
| `HBY_CONTROL_MAX_READ_BYTES` | `1048576` | Maximum file size returned to the editor. |
| `HBY_CONTROL_MAX_UPLOAD_BYTES` | `67108864` | Maximum upload size. |

`HBY_WEBUI_*` aliases are accepted for the bind address, root path, TLS paths, username, and password.
