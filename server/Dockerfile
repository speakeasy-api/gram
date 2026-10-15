FROM gcr.io/distroless/static-debian13:nonroot@sha256:e2e927ec666bae08560abb3c55d0659eceabb657f56b6782ab500a9fc7f555e3

WORKDIR /app

# Numeric IDs let kubelet verify runAsNonRoot without resolving the distroless user name.
USER 65532:65532

COPY --chown=nonroot:nonroot --chmod=0755 ./bin/gram /app/gram
COPY --chown=nonroot:nonroot --chmod=0755 ./bin/tunnel-gateway /app/tunnel-gateway
COPY ./migrations ./migrations

ENTRYPOINT ["/app/gram"]
