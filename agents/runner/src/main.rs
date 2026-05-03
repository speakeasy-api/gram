mod errors;
mod http_layer;
mod idempotency;
mod runtime;
mod server;
mod tools;
mod wire;
mod workdir;

use std::net::SocketAddr;
use std::process::ExitCode;

use clap::{Parser, Subcommand};

#[derive(Parser, Debug)]
#[command(name = "gram-assistant-runner", version)]
struct Cli {
    #[command(subcommand)]
    mode: Mode,
}

#[derive(Subcommand, Debug)]
enum Mode {
    /// Run the HTTP server that hosts /configure and /turn.
    Serve {
        #[arg(long, default_value = "0.0.0.0:8081", env = "GRAM_RUNNER_ADDR")]
        addr: SocketAddr,
    },
}

#[tokio::main]
async fn main() -> ExitCode {
    init_tracing();

    let cli = Cli::parse();
    let result = match cli.mode {
        Mode::Serve { addr } => server::serve(addr).await.map_err(|e| e.to_string()),
    };

    match result {
        Ok(()) => ExitCode::SUCCESS,
        Err(e) => {
            tracing::error!(error = %e, "runner exited with error");
            ExitCode::FAILURE
        }
    }
}

fn init_tracing() {
    // Route tracing events (including agentkit's TracingReporter output) to
    // stderr so they flow through the guest's serial console into the
    // `assistant-runtime` log. RUST_LOG tunes the filter; default shows info+.
    tracing_subscriber::fmt()
        .with_env_filter(
            tracing_subscriber::EnvFilter::try_from_default_env().unwrap_or_else(|_| {
                tracing_subscriber::EnvFilter::new(
                    "info,agentkit=trace,agentkit_loop=trace,agentkit_reporting=trace,agentkit_mcp=trace",
                )
            }),
        )
        .with_writer(std::io::stderr)
        .with_target(true)
        .init();
}
