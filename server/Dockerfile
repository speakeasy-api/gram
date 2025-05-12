FROM golang:1.24.3-alpine3.21 AS builder

ARG GIT_SHA=unset

WORKDIR /app

COPY go.* ./

RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

COPY . ./

RUN --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build \
    -mod=readonly \
    -ldflags="-X github.com/speakeasy-api/gram/cmd/gram.GitSHA=${GIT_SHA} -X goa.design/clue/health.Version=${GIT_SHA}" \
    -o /app/bin/gram \
    ./main.go

RUN chmod +x /app/bin/gram

### --- ###

FROM gcr.io/distroless/static-debian12:nonroot

WORKDIR /app

USER nonroot

COPY --from=builder --chown=nonroot:nonroot /app/bin/gram .

ENTRYPOINT ["/app/gram"]
