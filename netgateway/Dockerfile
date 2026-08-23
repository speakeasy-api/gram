FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
COPY netgateway ./netgateway
ARG TARGETOS TARGETARCH VERSION=dev
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags="-s -w" -o /out/network-gateway ./netgateway/cmd/network-gateway

FROM alpine:3.22
ARG VERSION=dev
ENV NETWORK_GATEWAY_SERVICE_VERSION=$VERSION
COPY --from=build /out/network-gateway /usr/local/bin/network-gateway
USER 65532:65532
ENTRYPOINT ["/usr/local/bin/network-gateway"]
