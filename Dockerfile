# syntax=docker/dockerfile:1.7

FROM --platform=$BUILDPLATFORM node:20-bookworm-slim AS chatdemo-build
WORKDIR /src
RUN corepack enable
COPY demo/chatdemo/package.json demo/chatdemo/yarn.lock ./
RUN --mount=type=cache,id=yarn-chatdemo,target=/usr/local/share/.cache/yarn \
    yarn install --frozen-lockfile
COPY demo/chatdemo/ ./
RUN yarn build

FROM --platform=$BUILDPLATFORM node:20-bookworm-slim AS monitor-build
WORKDIR /src
RUN corepack enable
COPY web/package.json web/yarn.lock ./
RUN --mount=type=cache,id=yarn-monitor,target=/usr/local/share/.cache/yarn \
    yarn install --frozen-lockfile
COPY web/ ./
RUN yarn build

FROM --platform=$BUILDPLATFORM golang:1.23 AS build
ARG TARGETOS=linux
ARG TARGETARCH=amd64
ARG GIT_COMMIT=unknown
ARG GIT_COMMIT_DATE=unknown
ARG GIT_VERSION=dev
ARG GIT_TREE_STATE=clean

ENV CGO_ENABLED=0 \
    GO111MODULE=on

WORKDIR /go/release
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

COPY . .
RUN rm -rf web demo/chatdemo && \
    mkdir -p web/dist demo/chatdemo/dist
COPY --from=monitor-build /src/dist/ ./web/dist/
COPY --from=chatdemo-build /src/dist/ ./demo/chatdemo/dist/

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    GOOS="${TARGETOS}" GOARCH="${TARGETARCH}" go build \
      -trimpath \
      -ldflags="-s -w -X main.Commit=${GIT_COMMIT} -X main.CommitDate=${GIT_COMMIT_DATE} -X main.Version=${GIT_VERSION} -X main.TreeState=${GIT_TREE_STATE}" \
      -o app ./main.go

FROM alpine AS prod
COPY --from=build /etc/passwd /etc/passwd
COPY --from=build /usr/share/zoneinfo/Asia/Shanghai /etc/localtime
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
WORKDIR /home
COPY --from=build /go/release/app /home
COPY --from=build /go/release/config/wk.yaml /root/wukongim/wk.yaml
ENTRYPOINT ["/home/app","--config=/root/wukongim/wk.yaml","--ignoreMissingConfig=true"]
