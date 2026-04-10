FROM gcr.io/distroless/static-debian13:nonroot@sha256:e3f945647ffb95b5839c07038d64f9811adf17308b9121d8a2b87b6a22a80a39

WORKDIR /app

USER nonroot

COPY --chown=nonroot:nonroot --chmod=0755 ./bin/gram /app/gram
# Add this line to copy the migrations folder
COPY ./migrations ./migrations

ENTRYPOINT ["/app/gram"]
