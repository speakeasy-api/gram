# The upstream collector binary predates Go's eMudhra fix. Supply the verified
# CA bundle independently until upstream ships a patched Go build.
# See docs/runbooks/emudhra-ca-update.md.
FROM gcr.io/distroless/static-debian13:nonroot@sha256:516757106de35205efb70b91ba03f6f1298424a0678c603255cf2852a7000087 AS certificates

FROM otel/opentelemetry-collector-contrib:0.165.0@sha256:e40d61f10c9a617cca1ffd1be4d7e2b3a4b0f0001798ff07e58cabe82e618cea

COPY --from=certificates /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt

COPY config.yaml /etc/otelcol-contrib/config.yaml
