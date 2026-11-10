# The upstream collector binary predates Go's eMudhra fix. Supply the verified
# CA bundle independently until upstream ships a patched Go build.
# See docs/runbooks/emudhra-ca-update.md.
FROM gcr.io/distroless/static-debian13:nonroot@sha256:516757106de35205efb70b91ba03f6f1298424a0678c603255cf2852a7000087 AS certificates

FROM otel/opentelemetry-collector-contrib:0.163.1@sha256:1175b03003053962012f5a231f5567f66a3e8fdb960c7835c0e48f7aa1555806

COPY --from=certificates /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt

COPY config.yaml /etc/otelcol-contrib/config.yaml
