use std::sync::{Arc, OnceLock};
use std::time::Duration;

use opentelemetry::trace::Span as _;
use opentelemetry::{Context, KeyValue};
use opentelemetry_sdk::error::OTelSdkResult;
use opentelemetry_sdk::trace::{Span, SpanData, SpanProcessor};

/// Gram identity shared between the runtime host and the span processor
/// that stamps it onto exported spans. Both ids come from the
/// GRAM_ASSISTANT_ID / GRAM_ASSISTANT_PROJECT_ID boot envs when present
/// (Fly, GKE cold-start), or from the first /turn that carries them (GKE
/// warm-pool sandbox). Set-once cells — the boot env wins because it is
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
    /// trusted sources (the boot env); request-borne ids go through
    /// [`SpanIdentity::bind_request`].
    pub fn bind(cell: &OnceLock<String>, raw: Option<&str>) {
        if cell.get().is_some() {
            return;
        }
        if let Some(id) = raw.map(str::trim).filter(|id| !id.is_empty()) {
            let _ = cell.set(id.to_string());
        }
    }

    /// [`SpanIdentity::bind`] for ids arriving on unauthenticated requests:
    /// the cells are permanent once set, so a stray or malformed POST must
    /// not get to misattribute the pod's spans. The backend only ever sends
    /// UUIDs, so anything else is rejected and leaves the cell open for the
    /// real first turn.
    pub fn bind_request(cell: &OnceLock<String>, raw: Option<&str>) {
        Self::bind(cell, raw.map(str::trim).filter(|id| is_uuid(id)));
    }
}

fn is_uuid(s: &str) -> bool {
    let bytes = s.as_bytes();
    bytes.len() == 36
        && bytes.iter().enumerate().all(|(i, b)| match i {
            8 | 13 | 18 | 23 => *b == b'-',
            _ => b.is_ascii_hexdigit(),
        })
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

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn bind_request_only_accepts_uuids() {
        let cell = OnceLock::new();
        for rejected in [
            None,
            Some(""),
            Some("   "),
            Some("not-a-uuid"),
            Some("12345678-1234-1234-1234-12345678901"),
            Some("12345678-1234-1234-1234-1234567890123"),
            Some("12345678x1234-1234-1234-123456789012"),
            Some("gggggggg-gggg-gggg-gggg-gggggggggggg"),
        ] {
            SpanIdentity::bind_request(&cell, rejected);
            assert_eq!(cell.get(), None, "should reject {rejected:?}");
        }

        SpanIdentity::bind_request(&cell, Some(" 0198a7c2-1234-7bcd-8ef0-A0b1c2d3e4f5 "));
        assert_eq!(
            cell.get().map(String::as_str),
            Some("0198a7c2-1234-7bcd-8ef0-A0b1c2d3e4f5")
        );

        SpanIdentity::bind_request(&cell, Some("11111111-2222-3333-4444-555555555555"));
        assert_eq!(
            cell.get().map(String::as_str),
            Some("0198a7c2-1234-7bcd-8ef0-A0b1c2d3e4f5"),
            "set-once: a later value must not overwrite"
        );
    }
}
