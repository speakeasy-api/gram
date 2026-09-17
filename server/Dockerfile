FROM gcr.io/distroless/static-debian13:nonroot@sha256:f7f8f729987ad0fdf6b05eeeae94b26e6a0f613bdf46feea7fc40f7bd72953e6

WORKDIR /app

# Numeric IDs let kubelet verify runAsNonRoot without resolving the distroless user name.
USER 65532:65532

COPY --chown=nonroot:nonroot --chmod=0755 ./bin/gram /app/gram
COPY --chown=nonroot:nonroot --chmod=0755 ./bin/tunnel-gateway /app/tunnel-gateway
COPY ./migrations ./migrations

ENTRYPOINT ["/app/gram"]
