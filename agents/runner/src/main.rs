mod clip;
mod compaction;
mod errors;
mod gram_client;
mod http_layer;
mod runtime;
mod server;
mod tools;
mod wire;
mod workdir;

use std::net::SocketAddr;
use std::process::ExitCode;

use clap::{Parser, Subcommand};

use crate::server::ServeConfig;

#[derive(Parser, Debug)]
#[command(name = "gram-assistant-runner", version)]
struct Cli {
    #[command(subcommand)]
    mode: Mode,
}

#[derive(Subcommand, Debug)]
enum Mode {
    /// Run the HTTP server hosting `/threads/{thread_id}/turn` and `/state`.
    /// One VM per assistant; per-thread state is created lazily on first
    /// /turn via the bootstrap callback to the management API.
    Serve {
        #[arg(long, default_value = "0.0.0.0:8081", env = "GRAM_RUNNER_ADDR")]
        addr: SocketAddr,

        /// The assistant id this VM serves. Stamped into /state for the
        /// reaper's belt-and-suspenders identity check.
        #[arg(long, env = "GRAM_ASSISTANT_ID")]
        assistant_id: String,

        /// Base URL of the management API the runner calls back into for
        /// bootstrap, completions, and MCP traffic.
        #[arg(long, env = "GRAM_SERVER_URL")]
        server_url: String,

        /// Initial bearer token. The first /turn rotates it; included only
        /// to satisfy the bootstrap call before any /turn lands. In normal
        /// admit the backend stamps an empty value here and the very first
        /// /turn brings the live token.
        #[arg(long, env = "GRAM_INITIAL_TOKEN", default_value = "")]
        initial_token: String,
    },
}

#[tokio::main]
async fn main() -> ExitCode {
    init_tracing();

    let cli = Cli::parse();
    let result = match cli.mode {
        Mode::Serve {
            addr,
            assistant_id,
            server_url,
            initial_token,
        } => server::serve(ServeConfig {
            addr,
            assistant_id,
            server_url,
            initial_token,
        })
        .await
        .map_err(|e| e.to_string()),
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
