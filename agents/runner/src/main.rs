mod clip;
mod compaction;
mod errors;
mod gram_client;
mod http_layer;
mod runtime;
mod server;
mod telemetry;
mod tools;
mod wire;
mod workdir;

use std::net::SocketAddr;
use std::process::ExitCode;
use std::sync::Arc;

use clap::{Parser, Subcommand, ValueEnum};
use opentelemetry::trace::TracerProvider as _;
use opentelemetry_otlp::{Protocol, SpanExporter, WithExportConfig};
use opentelemetry_sdk::Resource;
use opentelemetry_sdk::trace::SdkTracerProvider;
use tracing_subscriber::Layer;
use tracing_subscriber::layer::SubscriberExt;
use tracing_subscriber::util::SubscriberInitExt;

use crate::server::ServeConfig;
use crate::telemetry::{IdentityStampProcessor, SpanIdentity};

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

        /// The assistant id this VM serves, stamped into /state. Optional: a
        /// generic warm-pool sandbox boots without it and learns it from the
        /// first turn instead (see ThreadTurnRequest.assistant_id).
        #[arg(long, env = "GRAM_ASSISTANT_ID")]
        assistant_id: Option<String>,

        /// The project the assistant belongs to, used only to tag exported
        /// trace spans. Optional like --assistant-id: a warm-pool sandbox
        /// learns it from the first turn instead.
        #[arg(long, env = "GRAM_ASSISTANT_PROJECT_ID")]
        assistant_project_id: Option<String>,

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

        /// Export spans over OTLP. The exporter reads the standard
        /// OTEL_EXPORTER_OTLP_* environment variables (ENDPOINT, HEADERS,
        /// TIMEOUT, ...) for everything except transport selection, which
        /// comes from --otel-protocol.
        #[arg(long, env = "GRAM_ENABLE_OTEL_TRACES")]
        with_otel_tracing: bool,

        /// OTLP transport used when --with-otel-tracing is set.
        #[arg(
            long,
            value_enum,
            env = "OTEL_EXPORTER_OTLP_PROTOCOL",
            default_value = "grpc"
        )]
        otel_protocol: OtlpProtocol,
    },
}

#[derive(Clone, Copy, Debug, ValueEnum)]
enum OtlpProtocol {
    Grpc,
    #[value(name = "http/protobuf")]
    HttpProtobuf,
    #[value(name = "http/json")]
    HttpJson,
}

#[tokio::main]
async fn main() -> ExitCode {
    let cli = Cli::parse();
    let result = match cli.mode {
        Mode::Serve {
            addr,
            assistant_id,
            assistant_project_id,
            server_url,
            initial_token,
            with_otel_tracing,
            otel_protocol,
        } => {
            let identity = Arc::new(SpanIdentity::default());
            SpanIdentity::bind(&identity.assistant_id, assistant_id.as_deref());
            SpanIdentity::bind(&identity.project_id, assistant_project_id.as_deref());
            let tracer_provider = init_tracing(
                with_otel_tracing.then_some(otel_protocol),
                Arc::clone(&identity),
            );
            let result = server::serve(ServeConfig {
                addr,
                server_url,
                initial_token,
                identity,
            })
            .await
            .map_err(|e| e.to_string());
            if let Some(provider) = tracer_provider
                && let Err(e) = provider.shutdown()
            {
                tracing::warn!(error = %e, "otel tracer provider shutdown failed");
            }
            result
        }
    };

    match result {
        Ok(()) => ExitCode::SUCCESS,
        Err(e) => {
            tracing::error!(error = %e, "runner exited with error");
            ExitCode::FAILURE
        }
    }
}

/// Initializes the tracing subscriber, optionally layering an OTLP span
/// exporter on top of the stderr log output. Returns the tracer provider so
/// the caller can flush buffered spans via `shutdown` before process exit.
///
/// An exporter that fails to build degrades to log-only tracing rather than
/// failing the process: the runner serves user traffic and a misconfigured
/// collector endpoint should not take it down.
///
/// `identity` feeds an [`IdentityStampProcessor`] that stamps gram identity
/// onto every exported span; the same cells are shared with the runtime host.
fn init_tracing(
    otel_protocol: Option<OtlpProtocol>,
    identity: Arc<SpanIdentity>,
) -> Option<SdkTracerProvider> {
    let filter = tracing_subscriber::EnvFilter::try_from_default_env().unwrap_or_else(|_| {
        tracing_subscriber::EnvFilter::new(
            "info,agentkit=trace,agentkit_loop=trace,agentkit_reporting=trace,agentkit_mcp=trace",
        )
    });
    let fmt_layer = tracing_subscriber::fmt::layer()
        .with_writer(std::io::stderr)
        .with_target(true);

    let mut exporter_error = None;
    let provider = otel_protocol.and_then(|protocol| match build_span_exporter(protocol) {
        Ok(exporter) => {
            let resource = Resource::builder()
                .with_service_name("gram-assistant-runner")
                .with_attribute(opentelemetry::KeyValue::new(
                    "service.version",
                    env!("CARGO_PKG_VERSION"),
                ))
                .build();
            Some(
                SdkTracerProvider::builder()
                    .with_resource(resource)
                    .with_span_processor(IdentityStampProcessor::new(identity))
                    .with_batch_exporter(exporter)
                    .build(),
            )
        }
        Err(error) => {
            exporter_error = Some(error);
            None
        }
    });
    // INFO floor keeps per-token ContentDelta and other debug/trace events
    // out of exported spans; the stderr log keeps its env-driven verbosity.
    let otel_layer = provider.as_ref().map(|p| {
        tracing_opentelemetry::layer()
            .with_tracer(p.tracer("gram-assistant-runner"))
            .with_filter(tracing_subscriber::filter::LevelFilter::INFO)
    });

    tracing_subscriber::registry()
        .with(filter)
        .with(fmt_layer)
        .with(otel_layer)
        .init();

    if let Some(error) = exporter_error {
        tracing::error!(error = %error, "failed to build OTLP span exporter; traces will not be exported");
    }
    provider
}

fn build_span_exporter(
    protocol: OtlpProtocol,
) -> Result<SpanExporter, opentelemetry_otlp::ExporterBuildError> {
    match protocol {
        OtlpProtocol::Grpc => SpanExporter::builder().with_tonic().build(),
        OtlpProtocol::HttpProtobuf => SpanExporter::builder()
            .with_http()
            .with_protocol(Protocol::HttpBinary)
            .build(),
        OtlpProtocol::HttpJson => SpanExporter::builder()
            .with_http()
            .with_protocol(Protocol::HttpJson)
            .build(),
    }
}
