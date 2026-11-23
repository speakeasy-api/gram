use std::collections::HashMap;
use std::sync::atomic::{AtomicU64, Ordering};
use std::sync::{Arc, Mutex, OnceLock, PoisonError};
use std::time::{Duration, Instant};

use agentkit_loop::{AgentEvent, LoopObserver};

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
/// first-token time. A request starts at `TurnStarted` (the turn's first model
/// call) or after a tool result (each later call follows its tool round), so
/// both arm the timer and the first delta after either fires the event. The
/// event lands on the span current at emit time, agentkit's `agent.turn`.
#[derive(Debug, Default)]
pub struct FirstTokenObserver {
    armed_at: Mutex<Option<Instant>>,
}

impl LoopObserver for FirstTokenObserver {
    fn handle_event(&self, event: AgentEvent) {
        let mut armed_at = self.armed_at.lock().unwrap_or_else(PoisonError::into_inner);
        match event {
            AgentEvent::TurnStarted { .. } | AgentEvent::ToolResultReceived(_) => {
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

/// Registered contexts per argument key, oldest first, tagged with an id so
/// a call's guard can remove only its own entry.
type PendingContexts = HashMap<String, Vec<(u64, Context)>>;

/// Carries an MCP tool call's trace context from the task that dispatches it
/// to the rmcp worker task that sends its HTTP request.
///
/// rmcp posts every message from its own transport task, so the caller's span
/// is not current when the request is built. The dispatching tool registers
/// the current span's context under the call's arguments, and the transport
/// takes the oldest matching context when it posts a `tools/call` with those
/// arguments. Concurrent calls with identical arguments may swap contexts;
/// both parents are the same kind of span, so the trace stays readable.
#[derive(Clone, Debug, Default)]
pub struct McpTraceContexts {
    pending: Arc<Mutex<PendingContexts>>,
    next_id: Arc<AtomicU64>,
}

impl McpTraceContexts {
    /// Key for a call's arguments. Matches both the tool request input and
    /// the `params.arguments` of the JSON-RPC message built from it.
    fn key(arguments: Option<&serde_json::Value>) -> String {
        match arguments {
            None | Some(serde_json::Value::Null) => "null".to_string(),
            Some(value) => value.to_string(),
        }
    }

    /// Registers the current span's context for a call with `arguments`.
    /// The returned guard drops the entry if the transport never took it.
    pub fn register(&self, arguments: &serde_json::Value) -> McpTraceRegistration {
        let key = Self::key(Some(arguments));
        let id = self.next_id.fetch_add(1, Ordering::Relaxed);
        let cx = tracing::Span::current().context();
        self.pending
            .lock()
            .unwrap_or_else(PoisonError::into_inner)
            .entry(key.clone())
            .or_default()
            .push((id, cx));
        McpTraceRegistration {
            contexts: self.clone(),
            key,
            id,
        }
    }

    /// Takes the context registered for a `tools/call` message, if any.
    pub fn take_for_message(&self, message: &serde_json::Value) -> Option<Context> {
        if message.get("method").and_then(serde_json::Value::as_str) != Some("tools/call") {
            return None;
        }
        let key = Self::key(message.get("params").and_then(|p| p.get("arguments")));
        let mut pending = self.pending.lock().unwrap_or_else(PoisonError::into_inner);
        let entries = pending.get_mut(&key)?;
        if entries.is_empty() {
            return None;
        }
        let (_, cx) = entries.remove(0);
        if entries.is_empty() {
            pending.remove(&key);
        }
        Some(cx)
    }
}

/// Removes an unclaimed [`McpTraceContexts`] entry when the call ends.
#[derive(Debug)]
pub struct McpTraceRegistration {
    contexts: McpTraceContexts,
    key: String,
    id: u64,
}

impl Drop for McpTraceRegistration {
    fn drop(&mut self) {
        let mut pending = self
            .contexts
            .pending
            .lock()
            .unwrap_or_else(PoisonError::into_inner);
        if let Some(entries) = pending.get_mut(&self.key) {
            entries.retain(|(id, _)| *id != self.id);
            if entries.is_empty() {
                pending.remove(&self.key);
            }
        }
    }
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
    fn mcp_tool_calls_pick_up_the_dispatching_span_context() {
        use opentelemetry::trace::TracerProvider as _;
        use tracing_subscriber::layer::SubscriberExt as _;

        let provider = opentelemetry_sdk::trace::SdkTracerProvider::builder().build();
        let subscriber = tracing_subscriber::registry()
            .with(tracing_opentelemetry::layer().with_tracer(provider.tracer("test")));
        let contexts = McpTraceContexts::default();
        let args = serde_json::json!({"days": 7});
        let call = serde_json::json!({
            "jsonrpc": "2.0",
            "id": 3,
            "method": "tools/call",
            "params": {"name": "platform_overview", "arguments": {"days": 7}},
        });

        tracing::subscriber::with_default(subscriber, || {
            let span = tracing::info_span!("assistant.tool");
            let _entered = span.enter();
            let expected = span.context().span().span_context().trace_id();

            let other = contexts.register(&serde_json::json!({"days": 30}));
            let guard = contexts.register(&args);
            let initialize = serde_json::json!({"jsonrpc": "2.0", "id": 1, "method": "initialize"});
            assert!(contexts.take_for_message(&initialize).is_none());

            let cx = contexts.take_for_message(&call).unwrap();
            assert_eq!(cx.span().span_context().trace_id(), expected);
            let mut headers = HashMap::new();
            inject_trace_context_into(&cx, &mut headers);
            assert!(headers.keys().any(|name| name.as_str() == "traceparent"));
            assert!(contexts.take_for_message(&call).is_none(), "taken once");

            drop(guard);
            drop(other);
            assert!(
                contexts
                    .pending
                    .lock()
                    .unwrap_or_else(PoisonError::into_inner)
                    .is_empty(),
                "unclaimed entries leave with their call"
            );
        });
    }

    #[test]
    fn no_inbound_trace_context_injects_nothing_without_an_exporter() {
        let mut outbound = http::HeaderMap::new();
        inject_current_trace_context(&mut outbound);
        assert!(outbound.is_empty());
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
