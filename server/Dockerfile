FROM golang:1.25.1-alpine3.22@sha256:b6ed3fd0452c0e9bcdef5597f29cc1418f61672e9d3a2f55bf02e7222c014abd AS builder

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

FROM gcr.io/distroless/static-debian12:nonroot@sha256:e8a4044e0b4ae4257efa45fc026c0bc30ad320d43bd4c1a7d5271bd241e386d0

WORKDIR /app

USER nonroot

COPY --from=builder --chown=nonroot:nonroot /app/bin/gram .
# Add this line to copy the migrations folder
COPY --from=builder --chown=nonroot:nonroot /app/migrations ./migrations

ENTRYPOINT ["/app/gram"]
