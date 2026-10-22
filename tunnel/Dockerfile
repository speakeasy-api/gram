FROM --platform=$BUILDPLATFORM golang:1.27.3-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
COPY tunnel ./tunnel
ARG TARGETOS TARGETARCH VERSION=dev
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags="-s -w" -o /out/tunnel-agent ./tunnel/cmd/tunnel-agent

FROM alpine:3.22
RUN apk add --no-cache ca-certificates \
    && sed -i \
      -e 's|^mozilla/emSign_ECC_Root_CA_-_G3.crt|!&|' \
      -e 's|^mozilla/Comodo_AAA_Services_root.crt|!&|' \
      /etc/ca-certificates.conf \
    && update-ca-certificates
ARG VERSION=dev
ENV TUNNEL_SERVICE_VERSION=$VERSION
COPY --from=build /out/tunnel-agent /usr/local/bin/tunnel-agent
USER 65532:65532
ENTRYPOINT ["/usr/local/bin/tunnel-agent"]
