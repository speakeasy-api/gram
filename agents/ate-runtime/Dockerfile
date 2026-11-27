FROM --platform=$BUILDPLATFORM golang:1.27.4-alpine@sha256:0a5f7fe796599ff487605987432e7619ae45beeb2fba4c2ce0b907879932aebf AS build
ARG TARGETARCH
WORKDIR /src
COPY server/cmd/kit-ate-init/main.go ./main.go
RUN CGO_ENABLED=0 GOOS=linux GOARCH="${TARGETARCH:-$(go env GOARCH)}" \
    go build -trimpath -ldflags="-s -w" -o /kit-ate-init ./main.go

FROM ghcr.io/speakeasy-api/kit:v0.3.1@sha256:81b17712102093a3b3019f5012df6de132de0d07e90ec4a61fea8cb13b155539
USER 0:0
COPY --from=build --chmod=0755 /kit-ate-init /usr/local/bin/kit-ate-init
USER 1000:1000
ENV HOME=/home/kit
ENTRYPOINT ["/usr/local/bin/kit-ate-init", "--root=/home/kit", "--probe-addr=127.0.0.1:8081", "--"]
CMD ["/usr/local/bin/kit", "serve", "--root", "/home/kit/workspace", "--remote-acp", "--no-a2a", "--no-stdio", "--a2a", "0.0.0.0:8081", "--server-credential-file", "/home/kit/.kit/credentials/gateway-token"]
