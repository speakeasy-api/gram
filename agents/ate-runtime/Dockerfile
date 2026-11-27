FROM --platform=$BUILDPLATFORM golang:1.27.4-alpine@sha256:0a5f7fe796599ff487605987432e7619ae45beeb2fba4c2ce0b907879932aebf AS build
ARG TARGETARCH
WORKDIR /src
COPY server/cmd/kit-ate-init/main.go ./main.go
RUN CGO_ENABLED=0 GOOS=linux GOARCH="${TARGETARCH:-$(go env GOARCH)}" \
    go build -trimpath -ldflags="-s -w" -o /kit-ate-init ./main.go

FROM ghcr.io/speakeasy-api/kit@sha256:affcda1d5296d5d4fc17edd83fb6938363dc4f29367265fb66185ae0d56b8f13
USER 0:0
COPY --from=build --chmod=0755 /kit-ate-init /usr/local/bin/kit-ate-init
USER 1000:1000
ENV HOME=/home/kit \
    SSL_CERT_FILE=/run/ate/egress-trust-bundle/trust-bundle.pem \
    REQUESTS_CA_BUNDLE=/run/ate/egress-trust-bundle/trust-bundle.pem \
    CURL_CA_BUNDLE=/run/ate/egress-trust-bundle/trust-bundle.pem \
    GIT_SSL_CAINFO=/run/ate/egress-trust-bundle/trust-bundle.pem \
    PIP_CERT=/run/ate/egress-trust-bundle/trust-bundle.pem \
    NODE_EXTRA_CA_CERTS=/run/ate/egress-trust-bundle/trust-bundle.pem \
    DENO_CERT=/run/ate/egress-trust-bundle/trust-bundle.pem
ENTRYPOINT ["/usr/local/bin/kit-ate-init", "--root=/home/kit", "--probe-addr=127.0.0.1:8081", "--"]
CMD ["/usr/local/bin/kit", "serve", "--root", "/home/kit/workspace", "--remote-acp", "--no-a2a", "--no-stdio", "--a2a", "0.0.0.0:8081", "--server-credential-file", "/home/kit/.kit/credentials/gateway-token"]
