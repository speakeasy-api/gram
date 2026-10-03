use std::net::SocketAddr;
use std::sync::Arc;

use axum::extract::{DefaultBodyLimit, Path, State};
use axum::http::{HeaderMap, StatusCode};
use axum::routing::{get, post};
use axum::{Json, Router};
use tokio::net::TcpListener;
use tokio::sync::{Mutex, Notify};
use tracing::Instrument;

use crate::mcp_actor::McpCmd;
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

    // Bootstrap is the existing management API authentication boundary. It
    // validates the bearer and pins the thread to its assistant/project. Do not
    // allocate dedup slots, admission locks, actors, or identity cells before it.
    let bearer = request
        .auth_token
        .as_deref()
        .filter(|token| !token.is_empty())
        .ok_or_else(|| {
            (
                StatusCode::UNAUTHORIZED,
                "missing invocation token".to_string(),
            )
        })?;
    let tokens = crate::http_layer::TokenRegistry::new(bearer);
    let bootstrap = host
        .gram_client
        .fetch_bootstrap(&thread_id, &tokens)
        .await
        .map_err(|error| {
            let status = match &error {
                crate::gram_client::GramClientError::Status {
                    status: 401 | 403, ..
                } => StatusCode::UNAUTHORIZED,
                _ => StatusCode::SERVICE_UNAVAILABLE,
            };
            (status, "invocation authentication unavailable".to_string())
        })?;
    SpanIdentity::bind_request(&host.identity.assistant_id, request.assistant_id.as_deref());
    SpanIdentity::bind_request(&host.identity.project_id, request.project_id.as_deref());

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

    // Independent idempotency keys must not race to claim a freshly bootstrapped
    // invocation. Hold this through reconcile and enqueue; never rotate the
    // credentials of an earlier accepted event.
    let admission = crate::runtime::admission_lock(&host, &thread_id);
    let _turn_admission = admission.lock().await;

    let thread = ensure_thread(&host, &thread_id, bootstrap, tokens)
        .await
        .map_err(|e| {
            let status = if matches!(e, crate::errors::RunnerError::InvocationBusy) {
                StatusCode::TOO_MANY_REQUESTS
            } else {
                StatusCode::SERVICE_UNAVAILABLE
            };
            (status, e.to_string())
        })?;

    let mut unclaimed = crate::runtime::UnclaimedInvocation::new(host.clone(), thread.clone());

    // Reconciliation belongs to this invocation and completes before its first
    // model step. Its notice travels with input even when no tool is called.
    let notice = if let Some(desired) = request.mcp_servers {
        let (reply, response) = tokio::sync::oneshot::channel();
        let reconcile = async {
            thread
                .mcp_cmd_tx
                .send(McpCmd::Reconcile { desired, reply })
                .await
                .map_err(|_| "mcp reconcile actor unavailable")?;
            response
                .await
                .map_err(|_| "mcp reconcile response unavailable")
        };
        let result = tokio::time::timeout(std::time::Duration::from_secs(30), reconcile)
            .await
            .unwrap_or(Err("mcp reconciliation timed out"));
        match result {
            Ok(notice) => notice,
            Err(reason) => {
                tracing::warn!(thread_id = %thread_id, failure = reason, "mcp reconciliation failed before enqueue");
                crate::runtime::discard_unclaimed(&host, &thread).await;
                return Err((
                    StatusCode::SERVICE_UNAVAILABLE,
                    "mcp reconciliation unavailable".into(),
                ));
            }
        }
    } else {
        None
    };

    if let Err(err) = thread.enqueue(
        RunnerContent::from_turn(request.input, request.input_parts),
        notice,
    ) {
        crate::runtime::discard_unclaimed(&host, &thread).await;
        return Err((StatusCode::SERVICE_UNAVAILABLE, err.to_string()));
    }

    unclaimed.accept();

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
#[allow(clippy::unwrap_used, clippy::expect_used, clippy::panic)]
mod tests {
    use super::*;
    use std::time::Duration;

    #[tokio::test]
    async fn unauthenticated_request_cannot_hold_same_thread_admission() {
        let started = Arc::new(Notify::new());
        let release = Arc::new(Notify::new());
        let app = Router::new().route("/rpc/assistants.getThreadBootstrap", post({
            let started = started.clone();
            let release = release.clone();
            move |headers: HeaderMap| {
                let started = started.clone();
                let release = release.clone();
                async move {
                    if headers.get("authorization").unwrap() != "Bearer valid" {
                        started.notify_one();
                        release.notified().await;
                        return (StatusCode::UNAUTHORIZED, "invalid");
                    }
                    (StatusCode::OK, r#"{"model":"test","completions_url":"http://localhost","chat_id":"chat"}"#)
                }
            }
        }));
        let listener = TcpListener::bind("127.0.0.1:0").await.unwrap();
        let url = format!("http://{}", listener.local_addr().unwrap());
        let server = tokio::spawn(async move {
            axum::serve(listener, app).await.unwrap();
        });
        let host = build_host(
            Arc::new(SpanIdentity::default()),
            url,
            "ambient-must-not-authenticate".into(),
            Duration::from_secs(60),
        )
        .await
        .unwrap();
        let missing = serde_json::from_str(r#"{"input":"test"}"#).unwrap();
        let error = thread_turn_inner(host.clone(), "shared".into(), HeaderMap::new(), missing)
            .await
            .err()
            .unwrap();
        assert_eq!(error.0, StatusCode::UNAUTHORIZED);
        assert!(host.seen.is_empty());
        assert!(host.turn_admissions.is_empty());
        let invalid_host = host.clone();
        let invalid = tokio::spawn(async move {
            let request =
                serde_json::from_str(r#"{"input":"test","auth_token":"invalid"}"#).unwrap();
            thread_turn_inner(invalid_host, "shared".into(), HeaderMap::new(), request).await
        });
        started.notified().await;
        assert!(host.seen.is_empty());
        assert!(host.turn_admissions.is_empty());
        assert!(host.threads.is_empty());
        // A valid retry of an already accepted event can finish authentication
        // and dedup while the unauthenticated request remains stuck upstream.
        host.seen
            .insert("shared:accepted".into(), Arc::new(Mutex::new(true)));
        let mut headers = HeaderMap::new();
        headers.insert(IDEMPOTENCY_HEADER, "accepted".parse().unwrap());
        let valid = serde_json::from_str(r#"{"input":"test","auth_token":"valid"}"#).unwrap();
        assert!(
            tokio::time::timeout(
                Duration::from_secs(2),
                thread_turn_inner(host.clone(), "shared".into(), headers, valid)
            )
            .await
            .unwrap()
            .is_ok()
        );
        release.notify_one();
        assert_eq!(
            invalid.await.unwrap().err().unwrap().0,
            StatusCode::UNAUTHORIZED
        );
        assert!(host.turn_admissions.is_empty());
        assert!(host.threads.is_empty());
        server.abort();
    }
}
