use std::net::SocketAddr;

use anyhow::{Context, Result};
use clap::Parser;
use gram_code_runner::{State, serve};
use tokio::net::TcpListener;
use tokio_util::sync::CancellationToken;

#[derive(Parser)]
#[command(about = "Ephemeral OSS Monty execution for Gram gateways")]
struct Args {
    #[arg(
        long,
        env = "GRAM_CODE_RUNNER_LISTEN",
        default_value = "127.0.0.1:8081"
    )]
    listen: SocketAddr,
    #[arg(long, env = "GRAM_CODE_RUNNER_MONTY_BIN", default_value = "monty")]
    monty_bin: String,
    #[arg(long, env = "GRAM_CODE_RUNNER_CAPACITY", default_value_t = 4)]
    capacity: usize,
}

#[tokio::main]
async fn main() -> Result<()> {
    let args = Args::parse();
    // The credential stays out of argv and child environments. On Linux,
    // prevent a same-UID worker from inspecting the parent's environment.
    #[cfg(target_os = "linux")]
    if unsafe { libc::prctl(libc::PR_SET_DUMPABLE, 0, 0, 0, 0) } != 0 {
        return Err(std::io::Error::last_os_error()).context("disable parent process inspection");
    }
    let token =
        std::env::var("GRAM_CODE_RUNNER_TOKEN").context("GRAM_CODE_RUNNER_TOKEN is required")?;
    let state = State::new(&args.monty_bin, token, args.capacity).await?;
    let listener = TcpListener::bind(args.listen).await?;
    eprintln!(
        "gram-code-runner protocol 1 listening on {}",
        listener.local_addr()?
    );
    let shutdown = CancellationToken::new();
    let signal = shutdown.clone();
    tokio::spawn(async move {
        #[cfg(unix)]
        {
            let mut term =
                tokio::signal::unix::signal(tokio::signal::unix::SignalKind::terminate())
                    .expect("install SIGTERM handler");
            tokio::select! { _ = tokio::signal::ctrl_c() => {}, _ = term.recv() => {} }
        }
        #[cfg(not(unix))]
        let _ = tokio::signal::ctrl_c().await;
        signal.cancel();
    });
    serve(listener, state, shutdown).await
}
