use std::net::SocketAddr;
use std::sync::Arc;

use axum::extract::State;
use axum::http::{HeaderMap, StatusCode};
use axum::routing::{get, post};
use axum::{Json, Router};
use tokio::net::TcpListener;
use tokio::sync::Mutex;

use crate::idempotency;
use crate::runtime::{build_runtime, handle_turn, AppState, RuntimeHost};
use crate::wire::{RunnerConfig, RunnerRequest, RunnerResponse, RunnerStateResponse};

pub async fn serve(addr: SocketAddr) -> Result<(), std::io::Error> {
    let state: AppState = Arc::new(Mutex::new(RuntimeHost::default()));

    let app = Router::new()
        .route("/healthz", get(healthz))
        .route("/state", get(state_handler))
        .route("/configure", post(configure))
        .route("/turn", post(turn))
        .with_state(state);

    let listener = TcpListener::bind(addr).await?;
    axum::serve(listener, app).await?;
    Ok(())
}

async fn healthz() -> &'static str {
    "ok"
}

async fn state_handler(State(state): State<AppState>) -> Json<RunnerStateResponse> {
    let guard = state.lock().await;
    Json(RunnerStateResponse {
        configured: guard.configured,
    })
}

async fn configure(
    State(state): State<AppState>,
    Json(config): Json<RunnerConfig>,
) -> Result<StatusCode, (StatusCode, String)> {
    let runtime = build_runtime(&config)
        .await
        .map_err(|e| (StatusCode::BAD_REQUEST, e.to_string()))?;

    let mut guard = state.lock().await;
    guard.set_runtime(runtime);
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
        tracing::info!(key = %key, "dedup: skipping already-processed turn");
        return Ok(Json(RunnerResponse::deduped()));
    }

    let runtime = guard
        .runtime_mut()
        .ok_or_else(|| (StatusCode::CONFLICT, "runner is not configured".to_string()))?;

    let response = handle_turn(runtime, request)
        .await
        .map_err(|e| (StatusCode::INTERNAL_SERVER_ERROR, e.to_string()))?;

    if let Some(key) = idempotency_key {
        guard.seen.insert(key);
    }

    Ok(Json(response))
}

#[cfg(test)]
mod tests {
    use super::*;

    #[tokio::test]
    async fn state_reports_unconfigured_before_configure() {
        let state_value = Arc::new(Mutex::new(RuntimeHost::default()));

        let Json(response) = state_handler(State(state_value)).await;

        assert!(!response.configured);
    }
}
