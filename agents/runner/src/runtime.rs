use std::collections::BTreeSet;
use std::panic::AssertUnwindSafe;
use std::path::PathBuf;
use std::sync::{Arc, Mutex, OnceLock};
use std::time::{Duration, Instant};

use agentkit_adapter_completions::CompletionsAdapter;
use agentkit_core::{Item, ItemKind, Part, TextPart, ToolCallPart, ToolOutput, ToolResultPart};
use agentkit_loop::{
    Agent, LoopDriver, LoopInterrupt, LoopStep, ModelSession, PromptCacheRequest,
    PromptCacheRetention, SessionConfig,
};
use agentkit_mcp::{
    McpError, McpServerConfig, McpServerId, McpServerManager, McpServerOptions,
    McpTransportBinding, StreamableHttpTransportConfig,
};
use agentkit_provider_openrouter::{OpenRouterConfig, OpenRouterProvider};
use agentkit_reporting::TracingReporter;
use agentkit_tool_fs::{FileSystemToolPolicy, FileSystemToolResources};
use agentkit_tools_core::{
    CatalogReader, CompositePermissionChecker, PathPolicy, PermissionDecision, ToolRegistry,
};
use dashmap::DashMap;
use futures::FutureExt;
use serde_json::Value;
use tokio::sync::mpsc::{self, UnboundedReceiver, UnboundedSender};
use tokio::sync::{OnceCell, oneshot};

use agentkit_compaction::{AgentBuilderCompactorExt, CompactionReason, Compactor};

use crate::clip::ClippedToolSource;
use crate::compaction::{Compaction, PersistingCompactor, PrimaryCompaction, build_compactor};
use crate::errors::RunnerError;
use crate::gram_client::GramBootstrapClient;
use crate::http_layer::{McpRotatingClient, TokenRegistry, build_bootstrap_client, build_http};
use crate::tools;
use crate::wire::{McpServer, RunnerMessage, ThreadBootstrap};
use crate::workdir::ASSISTANT_WORKDIR;

const TOOL_RESULT_SPILL_DIR: &str = "tool-results";
const MCP_CMD_CAPACITY: usize = 32;

/// TCP/TLS connect bound for runner-originated HTTP requests.
const HTTP_CONNECT_TIMEOUT: Duration = Duration::from_secs(3);

/// Per-server bound on the MCP discovery handshake at connect time.
const MCP_HANDSHAKE_TIMEOUT: Duration = Duration::from_secs(10);

/// How long a thread's per-task state can sit idle before the host evicts
/// it. The VM stays alive across all per-thread events; only individual
/// thread tasks expire.
pub const DEFAULT_THREAD_IDLE_TTL: Duration = Duration::from_secs(30 * 60);

/// How often the eviction sweep runs. Picked to keep the worst-case
/// over-retention small relative to the TTL while still being cheap.
const EVICTION_SWEEP_INTERVAL: Duration = Duration::from_secs(60);

pub type AppState = Arc<RuntimeHost>;

/// Singleton host shared by every per-thread task on the VM.
pub struct RuntimeHost {
    /// The assistant this VM serves, surfaced in /state. Set from the
    /// GRAM_ASSISTANT_ID boot env when present (Fly, GKE cold-start), or
    /// stamped by the first turn that carries one (GKE warm-pool sandbox).
    /// Set-once: boot env wins.
    pub assistant_id: OnceLock<String>,
    pub started_at: Instant,
    /// Per-idempotency-key admission slot. The bool tracks whether the
    /// keyed turn has actually been enqueued: holding the mutex covers
    /// the check + bootstrap + enqueue + mark-done sequence so concurrent
    /// retries with the same key serialize. A failed admission drops the
    /// guard with `false`, leaving the slot retryable.
    pub seen: DashMap<String, Arc<tokio::sync::Mutex<bool>>>,
    pub threads: DashMap<String, Arc<OnceCell<Arc<ConfiguredThread>>>>,
    pub gram_client: GramBootstrapClient,
    pub thread_idle_ttl: Duration,
    pub http_client: reqwest::Client,
    pub spill_root: PathBuf,
    /// Fallback bearer used only when `/threads/turn` arrives with no
    /// `auth_token` so the bootstrap fetch still has a credential.
    pub initial_token: String,
    /// Most recently delivered runtime bearer, used only by the workspace
    /// growth callback. It never leaves the runner or enters a subprocess.
    pub workspace_tokens: TokenRegistry,
}

/// Live per-thread state. Concurrent first-turn requests for the same
/// thread race through an `OnceCell` so only one bootstrap fetch and one
/// task spawn happen.
pub struct ConfiguredThread {
    pub thread_id: String,
    pub chat_id: String,
    pub idle_since: Arc<Mutex<Option<Instant>>>,
    pub inbox_tx: UnboundedSender<String>,
    pub task_handle: Mutex<Option<tokio::task::JoinHandle<()>>>,
    pub tokens: TokenRegistry,
    pub mcp_cmd_tx: mpsc::Sender<McpCmd>,
}

pub enum McpCmd {
    ForceReconnect {
        server_id: McpServerId,
        reply: oneshot::Sender<Result<(), String>>,
    },
    /// Sent by `/threads/{id}/turn` when the server-side toolset has
    /// drifted from the snapshot the runner bootstrapped with. The actor
    /// diffs `desired` against currently-registered servers, connects
    /// any new ones, and disconnects ones that no longer apply.
    Reconcile { desired: Vec<McpServer> },
}

impl ConfiguredThread {
    pub fn idle_for(&self) -> Duration {
        let guard = match self.idle_since.lock() {
            Ok(g) => g,
            Err(_) => return Duration::ZERO,
        };
        match *guard {
            None => Duration::ZERO,
            Some(t) => Instant::now().saturating_duration_since(t),
        }
    }

    pub fn enqueue(&self, input: String) -> Result<(), RunnerError> {
        self.inbox_tx
            .send(input)
            .map_err(|_| RunnerError::SubmitInput("loop inbox closed".into()))?;
        mark_busy(&self.idle_since);
        Ok(())
    }
}

pub async fn build_host(
    assistant_id: Option<String>,
    server_url: String,
    initial_token: String,
    thread_idle_ttl: Duration,
) -> Result<Arc<RuntimeHost>, RunnerError> {
    let workspace_tokens = TokenRegistry::new(initial_token.clone());
    let mut default_headers = http::HeaderMap::new();
    default_headers.insert(
        http::HeaderName::from_static("x-gram-source"),
        http::HeaderValue::from_static("assistant"),
    );
    let http_client = reqwest::Client::builder()
        .user_agent(concat!("gram-assistant-runner/", env!("CARGO_PKG_VERSION")))
        .default_headers(default_headers)
        .connect_timeout(HTTP_CONNECT_TIMEOUT)
        .build()?;

    let spill_root = PathBuf::from(ASSISTANT_WORKDIR).join(TOOL_RESULT_SPILL_DIR);

    let gram_client =
        GramBootstrapClient::new(server_url, build_bootstrap_client(http_client.clone()));
    let assistant_id_cell = OnceLock::new();
    // Ignore an empty/whitespace boot env so warm-pool pods stay unbound and
    // can learn their assistant from the first turn.
    if let Some(id) = assistant_id.filter(|id| !id.trim().is_empty()) {
        let _ = assistant_id_cell.set(id);
    }
    let host = Arc::new(RuntimeHost {
        assistant_id: assistant_id_cell,
        started_at: Instant::now(),
        seen: DashMap::new(),
        threads: DashMap::new(),
        gram_client,
        thread_idle_ttl,
        http_client,
        spill_root,
        initial_token,
        workspace_tokens,
    });

    // Background eviction task: walks the threads map and drops any whose
    // idle clock has run past the TTL. Runs for the lifetime of the host.
    let evict_host = Arc::clone(&host);
    tokio::spawn(async move {
        let mut interval = tokio::time::interval(EVICTION_SWEEP_INTERVAL);
        interval.tick().await;
        loop {
            interval.tick().await;
            sweep_idle(&evict_host).await;
        }
    });

    Ok(host)
}

/// Snapshot active threads — used by /state and the eviction sweep.
pub fn snapshot_threads(host: &RuntimeHost) -> Vec<(String, String, Duration)> {
    host.threads
        .iter()
        .filter_map(|entry| {
            let cell = entry.value().clone();
            cell.get().map(|thread| {
                (
                    thread.thread_id.clone(),
                    thread.chat_id.clone(),
                    thread.idle_for(),
                )
            })
        })
        .collect()
}

async fn sweep_idle(host: &Arc<RuntimeHost>) {
    let ttl = host.thread_idle_ttl;
    let mut to_evict = Vec::new();
    for entry in host.threads.iter() {
        let cell = entry.value().clone();
        if let Some(thread) = cell.get()
            && thread.idle_for() > ttl
        {
            to_evict.push(thread.thread_id.clone());
        }
    }
    for thread_id in to_evict {
        evict_thread(host, &thread_id);
    }
}

fn evict_thread(host: &RuntimeHost, thread_id: &str) {
    if let Some((_, cell)) = host.threads.remove(thread_id)
        && let Some(thread) = cell.get()
    {
        tracing::info!(thread_id = %thread_id, "evicting thread");
        // Closing the inbox causes run_loop to return; abort the task
        // for prompt teardown of any blocked compactor / model call.
        if let Ok(mut handle_slot) = thread.task_handle.lock()
            && let Some(handle) = handle_slot.take()
        {
            handle.abort();
        }
    }
    // Idempotency keys are scoped per thread (`{thread_id}:{event_id}`),
    // so an evicted thread's keys can never match a future /turn. Drop
    // them so `seen` does not grow without bound over the VM lifetime.
    let prefix = format!("{thread_id}:");
    host.seen.retain(|key, _| !key.starts_with(&prefix));
}

fn reap_oldest_idle(host: &RuntimeHost) {
    let victim = host
        .threads
        .iter()
        .filter_map(|entry| {
            let thread = entry.value().get()?;
            let guard = thread.idle_since.lock().ok()?;
            let since = (*guard)?;
            Some((thread.thread_id.clone(), since))
        })
        .min_by_key(|(_, since)| *since)
        .map(|(id, _)| id);
    if let Some(thread_id) = victim {
        evict_thread(host, &thread_id);
    }
}

/// First-turn bootstrap path. Concurrent /turn requests for the same thread
/// race through the `OnceCell`; only one wins the bootstrap fetch and task
/// spawn. Subsequent turns rotate the existing thread's bearer slot.
pub async fn ensure_thread(
    host: &Arc<RuntimeHost>,
    thread_id: &str,
    auth_token: Option<String>,
) -> Result<Arc<ConfiguredThread>, RunnerError> {
    let cell = host
        .threads
        .entry(thread_id.to_string())
        .or_insert_with(|| Arc::new(OnceCell::new()))
        .clone();

    let bearer = auth_token
        .filter(|t| !t.is_empty())
        .unwrap_or_else(|| host.initial_token.clone());
    if !bearer.is_empty() {
        host.workspace_tokens.rotate(&bearer)?;
    }

    let mut initialized = false;
    let thread = cell
        .get_or_try_init(|| async {
            initialized = true;
            // Reap skips busy threads and our own (still-uninitialized)
            // OnceCell, so worst case is a no-op.
            reap_oldest_idle(host);
            let tokens = TokenRegistry::new(bearer.clone());
            let bootstrap = host
                .gram_client
                .fetch_bootstrap(thread_id, &tokens)
                .await
                .map_err(|e| RunnerError::Loop(format!("bootstrap fetch failed: {e}")))?;
            spawn_thread(host, thread_id.to_string(), bootstrap, tokens).await
        })
        .await?;

    if !initialized && !bearer.is_empty() {
        thread.tokens.rotate(&bearer)?;
    }
    Ok(thread.clone())
}

/// Builds a per-thread agent and spawns its tokio task. Each task is wrapped
/// in `catch_unwind` so a panic inside one thread's tool call, stream
/// parser, or MCP client does not take down the VM or sibling threads.
async fn spawn_thread(
    host: &Arc<RuntimeHost>,
    thread_id: String,
    bootstrap: ThreadBootstrap,
    tokens: TokenRegistry,
) -> Result<Arc<ConfiguredThread>, RunnerError> {
    let (inbox_tx, inbox_rx) = mpsc::unbounded_channel::<String>();

    let (mcp_cmd_tx, mcp_catalog, mcp_auth_notices) = build_thread_mcp(
        host,
        &thread_id,
        &bootstrap.mcp_servers,
        &tokens,
        inbox_tx.clone(),
    )
    .await?;

    let chat_id = bootstrap.chat_id.clone();

    // Per-thread completions adapter. Outbound /chat/completions calls
    // carry the thread's chat id so the server's revalidation check can
    // confirm the chat belongs to the assistant on the JWT.
    let mut chat_headers = http::HeaderMap::new();
    chat_headers.insert(
        http::HeaderName::from_static("gram-chat-id"),
        http::HeaderValue::from_str(&chat_id)
            .map_err(|source| RunnerError::HeaderValue { source })?,
    );
    chat_headers.insert(
        http::HeaderName::from_static("x-gram-source"),
        http::HeaderValue::from_static("assistant"),
    );
    let thread_http_client = reqwest::Client::builder()
        .user_agent(concat!("gram-assistant-runner/", env!("CARGO_PKG_VERSION")))
        .default_headers(chat_headers)
        .build()?;

    let openrouter_config = OpenRouterConfig::new(String::new(), bootstrap.model.clone())
        .with_base_url(bootstrap.completions_url.clone());
    let provider = OpenRouterProvider::from(openrouter_config);

    let completions_http = build_http(thread_http_client.clone(), tokens.clone());
    let adapter = CompletionsAdapter::with_client(provider.clone(), completions_http);

    // Compactor outbound headers carry the same gram-chat-id as the main
    // adapter so the server's assistant-scope guard (which rejects any
    // assistant-runtime request without a chat id) lets the call through,
    // plus gram-skip-capture: 1 so the capture pipeline drops the
    // compactor's "summarise this transcript" turn instead of persisting
    // it as divergence on the user's chat.
    let mut compactor_headers = http::HeaderMap::new();
    compactor_headers.insert(
        http::HeaderName::from_static("gram-chat-id"),
        http::HeaderValue::from_str(&chat_id)
            .map_err(|source| RunnerError::HeaderValue { source })?,
    );
    compactor_headers.insert(
        http::HeaderName::from_static("gram-skip-capture"),
        http::HeaderValue::from_static("1"),
    );
    compactor_headers.insert(
        http::HeaderName::from_static("x-gram-source"),
        http::HeaderValue::from_static("assistant"),
    );
    let compactor_http_client = reqwest::Client::builder()
        .user_agent(concat!("gram-assistant-runner/", env!("CARGO_PKG_VERSION")))
        .default_headers(compactor_headers)
        .build()?;
    let compactor_http = build_http(compactor_http_client, tokens.clone());
    let compactor_adapter = CompletionsAdapter::with_client(provider, compactor_http);

    let compaction = build_compactor(
        &bootstrap.compaction,
        &bootstrap.chat_id,
        &thread_id,
        bootstrap.context_window.unwrap_or(0),
        compactor_adapter,
        host.gram_client.clone(),
        tokens.clone(),
    )?;

    let mut transcript = Vec::new();
    if !bootstrap.instructions.is_empty() {
        transcript.push(Item::text(ItemKind::System, &bootstrap.instructions));
    }
    transcript.extend(normalize_history(&bootstrap.history)?);
    for notice in mcp_auth_notices {
        transcript.push(Item::text(ItemKind::User, &notice));
    }

    let permissions = CompositePermissionChecker::new(PermissionDecision::Allow).with_policy(
        PathPolicy::new()
            .allow_root(ASSISTANT_WORKDIR)
            .read_only_root(format!("{ASSISTANT_WORKDIR}/node_modules"))
            .read_only_root(format!("{ASSISTANT_WORKDIR}/browser.ts"))
            .read_only_root(format!("{ASSISTANT_WORKDIR}/package.json")),
    );
    let fs_resources = FileSystemToolResources::new()
        .with_policy(FileSystemToolPolicy::new().require_read_before_write(true));

    let native_tools = ToolRegistry::new().with(tools::bun_run::bun_run).with(
        tools::mcp_force_reconnect::McpForceReconnectTool::new(Arc::clone(host)),
    );

    let compose_source = agentkit_tool_compose::ComposeTool::wrap(mcp_catalog)
        .with_source(native_tools.merge(agentkit_tool_fs::registry()));
    let clipped_source = ClippedToolSource::new(compose_source, host.spill_root.clone());

    let mut builder = Agent::builder()
        .model(adapter)
        .add_tool_source(clipped_source)
        .permissions(permissions)
        .resources(fs_resources)
        .observer(TracingReporter::new())
        .transcript(transcript);

    // Register the loop mutator(s): the policy's own compactor (Threshold's
    // shrink-to-fit, or OnTurnEnd's cache-replay mutator) plus the universal
    // mid-turn safety fallback when present. The OnTurnEnd `terminal` pass runs
    // explicitly at turn end in `run_loop`.
    let turn_end_compactor = match compaction {
        Some(Compaction { primary, fallback }) => {
            let terminal = match primary {
                Some(PrimaryCompaction::Inline(compactor)) => {
                    builder = builder.compactor(compactor);
                    None
                }
                Some(PrimaryCompaction::TurnEnd { mutator, terminal }) => {
                    builder = builder.compactor(mutator);
                    Some(terminal)
                }
                None => None,
            };
            if let Some(fallback) = fallback {
                builder = builder.compactor(fallback);
            }
            terminal
        }
        None => None,
    };

    let agent = builder
        .build()
        .map_err(|e| RunnerError::AgentBuild(e.to_string()))?;

    let session = SessionConfig::new(bootstrap.chat_id.clone())
        .with_cache(PromptCacheRequest::automatic().with_retention(PromptCacheRetention::Short));
    let driver = agent
        .start(session)
        .await
        .map_err(|e| RunnerError::AgentStart(e.to_string()))?;

    let idle_since = Arc::new(Mutex::new(Some(Instant::now())));
    let loop_idle = Arc::clone(&idle_since);
    let log_thread_id = thread_id.clone();
    let host_for_eviction = Arc::clone(host);
    let evict_thread_id = thread_id.clone();

    let task_handle = tokio::spawn(async move {
        let outcome = AssertUnwindSafe(run_loop(driver, inbox_rx, loop_idle, turn_end_compactor))
            .catch_unwind()
            .await;
        match outcome {
            Ok(Ok(reason)) => {
                tracing::info!(thread_id = %log_thread_id, reason = %reason, "thread loop exited")
            }
            Ok(Err(err)) => {
                tracing::error!(thread_id = %log_thread_id, error = %err, "thread loop exited with error")
            }
            Err(panic_payload) => {
                let msg = panic_payload
                    .downcast_ref::<&'static str>()
                    .map(|s| (*s).to_string())
                    .or_else(|| panic_payload.downcast_ref::<String>().cloned())
                    .unwrap_or_else(|| "<panic payload>".to_string());
                tracing::error!(thread_id = %log_thread_id, panic = %msg, "thread loop panicked");
            }
        }
        // Drop the entry on exit so a stale ConfiguredThread doesn't keep
        // holding state for a dead task.
        host_for_eviction.threads.remove(&evict_thread_id);
    });

    let configured = Arc::new(ConfiguredThread {
        thread_id,
        chat_id,
        idle_since,
        inbox_tx,
        task_handle: Mutex::new(Some(task_handle)),
        tokens,
        mcp_cmd_tx,
    });
    Ok(configured)
}

async fn build_thread_mcp(
    host: &Arc<RuntimeHost>,
    thread_id: &str,
    servers: &[McpServer],
    tokens: &TokenRegistry,
    inbox_tx: UnboundedSender<String>,
) -> Result<(mpsc::Sender<McpCmd>, CatalogReader, Vec<String>), RunnerError> {
    let mut manager = McpServerManager::new();
    let catalog = manager.source();
    let mut auth_notices = Vec::new();
    let mut known = BTreeSet::new();
    let configured: BTreeSet<String> = servers.iter().map(|s| s.id.clone()).collect();

    for server in servers {
        let config = build_mcp_server_config(server, &host.http_client, tokens)?;
        manager.register_server_with_options(
            config,
            McpServerOptions::new().with_timeout(MCP_HANDSHAKE_TIMEOUT),
        );
    }

    let settled = manager.connect_all_settled().await;
    for handle in settled.connected() {
        tracing::info!(
            server_id = %handle.server_id(),
            tools = handle.snapshot().tools.len(),
            action = "register",
            "mcp connect ok"
        );
        known.insert(handle.server_id().0.clone());
    }
    for failure in settled.failed() {
        let server_id = &failure.server_id;
        tracing::warn!(
            server_id = %server_id,
            error = %failure.error,
            action = "register",
            "mcp connect failed"
        );
        // Non-auth failures are transient transport errors: leave them out of
        // `known` so the next /turn reconcile retries instead of silently
        // dropping the integration for the rest of the thread.
        if !matches!(failure.error, McpError::AuthRequired(_)) {
            continue;
        }
        let Some(server) = servers.iter().find(|s| s.id == server_id.0) else {
            continue;
        };
        // Mark auth-pending as known only once the prompt is created, so a
        // transient auth-flow failure leaves the server out of `known` and the
        // next /turn reconcile retries the prompt instead of stranding the
        // integration unprompted for the thread's lifetime.
        if let Some(notice) =
            create_auth_notice(host, thread_id, &server.id, &server.url, tokens).await
        {
            known.insert(server_id.0.clone());
            auth_notices.push(notice);
        } else {
            tracing::warn!(
                server_id = %server_id,
                "auth prompt creation failed; will retry on next reconcile"
            );
        }
    }

    let (cmd_tx, cmd_rx) = mpsc::channel(MCP_CMD_CAPACITY);
    let actor_ctx = McpActorContext {
        host: Arc::clone(host),
        thread_id: thread_id.to_string(),
        tokens: tokens.clone(),
        inbox_tx,
        known,
        configured,
    };
    tokio::spawn(run_mcp_actor(manager, cmd_rx, actor_ctx));
    Ok((cmd_tx, catalog, auth_notices))
}

struct McpActorContext {
    host: Arc<RuntimeHost>,
    thread_id: String,
    tokens: TokenRegistry,
    inbox_tx: UnboundedSender<String>,
    // Servers currently connected or auth-pending. Drives reconcile's
    // add/remove diff and is mutated as connections come and go.
    known: BTreeSet<String>,
    // Server ids in the assistant's current configuration (the latest
    // reconcile's desired set). Gates ForceReconnect so a configured but
    // not-yet-connected server can still be retried, while a server detached
    // from the configuration cannot be resurrected.
    configured: BTreeSet<String>,
}

async fn create_auth_notice(
    host: &Arc<RuntimeHost>,
    thread_id: &str,
    server_id: &str,
    server_url: &str,
    tokens: &TokenRegistry,
) -> Option<String> {
    match host
        .gram_client
        .create_mcp_auth_flow(thread_id, server_id, server_url, tokens)
        .await
    {
        Ok(flow) => Some(format!(
            "<message-context>\nEventType: assistant_mcp_auth_required\nMCPServerID: {server_id}\nMCPSlug: {mcp_slug}\nAuthURL: {auth_url}\n</message-context>",
            server_id = flow.server_id,
            mcp_slug = flow.mcp_slug,
            auth_url = flow.auth_url,
        )),
        Err(flow_err) => {
            tracing::warn!(
                server_id,
                error = %flow_err,
                "failed to create assistant mcp auth flow"
            );
            None
        }
    }
}

async fn reconcile_servers(
    manager: &mut McpServerManager,
    ctx: &mut McpActorContext,
    desired: Vec<McpServer>,
) {
    let desired_ids: BTreeSet<String> = desired.iter().map(|s| s.id.clone()).collect();
    ctx.configured = desired_ids.clone();

    for server in &desired {
        if ctx.known.contains(&server.id) {
            continue;
        }
        let config = match build_mcp_server_config(server, &ctx.host.http_client, &ctx.tokens) {
            Ok(cfg) => cfg,
            Err(err) => {
                tracing::warn!(
                    server_id = %server.id,
                    error = %err,
                    "skip reconcile-added mcp server: config build failed"
                );
                continue;
            }
        };
        manager.register_server_with_options(
            config,
            McpServerOptions::new().with_timeout(MCP_HANDSHAKE_TIMEOUT),
        );
        let server_uid = McpServerId::new(server.id.clone());
        match connect_and_log(manager, &server_uid, "reconcile_add").await {
            Ok(()) => {
                ctx.known.insert(server.id.clone());
            }
            Err(err) if err.auth_required => {
                // Mark known only once the auth prompt is created, so a
                // transient auth-flow failure leaves the server out of `known`
                // and the next reconcile retries the prompt instead of
                // stranding the integration unprompted for the thread.
                match create_auth_notice(
                    &ctx.host,
                    &ctx.thread_id,
                    &server.id,
                    &server.url,
                    &ctx.tokens,
                )
                .await
                {
                    Some(notice) => {
                        ctx.known.insert(server.id.clone());
                        if ctx.inbox_tx.send(notice).is_err() {
                            tracing::warn!(
                                server_id = %server.id,
                                "drop reconcile auth notice: thread inbox closed"
                            );
                        }
                    }
                    None => {
                        tracing::warn!(
                            server_id = %server.id,
                            "reconcile auth prompt failed; will retry on next reconcile"
                        );
                    }
                }
            }
            Err(_) => {
                // Transient transport failure on connect: leave out of `known`
                // so a later reconcile re-attempts the connect. The server
                // stays in `configured`, so a manual mcp_force_reconnect is
                // still allowed to retry it immediately.
            }
        }
    }

    let removed: Vec<String> = ctx
        .known
        .iter()
        .filter(|id| !desired_ids.contains(*id))
        .cloned()
        .collect();
    for id in removed {
        let server_uid = McpServerId::new(id.clone());
        if let Err(err) = manager.disconnect_server(&server_uid).await {
            // Keep the id in `known` so the next reconcile retries the detach;
            // dropping it now would remove it from the `removed` diff forever,
            // leaving the server connected for the thread's lifetime.
            tracing::warn!(server_id = %id, error = %err, "reconcile disconnect failed; will retry");
            continue;
        }
        ctx.known.remove(&id);
    }
}

struct McpConnectFailure {
    message: String,
    auth_required: bool,
}

async fn connect_and_log(
    manager: &mut McpServerManager,
    server_id: &McpServerId,
    action: &'static str,
) -> Result<(), McpConnectFailure> {
    match manager.connect_server(server_id).await {
        Ok(handle) => {
            tracing::info!(
                server_id = %server_id,
                tools = handle.snapshot().tools.len(),
                action,
                "mcp connect ok"
            );
            Ok(())
        }
        Err(e) => {
            let auth_required = matches!(e, McpError::AuthRequired(_));
            tracing::warn!(server_id = %server_id, error = %e, action, "mcp connect failed");
            Err(McpConnectFailure {
                message: e.to_string(),
                auth_required,
            })
        }
    }
}

fn build_mcp_server_config(
    server: &McpServer,
    http_client: &reqwest::Client,
    tokens: &TokenRegistry,
) -> Result<McpServerConfig, RunnerError> {
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

async fn run_mcp_actor(
    mut manager: McpServerManager,
    mut cmd_rx: mpsc::Receiver<McpCmd>,
    mut ctx: McpActorContext,
) {
    while let Some(cmd) = cmd_rx.recv().await {
        match cmd {
            McpCmd::ForceReconnect { server_id, reply } => {
                // Reject ids that aren't part of the assistant's current
                // configuration. `disconnect_server` only clears `connections`;
                // the underlying config lingers in the manager for the thread's
                // lifetime (agentkit-mcp exposes no unregister path). Gating on
                // `configured` (not `known`) lets a user retry a configured
                // server that is not yet connected, while still refusing to
                // resurrect one that has been detached from the configuration.
                if !ctx.configured.contains(server_id.0.as_str()) {
                    let _ = reply.send(Err(format!(
                        "mcp server {server_id} is not part of this assistant's current configuration"
                    )));
                    continue;
                }
                if let Err(e) = manager.disconnect_server(&server_id).await {
                    tracing::debug!(server_id = %server_id, error = %e, "disconnect during force reconnect");
                }
                let result = connect_and_log(&mut manager, &server_id, "force_reconnect")
                    .await
                    .map_err(|err| err.message);
                let _ = reply.send(result);
            }
            McpCmd::Reconcile { desired } => {
                reconcile_servers(&mut manager, &mut ctx, desired).await;
            }
        }
    }
}

async fn run_loop<S>(
    mut driver: LoopDriver<S>,
    mut inbox: UnboundedReceiver<String>,
    idle_since: Arc<Mutex<Option<Instant>>>,
    turn_end_compactor: Option<PersistingCompactor>,
) -> Result<&'static str, RunnerError>
where
    S: ModelSession,
{
    loop {
        match driver.next().await? {
            LoopStep::Finished(_turn) => {
                if let Some(compactor) = &turn_end_compactor {
                    compact_at_turn_end(compactor, &driver).await;
                }
                mark_idle(&idle_since);
            }
            LoopStep::Interrupt(LoopInterrupt::AwaitingInput(req)) => {
                let drained = drain(&mut inbox);
                let items = if !drained.is_empty() {
                    drained_into_items(drained)
                } else {
                    match inbox.recv().await {
                        Some(msg) => vec![Item::text(ItemKind::User, &msg)],
                        None => return Ok("inbox closed"),
                    }
                };
                mark_busy(&idle_since);
                req.submit(&mut driver, items)?;
            }
            LoopStep::Interrupt(LoopInterrupt::AfterToolResult(info)) => {
                let drained = drain(&mut inbox);
                if !drained.is_empty() {
                    info.submit(&mut driver, drained_into_items(drained))?;
                }
            }
            LoopStep::Interrupt(LoopInterrupt::ApprovalRequest(pending)) => {
                tracing::warn!(
                    "unexpected approval request — runner auto-approves; tools should \
                     not require approval in this environment"
                );
                pending.approve(&mut driver)?;
            }
        }
    }
}

/// Compacts and persists the finished transcript for the next cold bootstrap,
/// discarding the result — it is never fed back to the model. Failures are
/// logged, not propagated, so a persistence hiccup can't kill the thread loop.
async fn compact_at_turn_end<S: ModelSession>(
    compactor: &PersistingCompactor,
    driver: &LoopDriver<S>,
) {
    let transcript = driver.snapshot().transcript;
    if let Err(err) = compactor
        .compact(
            &transcript,
            CompactionReason::Custom("on_turn_end".to_string()),
            None,
        )
        .await
    {
        tracing::warn!(error = %err, "turn-end compaction failed; skipping persist for this turn");
    }
}

fn drained_into_items(drained: Vec<String>) -> Vec<Item> {
    drained
        .into_iter()
        .map(|s| Item::text(ItemKind::User, &s))
        .collect()
}

fn drain(inbox: &mut UnboundedReceiver<String>) -> Vec<String> {
    let mut out = Vec::new();
    while let Ok(msg) = inbox.try_recv() {
        out.push(msg);
    }
    out
}

fn mark_busy(idle_since: &Arc<Mutex<Option<Instant>>>) {
    if let Ok(mut slot) = idle_since.lock() {
        *slot = None;
    }
}

fn mark_idle(idle_since: &Arc<Mutex<Option<Instant>>>) {
    if let Ok(mut slot) = idle_since.lock() {
        *slot = Some(Instant::now());
    }
}

fn normalize_history(history: &[RunnerMessage]) -> Result<Vec<Item>, RunnerError> {
    let mut items = Vec::with_capacity(history.len());
    for message in history {
        match message.role.as_str() {
            "user" => {
                items.push(Item::text(ItemKind::User, &message.content));
            }
            "assistant" => {
                let mut parts: Vec<Part> = Vec::new();
                if !message.content.is_empty() {
                    parts.push(Part::Text(TextPart::new(message.content.clone())));
                }
                for call in &message.tool_calls {
                    let input: Value = if call.arguments.is_empty() {
                        Value::Object(Default::default())
                    } else {
                        serde_json::from_str(&call.arguments).map_err(|source| {
                            RunnerError::ToolCallArguments {
                                id: call.id.clone(),
                                source,
                            }
                        })?
                    };
                    parts.push(Part::ToolCall(ToolCallPart::new(
                        call.id.clone(),
                        call.name.clone(),
                        input,
                    )));
                }
                items.push(Item::new(ItemKind::Assistant, parts));
            }
            "tool" => {
                let call_id = message
                    .tool_call_id
                    .as_deref()
                    .filter(|s| !s.is_empty())
                    .ok_or(RunnerError::MissingToolCallId)?;
                items.push(Item::new(
                    ItemKind::Tool,
                    vec![Part::ToolResult(ToolResultPart::success(
                        call_id,
                        ToolOutput::text(message.content.clone()),
                    ))],
                ));
            }
            "system" => {
                items.push(Item::text(ItemKind::System, &message.content));
            }
            other => {
                return Err(RunnerError::UnsupportedHistoryRole(other.to_string()));
            }
        }
    }
    Ok(items)
}

#[cfg(test)]
#[allow(clippy::unwrap_used, clippy::expect_used)]
mod tests {
    use super::*;
    use crate::http_layer::{TokenRegistry, build_bootstrap_client};

    fn empty_host() -> Arc<RuntimeHost> {
        let http_client = reqwest::Client::new();
        let gram_client = GramBootstrapClient::new(
            "http://localhost".to_string(),
            build_bootstrap_client(http_client.clone()),
        );
        let assistant_id = OnceLock::new();
        let _ = assistant_id.set("asst".to_string());
        Arc::new(RuntimeHost {
            assistant_id,
            started_at: Instant::now(),
            seen: DashMap::new(),
            threads: DashMap::new(),
            gram_client,
            thread_idle_ttl: Duration::from_secs(60 * 30),
            http_client,
            spill_root: PathBuf::from("/tmp/runtime-test-spill"),
            initial_token: String::new(),
            workspace_tokens: TokenRegistry::new(""),
        })
    }

    fn insert_thread(host: &RuntimeHost, thread_id: &str, idle_since: Option<Instant>) {
        let (inbox_tx, _inbox_rx) = mpsc::unbounded_channel::<String>();
        let (mcp_cmd_tx, _mcp_cmd_rx) = mpsc::channel::<McpCmd>(1);
        let handle = tokio::spawn(async {});
        let configured = Arc::new(ConfiguredThread {
            thread_id: thread_id.to_string(),
            chat_id: format!("chat-{thread_id}"),
            idle_since: Arc::new(Mutex::new(idle_since)),
            inbox_tx,
            task_handle: Mutex::new(Some(handle)),
            tokens: TokenRegistry::new(""),
            mcp_cmd_tx,
        });
        let cell = Arc::new(OnceCell::new());
        cell.set(configured)
            .map_err(|_| ())
            .expect("OnceCell should accept first set");
        host.threads.insert(thread_id.to_string(), cell);
    }

    #[tokio::test]
    async fn reap_oldest_idle_evicts_longest_idle_first() {
        let host = empty_host();
        let now = Instant::now();
        insert_thread(&host, "recent", Some(now));
        insert_thread(&host, "old", Some(now - Duration::from_secs(120)));
        insert_thread(&host, "medium", Some(now - Duration::from_secs(30)));

        reap_oldest_idle(&host);

        assert!(
            host.threads.get("old").is_none(),
            "longest-idle thread must be reaped first"
        );
        assert!(host.threads.get("recent").is_some());
        assert!(host.threads.get("medium").is_some());
    }

    #[tokio::test]
    async fn reap_oldest_idle_skips_busy_threads() {
        let host = empty_host();
        insert_thread(&host, "busy", None);

        reap_oldest_idle(&host);

        assert!(
            host.threads.get("busy").is_some(),
            "busy thread (idle_since == None) must never be reaped"
        );
    }

    #[tokio::test]
    async fn reap_oldest_idle_noop_on_empty() {
        let host = empty_host();
        reap_oldest_idle(&host);
        assert_eq!(host.threads.len(), 0);
    }

    #[tokio::test]
    async fn evict_thread_clears_seen_keys_with_prefix() {
        let host = empty_host();
        insert_thread(&host, "T", Some(Instant::now()));
        host.seen.insert(
            "T:evt-1".to_string(),
            Arc::new(tokio::sync::Mutex::new(true)),
        );
        host.seen.insert(
            "T:evt-2".to_string(),
            Arc::new(tokio::sync::Mutex::new(true)),
        );
        host.seen.insert(
            "other:evt-1".to_string(),
            Arc::new(tokio::sync::Mutex::new(true)),
        );

        evict_thread(&host, "T");

        assert!(host.threads.get("T").is_none());
        assert!(host.seen.get("T:evt-1").is_none());
        assert!(host.seen.get("T:evt-2").is_none());
        assert!(
            host.seen.get("other:evt-1").is_some(),
            "unrelated idempotency keys must survive eviction"
        );
    }
}
