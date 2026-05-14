use std::net::SocketAddr;
use std::sync::Arc;

use axum::extract::{Path, State};
use axum::http::{HeaderMap, StatusCode};
use axum::routing::{get, post};
use axum::{Json, Router};
use tokio::net::TcpListener;
use tokio::sync::{Mutex, Notify};

use crate::runtime::{
    AppState, DEFAULT_THREAD_IDLE_TTL, build_host, ensure_thread, snapshot_threads,
};

const IDEMPOTENCY_HEADER: &str = "x-idempotency-key";
use crate::wire::{RunnerStateResponse, ThreadStateView, ThreadTurnRequest, ThreadTurnResponse};

pub struct ServeConfig {
    pub addr: SocketAddr,
    pub assistant_id: String,
    pub server_url: String,
    pub initial_token: String,
}

pub async fn serve(config: ServeConfig) -> Result<(), std::io::Error> {
    let shutdown = Arc::new(Notify::new());
    let host = build_host(
        config.assistant_id,
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
        assistant_id: host.assistant_id.clone(),
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

async fn thread_turn(
    State(host): State<AppState>,
    Path(thread_id): Path<String>,
    headers: HeaderMap,
    Json(request): Json<ThreadTurnRequest>,
) -> Result<Json<ThreadTurnResponse>, (StatusCode, String)> {
    if thread_id.is_empty() {
        return Err((StatusCode::BAD_REQUEST, "missing thread_id".to_string()));
    }

    // Rotate the shared bearer ahead of bootstrap so the runner's outbound
    // call to /rpc/assistants.getThreadBootstrap uses the freshest token
    // the backend just minted for this admit.
    if let Some(token) = request.auth_token.as_deref()
        && !token.is_empty()
    {
        host.tokens
            .rotate(token)
            .map_err(|e| (StatusCode::INTERNAL_SERVER_ERROR, e.to_string()))?;
    }

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

    let thread = ensure_thread(&host, &thread_id)
        .await
        .map_err(|e| (StatusCode::SERVICE_UNAVAILABLE, e.to_string()))?;

    thread
        .enqueue(request.input)
        .map_err(|e| (StatusCode::SERVICE_UNAVAILABLE, e.to_string()))?;

    if let Some(ref mut guard) = admission_guard {
        **guard = true;
    }

    // The model's response goes out via /chat/completions on the per-thread
    // task; the HTTP response here is just an ack so the backend's RunTurn
    // activity can mark the event processed without blocking on the turn.
    Ok(Json(ThreadTurnResponse::accepted()))
}
