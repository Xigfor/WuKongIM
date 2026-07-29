GIT_COMMIT ?= $(shell git rev-parse --verify HEAD 2>/dev/null || echo unknown)
GIT_COMMIT_DATE ?= $(shell git show -s --format=%ct HEAD 2>/dev/null || date +%s)
GIT_VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
GIT_TREE_STATE ?= $(shell test -z "$$(git status --porcelain 2>/dev/null)" && echo clean || echo dirty)
DOCKER_BUILD_ARGS = \
	--build-arg GIT_COMMIT=$(GIT_COMMIT) \
	--build-arg GIT_COMMIT_DATE=$(GIT_COMMIT_DATE) \
	--build-arg GIT_VERSION=$(GIT_VERSION) \
	--build-arg GIT_TREE_STATE=$(GIT_TREE_STATE)

build:
	DOCKER_BUILDKIT=1 docker build $(DOCKER_BUILD_ARGS) -t wukongim .
push:
	docker tag wukongim wukongim/wukongim:latest-dev
	docker push wukongim/wukongim:latest-dev
deploy:
	DOCKER_BUILDKIT=1 docker build $(DOCKER_BUILD_ARGS) -t wukongim .
	docker tag wukongim wukongim/wukongim:latest
	docker push wukongim/wukongim:latest	
deploy-dev:
	DOCKER_BUILDKIT=1 docker build $(DOCKER_BUILD_ARGS) -t wukongim .
	docker tag wukongim wukongim/wukongim:latest-dev
	docker push wukongim/wukongim:latest-dev
deploy-arm:
	DOCKER_BUILDKIT=1 docker build $(DOCKER_BUILD_ARGS) -t wukongimarm64 . -f Dockerfile.arm64 --platform linux/arm64
	docker tag wukongimarm64 wukongim/wukongim:latest-arm64
	docker push wukongim/wukongim:latest-arm64
deploy-v2-dev:
	DOCKER_BUILDKIT=1 docker build $(DOCKER_BUILD_ARGS) -t wukongim . --platform linux/amd64
	docker tag wukongim registry.cn-shanghai.aliyuncs.com/wukongim/wukongim:v2.2.3-dev
	docker push registry.cn-shanghai.aliyuncs.com/wukongim/wukongim:v2.2.3-dev
deploy-v2:
	docker buildx build $(DOCKER_BUILD_ARGS) -t wukongim . --platform linux/amd64,linux/arm64
	docker tag wukongim registry.cn-shanghai.aliyuncs.com/wukongim/wukongim:v2.2.1-20250624
	docker tag wukongim wukongim/wukongim:v2.2.1-20250624
	docker tag wukongim ghcr.io/wukongim/wukongim:v2.2.1-20250624
	docker tag wukongim ghcr.io/wukongim/wukongim:v2
	docker push registry.cn-shanghai.aliyuncs.com/wukongim/wukongim:v2.2.1-20250624
	docker push wukongim/wukongim:v2.2.1-20250624
	docker push ghcr.io/wukongim/wukongim:v2
deploy-latest-v2:
	DOCKER_BUILDKIT=1 docker build $(DOCKER_BUILD_ARGS) -t wukongim .
	docker tag wukongim registry.cn-shanghai.aliyuncs.com/wukongim/wukongim:v2
	docker tag wukongim wukongim/wukongim:v2
	docker push registry.cn-shanghai.aliyuncs.com/wukongim/wukongim:v2
	docker push wukongim/wukongim:v2		

# docker push registry.cn-shanghai.aliyuncs.com/wukongim/wukongim:v1.2
# docker push registry.cn-shanghai.aliyuncs.com/wukongim/wukongim:v1.2-dev
