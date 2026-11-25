# CA bundle excludes the eMudhra-compromised trust paths (see docs/runbooks/emudhra-ca-update.md).
FROM gcr.io/distroless/static-debian13:nonroot@sha256:516757106de35205efb70b91ba03f6f1298424a0678c603255cf2852a7000087

WORKDIR /app

# Numeric IDs let kubelet verify runAsNonRoot without resolving the distroless user name.
USER 65532:65532

COPY --chown=nonroot:nonroot --chmod=0755 ./bin/gram /app/gram
COPY --chown=nonroot:nonroot --chmod=0755 ./bin/tunnel-gateway /app/tunnel-gateway
COPY ./migrations ./migrations

ENTRYPOINT ["/app/gram"]
