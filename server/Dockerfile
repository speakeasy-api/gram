FROM gcr.io/distroless/static-debian12:nonroot@sha256:a9329520abc449e3b14d5bc3a6ffae065bdde0f02667fa10880c49b35c109fd1

WORKDIR /app

USER nonroot

COPY --chown=nonroot:nonroot --chmod=0755 ./bin/gram /app/gram
# Add this line to copy the migrations folder
COPY ./migrations ./migrations

ENTRYPOINT ["/app/gram"]
