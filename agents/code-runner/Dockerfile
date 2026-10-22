FROM rust:1.96.1-bookworm@sha256:a339861ae23e9abb272cea45dfafde21760d2ce6577a70f8a926153677902663 AS monty-builder
RUN cargo install --locked --no-default-features --git https://github.com/pydantic/monty \
    --rev 15753b35e8eee6d569f223cd19f0303dbf07f896 \
    --root /monty --bin monty monty-runtime

FROM rust:1.96.1-bookworm@sha256:a339861ae23e9abb272cea45dfafde21760d2ce6577a70f8a926153677902663 AS build
WORKDIR /src
COPY agents/code-runner/Cargo.toml agents/code-runner/Cargo.lock ./
COPY agents/code-runner/src ./src
RUN cargo build --locked --release

FROM debian:bookworm-slim@sha256:7c7b2c966bc9ee8cedfeef67e0e279108992c77681fa595db4a9d65c06ccc587
COPY --from=build /src/target/release/gram-code-runner /usr/local/bin/gram-code-runner
COPY --from=monty-builder /monty/bin/monty /usr/local/bin/monty
USER 65532:65532
ENV GRAM_CODE_RUNNER_LISTEN=0.0.0.0:8081
# Worker environments are cleared; never rely on PATH to find the interpreter.
ENV GRAM_CODE_RUNNER_MONTY_BIN=/usr/local/bin/monty
EXPOSE 8081
ENTRYPOINT ["/usr/local/bin/gram-code-runner"]
