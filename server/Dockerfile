FROM gcr.io/distroless/static-debian13:nonroot@sha256:963fa6c544fe5ce420f1f54fb88b6fb01479f054c8056d0f74cc2c6000df5240

WORKDIR /app

USER nonroot

COPY --chown=nonroot:nonroot --chmod=0755 ./bin/gram /app/gram
# Add this line to copy the migrations folder
COPY ./migrations ./migrations

ENTRYPOINT ["/app/gram"]
