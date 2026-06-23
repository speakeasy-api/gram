# Generic distroless image for the tunnel binaries (gateway / agent / sample-mcp).
# The static CGO-free binary is prebuilt by build-and-load.sh into ./bin/<name>
# and selected via the BIN build arg. Context = a temp staging dir holding bin/.
FROM gcr.io/distroless/static-debian13:nonroot
ARG BIN
WORKDIR /app
COPY bin/${BIN} /app/app
ENTRYPOINT ["/app/app"]
