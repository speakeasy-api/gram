use std::collections::HashMap;
use std::sync::{Arc, Mutex, OnceLock, PoisonError};
use std::time::{Duration, Instant};

use agentkit_loop::{AgentEvent, LoopObserver, ObservedEvent};
use agentkit_mcp::{McpCallToolRequestParams, McpExtensions, McpRequestDecorator};

use opentelemetry::propagation::{Extractor, Injector, TextMapPropagator};
use opentelemetry::trace::{Span as _, TraceContextExt as _};
use opentelemetry::{Context, KeyValue};
use opentelemetry_sdk::error::OTelSdkResult;
use opentelemetry_sdk::propagation::TraceContextPropagator;
use opentelemetry_sdk::trace::{Span, SpanData, SpanProcessor};
use tracing_opentelemetry::OpenTelemetrySpanExt as _;

/// Speakeasy identity shared between the runtime host and the span processor
/// that stamps it onto exported spans. Both ids come from the
/// GRAM_ASSISTANT_ID / GRAM_ASSISTANT_PROJECT_ID boot envs when present
/// (local containers, GKE cold-start), or from authenticated bootstrap on the first /turn
/// (GKE warm-pool sandbox). Set-once cells — the boot env wins because it is
/// seeded first, and a sandbox binds to one assistant and project for its
/// lifetime.
#[derive(Debug, Default)]
pub struct SpanIdentity {
    pub assistant_id: OnceLock<String>,
    pub project_id: OnceLock<String>,
}

impl SpanIdentity {
    /// Set-once with blank input ignored: an empty boot env or a turn that
    /// omits the id leaves the cell open for a later, real value. Only for
    /// trusted sources (boot env and authenticated bootstrap).
    pub fn bind(cell: &OnceLock<String>, raw: Option<&str>) {
        if cell.get().is_some() {
            return;
        }
        if let Some(id) = raw.map(str::trim).filter(|id| !id.is_empty()) {
            let _ = cell.set(id.to_string());
        }
    }
}

/// Appends `gram.assistant.id` / `gram.project.id` to every span at start,
/// once known, so per-span filtering in the backend works. Resource
/// attributes can't carry them because a warm-pool runner learns its
/// identity from the first /turn, after the tracer provider is built.
///
/// The keys mirror `server/internal/attr`'s `AssistantIDKey`/`ProjectIDKey`,
/// which the telemetry backend filters on — keep them in sync.
#[derive(Debug)]
pub struct IdentityStampProcessor {
    identity: Arc<SpanIdentity>,
}

impl IdentityStampProcessor {
    pub fn new(identity: Arc<SpanIdentity>) -> Self {
        Self { identity }
    }
}

impl SpanProcessor for IdentityStampProcessor {
    fn on_start(&self, span: &mut Span, _cx: &Context) {
        if let Some(id) = self.identity.assistant_id.get() {
            span.set_attribute(KeyValue::new("gram.assistant.id", id.clone()));
        }
        if let Some(id) = self.identity.project_id.get() {
            span.set_attribute(KeyValue::new("gram.project.id", id.clone()));
        }
    }

    fn on_end(&self, _span: SpanData) {}

    fn force_flush(&self) -> OTelSdkResult {
        Ok(())
    }

    fn shutdown_with_timeout(&self, _timeout: Duration) -> OTelSdkResult {
        Ok(())
    }
}

/// Marks the first streamed delta of each model request with a
/// `gen_ai.first_token` event carrying `time_to_first_token_ms`, so a slow
/// answer can be split into waiting for the provider versus streaming.
///
/// agentkit's `chat` span covers the whole request and stream but records no
/// first-token time. A request follows `TurnStarted` (the turn's first model
/// call) or a tool result (each later call follows its tool round), so both
/// arm the timer and the first delta after either fires the event. Inline
/// compaction can run between those events and the request; its
/// `MutationFinished` restarts an armed timer, so compaction time is not
/// counted as model latency. The event lands on the span current at emit
/// time, agentkit's `agent.turn`.
#[derive(Debug, Default)]
pub struct FirstTokenObserver {
    armed_at: Mutex<Option<Instant>>,
}

impl LoopObserver for FirstTokenObserver {
    fn handle_event(&self, observed: ObservedEvent) {
        let mut armed_at = self.armed_at.lock().unwrap_or_else(PoisonError::into_inner);
        match observed.event {
            AgentEvent::TurnStarted { .. } | AgentEvent::ToolResultReceived(_) => {
                *armed_at = Some(Instant::now());
            }
            AgentEvent::MutationFinished { .. } if armed_at.is_some() => {
                *armed_at = Some(Instant::now());
            }
            AgentEvent::ContentDelta(_) => {
                if let Some(started) = armed_at.take() {
                    let elapsed_ms =
                        u64::try_from(started.elapsed().as_millis()).unwrap_or(u64::MAX);
                    tracing::info!(time_to_first_token_ms = elapsed_ms, "gen_ai.first_token");
                }
            }
            _ => {}
        }
    }
}

/// W3C trace headers captured for one MCP `tools/call` request, carried in
/// the request's extensions to [`crate::http_layer::McpSessionClient`].
#[derive(Clone, Debug, Default)]
pub struct McpTraceHeaders(pub HashMap<http::HeaderName, http::HeaderValue>);

/// agentkit request decorator that snapshots the current span into each
/// `tools/call` request. agentkit calls it inside its `mcp.call_tool` span,
/// so the server's MCP request links to the span for that wire call. When
/// tracing filters that span out, the nearest enabled span (the runner's
/// `assistant.tool`) is used instead, which still links the same tool call.
pub fn mcp_trace_decorator() -> Arc<dyn McpRequestDecorator> {
    Arc::new(
        |_params: &McpCallToolRequestParams, extensions: &mut McpExtensions| {
            let mut headers = HashMap::new();
            inject_trace_context_into(&tracing::Span::current().context(), &mut headers);
            if !headers.is_empty() {
                extensions.insert(McpTraceHeaders(headers));
            }
        },
    )
}

/// Writes `cx` as W3C `traceparent`/`tracestate` into an MCP request's extra
/// headers. A context without a valid span writes nothing.
pub fn inject_trace_context_into(
    cx: &Context,
    headers: &mut HashMap<http::HeaderName, http::HeaderValue>,
) {
    TraceContextPropagator::new().inject_context(cx, &mut HeaderMapInjector(headers));
}

struct HeaderMapInjector<'a>(&'a mut HashMap<http::HeaderName, http::HeaderValue>);

impl Injector for HeaderMapInjector<'_> {
    fn set(&mut self, key: &str, value: String) {
        if let (Ok(name), Ok(value)) = (
            http::HeaderName::from_bytes(key.as_bytes()),
            http::HeaderValue::from_str(&value),
        ) {
            self.0.insert(name, value);
        }
    }
}

/// Writes the current tracing span's W3C `traceparent` onto outbound request
/// headers so the management API can tie its own spans to the runner's
/// trace. A no-op when the span is not exported (no OTLP layer, or a level
/// the OTLP layer filters out), because the context then carries no valid
/// span.
pub fn inject_current_trace_context(headers: &mut http::HeaderMap) {
    let cx = tracing::Span::current().context();
    TraceContextPropagator::new().inject_context(&cx, &mut HeaderInjector(headers));
}

/// Parents `span` under the W3C trace context the management API sent with
/// the request, so a turn's runner spans join the backend trace that
/// dispatched it. Headers without a valid `traceparent` leave `span` as is.
pub fn set_parent_from_headers(span: &tracing::Span, headers: &http::HeaderMap) {
    let cx = TraceContextPropagator::new().extract(&HeaderExtractor(headers));
    if cx.span().span_context().is_valid() {
        let _ = span.set_parent(cx);
    }
}

struct HeaderInjector<'a>(&'a mut http::HeaderMap);

impl Injector for HeaderInjector<'_> {
    fn set(&mut self, key: &str, value: String) {
        if let (Ok(name), Ok(value)) = (
            http::HeaderName::from_bytes(key.as_bytes()),
            http::HeaderValue::from_str(&value),
        ) {
            self.0.insert(name, value);
        }
    }
}

struct HeaderExtractor<'a>(&'a http::HeaderMap);

impl Extractor for HeaderExtractor<'_> {
    fn get(&self, key: &str) -> Option<&str> {
        self.0.get(key).and_then(|value| value.to_str().ok())
    }

    fn keys(&self) -> Vec<&str> {
        self.0.keys().map(http::HeaderName::as_str).collect()
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    #[allow(clippy::unwrap_used)]
    fn runner_spans_join_the_inbound_trace_and_propagate_it_outbound() {
        use opentelemetry::trace::TracerProvider as _;
        use tracing_subscriber::layer::SubscriberExt as _;

        let provider = opentelemetry_sdk::trace::SdkTracerProvider::builder().build();
        let subscriber = tracing_subscriber::registry()
            .with(tracing_opentelemetry::layer().with_tracer(provider.tracer("test")));
        let trace_id = "4bf92f3577b34da6a3ce929d0e0e4736";
        let mut inbound = http::HeaderMap::new();
        inbound.insert(
            "traceparent",
            http::HeaderValue::from_str(&format!("00-{trace_id}-00f067aa0ba902b7-01")).unwrap(),
        );

        tracing::subscriber::with_default(subscriber, || {
            let span = tracing::info_span!("thread_turn");
            set_parent_from_headers(&span, &inbound);
            let _entered = span.enter();
            let mut outbound = http::HeaderMap::new();
            inject_current_trace_context(&mut outbound);
            let traceparent = outbound.get("traceparent").unwrap().to_str().unwrap();
            assert!(traceparent.starts_with(&format!("00-{trace_id}-")));
            assert!(!traceparent.contains("00f067aa0ba902b7"), "child span id");
        });
    }

    #[test]
    #[allow(clippy::unwrap_used)]
    fn mcp_trace_decorator_captures_the_wire_call_span() {
        use opentelemetry::trace::TracerProvider as _;
        use tracing_subscriber::layer::SubscriberExt as _;

        let provider = opentelemetry_sdk::trace::SdkTracerProvider::builder().build();
        let subscriber = tracing_subscriber::registry()
            .with(tracing_opentelemetry::layer().with_tracer(provider.tracer("test")));
        let decorator = mcp_trace_decorator();

        tracing::subscriber::with_default(subscriber, || {
            let tool = tracing::info_span!("assistant.tool");
            let _tool = tool.enter();
            let call = tracing::info_span!("mcp.call_tool");
            let _call = call.enter();
            let expected = call.context().span().span_context().clone();

            let mut extensions = McpExtensions::new();
            decorator.decorate_call_tool(
                &McpCallToolRequestParams::new("platform_overview"),
                &mut extensions,
            );
            let McpTraceHeaders(headers) = extensions.get::<McpTraceHeaders>().unwrap();
            let traceparent = headers
                .get(&http::HeaderName::from_static("traceparent"))
                .unwrap()
                .to_str()
                .unwrap();
            assert_eq!(
                traceparent,
                format!("00-{}-{}-01", expected.trace_id(), expected.span_id()),
                "the parent is the mcp.call_tool span itself"
            );
        });
    }

    #[test]
    fn mcp_trace_decorator_adds_nothing_without_a_span() {
        let mut extensions = McpExtensions::new();
        mcp_trace_decorator().decorate_call_tool(
            &McpCallToolRequestParams::new("platform_overview"),
            &mut extensions,
        );
        assert!(extensions.get::<McpTraceHeaders>().is_none());
    }

    #[test]
    fn no_inbound_trace_context_injects_nothing_without_an_exporter() {
        let mut outbound = http::HeaderMap::new();
        inject_current_trace_context(&mut outbound);
        assert!(outbound.is_empty());
    }

    fn observe(observer: &FirstTokenObserver, event: AgentEvent) {
        observer.handle_event(ObservedEvent {
            session_id: Arc::new(agentkit_core::SessionId::new("s")),
            event,
        });
    }

    fn mutation_finished() -> AgentEvent {
        AgentEvent::MutationFinished {
            session_id: agentkit_core::SessionId::new("s"),
            turn_id: None,
            mutator: "compaction".into(),
            dirty: true,
            metadata: agentkit_core::MetadataMap::new(),
        }
    }

    fn armed_at(observer: &FirstTokenObserver) -> Option<Instant> {
        *observer
            .armed_at
            .lock()
            .unwrap_or_else(PoisonError::into_inner)
    }

    #[test]
    #[allow(clippy::expect_used)]
    fn first_token_timer_restarts_after_inline_compaction() {
        let observer = FirstTokenObserver::default();
        observe(
            &observer,
            AgentEvent::TurnStarted {
                session_id: agentkit_core::SessionId::new("s"),
                turn_id: agentkit_core::TurnId::new("t"),
            },
        );
        let armed = armed_at(&observer).expect("turn start arms the timer");
        std::thread::sleep(Duration::from_millis(5));
        observe(&observer, mutation_finished());
        let rearmed = armed_at(&observer).expect("compaction keeps the timer armed");
        assert!(rearmed > armed, "compaction must restart the timer");
    }

    #[test]
    fn first_token_timer_ignores_compaction_when_not_armed() {
        let observer = FirstTokenObserver::default();
        observe(&observer, mutation_finished());
        assert!(armed_at(&observer).is_none());
    }

    #[test]
    fn trusted_identity_binds_once_and_ignores_empty_values() {
        let cell = OnceLock::new();
        SpanIdentity::bind(&cell, Some(" "));
        assert!(cell.get().is_none());
        SpanIdentity::bind(&cell, Some(" authenticated-id "));
        SpanIdentity::bind(&cell, Some("replacement"));
        assert_eq!(cell.get().map(String::as_str), Some("authenticated-id"));
    }
}
