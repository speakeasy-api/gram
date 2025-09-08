FROM golang:1.25.0-alpine3.22@sha256:f18a072054848d87a8077455f0ac8a25886f2397f88bfdd222d6fafbb5bba440 AS builder

ARG GIT_SHA=unset

WORKDIR /app

COPY go.* ./

RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

COPY . ./

RUN --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build \
    -mod=readonly \
    -ldflags="-X github.com/speakeasy-api/gram/server/cmd/gram.GitSHA=${GIT_SHA} -X goa.design/clue/health.Version=${GIT_SHA}" \
    -o /app/bin/gram \
    ./main.go

RUN chmod +x /app/bin/gram

### --- ###

FROM gcr.io/distroless/static-debian12:nonroot@sha256:a9f88e0d99c1ceedbce565fad7d3f96744d15e6919c19c7dafe84a6dd9a80c61

WORKDIR /app

USER nonroot

COPY --from=builder --chown=nonroot:nonroot /app/bin/gram .
# Add this line to copy the migrations folder
COPY --from=builder --chown=nonroot:nonroot /app/migrations ./migrations

ENTRYPOINT ["/app/gram"]
