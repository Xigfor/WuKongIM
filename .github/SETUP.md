# Xigfor WuKongIM Release

## Source Of Truth

- Workflow: `.github/workflows/release-xigfor-wukongim.yml`
- Remote deploy executor: `scripts/release-wukongim.sh`
- Production branch: `main`
- Production compose: `/opt/mall/docker-compose-im-server2.yml`
- Production service/container: `wukongim` / `mall-wukongim`
- Health check: `http://127.0.0.1:5001/health`

The production host must never clone the repository or build the image. GitHub
Actions builds the image with persistent BuildKit caches, streams the compressed
image to the host, then runs the guarded deploy executor.

## Rollout Phases

The initial repository configuration must remain:

- `RELEASE_WUKONGIM_DEPLOY_ENABLED=false`
- `RELEASE_WUKONGIM_ALLOW_PRODUCTION=false`

In this state, pull requests and pushes to `main` validate the complete image
build without changing production.

After the workflow has succeeded and a reviewed manual production run is ready:

1. Set both variables to `true`.
2. Run `Release Xigfor WuKongIM` manually with `deploy=true` and
   `allow_production=true`.
3. Confirm the workflow health check and
   `/opt/mall/releases/wukongim/status.env`.
4. Keep both variables enabled only after the first production run succeeds.

Once enabled, the only normal production release action is:

```bash
git push origin main
```

The workflow builds, transfers, deploys, verifies, and rolls back on a failed
health check. Do not SSH to the production host to run `docker build`.

## Required GitHub Configuration

Variables:

- `RELEASE_AWS_HOST`
- `RELEASE_AWS_USER`
- `RELEASE_WUKONGIM_DEPLOY_ENABLED`
- `RELEASE_WUKONGIM_ALLOW_PRODUCTION`
- `RELEASE_WUKONGIM_COMPOSE_FILE`
- `RELEASE_WUKONGIM_SERVICE`
- `RELEASE_WUKONGIM_CONTAINER`
- `RELEASE_WUKONGIM_HEALTH_URL`
- `RELEASE_WUKONGIM_RELEASE_DIR`

Secret:

- `RELEASE_AWS_SSH_KEY`

## Rollback

The deploy executor records the previous image before switching. If compose
deployment or the health check fails, it recreates the service with that image
and writes the result to `/opt/mall/releases/wukongim/status.env`.
