use std::net::SocketAddr;
use std::sync::Arc;

use axum::extract::{Path, State};
use axum::http::{HeaderMap, StatusCode};
use axum::routing::{get, post};
use axum::{Json, Router};
use tokio::net::TcpListener;
use tokio::sync::{Mutex, Notify};

use crate::runtime::{
    AppState, DEFAULT_THREAD_IDLE_TTL, McpCmd, build_host, ensure_thread, snapshot_threads,
};

const IDEMPOTENCY_HEADER: &str = "x-idempotency-key";
use crate::wire::{RunnerStateResponse, ThreadStateView, ThreadTurnRequest, ThreadTurnResponse};
use crate::workspace::{self, WorkspaceMonitorConfig};

pub struct ServeConfig {
    pub addr: SocketAddr,
    pub assistant_id: Option<String>,
    pub server_url: String,
    pub initial_token: String,
    pub workspace_monitor: Option<WorkspaceMonitorConfig>,
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
    if let Some(workspace_config) = config.workspace_monitor {
        workspace::spawn_monitor(
            workspace_config,
            host.gram_client.clone(),
            host.workspace_tokens.clone(),
        );
    }

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
        assistant_id: host.assistant_id.get().cloned().unwrap_or_default(),
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

    let thread = ensure_thread(&host, &thread_id, request.auth_token)
        .await
        .map_err(|e| (StatusCode::SERVICE_UNAVAILABLE, e.to_string()))?;

    // A warm-pool sandbox boots without GRAM_ASSISTANT_ID and learns its
    // assistant from the first turn that carries one. Bind it only after
    // bootstrap succeeds (so a failed turn can't poison the pod's identity) and
    // never from an empty/whitespace id. Set-once: a boot env value wins.
    if let Some(assistant_id) = request
        .assistant_id
        .as_deref()
        .map(str::trim)
        .filter(|id| !id.is_empty())
    {
        let _ = host.assistant_id.set(assistant_id.to_string());
    }

    // Hand reconcile to the actor and proceed to enqueue. The actor runs
    // concurrently with the agent loop, so a server added by this /turn
    // may surface on the very next model step or on the one after,
    // depending on whether the connect finishes before tool catalog is
    // sampled. Either way it lands before the user notices.
    if let Some(desired) = request.mcp_servers
        && thread
            .mcp_cmd_tx
            .send(McpCmd::Reconcile { desired })
            .await
            .is_err()
    {
        // The MCP actor is gone, so we can't reconcile the thread's server set
        // for this turn. Don't accept the turn on stale state — fail so the
        // backend retries instead of silently running with the wrong tools.
        tracing::warn!(thread_id = %thread_id, "mcp reconcile failed: actor channel closed");
        return Err((
            StatusCode::SERVICE_UNAVAILABLE,
            "mcp reconcile actor unavailable".to_string(),
        ));
    }

    thread
        .enqueue(request.input)
        .map_err(|e| (StatusCode::SERVICE_UNAVAILABLE, e.to_string()))?;

    if let Some(ref mut guard) = admission_guard {
        **guard = true;
    }

    // The model's response goes out via /chat/completions on the
    // per-thread task; the HTTP response here is just an ack so the
    // backend's RunTurn activity can mark the event processed without
    // blocking on the turn.
    Ok(Json(ThreadTurnResponse::accepted()))
}
