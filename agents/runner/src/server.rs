use std::net::SocketAddr;
use std::sync::Arc;

use axum::extract::{DefaultBodyLimit, Path, State};
use axum::http::{HeaderMap, StatusCode};
use axum::routing::{get, post};
use axum::{Json, Router};
use tokio::net::TcpListener;
use tokio::sync::Notify;
use tracing::Instrument;

use crate::runtime::{
    AppState, DEFAULT_THREAD_IDLE_TTL, build_host, ensure_thread, lookup_thread, snapshot_threads,
};
use crate::telemetry::SpanIdentity;

// Under the server's 30-minute /turn budget: 25 minutes of queue wait plus
// one 30-minute turn fits the shared 60-minute token lifetime.
const QUEUE_ADMISSION_TIMEOUT: std::time::Duration = std::time::Duration::from_secs(25 * 60);

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
    thread_turn_inner(host, thread_id, headers, request).await
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
    // Only authenticated bootstrap may establish permanent runtime identity.
    // Older servers omit these fields; never substitute untrusted request hints.
    for (cell, authenticated, hint) in [
        (
            &host.identity.assistant_id,
            bootstrap.assistant_id.as_deref(),
            request.assistant_id.as_deref(),
        ),
        (
            &host.identity.project_id,
            bootstrap.project_id.as_deref(),
            request.project_id.as_deref(),
        ),
    ] {
        if let Some(id) = authenticated
            && (hint.is_some_and(|hint| hint != id)
                || cell.get().is_some_and(|existing| existing != id))
        {
            return Err((
                StatusCode::UNAUTHORIZED,
                "invocation identity mismatch".into(),
            ));
        }
    }
    for (cell, authenticated) in [
        (
            &host.identity.assistant_id,
            bootstrap.assistant_id.as_deref(),
        ),
        (&host.identity.project_id, bootstrap.project_id.as_deref()),
    ] {
        if let Some(id) = authenticated {
            SpanIdentity::bind_request(cell, Some(id));
            if cell.get().is_none_or(|bound| bound != id) {
                return Err((
                    StatusCode::UNAUTHORIZED,
                    "invocation identity mismatch".into(),
                ));
            }
        }
    }
    let span = tracing::info_span!("thread_turn", thread_id = %thread_id);
    tokio::time::timeout(
        QUEUE_ADMISSION_TIMEOUT,
        admit_authenticated_turn(host, thread_id, headers, request, bootstrap, tokens)
            .instrument(span),
    )
    .await
    .map_err(|_| {
        (
            StatusCode::TOO_MANY_REQUESTS,
            crate::errors::RunnerError::InvocationBusy.to_string(),
        )
    })?
}

async fn admit_authenticated_turn(
    host: AppState,
    thread_id: String,
    headers: HeaderMap,
    request: ThreadTurnRequest,
    bootstrap: crate::wire::ThreadBootstrap,
    tokens: crate::http_layer::TokenRegistry,
) -> Result<Json<ThreadTurnResponse>, (StatusCode, String)> {
    // Idempotency key is namespaced by thread so two threads sharing an
    // event_id namespace can't collide.
    let idempotency_key = headers
        .get(IDEMPOTENCY_HEADER)
        .and_then(|v| v.to_str().ok())
        .map(|s| format!("{thread_id}:{s}"));

    // Declare the slot owner before the borrowed mutex guard: on every return
    // or cancellation the lock drops first, then the last unsuccessful owner
    // can retire the slot without separating existing waiters from new retries.
    let event_admission = idempotency_key
        .as_ref()
        .map(|key| crate::runtime::EventAdmission::new(host.clone(), key.clone()));
    let _admission_guard = if let Some(ref admission) = event_admission {
        Some(admission.slot.gate.lock().await)
    } else {
        None
    };
    if event_admission.as_ref().is_some_and(|admission| {
        admission
            .slot
            .accepted
            .load(std::sync::atomic::Ordering::Acquire)
    }) {
        return Ok(Json(ThreadTurnResponse::deduped()));
    }

    // Serialize bootstrap and enqueue for independent event keys. Queued tuples
    // cannot reconcile tools or rotate the active turn's credentials.
    let admission = crate::runtime::admission_lock(&host, &thread_id);
    let _turn_admission = admission.lock().await;

    let _ = request.mcp_servers; // Authenticated bootstrap, not request overrides, selects destinations.
    let (reply, admitted) = tokio::sync::oneshot::channel();
    let accepted = event_admission
        .as_ref()
        .map(|admission| admission.slot.accepted.clone())
        .unwrap_or_else(|| Arc::new(std::sync::atomic::AtomicBool::new(false)));
    let turn = crate::runtime::QueuedTurn {
        input: RunnerContent::from_turn(request.input, request.input_parts),
        bearer: tokens
            .current()
            .map_err(|e| (StatusCode::SERVICE_UNAVAILABLE, e.to_string()))?,
        // Only authenticated bootstrap state may choose credential destinations.
        mcp_servers: bootstrap.mcp_servers.clone(),
        admission: Some(crate::runtime::TurnAdmission {
            event: event_admission.clone(),
            accepted,
            reply,
        }),
    };
    let thread = ensure_thread(&host, &thread_id, bootstrap, tokens)
        .await
        .map_err(|e| (StatusCode::SERVICE_UNAVAILABLE, e.to_string()))?;

    {
        let _submission = event_admission
            .as_ref()
            .map(|event| event.slot.submit.lock())
            .transpose()
            .map_err(|_| {
                (
                    StatusCode::SERVICE_UNAVAILABLE,
                    "admission lock poisoned".into(),
                )
            })?;
        if event_admission.as_ref().is_some_and(|event| {
            event
                .slot
                .accepted
                .load(std::sync::atomic::Ordering::Acquire)
        }) {
            return Ok(Json(ThreadTurnResponse::deduped()));
        }
        thread.enqueue(turn).map_err(|e| {
            let status = if matches!(e, crate::errors::RunnerError::InvocationBusy) {
                StatusCode::TOO_MANY_REQUESTS
            } else {
                StatusCode::SERVICE_UNAVAILABLE
            };
            (status, e.to_string())
        })?;
    }
    drop(_turn_admission);
    // Pending tuples remain durable at the server until the loop admits them.
    // A loop/activation failure drops this sender and leaves the event retryable.
    admitted.await.map_err(|_| {
        (
            StatusCode::SERVICE_UNAVAILABLE,
            "turn admission failed".into(),
        )
    })?;

    Ok(Json(ThreadTurnResponse::accepted()))
}

#[cfg(test)]
#[allow(clippy::unwrap_used, clippy::expect_used, clippy::panic)]
mod tests {
    use super::*;
    use std::time::Duration;
    use tokio::sync::Mutex;

    #[tokio::test]
    async fn pending_tuple_uses_only_authenticated_destinations_and_waits_for_admission() {
        let host = build_host(
            Arc::new(SpanIdentity::default()),
            "http://localhost".into(),
            "".into(),
            Duration::from_secs(60),
        )
        .await
        .unwrap();
        let (tx, mut rx) = tokio::sync::mpsc::channel(1);
        let thread = Arc::new(crate::runtime::ConfiguredThread {
            thread_id: "thread".into(),
            chat_id: "chat".into(),
            idle_since: Arc::new(std::sync::Mutex::new(None)),
            inbox_tx: tx,
            task_handle: std::sync::Mutex::new(None),
            cancellation: agentkit_core::CancellationController::new(),
        });
        let cell = Arc::new(tokio::sync::OnceCell::new());
        assert!(cell.set(thread).is_ok());
        host.threads.insert("thread".into(), cell);
        let request = serde_json::from_str(r#"{"input":"message","auth_token":"token","mcp_servers":[{"id":"server","url":"https://untrusted.example/mcp"}]}"#).unwrap();
        let bootstrap = serde_json::from_str(r#"{"model":"test","completions_url":"http://localhost","chat_id":"chat","mcp_servers":[{"id":"server","url":"https://trusted.example/mcp"}]}"#).unwrap();
        let mut headers = HeaderMap::new();
        headers.insert(IDEMPOTENCY_HEADER, "event".parse().unwrap());
        let request_host = host.clone();
        let pending = tokio::spawn(admit_authenticated_turn(
            request_host,
            "thread".into(),
            headers,
            request,
            bootstrap,
            crate::http_layer::TokenRegistry::new("token"),
        ));
        let turn = rx.recv().await.unwrap();
        assert_eq!(turn.mcp_servers[0].url, "https://trusted.example/mcp");
        assert_eq!(turn.bearer, "token");
        assert!(!pending.is_finished(), "queued does not mean accepted");
        let admission = turn.admission.unwrap();
        assert!(
            !admission
                .accepted
                .load(std::sync::atomic::Ordering::Acquire)
        );
        admission
            .accepted
            .store(true, std::sync::atomic::Ordering::Release);
        admission.reply.send(()).unwrap();
        assert!(pending.await.unwrap().is_ok());
        assert!(
            host.seen
                .get("thread:event")
                .unwrap()
                .accepted
                .load(std::sync::atomic::Ordering::Acquire)
        );
    }

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
                    (StatusCode::OK, r#"{"model":"test","completions_url":"http://localhost","chat_id":"chat","assistant_id":"11111111-1111-4111-8111-111111111111","project_id":"22222222-2222-4222-8222-222222222222"}"#)
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
        let forged = serde_json::from_str(r#"{"input":"test","auth_token":"valid","assistant_id":"33333333-3333-4333-8333-333333333333"}"#).unwrap();
        assert_eq!(
            thread_turn_inner(host.clone(), "shared".into(), HeaderMap::new(), forged)
                .await
                .err()
                .unwrap()
                .0,
            StatusCode::UNAUTHORIZED
        );
        assert!(host.identity.assistant_id.get().is_none());
        assert!(host.identity.project_id.get().is_none());
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
        host.seen.insert(
            "shared:accepted".into(),
            Arc::new(crate::runtime::EventSlot {
                submit: std::sync::Mutex::new(()),
                gate: Mutex::new(()),
                accepted: Arc::new(std::sync::atomic::AtomicBool::new(true)),
            }),
        );
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
        assert_eq!(
            host.identity.assistant_id.get().unwrap(),
            "11111111-1111-4111-8111-111111111111"
        );
        assert_eq!(
            host.identity.project_id.get().unwrap(),
            "22222222-2222-4222-8222-222222222222"
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
