FROM --platform=$BUILDPLATFORM golang:1.27.4-alpine@sha256:0a5f7fe796599ff487605987432e7619ae45beeb2fba4c2ce0b907879932aebf AS build
ARG TARGETARCH
WORKDIR /src
COPY server/cmd/kit-ate-init/main.go ./main.go
RUN CGO_ENABLED=0 GOOS=linux GOARCH="${TARGETARCH:-$(go env GOARCH)}" \
    go build -trimpath -ldflags="-s -w" -o /kit-ate-init ./main.go

FROM ghcr.io/speakeasy-api/kit@sha256:affcda1d5296d5d4fc17edd83fb6938363dc4f29367265fb66185ae0d56b8f13
USER 0:0
RUN mkdir -p /var/run/speakeasy/workload \
    && chown 1000:1000 /var/run/speakeasy/workload
COPY --from=build --chmod=0755 /kit-ate-init /usr/local/bin/kit-ate-init
USER 1000:1000
ENV HOME=/home/kit
ENTRYPOINT ["/usr/local/bin/kit-ate-init"]
