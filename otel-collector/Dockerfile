# The upstream collector binary predates Go's eMudhra fix. Supply the verified
# CA bundle independently until upstream ships a patched Go build.
# See docs/runbooks/emudhra-ca-update.md.
FROM gcr.io/distroless/static-debian13:nonroot@sha256:516757106de35205efb70b91ba03f6f1298424a0678c603255cf2852a7000087 AS certificates

FROM otel/opentelemetry-collector-contrib:0.163.1-386@sha256:cc79ec8a69421354e7dbcc524d34cbe9a88bb08ed601f5b7b2a55387392f1aba

COPY --from=certificates /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt

COPY config.yaml /etc/otelcol-contrib/config.yaml
