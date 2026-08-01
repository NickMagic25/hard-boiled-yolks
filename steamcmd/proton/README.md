# Wolfi Proton experiment

This experiment pins GE-Proton 11-1 and packages it for an x86_64 Wolfi SteamCMD image. It is not a released image until all of these gates pass:

1. Melange and apko builds succeed.
2. `steamcmd` and `proton --version` run as UID 1000.
3. Proton creates a writable App-ID prefix and executes a trivial command.
4. `hby-control` supervises the Proton process.

The `charts/steamcmd-proton-chart` source is available for integration testing. Do not publish its release package or image if a gate fails.

## Latest gate result

The pinned package and apko image build succeeded locally. The Melange recipe now assigns the GE-Proton tree and launcher to UID/GID 1000 and normalizes executable/read permissions. The Linux CI gate asserts that ownership before running Proton.

Melange's Docker runner on macOS rewrites ownership while retrieving its build workspace (to the host UID, with owner-only modes), so a package produced by that runner is not a valid ownership test. The release gate remains the Linux build and runtime job; the Proton image and chart release package stay unpublished until that job passes every runtime check.
