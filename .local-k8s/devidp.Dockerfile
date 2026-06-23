# dev-idp (mock identity provider). Pure-Go (modernc sqlite) static binary.
# dev-idp's default-user bootstrap shells out to `git config --get user.email`
# (dev-idp/internal/defaultuser/defaultuser.go), so the image needs git + a
# baked committer identity. alpine (not distroless) provides both.
# Build context = ./dev-idp (binary prebuilt at dev-idp/bin/dev-idp).
FROM alpine:3.20
RUN apk add --no-cache git \
  && git config --system user.email "dev@gram.local" \
  && git config --system user.name "Gram Dev"
WORKDIR /app
COPY bin/dev-idp /app/dev-idp
ENTRYPOINT ["/app/dev-idp"]
