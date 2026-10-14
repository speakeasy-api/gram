FROM rust:1.97-bookworm@sha256:77fac8b98f9f46062bb680b6d25d5bcaabfc400143952ebc572e924bcbedc3fa AS monty-builder
RUN cargo install --locked --no-default-features --git https://github.com/pydantic/monty \
    --rev 15753b35e8eee6d569f223cd19f0303dbf07f896 \
    --root /monty --bin monty monty-runtime

FROM rust:1.97-bookworm@sha256:77fac8b98f9f46062bb680b6d25d5bcaabfc400143952ebc572e924bcbedc3fa AS build
WORKDIR /src
COPY agents/code-runner/Cargo.toml agents/code-runner/Cargo.lock ./
COPY agents/code-runner/src ./src
RUN cargo build --locked --release

FROM debian:bookworm-slim@sha256:7b140f374b289a7c2befc338f42ebe6441b7ea838a042bbd5acbfca6ec875818
COPY --from=build /src/target/release/gram-code-runner /usr/local/bin/gram-code-runner
COPY --from=monty-builder /monty/bin/monty /usr/local/bin/monty
USER 65532:65532
ENV GRAM_CODE_RUNNER_LISTEN=0.0.0.0:8081
# Worker environments are cleared; never rely on PATH to find the interpreter.
ENV GRAM_CODE_RUNNER_MONTY_BIN=/usr/local/bin/monty
EXPOSE 8081
ENTRYPOINT ["/usr/local/bin/gram-code-runner"]
