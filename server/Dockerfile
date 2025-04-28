FROM golang:1.24.2-alpine3.21 AS builder

ARG GIT_SHA=unset

WORKDIR /app

COPY . ./

RUN go mod download

RUN CGO_ENABLED=0 go build \
  -mod=readonly \
  -ldflags="-X github.com/speakeasy-api/gram/cmd/gram.GitSHA=${GIT_SHA} -X goa.design/clue/health.Version=${GIT_SHA}" \
  -o bin/gram \
  ./main.go

RUN chmod +x /app/bin/gram

### --- ###

FROM gcr.io/distroless/static-debian12:nonroot

WORKDIR /app

USER nonroot

COPY --from=builder --chown=nonroot:nonroot /app/bin/gram .

ENTRYPOINT ["/app/gram"]
