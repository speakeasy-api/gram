//! Per-thread MCP actor.
//!
//! Owns the thread's [`McpServerManager`] and serializes every connection
//! lifecycle operation — deferred connects, auth flows, reconciliation
//! against toolset edits, and implicit reconnects — behind a command
//! channel. Connection liveness is read from the manager itself rather
//! than mirrored: agentkit's `disconnect_server` removes the handle
//! before `close()` can fail, so any mirror of "connected" drifts on
//! exactly the error paths it would exist to handle.

use std::collections::BTreeMap;
use std::sync::Arc;
use std::time::{Duration, Instant};

use agentkit_mcp::{
    McpError, McpServerConfig, McpServerId, McpServerManager, McpServerOptions,
    McpTransportBinding, StreamableHttpTransportConfig,
};
use agentkit_tools_core::CatalogReader;
use serde::Serialize;
use tokio::sync::mpsc;
use tokio::sync::mpsc::UnboundedSender;
use tokio::sync::oneshot;

use crate::errors::RunnerError;
use crate::gram_client::GramBootstrapClient;
use crate::http_layer::{McpRotatingClient, TokenRegistry};
use crate::wire::McpServer;

const MCP_CMD_CAPACITY: usize = 32;

/// Per-server bound on the MCP discovery handshake at connect time.
const MCP_HANDSHAKE_TIMEOUT: Duration = Duration::from_secs(10);

/// Floor between connect retries against a hard-failing server, and between
/// implicit reconnects of the same server. Every MCP call failure surfaces
/// as `ToolError::ExecutionFailed` (agentkit folds JSON-RPC application
/// errors and transport errors into one variant), so without a cooldown a
/// model looping on bad arguments would pay a full disconnect + handshake
/// per failed call, and every tool_search would re-pay the handshake
/// timeout for a server that is down.
const MCP_RETRY_COOLDOWN: Duration = Duration::from_secs(30);

/// Age past which a stored auth flow URL is re-minted on the next
/// authorization-required connect, in case the earlier flow expired before
/// the owner completed it. Long enough that repeated searches don't spam
/// fresh `assistant_mcp_auth_required` notices.
const MCP_AUTH_URL_TTL: Duration = Duration::from_secs(15 * 60);

pub enum McpCmd {
    /// Sent by `tool_search` before every search. The actor connects any
    /// configured server that is not yet connected (creating an auth flow
    /// for servers that demand one) and replies with the status of every
    /// configured server, so search results always carry live catalog
    /// coverage plus authorization links.
    EnsureConnected {
        reply: oneshot::Sender<Vec<McpServerStatus>>,
    },
    /// Sent by the catalog dispatch layer when an MCP-backed tool call
    /// fails with a transport-shaped error. The actor reseats the owning
    /// server's connection; the failed call is not replayed.
    ReconnectTool {
        tool_name: String,
        reply: oneshot::Sender<Result<(), String>>,
    },
    /// Sent by the serialized loop after the prior turn and compaction.
    /// Clears credential-specific MCP sessions/auth/retry state before rotating
    /// the opaque token, then applies the queued desired server configuration.
    /// The loop awaits acknowledgement before submitting input. Connections
    /// stay deferred to the next `EnsureConnected`.
    TurnBoundary {
        token: String,
        desired: Option<Vec<McpServer>>,
        reply: oneshot::Sender<Result<(), RunnerError>>,
    },
}

#[derive(Clone, Copy, Debug, PartialEq, Eq, Serialize)]
#[serde(rename_all = "snake_case")]
pub enum McpServerConnectionStatus {
    Connected,
    AuthorizationRequired,
    Unavailable,
}

/// Per-server view returned to `tool_search`, embedded verbatim in its
/// tool result.
#[derive(Clone, Debug, Serialize)]
pub struct McpServerStatus {
    pub id: String,
    pub status: McpServerConnectionStatus,
    #[serde(skip_serializing_if = "Vec::is_empty")]
    pub tools: Vec<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub authorization_url: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub error: Option<String>,
}

/// Registers the bootstrap server set (without connecting — thread
/// bootstrap, and therefore first-token latency, is never gated on MCP
/// handshakes) and spawns the actor task.
pub fn spawn_mcp_actor(
    gram_client: GramBootstrapClient,
    http_client: reqwest::Client,
    thread_id: &str,
    servers: &[McpServer],
    tokens: &TokenRegistry,
    inbox_tx: UnboundedSender<String>,
) -> Result<(mpsc::Sender<McpCmd>, CatalogReader), RunnerError> {
    let mut manager = McpServerManager::new();
    let catalog = manager.source();
    let mut configured = BTreeMap::new();

    for server in servers {
        let config = build_mcp_server_config(server, &http_client, tokens)?;
        manager.register_server_with_options(
            config,
            McpServerOptions::new().with_timeout(MCP_HANDSHAKE_TIMEOUT),
        );
        configured.insert(server.id.clone(), server.clone());
    }

    let (cmd_tx, cmd_rx) = mpsc::channel(MCP_CMD_CAPACITY);
    let actor = McpActor {
        manager,
        gram_client,
        http_client,
        thread_id: thread_id.to_string(),
        tokens: tokens.clone(),
        inbox_tx,
        auth_pending: BTreeMap::new(),
        last_errors: BTreeMap::new(),
        last_reconnects: BTreeMap::new(),
        configured,
    };
    tokio::spawn(actor.run(cmd_rx));
    Ok((cmd_tx, catalog))
}

struct McpActor {
    manager: McpServerManager,
    gram_client: GramBootstrapClient,
    http_client: reqwest::Client,
    thread_id: String,
    tokens: TokenRegistry,
    inbox_tx: UnboundedSender<String>,
    // Servers whose last connect demanded authorization, mapped to the auth
    // flow (None while flow creation itself is failing). Retried on
    // every EnsureConnected so a completed authorization is picked up
    // without any explicit signal.
    auth_pending: BTreeMap<String, Option<AuthFlow>>,
    // Last transient connect failure per server, with its timestamp for
    // the retry cooldown. Surfaced in statuses.
    last_errors: BTreeMap<String, (String, Instant)>,
    // Last implicit reconnect per server, for the reconnect debounce.
    last_reconnects: BTreeMap<String, Instant>,
    // The assistant's current configuration (latest reconcile's desired
    // set). Drives EnsureConnected's pending diff and gates ReconnectTool
    // so a detached server cannot be resurrected.
    configured: BTreeMap<String, McpServer>,
}

impl McpActor {
    async fn run(mut self, mut cmd_rx: mpsc::Receiver<McpCmd>) {
        while let Some(cmd) = cmd_rx.recv().await {
            match cmd {
                McpCmd::EnsureConnected { reply } => {
                    let statuses = self.ensure_connected().await;
                    let _ = reply.send(statuses);
                }
                McpCmd::ReconnectTool { tool_name, reply } => {
                    let result = self.reconnect_for_tool(&tool_name).await;
                    let _ = reply.send(result);
                }
                McpCmd::TurnBoundary {
                    token,
                    desired,
                    reply,
                } => {
                    let result = self.turn_boundary(token, desired).await;
                    let _ = reply.send(result);
                }
            }
        }
    }

    async fn turn_boundary(
        &mut self,
        token: String,
        desired: Option<Vec<McpServer>>,
    ) -> Result<(), RunnerError> {
        let desired = desired.unwrap_or_else(|| self.configured.values().cloned().collect());
        self.reconcile(desired, token).await
    }

    fn is_connected(&self, id: &str) -> bool {
        self.manager
            .connected_server(&McpServerId::new(id))
            .is_some()
    }

    /// Connects every configured-but-unconnected server (including
    /// auth-pending ones, so a completed authorization is picked up) and
    /// finishes any detach a previous reconcile could not complete.
    /// Returns the status of every configured server.
    async fn ensure_connected(&mut self) -> Vec<McpServerStatus> {
        let stale: Vec<String> = self
            .manager
            .connected_servers()
            .iter()
            .map(|handle| handle.server_id().0.clone())
            .filter(|id| !self.configured.contains_key(id))
            .collect();
        for id in stale {
            self.drop_connection(&id, "stale_detach").await;
        }

        let pending: Vec<String> = self
            .configured
            .keys()
            .filter(|id| !self.is_connected(id))
            .cloned()
            .collect();
        for id in pending {
            // Auth-pending servers fail fast, so they retry unconditionally;
            // hard failures wait out the cooldown to keep repeated searches
            // from re-paying the handshake timeout for a down server.
            if !self.auth_pending.contains_key(&id)
                && let Some((_, at)) = self.last_errors.get(&id)
                && at.elapsed() < MCP_RETRY_COOLDOWN
            {
                continue;
            }
            match self.connect_one(&id).await {
                Ok(()) => {
                    self.auth_pending.remove(&id);
                    self.last_errors.remove(&id);
                }
                Err(failure) if failure.auth_required => {
                    self.last_errors.remove(&id);
                    self.ensure_auth_flow(&id).await;
                }
                Err(failure) => {
                    self.last_errors
                        .insert(id, (failure.message, Instant::now()));
                }
            }
        }

        self.server_statuses()
    }

    fn server_statuses(&self) -> Vec<McpServerStatus> {
        self.configured
            .keys()
            .map(|id| {
                let server_uid = McpServerId::new(id.clone());
                if let Some(handle) = self.manager.connected_server(&server_uid) {
                    let tools = handle
                        .snapshot()
                        .tools
                        .iter()
                        .map(|tool| handle.namespace().apply(&server_uid, tool.name.as_ref()))
                        .collect();
                    McpServerStatus {
                        id: id.clone(),
                        status: McpServerConnectionStatus::Connected,
                        tools,
                        authorization_url: None,
                        error: None,
                    }
                } else if let Some(flow) = self.auth_pending.get(id) {
                    McpServerStatus {
                        id: id.clone(),
                        status: McpServerConnectionStatus::AuthorizationRequired,
                        tools: Vec::new(),
                        authorization_url: flow.as_ref().map(|f| f.url.clone()),
                        error: flow.is_none().then(|| {
                            "authorization link could not be created yet; search again to retry"
                                .to_string()
                        }),
                    }
                } else {
                    McpServerStatus {
                        id: id.clone(),
                        status: McpServerConnectionStatus::Unavailable,
                        tools: Vec::new(),
                        authorization_url: None,
                        error: self.last_errors.get(id).map(|(message, _)| message.clone()),
                    }
                }
            })
            .collect()
    }

    /// Reseats the connection owning `tool_name` after a transport-shaped
    /// call failure. The server is recovered by inverting the manager's
    /// tool namespace; the longest matching id wins so a server id that
    /// prefixes another cannot capture its sibling's tools.
    async fn reconnect_for_tool(&mut self, tool_name: &str) -> Result<(), String> {
        let Some(id) = self
            .configured
            .keys()
            .filter(|id| {
                self.manager
                    .namespace()
                    .unapply(&McpServerId::new((*id).clone()), tool_name)
                    .is_some()
            })
            .max_by_key(|id| id.len())
            .cloned()
        else {
            return Err(format!(
                "tool {tool_name} does not map to a configured MCP server"
            ));
        };

        if let Some(at) = self.last_reconnects.get(&id)
            && at.elapsed() < MCP_RETRY_COOLDOWN
        {
            // Freshly reseated: report success without another handshake so
            // a model looping on a failing call can't thrash the connection.
            return Ok(());
        }
        self.last_reconnects.insert(id.clone(), Instant::now());

        self.drop_connection(&id, "reconnect").await;
        match self.connect_one(&id).await {
            Ok(()) => {
                self.auth_pending.remove(&id);
                self.last_errors.remove(&id);
                Ok(())
            }
            Err(failure) if failure.auth_required => {
                self.ensure_auth_flow(&id).await;
                match self.auth_pending.get(&id) {
                    Some(Some(flow)) => Err(format!(
                        "MCP server {id} requires authorization: {url}",
                        url = flow.url
                    )),
                    _ => Err(format!(
                        "MCP server {id} requires authorization; the authorization link \
                         could not be created yet"
                    )),
                }
            }
            Err(failure) => {
                self.last_errors
                    .insert(id.clone(), (failure.message.clone(), Instant::now()));
                Err(format!(
                    "MCP server {id} is unreachable: {}",
                    failure.message
                ))
            }
        }
    }

    async fn reconcile(
        &mut self,
        desired: Vec<McpServer>,
        token: String,
    ) -> Result<(), RunnerError> {
        let desired_map: BTreeMap<String, McpServer> =
            desired.into_iter().map(|s| (s.id.clone(), s)).collect();

        // Build every changed configuration before mutating credentials, live
        // sessions, or the desired set. An invalid replacement must not leave
        // the old endpoint registered under the next invocation's credential.
        let mut prepared = Vec::new();
        let mut attached = Vec::new();
        for (id, server) in &desired_map {
            let is_new = !self.configured.contains_key(id);
            let changed = self.configured.get(id).is_some_and(|prev| prev != server);
            if is_new {
                attached.push(id.clone());
            }
            if is_new || changed {
                let config = build_mcp_server_config(server, &self.http_client, &self.tokens)?;
                prepared.push((id.clone(), config, changed));
            }
        }
        if self.tokens.current()? != token {
            // HTTP token rotation does not invalidate MCP protocol sessions or
            // credential-specific OAuth/cooldown state. Tear those down under
            // the old credential before publishing the next one.
            let ids: Vec<_> = self
                .manager
                .connected_servers()
                .iter()
                .map(|handle| handle.server_id().0.clone())
                .collect();
            for id in ids {
                if self.is_connected(&id) {
                    self.drop_connection(&id, "credential_boundary").await;
                }
            }
            self.auth_pending.clear();
            self.last_errors.clear();
            self.last_reconnects.clear();
            self.tokens.rotate(token)?;
        }
        for (id, config, changed) in prepared {
            self.manager.register_server_with_options(
                config,
                McpServerOptions::new().with_timeout(MCP_HANDSHAKE_TIMEOUT),
            );
            if changed {
                // Config drift: drop the live connection and stale auth,
                // error, and reconnect-debounce state so the next
                // EnsureConnected reconnects fresh and an immediate
                // reconnect is not suppressed by a pre-drift timestamp.
                self.auth_pending.remove(&id);
                self.last_errors.remove(&id);
                self.last_reconnects.remove(&id);
                if self.is_connected(&id) {
                    self.drop_connection(&id, "config_drift").await;
                }
            }
        }

        let detached: Vec<String> = self
            .configured
            .keys()
            .filter(|id| !desired_map.contains_key(*id))
            .cloned()
            .collect();
        for id in &detached {
            self.auth_pending.remove(id);
            self.last_errors.remove(id);
            self.last_reconnects.remove(id);
            if self.is_connected(id) {
                self.drop_connection(id, "detach").await;
            }
        }

        self.configured = desired_map;

        if attached.is_empty() && detached.is_empty() {
            return Ok(());
        }
        let mut notice =
            String::from("<message-context>\nEventType: assistant_mcp_servers_updated\n");
        if !attached.is_empty() {
            notice.push_str(&format!("Attached: {}\n", attached.join(", ")));
        }
        if !detached.is_empty() {
            notice.push_str(&format!("Detached: {}\n", detached.join(", ")));
        }
        notice
            .push_str("Use tool_search to discover tools on attached servers.\n</message-context>");
        self.send_notice(notice);
        Ok(())
    }

    /// Creates (or reuses) the auth flow for a server whose connect
    /// demanded authorization, storing the URL for statuses and emitting
    /// the `assistant_mcp_auth_required` notice the assistant
    /// instructions key on.
    async fn ensure_auth_flow(&mut self, server_id: &str) {
        if let Some(Some(flow)) = self.auth_pending.get(server_id)
            && flow.minted_at.elapsed() < MCP_AUTH_URL_TTL
        {
            return;
        }
        let Some(server) = self.configured.get(server_id) else {
            return;
        };
        match self
            .gram_client
            .create_mcp_auth_flow(&self.thread_id, server_id, &server.url, &self.tokens)
            .await
        {
            Ok(flow) => {
                self.send_notice(format!(
                    "<message-context>\nEventType: assistant_mcp_auth_required\nMCPServerID: {server_id}\nMCPSlug: {mcp_slug}\nAuthURL: {auth_url}\n</message-context>",
                    server_id = flow.server_id,
                    mcp_slug = flow.mcp_slug,
                    auth_url = flow.auth_url,
                ));
                self.auth_pending.insert(
                    server_id.to_string(),
                    Some(AuthFlow {
                        url: flow.auth_url,
                        minted_at: Instant::now(),
                    }),
                );
            }
            Err(flow_err) => {
                tracing::warn!(
                    server_id,
                    error = %flow_err,
                    "failed to create assistant mcp auth flow; will retry on next search"
                );
                self.auth_pending.insert(server_id.to_string(), None);
            }
        }
    }

    /// Disconnects a server, treating any outcome as "connection gone":
    /// agentkit removes the handle before `close()` can fail, so close
    /// errors are log-only and never retried.
    async fn drop_connection(&mut self, id: &str, action: &'static str) {
        if let Err(err) = self.manager.disconnect_server(&McpServerId::new(id)).await {
            tracing::warn!(server_id = %id, error = %err, action, "mcp disconnect reported error");
        }
    }

    async fn connect_one(&mut self, id: &str) -> Result<(), McpConnectFailure> {
        let server_uid = McpServerId::new(id);
        match self.manager.connect_server(&server_uid).await {
            Ok(handle) => {
                tracing::info!(
                    server_id = %server_uid,
                    tools = handle.snapshot().tools.len(),
                    "mcp connect ok"
                );
                Ok(())
            }
            Err(e) => {
                let auth_required = matches!(e, McpError::AuthRequired(_));
                tracing::warn!(server_id = %server_uid, error = %e, "mcp connect failed");
                Err(McpConnectFailure {
                    message: e.to_string(),
                    auth_required,
                })
            }
        }
    }

    fn send_notice(&self, notice: String) {
        if self.inbox_tx.send(notice).is_err() {
            tracing::warn!("drop mcp notice: thread inbox closed");
        }
    }
}

struct McpConnectFailure {
    message: String,
    auth_required: bool,
}

struct AuthFlow {
    url: String,
    minted_at: Instant,
}

pub fn validate_endpoint(raw: &str) -> Result<(), RunnerError> {
    let url =
        reqwest::Url::parse(raw).map_err(|_| RunnerError::Loop("invalid HTTP endpoint".into()))?;
    if !matches!(url.scheme(), "http" | "https")
        || url.host_str().is_none()
        || !url.username().is_empty()
        || url.password().is_some()
    {
        return Err(RunnerError::Loop(
            "HTTP endpoint requires a host and no userinfo".into(),
        ));
    }
    Ok(())
}

pub fn validated_server_headers(server: &McpServer) -> Result<http::HeaderMap, RunnerError> {
    validate_endpoint(&server.url)?;
    let mut server_headers = http::HeaderMap::new();
    for (k, v) in &server.headers {
        let name = http::HeaderName::from_bytes(k.as_bytes()).map_err(|source| {
            RunnerError::McpHeaderName {
                server: server.id.clone(),
                name: k.clone(),
                source,
            }
        })?;
        let value =
            http::HeaderValue::from_str(v).map_err(|source| RunnerError::McpHeaderValue {
                server: server.id.clone(),
                name: k.clone(),
                source,
            })?;
        server_headers.insert(name, value);
    }
    Ok(server_headers)
}

fn build_mcp_server_config(
    server: &McpServer,
    http_client: &reqwest::Client,
    tokens: &TokenRegistry,
) -> Result<McpServerConfig, RunnerError> {
    let server_headers = validated_server_headers(server)?;
    let mcp_http = Arc::new(McpRotatingClient::new(
        http_client.clone(),
        tokens.clone(),
        server_headers,
    ));
    let transport = StreamableHttpTransportConfig::new(&server.url).with_http_client(mcp_http);
    Ok(McpServerConfig::new(
        &server.id,
        McpTransportBinding::StreamableHttp(transport),
    ))
}

#[cfg(test)]
#[allow(clippy::unwrap_used)]
mod tests {
    use super::*;

    #[test]
    fn endpoints_require_http_host_without_userinfo() {
        for url in [
            "file:///tmp/test",
            "mailto:test@example.com",
            "https://user@example.com",
            "https://user:secret@example.com",
            "not a url",
        ] {
            assert!(validate_endpoint(url).is_err(), "{url}");
        }
        assert!(validate_endpoint("https://example.com/mcp").is_ok());
        assert!(validate_endpoint("http://127.0.0.1:8080/mcp").is_ok());
    }

    #[tokio::test]
    async fn invalid_same_id_replacement_rejects_ack_without_reconnecting_old_endpoint() {
        use axum::{
            Json, Router, extract::Path, http::HeaderMap, response::IntoResponse, routing::post,
        };
        use serde_json::{Value, json};
        let (events_tx, mut events) = mpsc::unbounded_channel();
        let post_events = events_tx.clone();
        let app = Router::new().route("/{endpoint}", post(move |Path(endpoint): Path<String>, headers: HeaderMap, Json(message): Json<Value>| {
            let events = post_events.clone();
            async move {
                let bearer = headers.get("authorization").unwrap().to_str().unwrap().to_string();
                let method = message["method"].as_str().unwrap().to_string();
                events.send((endpoint.clone(), bearer, method.clone())).unwrap();
                if message.get("id").is_none() { return axum::http::StatusCode::ACCEPTED.into_response(); }
                let result = if method == "initialize" {
                    json!({"protocolVersion": message["params"]["protocolVersion"], "capabilities":{"tools":{}}, "serverInfo":{"name":"fixture", "version":"1"}})
                } else { json!({"tools":[]}) };
                let mut response = Json(json!({"jsonrpc":"2.0", "id":message["id"], "result":result})).into_response();
                response.headers_mut().insert("mcp-session-id", format!("session-{endpoint}").parse().unwrap());
                response
            }
        }).get(|| async { axum::http::StatusCode::METHOD_NOT_ALLOWED }).delete(move |Path(endpoint): Path<String>, headers: HeaderMap| {
            let events = events_tx.clone();
            async move {
                events.send((endpoint, headers.get("authorization").unwrap().to_str().unwrap().to_string(), "DELETE".into())).unwrap();
                axum::http::StatusCode::OK
            }
        }));
        let listener = tokio::net::TcpListener::bind("127.0.0.1:0").await.unwrap();
        let addr = listener.local_addr().unwrap();
        let server_task = tokio::spawn(async move {
            axum::serve(listener, app).await.unwrap();
        });
        let tokens = TokenRegistry::new("opaque-a");
        let client = reqwest::Client::new();
        let (notice_tx, _notices) = mpsc::unbounded_channel();
        let old = McpServer {
            id: "same-id".into(),
            url: format!("http://{addr}/old"),
            headers: BTreeMap::new(),
        };
        let (cmd, _catalog) = spawn_mcp_actor(
            GramBootstrapClient::new(
                format!("http://{addr}"),
                crate::http_layer::build_bootstrap_client(client.clone()),
            ),
            client,
            "T",
            std::slice::from_ref(&old),
            &tokens,
            notice_tx,
        )
        .unwrap();
        let (reply, ack) = oneshot::channel();
        cmd.send(McpCmd::EnsureConnected { reply }).await.unwrap();
        assert_eq!(
            ack.await.unwrap()[0].status,
            McpServerConnectionStatus::Connected
        );
        for token in ["opaque-a", "opaque-b"] {
            for headers in [
                BTreeMap::from([("bad header".into(), "value".into())]),
                BTreeMap::from([("x-test".into(), "bad\nvalue".into())]),
            ] {
                let replacement = McpServer {
                    url: format!("http://{addr}/replacement"),
                    headers,
                    ..old.clone()
                };
                let (reply, ack) = oneshot::channel();
                cmd.send(McpCmd::TurnBoundary {
                    token: token.into(),
                    desired: Some(vec![replacement]),
                    reply,
                })
                .await
                .unwrap();
                assert!(
                    ack.await.unwrap().is_err(),
                    "invalid config must fail boundary acknowledgement"
                );
                assert_eq!(
                    tokens.current().unwrap(),
                    "opaque-a",
                    "failed preparation must not publish the next credential"
                );
                let (reply, ack) = oneshot::channel();
                cmd.send(McpCmd::EnsureConnected { reply }).await.unwrap();
                assert_eq!(
                    ack.await.unwrap()[0].status,
                    McpServerConnectionStatus::Connected
                );
            }
        }
        // A valid boundary closes the actual prior protocol session under A,
        // then reconnects only the replacement endpoint under B.
        let (reply, ack) = oneshot::channel();
        cmd.send(McpCmd::TurnBoundary {
            token: "opaque-b".into(),
            desired: Some(vec![McpServer {
                url: format!("http://{addr}/replacement"),
                ..old
            }]),
            reply,
        })
        .await
        .unwrap();
        ack.await.unwrap().unwrap();
        let (reply, ack) = oneshot::channel();
        cmd.send(McpCmd::EnsureConnected { reply }).await.unwrap();
        assert_eq!(
            ack.await.unwrap()[0].status,
            McpServerConnectionStatus::Connected
        );
        let mut initialized = Vec::new();
        let mut deleted_old_under_a = false;
        while let Ok((endpoint, bearer, method)) = events.try_recv() {
            assert!(
                !(endpoint == "old" && bearer == "Bearer opaque-b"),
                "old endpoint must never receive the next credential"
            );
            if method == "initialize" {
                initialized.push((endpoint.clone(), bearer.clone()));
            }
            if method == "DELETE" && endpoint == "old" && bearer == "Bearer opaque-a" {
                deleted_old_under_a = true;
            }
        }
        assert_eq!(
            initialized,
            vec![
                ("old".into(), "Bearer opaque-a".into()),
                ("replacement".into(), "Bearer opaque-b".into())
            ]
        );
        assert!(
            deleted_old_under_a,
            "credential boundary must tear down the actual prior MCP session"
        );
        server_task.abort();
    }

    #[tokio::test]
    async fn credential_boundary_clears_auth_and_retry_state_only_on_change() {
        let tokens = TokenRegistry::new("opaque-a");
        let (inbox_tx, _inbox_rx) = mpsc::unbounded_channel();
        let http_client = reqwest::Client::new();
        let mut actor = McpActor {
            manager: McpServerManager::new(),
            gram_client: GramBootstrapClient::new(
                "http://127.0.0.1".into(),
                crate::http_layer::build_bootstrap_client(http_client.clone()),
            ),
            http_client,
            thread_id: "T".into(),
            tokens: tokens.clone(),
            inbox_tx,
            configured: BTreeMap::new(),
            auth_pending: BTreeMap::from([("server".into(), None)]),
            last_errors: BTreeMap::from([("server".into(), ("old error".into(), Instant::now()))]),
            last_reconnects: BTreeMap::from([("server".into(), Instant::now())]),
        };
        actor.turn_boundary("opaque-a".into(), None).await.unwrap();
        assert_eq!(actor.auth_pending.len(), 1);
        actor
            .turn_boundary(
                "opaque-b".into(),
                Some(vec![McpServer {
                    id: "new".into(),
                    url: "https://example.com/mcp".into(),
                    headers: BTreeMap::new(),
                }]),
            )
            .await
            .unwrap();
        assert_eq!(tokens.current().unwrap(), "opaque-b");
        assert!(actor.auth_pending.is_empty());
        assert!(actor.last_errors.is_empty());
        assert!(actor.last_reconnects.is_empty());
        assert!(actor.configured.contains_key("new"));
    }
}
