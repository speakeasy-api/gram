FROM gcr.io/distroless/static-debian12:nonroot@sha256:e8a4044e0b4ae4257efa45fc026c0bc30ad320d43bd4c1a7d5271bd241e386d0

WORKDIR /app

USER nonroot

COPY --chown=nonroot:nonroot ./bin/gram ./
# Add this line to copy the migrations folder
COPY ./migrations ./migrations

ENTRYPOINT ["/app/gram"]
