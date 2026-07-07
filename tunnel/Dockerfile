FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
COPY tunnel ./tunnel
ARG TARGETOS TARGETARCH VERSION=dev
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags="-s -w" -o /out/tunnel-agent ./tunnel/cmd/tunnel-agent

FROM alpine:3.22
ARG VERSION=dev
ENV TUNNEL_SERVICE_VERSION=$VERSION
COPY --from=build /out/tunnel-agent /usr/local/bin/tunnel-agent
USER 65532:65532
ENTRYPOINT ["/usr/local/bin/tunnel-agent"]
