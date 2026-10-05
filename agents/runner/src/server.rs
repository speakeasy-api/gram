use std::net::SocketAddr;
use std::sync::Arc;

use axum::extract::{DefaultBodyLimit, Path, State};
use axum::http::{HeaderMap, StatusCode};
use axum::routing::{get, post};
use axum::{Json, Router};
use tokio::net::TcpListener;
use tokio::sync::{Mutex, Notify};
use tracing::Instrument;

use crate::http_layer::TokenRegistry;
use crate::runtime::{
    AppState, DEFAULT_THREAD_IDLE_TTL, build_host, ensure_thread, lookup_thread, snapshot_threads,
};
use crate::telemetry::SpanIdentity;

const IDEMPOTENCY_HEADER: &str = "x-idempotency-key";

/// Turn requests can carry base64 `input_parts` images (the server-side
/// per-turn inline budget is 8 MiB of raw bytes ≈ 11 MB encoded); axum's
/// 2 MB default body limit would 413 them before the handler runs.
const TURN_BODY_LIMIT_BYTES: usize = 32 * 1024 * 1024;
use crate::wire::{
    RunnerContent, RunnerStateResponse, ThreadInterruptResponse, ThreadStateView,
    ThreadTurnRequest, ThreadTurnResponse,
};

pub struct ServeConfig {
    pub addr: SocketAddr,
    pub server_url: String,
    pub initial_token: String,
    /// Shared with the span processor registered in `init_tracing` so spans
    /// pick up the identity as soon as the host learns it.
    pub identity: Arc<SpanIdentity>,
}

pub async fn serve(config: ServeConfig) -> Result<(), std::io::Error> {
    let shutdown = Arc::new(Notify::new());
    let host = build_host(
        config.identity,
        config.server_url,
        config.initial_token,
        DEFAULT_THREAD_IDLE_TTL,
    )
    .await
    .map_err(std::io::Error::other)?;

    let app = Router::new()
        .route("/healthz", get(healthz))
        .route("/state", get(state_handler))
        .route("/threads/{thread_id}/turn", post(thread_turn))
        .route("/threads/{thread_id}/interrupt", post(thread_interrupt))
        .layer(DefaultBodyLimit::max(TURN_BODY_LIMIT_BYTES))
        .with_state(host);

    let listener = TcpListener::bind(config.addr).await?;
    let shutdown_wait = shutdown.clone();
    axum::serve(listener, app)
        .with_graceful_shutdown(async move {
            shutdown_wait.notified().await;
            tracing::info!("graceful shutdown requested — draining in-flight requests");
        })
        .await?;
    Ok(())
}

async fn healthz() -> &'static str {
    "ok"
}

async fn state_handler(State(host): State<AppState>) -> Json<RunnerStateResponse> {
    let snapshot = snapshot_threads(&host);
    Json(RunnerStateResponse {
        assistant_id: host
            .identity
            .assistant_id
            .get()
            .cloned()
            .unwrap_or_default(),
        uptime_seconds: host.started_at.elapsed().as_secs(),
        threads: snapshot
            .into_iter()
            .map(|(thread_id, chat_id, idle)| ThreadStateView {
                thread_id,
                chat_id,
                idle_seconds: idle.as_secs(),
            })
            .collect(),
    })
}

/// Stops the turn in flight on a thread.
///
/// Never bootstraps: a thread this VM has not configured has no turn to stop,
/// and bringing one up here would spend a bootstrap fetch to cancel nothing.
/// That case answers 200 with `interrupted: false` rather than 404 — the
/// caller's intent ("this thread should not be generating") holds either way,
/// and the server treats a miss as success so a stop the user pressed just
/// after the reply landed does not surface as an error. A thread this VM holds
/// but which is idle between turns answers `interrupted: false` for the same
/// reason: nothing was generating.
///
/// The response is an ack, like /turn: the loop unwinds its cancelled turn on
/// the per-thread task, and the cancelled turn's partial output has already
/// gone out through /chat/completions.
async fn thread_interrupt(
    State(host): State<AppState>,
    Path(thread_id): Path<String>,
) -> Result<Json<ThreadInterruptResponse>, (StatusCode, String)> {
    if thread_id.is_empty() {
        return Err((StatusCode::BAD_REQUEST, "missing thread_id".to_string()));
    }

    let Some(thread) = lookup_thread(&host, &thread_id) else {
        tracing::info!(thread_id = %thread_id, "interrupt: no live thread on this runtime");
        return Ok(Json(ThreadInterruptResponse { interrupted: false }));
    };

    let interrupted = thread.interrupt();
    tracing::info!(thread_id = %thread_id, interrupted, "interrupting thread turn");
    Ok(Json(ThreadInterruptResponse { interrupted }))
}

async fn thread_turn(
    State(host): State<AppState>,
    Path(thread_id): Path<String>,
    headers: HeaderMap,
    Json(request): Json<ThreadTurnRequest>,
) -> Result<Json<ThreadTurnResponse>, (StatusCode, String)> {
    let span = tracing::info_span!("thread_turn", thread_id = %thread_id);
    thread_turn_inner(host, thread_id, headers, request)
        .instrument(span)
        .await
}

async fn thread_turn_inner(
    host: AppState,
    thread_id: String,
    headers: HeaderMap,
    request: ThreadTurnRequest,
) -> Result<Json<ThreadTurnResponse>, (StatusCode, String)> {
    if thread_id.is_empty() {
        return Err((StatusCode::BAD_REQUEST, "missing thread_id".to_string()));
    }

    let token = request
        .auth_token
        .as_ref()
        .filter(|t| !t.trim().is_empty())
        .cloned()
        .ok_or((StatusCode::UNAUTHORIZED, "missing auth_token".into()))?;
    // Authenticate every invocation, including warm turns and duplicate requests,
    // before allocating admission/thread entries. Tokens remain opaque.
    let bootstrap = host
        .gram_client
        .fetch_bootstrap(&thread_id, &TokenRegistry::new(token.clone()))
        .await
        .map_err(|error| {
            let status = match error {
                crate::gram_client::GramClientError::Status { status: 401, .. } => {
                    StatusCode::UNAUTHORIZED
                }
                crate::gram_client::GramClientError::Status { status: 403, .. } => {
                    StatusCode::FORBIDDEN
                }
                _ => StatusCode::SERVICE_UNAVAILABLE,
            };
            (status, "bootstrap authorization failed".into())
        })?;
    crate::mcp_actor::validate_endpoint(&bootstrap.completions_url).map_err(|_| {
        (
            StatusCode::BAD_REQUEST,
            "invalid completions endpoint".into(),
        )
    })?;
    for server in request
        .mcp_servers
        .as_ref()
        .unwrap_or(&bootstrap.mcp_servers)
    {
        crate::mcp_actor::validated_server_headers(server)
            .map_err(|_| (StatusCode::BAD_REQUEST, "invalid MCP configuration".into()))?;
    }
    let _admission = host.admission.lock().await;
    for (cell, actual, hint) in [
        (
            &host.identity.assistant_id,
            &bootstrap.assistant_id,
            request.assistant_id.as_deref(),
        ),
        (
            &host.identity.project_id,
            &bootstrap.project_id,
            request.project_id.as_deref(),
        ),
    ] {
        if actual.is_empty()
            || hint.is_some_and(|h| h != actual)
            || cell.get().is_some_and(|bound| bound != actual)
        {
            return Err((
                StatusCode::FORBIDDEN,
                "conflicting bootstrap identity".into(),
            ));
        }
    }
    SpanIdentity::bind(&host.identity.assistant_id, Some(&bootstrap.assistant_id));
    SpanIdentity::bind(&host.identity.project_id, Some(&bootstrap.project_id));

    // Idempotency key is namespaced by thread so two threads sharing an
    // event_id namespace can't collide.
    let idempotency_key = headers
        .get(IDEMPOTENCY_HEADER)
        .and_then(|v| v.to_str().ok())
        .map(|s| format!("{thread_id}:{s}"));

    // Per-key admission lock: serialize concurrent retries with the same
    // key across the bootstrap + enqueue window so we can't enqueue twice.
    // A failed admission drops the guard with `*done == false`, leaving
    // the slot available for a fresh retry.
    let admission = idempotency_key.as_ref().map(|key| {
        host.seen
            .entry(key.clone())
            .or_insert_with(|| Arc::new(Mutex::new(false)))
            .clone()
    });
    let mut admission_guard = if let Some(ref slot) = admission {
        Some(slot.lock().await)
    } else {
        None
    };
    if let Some(ref guard) = admission_guard
        && **guard
    {
        tracing::info!(key = ?idempotency_key, "dedup: skipping already-queued turn");
        return Ok(Json(ThreadTurnResponse::deduped()));
    }

    let thread = match ensure_thread(&host, &thread_id, bootstrap, token.clone()).await {
        Ok(thread) => thread,
        Err(error) => {
            if let Some(key) = &idempotency_key {
                host.seen.remove(key);
            }
            return Err((StatusCode::SERVICE_UNAVAILABLE, error.to_string()));
        }
    };

    if let Err(error) = thread.enqueue(
        RunnerContent::from_turn(request.input, request.input_parts),
        token,
        request.mcp_servers,
    ) {
        if let Some(key) = &idempotency_key {
            host.seen.remove(key);
        }
        host.threads.remove_if(&thread_id, |_, cell| {
            cell.get()
                .is_some_and(|current| Arc::ptr_eq(current, &thread))
        });
        return Err((StatusCode::SERVICE_UNAVAILABLE, error.to_string()));
    }

    if let Some(ref mut guard) = admission_guard {
        **guard = true;
    }

    // The model's response goes out via /chat/completions on the
    // per-thread task; the HTTP response here is just an ack so the
    // backend's RunTurn activity can mark the event processed without
    // blocking on the turn.
    Ok(Json(ThreadTurnResponse::accepted()))
}

#[cfg(test)]
#[allow(clippy::unwrap_used)]
mod tests {
    use super::*;
    use crate::runtime::ConfiguredThread;
    use agentkit_core::CancellationController;
    use std::sync::atomic::{AtomicUsize, Ordering};

    fn request(token: Option<&str>, hint: Option<&str>) -> ThreadTurnRequest {
        serde_json::from_value(
            serde_json::json!({"input":"test", "auth_token":token, "assistant_id":hint}),
        )
        .unwrap()
    }

    #[tokio::test]
    async fn every_invocation_authenticates_before_admission_and_binds_bootstrap_identity() {
        let calls = Arc::new(AtomicUsize::new(0));
        let counter = calls.clone();
        let app = Router::new().route("/rpc/assistants.getThreadBootstrap", post(move |headers: HeaderMap| {
            let counter = counter.clone();
            async move {
                counter.fetch_add(1, Ordering::SeqCst);
                if headers.get("authorization").and_then(|h| h.to_str().ok()) != Some("Bearer opaque-valid") {
                    return (StatusCode::UNAUTHORIZED, "denied".to_string());
                }
                (StatusCode::OK, serde_json::json!({"assistant_id":"authenticated-assistant", "project_id":"authenticated-project", "model":"test", "chat_id":"chat", "completions_url":"http://127.0.0.1/completions"}).to_string())
            }
        }));
        let listener = TcpListener::bind("127.0.0.1:0").await.unwrap();
        let addr = listener.local_addr().unwrap();
        let task = tokio::spawn(async move {
            axum::serve(listener, app).await.unwrap();
        });
        let host = build_host(
            Arc::new(SpanIdentity::default()),
            format!("http://{addr}"),
            "ambient-must-not-work".into(),
            DEFAULT_THREAD_IDLE_TTL,
        )
        .await
        .unwrap();
        let mut headers = HeaderMap::new();
        headers.insert(IDEMPOTENCY_HEADER, "event".parse().unwrap());
        let missing = thread_turn_inner(
            host.clone(),
            "T".into(),
            headers.clone(),
            request(None, None),
        )
        .await
        .unwrap_err();
        assert_eq!(missing.0, StatusCode::UNAUTHORIZED);
        assert_eq!(calls.load(Ordering::SeqCst), 0);
        let denied = thread_turn_inner(
            host.clone(),
            "T".into(),
            headers.clone(),
            request(Some("invalid"), None),
        )
        .await
        .unwrap_err();
        assert_eq!(denied.0, StatusCode::UNAUTHORIZED);
        assert!(host.seen.is_empty() && host.threads.is_empty());
        let conflict = thread_turn_inner(
            host.clone(),
            "T".into(),
            headers.clone(),
            request(Some("opaque-valid"), Some("untrusted-hint")),
        )
        .await
        .unwrap_err();
        assert_eq!(conflict.0, StatusCode::FORBIDDEN);
        assert!(host.identity.assistant_id.get().is_none());
        assert!(host.seen.is_empty() && host.threads.is_empty());

        for invalid_headers in [
            std::collections::BTreeMap::from([("bad header".into(), "value".into())]),
            std::collections::BTreeMap::from([("x-test".into(), "bad\nvalue".into())]),
        ] {
            let mut invalid_config = request(Some("opaque-valid"), None);
            invalid_config.mcp_servers = Some(vec![crate::wire::McpServer {
                id: "same-id".into(),
                url: "http://127.0.0.1/mcp".into(),
                headers: invalid_headers,
            }]);
            let rejected =
                thread_turn_inner(host.clone(), "T".into(), headers.clone(), invalid_config)
                    .await
                    .unwrap_err();
            assert_eq!(rejected.0, StatusCode::BAD_REQUEST);
            assert!(host.seen.is_empty() && host.threads.is_empty());
            assert!(host.identity.assistant_id.get().is_none());
        }

        // Install warm state without a model network call; retain the inbox.
        let (tx, mut rx) = tokio::sync::mpsc::unbounded_channel();
        let warm = Arc::new(ConfiguredThread {
            thread_id: "T".into(),
            chat_id: "chat".into(),
            idle_since: Arc::new(std::sync::Mutex::new(Some(std::time::Instant::now()))),
            inbox_tx: tx,
            task_handle: std::sync::Mutex::new(None),
            cancellation: CancellationController::new(),
        });
        let cell = Arc::new(tokio::sync::OnceCell::new());
        assert!(cell.set(warm).is_ok());
        host.threads.insert("T".into(), cell);
        let accepted = thread_turn_inner(
            host.clone(),
            "T".into(),
            headers.clone(),
            request(Some("opaque-valid"), None),
        )
        .await
        .unwrap();
        assert_eq!(accepted.0.finish_reason, "accepted");
        assert_eq!(rx.try_recv().unwrap().token, "opaque-valid");
        assert_eq!(
            host.identity.assistant_id.get().map(String::as_str),
            Some("authenticated-assistant")
        );
        let deduped = thread_turn_inner(
            host.clone(),
            "T".into(),
            headers.clone(),
            request(Some("opaque-valid"), None),
        )
        .await
        .unwrap();
        assert_eq!(deduped.0.finish_reason, "deduped");
        let denied_duplicate = thread_turn_inner(
            host.clone(),
            "T".into(),
            headers,
            request(Some("invalid"), None),
        )
        .await
        .unwrap_err();
        assert_eq!(denied_duplicate.0, StatusCode::UNAUTHORIZED);
        assert_eq!(calls.load(Ordering::SeqCst), 7);
        assert!(rx.try_recv().is_err());
        task.abort();
    }
}
