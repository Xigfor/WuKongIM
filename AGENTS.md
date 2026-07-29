# Xigfor WuKongIM Agent Rules

## Production Release

The only supported Xigfor production release entry is:

- `.github/workflows/release-xigfor-wukongim.yml`

Before the guarded rollout phase is complete, production deploy variables remain
disabled and the workflow performs build validation only. After rollout, a push
to `origin/main` triggers the complete release.

Never build WuKongIM on the production host. Do not use `make deploy*`, a manual
`docker build`, or a direct compose image edit as an equivalent production
release path.

The workflow must retain:

- immutable `sha-*` image tags;
- persistent BuildKit caches;
- explicit production gates;
- host-local health verification;
- automatic rollback to the previous image.

The release contract and required GitHub configuration are documented in
`.github/SETUP.md`.
