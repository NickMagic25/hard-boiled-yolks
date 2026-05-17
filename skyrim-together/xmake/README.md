# Xmake Package

Build this local package first to avoid bootstrapping xmake from source during
the Skyrim Together server melange build.

From `/Users/nmajkic/git/hard-boiled-yolks/skyrim-together`:

```sh
melange keygen
melange build xmake/melange.yaml --out-dir ./packages --signing-key ./melange.rsa
```

On macOS, prefer building the package through the Linux melange container. In
validation, the host-installed melange binary produced APKs with stripped
executable permissions for ELF files.

```sh
docker run --privileged --rm -v "$PWD":/work -w /work \
  cgr.dev/chainguard/melange \
  build xmake/melange.yaml --out-dir ./packages
```

Then build the server package using the local repository:

```sh
melange build server/melange.yaml \
  --out-dir ./packages \
  --repository-append ./packages \
  --keyring-append ./melange.rsa.pub
```

For local debugging without a signing key, you can swap the signing flags for
`--ignore-signatures` on the server build, and you can omit `--signing-key`
when building `xmake/melange.yaml`.

If you need the final server APK itself to preserve executable permissions on
macOS, run that melange build through the same Docker wrapper as well.
