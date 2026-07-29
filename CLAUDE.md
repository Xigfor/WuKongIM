# Xigfor WuKongIM Development Notes

## Release Source Of Truth

Xigfor production releases use
`.github/workflows/release-xigfor-wukongim.yml`.

The workflow is responsible for image construction, transfer, deployment,
health verification, and rollback. The production server is a runtime target
only and must not build from a Git checkout.

Before the first guarded production smoke, both deployment variables remain
disabled. The rollout phases, required configuration, and recovery evidence are
defined in `.github/SETUP.md`.

Do not treat upstream `make deploy*` targets or `.github/workflows/docker.yml`
as Xigfor production release entry points.
