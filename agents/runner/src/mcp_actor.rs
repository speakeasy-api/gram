//! Per-thread MCP actor.
//!
//! Owns the thread's [`McpServerManager`] and serializes every connection
//! lifecycle operation — deferred connects, auth flows, reconciliation
//! against toolset edits, and implicit reconnects — behind a command
//! channel. Connection liveness is read from the manager itself rather
//! than mirrored: agentkit's `disconnect_server` removes the handle
//! before `close()` can fail, so any mirror of "connected" drifts on
//! exactly the error paths it would exist to handle.

use std::collections::{BTreeMap, BTreeSet};
use std::sync::{Arc, PoisonError, RwLock};
use std::time::{Duration, Instant};

use agentkit_mcp::{
    McpError, McpServerConfig, McpServerId, McpServerManager, McpServerOptions,
    McpTransportBinding, StreamableHttpTransportConfig,
};
use agentkit_tools_core::{CatalogReader, ToolName, ToolSource, ToolSpec};
use serde::Serialize;
use tokio::sync::mpsc;
use tokio::sync::mpsc::UnboundedSender;
use tokio::sync::oneshot;
use tracing::Instrument;

use crate::errors::RunnerError;
use crate::gram_client::GramBootstrapClient;
use crate::http_layer::{McpSessionClient, TokenRegistry};
use crate::telemetry::mcp_trace_decorator;
use crate::wire::{McpServer, RunnerContent, is_gateway_mode};

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

/// Every command carries `parent`, the sender's span. The actor runs on its
/// own task, so without it connection work would export as detached root
/// spans instead of nesting under the tool call or turn that asked for it.
pub enum McpCmd {
    /// Sent by `tool_search` before every search. The actor connects any
    /// configured server that is not yet connected (creating an auth flow
    /// for servers that demand one) and replies with the status of every
    /// configured server, so search results always carry live catalog
    /// coverage plus authorization links.
    EnsureConnected {
        reply: oneshot::Sender<Vec<McpServerStatus>>,
        parent: tracing::Span,
    },
    /// Sent by the catalog dispatch layer when the model calls a tool that a
    /// configured server exposed before its connection was closed — at a
    /// credential boundary, typically. The actor connects the owning server
    /// so the call can dispatch against the fresh session.
    ConnectForTool {
        tool_name: String,
        reply: oneshot::Sender<Result<(), String>>,
        parent: tracing::Span,
    },
    /// Sent by the catalog dispatch layer when an MCP-backed tool call
    /// fails with a transport-shaped error. The actor reseats the owning
    /// server's connection; the failed call is not replayed.
    ReconnectTool {
        tool_name: String,
        reply: oneshot::Sender<Result<(), String>>,
        parent: tracing::Span,
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
        parent: tracing::Span,
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

/// Tool names each configured server exposed when it last connected.
///
/// Closing a connection unregisters its tools from the live catalog, but the
/// model still holds their schemas from earlier `tool_search` results. This
/// record survives disconnects so dispatch can reconnect the owning server
/// instead of treating a discovered tool as unknown. A server's entry is
/// forgotten only when its configuration changes or it is detached.
#[derive(Clone, Default)]
pub struct KnownTools(Arc<RwLock<BTreeMap<String, BTreeSet<String>>>>);

impl KnownTools {
    pub fn contains(&self, tool: &str) -> bool {
        self.server_for(tool).is_some()
    }

    fn server_for(&self, tool: &str) -> Option<String> {
        self.0
            .read()
            .unwrap_or_else(PoisonError::into_inner)
            .iter()
            .find(|(_, tools)| tools.contains(tool))
            .map(|(server, _)| server.clone())
    }

    pub(crate) fn record(&self, server: &str, tools: BTreeSet<String>) {
        self.0
            .write()
            .unwrap_or_else(PoisonError::into_inner)
            .insert(server.to_string(), tools);
    }

    fn forget(&self, server: &str) {
        self.0
            .write()
            .unwrap_or_else(PoisonError::into_inner)
            .remove(server);
    }
}

/// Shared state tracking whether the thread is currently in single Project
/// Gateway mode and caching the gateway's direct tool specs (under bare names
/// such as `list_servers`, `execute_tool`, etc.) mapped back to their
/// namespaced `CatalogReader` keys.
#[derive(Clone, Default)]
pub struct GatewayModeState(Arc<RwLock<GatewayModeInner>>);

#[derive(Default)]
struct GatewayModeInner {
    active: bool,
    server_id: Option<String>,
    /// Pairs of (bare_spec, namespaced_tool_name).
    tools: Vec<(ToolSpec, ToolName)>,
}

impl GatewayModeState {
    pub fn from_servers(servers: &[McpServer]) -> Self {
        let state = Self::default();
        state.set_mode(servers);
        state
    }

    pub fn is_active(&self) -> bool {
        self.0
            .read()
            .unwrap_or_else(PoisonError::into_inner)
            .active
    }

    pub fn specs(&self) -> Vec<ToolSpec> {
        let inner = self.0.read().unwrap_or_else(PoisonError::into_inner);
        if !inner.active {
            return Vec::new();
        }
        inner.tools.iter().map(|(spec, _)| spec.clone()).collect()
    }

    /// Resolves a bare gateway tool name (e.g. `execute_tool`) to its
    /// namespaced `CatalogReader` name and bare `ToolSpec`.
    pub fn resolve_bare(&self, name: &str) -> Option<(ToolSpec, ToolName)> {
        let inner = self.0.read().unwrap_or_else(PoisonError::into_inner);
        if !inner.active {
            return None;
        }
        inner
            .tools
            .iter()
            .find(|(spec, _)| spec.name.0 == name)
            .cloned()
    }

    fn set_mode(&self, servers: &[McpServer]) {
        let mut inner = self.0.write().unwrap_or_else(PoisonError::into_inner);
        let active = is_gateway_mode(servers);
        let server_id = active.then(|| servers[0].id.clone());
        if inner.active != active || inner.server_id != server_id {
            inner.tools.clear();
        }
        inner.active = active;
        inner.server_id = server_id;
    }

    fn set_tools(&self, server_id: &str, tools: Vec<(ToolSpec, ToolName)>) {
        let mut inner = self.0.write().unwrap_or_else(PoisonError::into_inner);
        if inner.active && inner.server_id.as_deref() == Some(server_id) {
            inner.tools = tools;
        }
    }

    fn clear_tools(&self) {
        let mut inner = self.0.write().unwrap_or_else(PoisonError::into_inner);
        inner.tools.clear();
    }
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
    inbox_tx: UnboundedSender<RunnerContent>,
) -> Result<(mpsc::Sender<McpCmd>, CatalogReader, KnownTools, GatewayModeState), RunnerError> {
    // Each tool call carries its own trace context to the server.
    let mut manager = McpServerManager::new().with_request_decorator(mcp_trace_decorator());
    let catalog = manager.source();
    let mut configured = BTreeMap::new();
    let mut instruction_slots = BTreeMap::new();
    let token = tokens.current()?;

    for server in servers {
        let (config, slot) = build_mcp_server_config(server, &http_client, &token)?;
        manager.register_server_with_options(
            config,
            McpServerOptions::new().with_timeout(MCP_HANDSHAKE_TIMEOUT),
        );
        configured.insert(server.id.clone(), server.clone());
        if let Some(slot) = slot {
            instruction_slots.insert(server.id.clone(), slot);
        }
    }

    let gateway_mode = GatewayModeState::from_servers(servers);
    let (cmd_tx, cmd_rx) = mpsc::channel(MCP_CMD_CAPACITY);
    let actor = McpActor {
        manager,
        catalog: catalog.clone(),
        gram_client,
        http_client,
        thread_id: thread_id.to_string(),
        tokens: tokens.clone(),
        inbox_tx,
        auth_pending: BTreeMap::new(),
        last_errors: BTreeMap::new(),
        last_reconnects: BTreeMap::new(),
        configured,
        known_tools: KnownTools::default(),
        gateway_mode: gateway_mode.clone(),
        instruction_slots,
        last_gateway_instructions: None,
    };
    let known_tools = actor.known_tools.clone();
    tokio::spawn(actor.run(cmd_rx));
    Ok((cmd_tx, catalog, known_tools, gateway_mode))
}

type InstructionsSlot = Arc<RwLock<Option<String>>>;

struct McpActor {
    manager: McpServerManager,
    catalog: CatalogReader,
    gram_client: GramBootstrapClient,
    http_client: reqwest::Client,
    thread_id: String,
    tokens: TokenRegistry,
    inbox_tx: UnboundedSender<RunnerContent>,
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
    known_tools: KnownTools,
    gateway_mode: GatewayModeState,
    instruction_slots: BTreeMap<String, InstructionsSlot>,
    last_gateway_instructions: Option<String>,
}

impl McpActor {
    async fn run(mut self, mut cmd_rx: mpsc::Receiver<McpCmd>) {
        while let Some(cmd) = cmd_rx.recv().await {
            match cmd {
                McpCmd::EnsureConnected { reply, parent } => {
                    let span = tracing::info_span!(parent: &parent, "mcp.ensure_connected");
                    let statuses = self.ensure_connected().instrument(span).await;
                    let _ = reply.send(statuses);
                }
                McpCmd::ConnectForTool {
                    tool_name,
                    reply,
                    parent,
                } => {
                    let span = tracing::info_span!(
                        parent: &parent,
                        "mcp.connect_for_tool",
                        "gen_ai.tool.name" = %tool_name,
                    );
                    let result = self.connect_for_tool(&tool_name).instrument(span).await;
                    let _ = reply.send(result);
                }
                McpCmd::ReconnectTool {
                    tool_name,
                    reply,
                    parent,
                } => {
                    let span = tracing::info_span!(
                        parent: &parent,
                        "mcp.reconnect_for_tool",
                        "gen_ai.tool.name" = %tool_name,
                    );
                    let result = self.reconnect_for_tool(&tool_name).instrument(span).await;
                    let _ = reply.send(result);
                }
                McpCmd::TurnBoundary {
                    token,
                    desired,
                    reply,
                    parent,
                } => {
                    let span = tracing::info_span!(parent: &parent, "mcp.turn_boundary");
                    let result = self.turn_boundary(token, desired).instrument(span).await;
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
            self.connect_pending(&id, "first_use").await;
        }

        self.server_statuses()
    }

    fn is_gateway_server(&self, id: &str) -> bool {
        self.configured.get(id).is_some_and(|s| s.is_gateway)
    }

    fn read_gateway_instructions(&self, id: &str) -> Option<String> {
        self.instruction_slots
            .get(id)?
            .read()
            .unwrap_or_else(PoisonError::into_inner)
            .clone()
    }

    /// Connects one configured, unconnected server, recording an auth flow or
    /// transient failure for its status. Auth-pending servers fail fast, so
    /// they retry unconditionally; hard failures wait out the cooldown so
    /// repeated calls don't re-pay the handshake timeout for a down server.
    async fn connect_pending(&mut self, id: &str, reason: &'static str) {
        if !self.auth_pending.contains_key(id)
            && let Some((_, at)) = self.last_errors.get(id)
            && at.elapsed() < MCP_RETRY_COOLDOWN
        {
            return;
        }
        match self.connect_one(id, reason).await {
            Ok(()) => {
                self.auth_pending.remove(id);
                self.last_errors.remove(id);
            }
            Err(failure) if failure.auth_required => {
                if self.is_gateway_server(id) {
                    // Never attempt a per-server OAuth auth flow against the
                    // Project Gateway URL.
                    self.last_errors.insert(
                        id.to_string(),
                        (
                            "Project Gateway rejected this turn's credential (401 Unauthorized)"
                                .to_string(),
                            Instant::now(),
                        ),
                    );
                    return;
                }
                self.last_errors.remove(id);
                self.ensure_auth_flow(id).await;
            }
            Err(failure) => {
                self.last_errors
                    .insert(id.to_string(), (failure.message, Instant::now()));
            }
        }
    }

    /// Reconnects the configured server that last exposed `tool_name`, so a
    /// discovered tool keeps dispatching after its session was closed.
    async fn connect_for_tool(&mut self, tool_name: &str) -> Result<(), String> {
        let Some(id) = self
            .known_tools
            .server_for(tool_name)
            .filter(|id| self.configured.contains_key(id))
        else {
            return Err(format!(
                "tool {tool_name} does not map to a configured MCP server"
            ));
        };
        if !self.is_connected(&id) {
            self.connect_pending(&id, "deferred").await;
        }
        if self.is_connected(&id) {
            return Ok(());
        }
        Err(
            match (self.auth_pending.get(&id), self.last_errors.get(&id)) {
                (Some(Some(flow)), _) => format!(
                    "MCP server {id} requires authorization: {url}",
                    url = flow.url
                ),
                (Some(None), _) => format!(
                    "MCP server {id} requires authorization; the authorization link \
                 could not be created yet"
                ),
                (None, Some((message, _))) => format!("MCP server {id} is unreachable: {message}"),
                (None, None) => format!("MCP server {id} is unreachable"),
            },
        )
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
        match self.connect_one(&id, "reconnect").await {
            Ok(()) => {
                self.auth_pending.remove(&id);
                self.last_errors.remove(&id);
                Ok(())
            }
            Err(failure) if failure.auth_required => {
                if self.is_gateway_server(&id) {
                    let message = "Project Gateway rejected this turn's credential (401 Unauthorized); retrying on the next turn will re-evaluate gateway eligibility".to_string();
                    self.last_errors.insert(id.clone(), (message.clone(), Instant::now()));
                    return Err(message);
                }
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
        let was_gateway = self.gateway_mode.is_active();
        let now_gateway = is_gateway_mode(&desired);
        let gateway_server_id = now_gateway.then(|| desired[0].id.clone());
        let desired_map: BTreeMap<String, McpServer> =
            desired.iter().cloned().map(|s| (s.id.clone(), s)).collect();

        // Build every changed configuration before mutating credentials, live
        // sessions, or the desired set. An invalid replacement must not leave
        // the old endpoint registered under the next invocation's credential.
        // Every transport carries the credential it was built with, so a new
        // credential re-registers every server.
        let rotating = self.tokens.current()? != token;
        let mut prepared = Vec::new();
        let mut attached = Vec::new();
        for (id, server) in &desired_map {
            let is_new = !self.configured.contains_key(id);
            let changed = self.configured.get(id).is_some_and(|prev| prev != server);
            if is_new {
                attached.push(id.clone());
            }
            if is_new || changed || rotating {
                let (config, slot) = build_mcp_server_config(server, &self.http_client, &token)?;
                prepared.push((id.clone(), config, slot, changed));
            }
        }
        if rotating {
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
        for (id, config, slot, changed) in prepared {
            self.manager.register_server_with_options(
                config,
                McpServerOptions::new().with_timeout(MCP_HANDSHAKE_TIMEOUT),
            );
            if let Some(slot) = slot {
                self.instruction_slots.insert(id.clone(), slot);
            } else {
                self.instruction_slots.remove(&id);
            }
            if changed {
                // Config drift: drop the live connection and stale auth,
                // error, and reconnect-debounce state so the next
                // EnsureConnected reconnects fresh and an immediate
                // reconnect is not suppressed by a pre-drift timestamp.
                self.auth_pending.remove(&id);
                self.last_errors.remove(&id);
                self.last_reconnects.remove(&id);
                self.known_tools.forget(&id);
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
            self.known_tools.forget(id);
            self.auth_pending.remove(id);
            self.last_errors.remove(id);
            self.last_reconnects.remove(id);
            self.instruction_slots.remove(id);
            if self.is_connected(id) {
                self.drop_connection(id, "detach").await;
            }
        }

        self.configured = desired_map;
        self.gateway_mode.set_mode(&desired);
        if !now_gateway {
            self.last_gateway_instructions = None;
        }

        let mut gateway_instructions_notice = None;
        if let Some(gw_id) = gateway_server_id {
            if !self.is_connected(&gw_id) {
                match self.connect_one(&gw_id, "gateway_turn").await {
                    Ok(()) => {
                        self.auth_pending.remove(&gw_id);
                        self.last_errors.remove(&gw_id);
                    }
                    Err(failure) if failure.auth_required => {
                        self.gateway_mode.clear_tools();
                        return Err(RunnerError::Loop(
                            "Project Gateway rejected this turn's credential (401 Unauthorized); \
                             retrying on the next turn will re-evaluate gateway eligibility"
                                .into(),
                        ));
                    }
                    Err(failure) => {
                        self.gateway_mode.clear_tools();
                        return Err(RunnerError::Loop(format!(
                            "Project Gateway is unreachable: {}",
                            failure.message
                        )));
                    }
                }
            }
            if let Some(instructions) = self.read_gateway_instructions(&gw_id)
                && self.last_gateway_instructions.as_deref() != Some(instructions.as_str())
            {
                self.last_gateway_instructions = Some(instructions.clone());
                gateway_instructions_notice = Some(format!(
                    "<project-gateway-instructions>\n{instructions}\n</project-gateway-instructions>"
                ));
            }
        }

        let mode_flipped = was_gateway != now_gateway;
        if !attached.is_empty() || !detached.is_empty() || mode_flipped {
            let mut notice =
                String::from("<message-context>\nEventType: assistant_mcp_servers_updated\n");
            if !attached.is_empty() {
                notice.push_str(&format!("Attached: {}\n", attached.join(", ")));
            }
            if !detached.is_empty() {
                notice.push_str(&format!("Detached: {}\n", detached.join(", ")));
            }
            if now_gateway {
                notice.push_str(
                    "MCP delivery is now via the Project Gateway. Use list_servers, describe_server, \
                     describe_tools, execute_read_only_tool, execute_tool, and connection_status directly \
                     (tool_search is not available in Project Gateway mode).\n</message-context>",
                );
            } else {
                notice.push_str(
                    "Use tool_search to discover tools on attached servers.\n</message-context>",
                );
            }
            self.send_notice(notice);
        }
        if let Some(instructions_notice) = gateway_instructions_notice {
            self.send_notice(instructions_notice);
        }
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
            .instrument(tracing::info_span!("mcp.create_auth_flow", "mcp.server.id" = %server_id))
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
                // Class and status only: a non-2xx body could land in the
                // trace, since this runs inside an actor command span.
                tracing::warn!(
                    server_id,
                    error_class = flow_err.class(),
                    http_status = flow_err.http_status(),
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
        let span = tracing::info_span!("mcp.disconnect", "mcp.server.id" = %id, action);
        if let Err(err) = self
            .manager
            .disconnect_server(&McpServerId::new(id))
            .instrument(span)
            .await
        {
            tracing::warn!(
                server_id = %id,
                error_class = mcp_error_class(&err),
                action,
                "mcp disconnect reported error"
            );
        }
    }

    /// Connects one server: transport setup plus the MCP initialize and
    /// tool-discovery handshake, bounded by [`MCP_HANDSHAKE_TIMEOUT`]. The
    /// `mcp.connect` span (created before the connect and dropped after it,
    /// never entered while connecting) times that handshake; `reason` says why
    /// it ran (`first_use` from tool_search, `deferred` from a call to a
    /// tool whose session closed, `reconnect` after a failed call).
    async fn connect_one(
        &mut self,
        id: &str,
        reason: &'static str,
    ) -> Result<(), McpConnectFailure> {
        let server_uid = McpServerId::new(id);
        let span = tracing::info_span!(
            "mcp.connect",
            "mcp.server.id" = %id,
            reason,
            outcome = tracing::field::Empty,
            "mcp.tools.count" = tracing::field::Empty,
        );
        // rmcp spawns the session's service task under the span current at
        // connect, and that task lives as long as the session. Running the
        // connect under its own root `mcp.session` span keeps the session
        // from holding `mcp.connect` (and the turn above it) open, so
        // `mcp.connect` ends when the handshake does.
        let session = tracing::info_span!(parent: None, "mcp.session", "mcp.server.id" = %id);
        session.follows_from(&span);
        let result = self
            .manager
            .connect_server(&server_uid)
            .instrument(session)
            .await;
        let _entered = span.enter();
        span.record(
            "outcome",
            match &result {
                Ok(_) => "connected",
                Err(McpError::AuthRequired(_)) => "auth_required",
                Err(_) => "failed",
            },
        );
        match result {
            Ok(handle) => {
                let is_gateway = self.is_gateway_server(id);
                let mut tools = BTreeSet::new();
                let mut gateway_specs = Vec::new();
                for tool in &handle.snapshot().tools {
                    let bare_name = tool.name.as_ref();
                    let namespaced = handle.namespace().apply(&server_uid, bare_name);
                    if is_gateway {
                        let ns_tool_name = ToolName::from(namespaced.as_str());
                        if let Some(registered) = self.catalog.get(&ns_tool_name) {
                            let mut bare_spec = registered.spec().clone();
                            bare_spec.name = ToolName::from(bare_name);
                            gateway_specs.push((bare_spec, ns_tool_name));
                        }
                    }
                    tools.insert(namespaced);
                }
                span.record("mcp.tools.count", tools.len());
                tracing::info!(server_id = %server_uid, tools = tools.len(), "mcp connect ok");
                self.known_tools.record(id, tools);
                if is_gateway {
                    self.gateway_mode.set_tools(id, gateway_specs);
                }
                Ok(())
            }
            Err(e) => {
                let auth_required = matches!(e, McpError::AuthRequired(_));
                // Class only: the error text can come from the MCP server.
                tracing::warn!(
                    server_id = %server_uid,
                    error_class = mcp_error_class(&e),
                    "mcp connect failed"
                );
                Err(McpConnectFailure {
                    message: e.to_string(),
                    auth_required,
                })
            }
        }
    }

    fn send_notice(&self, notice: String) {
        if self.inbox_tx.send(RunnerContent::Text(notice)).is_err() {
            tracing::warn!("drop mcp notice: thread inbox closed");
        }
    }
}

/// Stable class name for an MCP error, for logs and spans. The `Display`
/// text can carry server-supplied content, so traces record only this.
fn mcp_error_class(err: &McpError) -> &'static str {
    match err {
        McpError::Io(_) => "io",
        McpError::Serialize(_) => "serialization",
        McpError::Transport(_) => "transport",
        McpError::Timeout { .. } => "timeout",
        McpError::Protocol(_) => "protocol",
        McpError::AuthRequired(_) => "auth_required",
        McpError::AuthResolution(_) => "auth_resolution",
        McpError::Invocation(_) => "invocation",
        McpError::UnknownServer(_) => "unknown_server",
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
    token: &str,
) -> Result<(McpServerConfig, Option<InstructionsSlot>), RunnerError> {
    let server_headers = validated_server_headers(server)?;
    let slot = server
        .is_gateway
        .then(|| Arc::new(RwLock::new(None::<String>)));
    let mut mcp_http = McpSessionClient::new(http_client.clone(), token.to_string(), server_headers);
    if let Some(slot) = &slot {
        mcp_http = mcp_http.with_instructions_slot(Arc::clone(slot));
    }
    let transport =
        StreamableHttpTransportConfig::new(&server.url).with_http_client(Arc::new(mcp_http));
    Ok((
        McpServerConfig::new(&server.id, McpTransportBinding::StreamableHttp(transport)),
        slot,
    ))
}

#[cfg(test)]
#[allow(clippy::unwrap_used, clippy::expect_used)]
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
                // A server that predates `server/discover` rejects it, which is
                // what makes the client fall back to `initialize`.
                if method == "server/discover" {
                    return Json(json!({"jsonrpc":"2.0", "id":message["id"], "error":{"code":-32601, "message":"Method not found"}})).into_response();
                }
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
            is_gateway: false,
        };
        let (cmd, _catalog, _known, _gw) = spawn_mcp_actor(
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
        cmd.send(McpCmd::EnsureConnected {
            reply,
            parent: tracing::Span::none(),
        })
        .await
        .unwrap();
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
                    parent: tracing::Span::none(),
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
                cmd.send(McpCmd::EnsureConnected {
                    reply,
                    parent: tracing::Span::none(),
                })
                .await
                .unwrap();
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
            parent: tracing::Span::none(),
        })
        .await
        .unwrap();
        ack.await.unwrap().unwrap();
        let (reply, ack) = oneshot::channel();
        cmd.send(McpCmd::EnsureConnected {
            reply,
            parent: tracing::Span::none(),
        })
        .await
        .unwrap();
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
        let manager = McpServerManager::new();
        let catalog = manager.source();
        let mut actor = McpActor {
            manager,
            catalog,
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
            known_tools: KnownTools::default(),
            gateway_mode: GatewayModeState::default(),
            instruction_slots: BTreeMap::new(),
            last_gateway_instructions: None,
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
                    is_gateway: false,
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

    #[tokio::test]
    async fn discovered_tools_reconnect_their_server_after_a_credential_boundary() {
        use agentkit_tools_core::ToolSource;
        use axum::{Json, Router, http::HeaderMap, response::IntoResponse, routing::post};
        use serde_json::{Value, json};
        let (events_tx, mut events) = mpsc::unbounded_channel();
        let post_events = events_tx.clone();
        let app = Router::new().route("/mcp", post(move |headers: HeaderMap, Json(message): Json<Value>| {
            let events = post_events.clone();
            async move {
                let bearer = headers.get("authorization").unwrap().to_str().unwrap().to_string();
                let method = message["method"].as_str().unwrap().to_string();
                events.send((bearer.clone(), method.clone())).unwrap();
                if message.get("id").is_none() { return axum::http::StatusCode::ACCEPTED.into_response(); }
                // A server that predates `server/discover` rejects it, which is
                // what makes the client fall back to `initialize`.
                if method == "server/discover" {
                    return Json(json!({"jsonrpc":"2.0", "id":message["id"], "error":{"code":-32601, "message":"Method not found"}})).into_response();
                }
                let result = if method == "initialize" {
                    json!({"protocolVersion": message["params"]["protocolVersion"], "capabilities":{"tools":{}}, "serverInfo":{"name":"fixture", "version":"1"}})
                } else { json!({"tools":[{"name":"echo", "inputSchema":{"type":"object"}}]}) };
                let mut response = Json(json!({"jsonrpc":"2.0", "id":message["id"], "result":result})).into_response();
                response.headers_mut().insert("mcp-session-id", bearer.replace("Bearer ", "session-").parse().unwrap());
                response
            }
        }).get(|| async { axum::http::StatusCode::METHOD_NOT_ALLOWED }).delete(move |headers: HeaderMap| {
            let events = events_tx.clone();
            async move {
                events.send((headers.get("authorization").unwrap().to_str().unwrap().to_string(), "DELETE".into())).unwrap();
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
        let server = McpServer {
            id: "srv".into(),
            url: format!("http://{addr}/mcp"),
            headers: BTreeMap::new(),
            is_gateway: false,
        };
        let (cmd, catalog, known, _gw) = spawn_mcp_actor(
            GramBootstrapClient::new(
                format!("http://{addr}"),
                crate::http_layer::build_bootstrap_client(client.clone()),
            ),
            client,
            "T",
            std::slice::from_ref(&server),
            &tokens,
            notice_tx,
        )
        .unwrap();
        let (reply, ack) = oneshot::channel();
        cmd.send(McpCmd::EnsureConnected {
            reply,
            parent: tracing::Span::none(),
        })
        .await
        .unwrap();
        let tool = ack.await.unwrap()[0].tools[0].clone();
        let name = agentkit_tools_core::ToolName::new(tool.clone());
        assert!(catalog.get(&name).is_some());

        let (reply, ack) = oneshot::channel();
        cmd.send(McpCmd::TurnBoundary {
            token: "opaque-b".into(),
            desired: None,
            reply,
            parent: tracing::Span::none(),
        })
        .await
        .unwrap();
        ack.await.unwrap().unwrap();
        assert!(
            catalog.get(&name).is_none(),
            "the boundary closed the session"
        );
        assert!(known.contains(&tool), "the discovered tool is still known");

        let (reply, ack) = oneshot::channel();
        cmd.send(McpCmd::ConnectForTool {
            tool_name: tool.clone(),
            reply,
            parent: tracing::Span::none(),
        })
        .await
        .unwrap();
        ack.await.unwrap().unwrap();
        assert!(
            catalog.get(&name).is_some(),
            "dispatch reconnected the server"
        );

        let (reply, ack) = oneshot::channel();
        cmd.send(McpCmd::ConnectForTool {
            tool_name: "mcp_other_tool".into(),
            reply,
            parent: tracing::Span::none(),
        })
        .await
        .unwrap();
        assert!(
            ack.await.unwrap().is_err(),
            "unknown tools never connect anything"
        );

        let (reply, ack) = oneshot::channel();
        cmd.send(McpCmd::TurnBoundary {
            token: "opaque-b".into(),
            desired: Some(Vec::new()),
            reply,
            parent: tracing::Span::none(),
        })
        .await
        .unwrap();
        ack.await.unwrap().unwrap();
        assert!(
            !known.contains(&tool),
            "detaching a server forgets its tools"
        );

        let mut seen = Vec::new();
        while let Ok(event) = events.try_recv() {
            seen.push(event);
        }
        let initialized: Vec<_> = seen
            .iter()
            .filter(|(_, method)| method == "initialize")
            .map(|(bearer, _)| bearer.as_str())
            .collect();
        assert_eq!(initialized, vec!["Bearer opaque-a", "Bearer opaque-b"]);
        let deleted: Vec<_> = seen
            .iter()
            .filter(|(_, method)| method == "DELETE")
            .map(|(bearer, _)| bearer.as_str())
            .collect();
        assert_eq!(
            deleted,
            vec!["Bearer opaque-a", "Bearer opaque-b"],
            "each session is closed with the credential it was opened with"
        );
        server_task.abort();
    }

    #[tokio::test]
    async fn gateway_mode_exposes_six_meta_tools_surfaces_instructions_and_hides_tool_search() {
        use crate::catalog::{HiddenCatalogSource, PerServerOnlyToolSource, UnknownToolSource};
        use crate::compaction::TranscriptEpoch;
        use crate::tools::tool_search::ToolSearchTool;
        use agentkit_tools_core::{
            CompositePermissionChecker, OwnedToolContext, PermissionDecision, ToolRequest,
        };
        use axum::{Json, Router, http::StatusCode, response::IntoResponse, routing::post};
        use serde_json::{Value, json};
        use std::sync::atomic::{AtomicUsize, Ordering};

        const GATEWAY_INSTRUCTIONS: &str =
            "Tools from these servers: \"Linear\", \"Speakeasy assistant tools\".\n\nUse list_servers and describe_server.";
        let meta_tools = [
            "list_servers",
            "describe_server",
            "describe_tools",
            "execute_tool",
            "execute_read_only_tool",
            "connection_status",
        ];
        let list_servers_calls = Arc::new(AtomicUsize::new(0));
        let calls_counter = Arc::clone(&list_servers_calls);

        let app = Router::new().route(
            "/gateway",
            post(move |Json(message): Json<Value>| {
                let calls_counter = Arc::clone(&calls_counter);
                async move {
                    if message.get("id").is_none() {
                        return StatusCode::ACCEPTED.into_response();
                    }
                    let method = message["method"].as_str().unwrap_or("");
                    if method == "server/discover" {
                        return Json(json!({
                            "jsonrpc": "2.0",
                            "id": message["id"],
                            "error": {"code": -32601, "message": "Method not found"}
                        }))
                        .into_response();
                    }
                    let result = match method {
                        "initialize" => json!({
                            "protocolVersion": message["params"]["protocolVersion"],
                            "capabilities": {"tools": {}},
                            "serverInfo": {"name": "project-gateway", "version": "1"},
                            "instructions": GATEWAY_INSTRUCTIONS,
                        }),
                        "tools/list" => json!({
                            "tools": meta_tools.iter().map(|name| json!({
                                "name": name,
                                "description": format!("gateway {name}"),
                                "inputSchema": {"type": "object"},
                            })).collect::<Vec<_>>()
                        }),
                        "tools/call" => {
                            if message["params"]["name"] == "list_servers" {
                                calls_counter.fetch_add(1, Ordering::SeqCst);
                            }
                            json!({
                                "content": [{"type": "text", "text": "{\"servers\":[]}"}],
                                "isError": false
                            })
                        }
                        _ => json!({}),
                    };
                    Json(json!({"jsonrpc": "2.0", "id": message["id"], "result": result}))
                        .into_response()
                }
            })
            .get(|| async { StatusCode::METHOD_NOT_ALLOWED })
            .delete(|| async { StatusCode::OK }),
        );
        let listener = tokio::net::TcpListener::bind("127.0.0.1:0").await.unwrap();
        let addr = listener.local_addr().unwrap();
        let server_task = tokio::spawn(async move {
            axum::serve(listener, app).await.unwrap();
        });

        let tokens = TokenRegistry::new("gw-token-1");
        let client = reqwest::Client::new();
        let (notice_tx, mut notices) = mpsc::unbounded_channel();
        let gateway = McpServer {
            id: "gateway".into(),
            url: format!("http://{addr}/gateway"),
            headers: BTreeMap::new(),
            is_gateway: true,
        };
        let (cmd, catalog, known, gateway_mode) = spawn_mcp_actor(
            GramBootstrapClient::new(
                format!("http://{addr}"),
                crate::http_layer::build_bootstrap_client(client.clone()),
            ),
            client,
            "T",
            std::slice::from_ref(&gateway),
            &tokens,
            notice_tx,
        )
        .unwrap();

        let catalog_source =
            HiddenCatalogSource::new(catalog.clone(), cmd.clone(), known, gateway_mode.clone());
        let search_source = PerServerOnlyToolSource::new(
            ToolSearchTool::new(catalog, cmd.clone(), TranscriptEpoch::default()),
            gateway_mode.clone(),
        );
        let unknown_source = UnknownToolSource::new(gateway_mode.clone());

        // Turn boundary eagerly connects the gateway, populates the 6 bare
        // meta-tools, and emits initialize.instructions.
        let (reply, ack) = oneshot::channel();
        cmd.send(McpCmd::TurnBoundary {
            token: "gw-token-1".into(),
            desired: Some(vec![gateway.clone()]),
            reply,
            parent: tracing::Span::none(),
        })
        .await
        .unwrap();
        ack.await.unwrap().unwrap();

        let declared: BTreeSet<String> = catalog_source
            .specs()
            .into_iter()
            .map(|s| s.name.0)
            .collect();
        let expected: BTreeSet<String> = meta_tools.iter().map(|s| (*s).to_string()).collect();
        assert_eq!(
            declared, expected,
            "gateway mode must expose all 6 meta-tools directly under bare names"
        );
        assert!(
            search_source.specs().is_empty(),
            "tool_search must be omitted from declared specs in gateway mode"
        );
        assert!(
            search_source.get(&ToolName::new("tool_search")).is_none(),
            "tool_search must not dispatch in gateway mode"
        );

        let mut received_notices = Vec::new();
        while let Ok(RunnerContent::Text(text)) = notices.try_recv() {
            received_notices.push(text);
        }
        assert!(
            received_notices.iter().any(|n| n.contains("<project-gateway-instructions>")
                && n.contains(GATEWAY_INSTRUCTIONS)),
            "gateway initialize.instructions must be surfaced: {received_notices:?}"
        );

        // Calling a bare gateway meta-tool dispatches to the gateway.
        let list_tool = catalog_source
            .get(&ToolName::new("list_servers"))
            .expect("bare list_servers must resolve");
        let owned_ctx = OwnedToolContext {
            session_id: "session-1".into(),
            turn_id: "turn-1".into(),
            metadata: Default::default(),
            permissions: Arc::new(CompositePermissionChecker::new(PermissionDecision::Allow)),
            resources: Arc::new(()),
            cancellation: None,
            execution_scope: None,
            approved_request: None,
        };
        let mut ctx = owned_ctx.borrowed();
        let result = list_tool
            .invoke(
                ToolRequest::new("call-1", "list_servers", json!({}), "session-1", "turn-1"),
                &mut ctx,
            )
            .await
            .unwrap();
        assert!(!result.result.is_error);
        assert_eq!(list_servers_calls.load(Ordering::SeqCst), 1);

        // Unknown tool in gateway mode points to gateway discovery tools, not tool_search.
        let unknown = unknown_source
            .get(&ToolName::new("nonexistent"))
            .expect("unknown source resolves fallback");
        let err_result = unknown
            .invoke(
                ToolRequest::new("call-2", "nonexistent", json!({}), "session-1", "turn-1"),
                &mut ctx,
            )
            .await
            .unwrap();
        assert!(err_result.result.is_error);
        let err_text = format!("{:?}", err_result.result.output);
        assert!(err_text.contains("list_servers"));
        assert!(!err_text.contains("tool_search"));

        // Flip mid-thread back to per-server mode: gateway tools disappear,
        // tool_search returns, and assistant_mcp_servers_updated notice fires.
        let per_server = McpServer {
            id: "linear".into(),
            url: format!("http://{addr}/gateway"),
            headers: BTreeMap::new(),
            is_gateway: false,
        };
        let (reply, ack) = oneshot::channel();
        cmd.send(McpCmd::TurnBoundary {
            token: "gw-token-2".into(),
            desired: Some(vec![per_server]),
            reply,
            parent: tracing::Span::none(),
        })
        .await
        .unwrap();
        ack.await.unwrap().unwrap();

        assert!(
            catalog_source.specs().is_empty(),
            "per-server mode must hide MCP tools again"
        );
        assert_eq!(
            search_source
                .specs()
                .into_iter()
                .map(|s| s.name.0)
                .collect::<Vec<_>>(),
            vec!["tool_search".to_string()],
            "per-server mode must restore tool_search"
        );
        let mut flip_notices = Vec::new();
        while let Ok(RunnerContent::Text(text)) = notices.try_recv() {
            flip_notices.push(text);
        }
        assert!(
            flip_notices.iter().any(|n| n.contains("assistant_mcp_servers_updated")
                && n.contains("Use tool_search")),
            "flip back to per-server must emit update notice: {flip_notices:?}"
        );

        server_task.abort();
    }

    #[tokio::test]
    async fn gateway_401_fails_clearly_without_creating_mcp_auth_flow() {
        use axum::{Router, http::StatusCode, routing::post};
        use std::sync::atomic::{AtomicUsize, Ordering};

        let auth_flow_calls = Arc::new(AtomicUsize::new(0));
        let auth_flow_counter = Arc::clone(&auth_flow_calls);

        let app = Router::new()
            .route(
                "/gateway",
                post(|| async {
                    (
                        StatusCode::UNAUTHORIZED,
                        [(
                            axum::http::header::WWW_AUTHENTICATE,
                            r#"Bearer error="invalid_token""#,
                        )],
                    )
                }),
            )
            .route(
                "/rpc/assistantMcpAuth.create",
                post(move || {
                    let counter = Arc::clone(&auth_flow_counter);
                    async move {
                        counter.fetch_add(1, Ordering::SeqCst);
                        StatusCode::OK
                    }
                }),
            );
        let listener = tokio::net::TcpListener::bind("127.0.0.1:0").await.unwrap();
        let addr = listener.local_addr().unwrap();
        let server_task = tokio::spawn(async move {
            axum::serve(listener, app).await.unwrap();
        });

        let tokens = TokenRegistry::new("gw-rejected");
        let client = reqwest::Client::new();
        let (notice_tx, mut notices) = mpsc::unbounded_channel();
        let gateway = McpServer {
            id: "gateway".into(),
            url: format!("http://{addr}/gateway"),
            headers: BTreeMap::new(),
            is_gateway: true,
        };
        let (cmd, _catalog, _known, _gw) = spawn_mcp_actor(
            GramBootstrapClient::new(
                format!("http://{addr}"),
                crate::http_layer::build_bootstrap_client(client.clone()),
            ),
            client,
            "T",
            std::slice::from_ref(&gateway),
            &tokens,
            notice_tx,
        )
        .unwrap();

        let (reply, ack) = oneshot::channel();
        cmd.send(McpCmd::TurnBoundary {
            token: "gw-rejected".into(),
            desired: Some(vec![gateway]),
            reply,
            parent: tracing::Span::none(),
        })
        .await
        .unwrap();
        let err = ack
            .await
            .unwrap()
            .expect_err("gateway 401 must fail the turn clearly");
        assert!(
            err.to_string().contains("401 Unauthorized"),
            "unexpected error: {err}"
        );
        assert_eq!(
            auth_flow_calls.load(Ordering::SeqCst),
            0,
            "gateway 401 must never invoke /rpc/assistantMcpAuth.create"
        );
        assert!(
            notices.try_recv().is_err(),
            "gateway 401 must not emit assistant_mcp_auth_required"
        );
        server_task.abort();
    }
}
