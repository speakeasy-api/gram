use std::sync::{Arc, OnceLock};
use std::time::Duration;

use opentelemetry::trace::Span as _;
use opentelemetry::{Context, KeyValue};
use opentelemetry_sdk::error::OTelSdkResult;
use opentelemetry_sdk::trace::{Span, SpanData, SpanProcessor};

/// Gram identity shared between the runtime host and the span processor
/// that stamps it onto exported spans. The assistant id comes from the
/// GRAM_ASSISTANT_ID boot env when present, or from the first /turn that
/// carries one (GKE warm-pool sandbox); the project id comes from the first
/// /turn only. Set-once cells — the boot env wins because it is seeded
/// first, and a sandbox binds to one assistant and project for its lifetime.
#[derive(Debug, Default)]
pub struct SpanIdentity {
    pub assistant_id: OnceLock<String>,
    pub project_id: OnceLock<String>,
}

impl SpanIdentity {
    /// Set-once with blank input ignored: an empty boot env or a turn that
    /// omits the id leaves the cell open for a later, real value.
    pub fn bind(cell: &OnceLock<String>, raw: Option<&str>) {
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
