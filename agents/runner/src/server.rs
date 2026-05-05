use std::net::SocketAddr;
use std::sync::Arc;

use axum::extract::State;
use axum::http::{HeaderMap, StatusCode};
use axum::routing::{get, post};
use axum::{Json, Router};
use tokio::net::TcpListener;
use tokio::sync::{Mutex, Notify};

use crate::idempotency;
use crate::runtime::{AppState, RuntimeHost, build_runtime};
use crate::wire::{RunnerConfig, RunnerRequest, RunnerResponse, RunnerStateResponse};

pub async fn serve(addr: SocketAddr) -> Result<(), std::io::Error> {
    let shutdown = Arc::new(Notify::new());
    let state: AppState = Arc::new(Mutex::new(RuntimeHost {
        runtime: None,
        seen: Default::default(),
        shutdown: Arc::clone(&shutdown),
    }));

    let app = Router::new()
        .route("/healthz", get(healthz))
        .route("/state", get(state_handler))
        .route("/configure", post(configure))
        .route("/turn", post(turn))
        .with_state(state);

    let listener = TcpListener::bind(addr).await?;
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

async fn state_handler(State(state): State<AppState>) -> Json<RunnerStateResponse> {
    let guard = state.lock().await;
    Json(RunnerStateResponse {
        configured: guard.runtime.is_some(),
        idle_seconds: guard
            .runtime
            .as_ref()
            .and_then(|rt| rt.idle_for())
            .map(|d| d.as_secs()),
    })
}

async fn configure(
    State(state): State<AppState>,
    Json(config): Json<RunnerConfig>,
) -> Result<StatusCode, (StatusCode, String)> {
    let mut guard = state.lock().await;

    // Idempotent re-entry: a matching config (ignoring auth_token) rotates the
    // token and returns 204, mirroring /turn's token-rotate behavior so callers
    // retrying after a refresh aren't stuck on the stale one. Different config
    // on the same runtime is a real conflict — 409.
    if let Some(ref runtime) = guard.runtime {
        if !runtime.matches(&config) {
            return Err((
                StatusCode::CONFLICT,
                "runner is already configured with a different config".to_string(),
            ));
        }
        runtime
            .rotate_token(&config.auth_token)
            .map_err(|e| (StatusCode::INTERNAL_SERVER_ERROR, e.to_string()))?;
        return Ok(StatusCode::NO_CONTENT);
    }

    let runtime = build_runtime(&config, Arc::clone(&guard.shutdown))
        .await
        .map_err(|e| (e.configure_status_code(), e.to_string()))?;

    guard.runtime = Some(runtime);
    Ok(StatusCode::NO_CONTENT)
}

async fn turn(
    State(state): State<AppState>,
    headers: HeaderMap,
    Json(request): Json<RunnerRequest>,
) -> Result<Json<RunnerResponse>, (StatusCode, String)> {
    let idempotency_key = headers
        .get(idempotency::HEADER)
        .and_then(|v| v.to_str().ok())
        .map(|s| s.to_string());

    let mut guard = state.lock().await;

    if let Some(ref key) = idempotency_key
        && guard.seen.contains(key)
    {
        tracing::info!(key = %key, "dedup: skipping already-queued turn");
        return Ok(Json(RunnerResponse::deduped()));
    }

    let runtime = guard
        .runtime
        .as_ref()
        .ok_or_else(|| (StatusCode::CONFLICT, "runner is not configured".to_string()))?;

    if let Some(token) = request.auth_token.as_deref()
        && !token.is_empty()
    {
        runtime
            .rotate_token(token)
            .map_err(|e| (StatusCode::INTERNAL_SERVER_ERROR, e.to_string()))?;
    }

    runtime
        .enqueue(request)
        .map_err(|e| (StatusCode::SERVICE_UNAVAILABLE, e.to_string()))?;

    if let Some(key) = idempotency_key {
        guard.seen.insert(key);
    }

    Ok(Json(RunnerResponse::accepted()))
}

#[cfg(test)]
mod tests {
    use super::*;

    #[tokio::test]
    async fn state_reports_unconfigured_before_configure() {
        let state_value: AppState = Arc::new(Mutex::new(RuntimeHost {
            runtime: None,
            seen: Default::default(),
            shutdown: Arc::new(Notify::new()),
        }));

        let Json(response) = state_handler(State(state_value)).await;

        assert!(!response.configured);
        assert!(response.idle_seconds.is_none());
    }
}
