FROM golang:1.25.0-alpine3.22@sha256:5dffbd69b37b8e9fc35d94817e94bc7bf4fd047147e3c5353d357bd3492992bf AS builder

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

FROM gcr.io/distroless/static-debian12:nonroot@sha256:cdf4daaf154e3e27cfffc799c16f343a384228f38646928a1513d925f473cb46

WORKDIR /app

USER nonroot

COPY --from=builder --chown=nonroot:nonroot /app/bin/gram .
# Add this line to copy the migrations folder
COPY --from=builder --chown=nonroot:nonroot /app/migrations ./migrations

ENTRYPOINT ["/app/gram"]
