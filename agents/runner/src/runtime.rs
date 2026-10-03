use std::panic::AssertUnwindSafe;
use std::path::PathBuf;
use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::{Arc, Mutex, Weak};
use std::time::{Duration, Instant};

use agentkit_adapter_completions::CompletionsAdapter;
use agentkit_core::{
    CancellationController, DataRef, FinishReason, Item, ItemKind, MediaPart, Modality, Part,
    TextPart, ToolCallPart, ToolOutput, ToolResultPart,
};
use agentkit_loop::{
    Agent, LoopDriver, LoopInterrupt, LoopStep, ModelSession, PromptCacheRequest,
    PromptCacheRetention, SessionConfig,
};
use agentkit_provider_openrouter::{OpenRouterConfig, OpenRouterProvider};
use agentkit_reporting::TracingReporter;
use agentkit_tool_fs::{FileSystemToolPolicy, FileSystemToolResources};
use agentkit_tools_core::{
    CompositePermissionChecker, PathPolicy, PermissionDecision, ToolRegistry,
};
use dashmap::DashMap;
use futures::FutureExt;
use serde_json::Value;
use tokio::sync::OnceCell;
use tokio::sync::mpsc::{self, UnboundedReceiver};
use tracing::Instrument;

use agentkit_compaction::{AgentBuilderCompactorExt, CompactionReason, Compactor};

use crate::catalog::{HiddenCatalogSource, UnknownToolSource};
use crate::clip::ClippedToolSource;
use crate::compaction::{Compaction, PersistingCompactor, PrimaryCompaction, build_compactor};
use crate::errors::RunnerError;
use crate::gram_client::GramBootstrapClient;
use crate::http_layer::{TokenRegistry, build_bootstrap_client, build_http};
use crate::mcp_actor::{McpCmd, spawn_mcp_actor};
use crate::telemetry::SpanIdentity;
use crate::tools;
use crate::wire::{McpServer, RunnerContent, RunnerContentPart, RunnerMessage, ThreadBootstrap};
use crate::workdir::ASSISTANT_WORKDIR;

const TOOL_RESULT_SPILL_DIR: &str = "tool-results";

/// TCP/TLS connect bound for runner-originated HTTP requests.
const HTTP_CONNECT_TIMEOUT: Duration = Duration::from_secs(3);

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
    /// Gram identity shared with the span processor registered in
    /// `init_tracing`; see [`SpanIdentity`] for the set-once discipline.
    pub identity: Arc<SpanIdentity>,
    pub started_at: Instant,
    /// Per-idempotency-key admission slot. The bool tracks whether the
    /// keyed turn has actually been enqueued: holding the mutex covers
    /// the check + bootstrap + enqueue + mark-done sequence so concurrent
    /// retries with the same key serialize. A failed admission drops the
    /// guard with `false`, leaving the slot retryable.
    pub seen: DashMap<String, Arc<EventSlot>>,
    /// Serializes bootstrap and enqueue without changing the active turn token.
    pub turn_admissions: DashMap<String, Weak<tokio::sync::Mutex<()>>>,
    pub threads: DashMap<String, Arc<OnceCell<Arc<ConfiguredThread>>>>,
    pub gram_client: GramBootstrapClient,
    pub thread_idle_ttl: Duration,
    pub mcp_http_client: reqwest::Client,
    pub spill_root: PathBuf,
}

/// Live per-thread state. Concurrent first-turn requests for the same
/// thread race through an `OnceCell` so only one bootstrap fetch and one
/// task spawn happen.
pub struct ConfiguredThread {
    pub thread_id: String,
    pub chat_id: String,
    pub idle_since: Arc<Mutex<Option<Instant>>>,
    pub inbox_tx: mpsc::Sender<QueuedTurn>,
    pub task_handle: Mutex<Option<tokio::task::JoinHandle<()>>>,
    /// Broadcasts user interrupts into the thread's agent loop. Bumping the
    /// generation cancels whatever checkpoint the turn in flight captured at
    /// its start; a bump while the thread is idle is inert, because the next
    /// turn checkpoints the new generation.
    pub cancellation: CancellationController,
}

/// A message and its credential travel together. Enqueueing never changes
/// credentials, reconciles tools, or injects input into the active model turn.
#[derive(Default)]
pub struct EventSlot {
    pub gate: tokio::sync::Mutex<()>,
    pub accepted: Arc<AtomicBool>,
}

pub struct TurnAdmission {
    pub event: Option<EventAdmission>,
    pub accepted: Arc<AtomicBool>,
    pub reply: tokio::sync::oneshot::Sender<()>,
}

pub struct QueuedTurn {
    pub input: RunnerContent,
    pub bearer: String,
    pub mcp_servers: Vec<McpServer>,
    pub admission: Option<TurnAdmission>,
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

    pub fn enqueue(&self, turn: QueuedTurn) -> Result<(), RunnerError> {
        // Serialize the busy transition with the loop's idle transition.
        let mut idle = self
            .idle_since
            .lock()
            .map_err(|_| RunnerError::SubmitInput("idle clock poisoned".into()))?;
        self.inbox_tx.try_send(turn).map_err(|error| match error {
            mpsc::error::TrySendError::Full(_) => RunnerError::InvocationBusy,
            mpsc::error::TrySendError::Closed(_) => {
                RunnerError::SubmitInput("loop inbox closed".into())
            }
        })?;
        *idle = None;
        Ok(())
    }

    /// Cancels the turn in flight, if any.
    ///
    /// Cooperative rather than abortive: the loop races the model stream and
    /// each tool call against this signal, so it unwinds through agentkit's own
    /// cancellation path — the partial assistant text stays in the transcript
    /// and the turn ends with [`FinishReason::Cancelled`] instead of leaving
    /// the driver mid-turn. The idle clock is NOT touched here; `run_loop`
    /// marks the thread idle when the cancelled turn actually finishes, so the
    /// warm-expiry sweep cannot retire a runtime that is still unwinding.
    ///
    /// Returns whether there was a turn to stop — input enqueued, or a driver
    /// step still running. A warm thread between turns has nothing in flight,
    /// and the bump is inert until a turn checkpoints the generation, so it
    /// answers false. A poisoned idle clock answers true: claiming a stop that
    /// did nothing is the safer error.
    pub fn interrupt(&self) -> bool {
        let busy = match self.idle_since.lock() {
            Ok(guard) => guard.is_none(),
            Err(_) => true,
        };
        self.cancellation.interrupt();
        busy
    }
}

pub async fn build_host(
    identity: Arc<SpanIdentity>,
    server_url: String,
    _initial_token: String,
    thread_idle_ttl: Duration,
) -> Result<Arc<RuntimeHost>, RunnerError> {
    let mut default_headers = http::HeaderMap::new();
    default_headers.insert(
        http::HeaderName::from_static("x-gram-source"),
        http::HeaderValue::from_static("assistant"),
    );
    let http_client = reqwest::Client::builder()
        .user_agent(concat!("gram-assistant-runner/", env!("CARGO_PKG_VERSION")))
        .default_headers(default_headers.clone())
        .connect_timeout(HTTP_CONNECT_TIMEOUT)
        .build()?;
    // MCP servers are externally configured endpoints. Never follow redirects so
    // per-server headers and bearer tokens only ever go to the configured origin.
    let mcp_http_client = reqwest::Client::builder()
        .user_agent(concat!("gram-assistant-runner/", env!("CARGO_PKG_VERSION")))
        .default_headers(default_headers)
        .connect_timeout(HTTP_CONNECT_TIMEOUT)
        .redirect(reqwest::redirect::Policy::none())
        .build()?;

    let spill_root = PathBuf::from(ASSISTANT_WORKDIR).join(TOOL_RESULT_SPILL_DIR);

    let gram_client =
        GramBootstrapClient::new(server_url, build_bootstrap_client(http_client.clone()));
    let host = Arc::new(RuntimeHost {
        identity,
        started_at: Instant::now(),
        seen: DashMap::new(),
        turn_admissions: DashMap::new(),
        threads: DashMap::new(),
        gram_client,
        thread_idle_ttl,
        mcp_http_client,
        spill_root,
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

/// Returns a thread's live state, or `None` when this VM holds none for it.
///
/// Deliberately non-bootstrapping, unlike [`ensure_thread`]: callers that only
/// act on a turn already in flight (the interrupt route) must not bring a
/// thread up as a side effect of asking about it.
pub fn lookup_thread(host: &RuntimeHost, thread_id: &str) -> Option<Arc<ConfiguredThread>> {
    host.threads
        .get(thread_id)
        .and_then(|entry| entry.value().get().cloned())
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
        let admission = admission_lock(host, &thread_id);
        let Ok(_guard) = admission.try_lock() else {
            continue;
        };
        if lookup_thread(host, &thread_id).is_some_and(|thread| thread.idle_for() > ttl) {
            evict_thread(host, &thread_id);
        }
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
        let admission = admission_lock(host, &thread_id);
        let Ok(_guard) = admission.try_lock() else {
            return;
        };
        if lookup_thread(host, &thread_id).is_some_and(|thread| thread.idle_for() > Duration::ZERO)
        {
            evict_thread(host, &thread_id);
        }
    }
}

/// Weak entries keep only concurrent admissions serialized, without retaining
/// one lock forever for every historical thread. No network work holds a map guard.
pub fn admission_lock(host: &RuntimeHost, thread_id: &str) -> Arc<tokio::sync::Mutex<()>> {
    host.turn_admissions
        .retain(|_, lock| lock.strong_count() > 0);
    let mut entry = host
        .turn_admissions
        .entry(thread_id.to_string())
        .or_default();
    if let Some(lock) = entry.upgrade() {
        return lock;
    }
    let lock = Arc::new(tokio::sync::Mutex::new(()));
    *entry = Arc::downgrade(&lock);
    lock
}

/// Owns a dedup slot across admission, including cancellation while waiting.
/// Map + this owner are the final two references only after every waiter leaves.
/// Removing under the map shard lock prevents new requests joining a detached
/// slot; successful entries remain until the normal thread eviction boundary.
#[derive(Clone)]
pub struct EventAdmission {
    host: Arc<RuntimeHost>,
    key: String,
    pub slot: Arc<EventSlot>,
}

impl EventAdmission {
    pub fn new(host: Arc<RuntimeHost>, key: String) -> Self {
        let slot = host
            .seen
            .entry(key.clone())
            .or_insert_with(|| Arc::new(EventSlot::default()))
            .clone();
        Self { host, key, slot }
    }
}

impl Drop for EventAdmission {
    fn drop(&mut self) {
        let slot = std::mem::take(&mut self.slot);
        self.host.seen.remove_if(&self.key, |_, current| {
            let same = Arc::ptr_eq(current, &slot);
            // Release our reference under the map lock so two concurrent
            // final owners cannot both observe the other and leave an orphan.
            drop(slot);
            same && Arc::strong_count(current) == 1
                && current.gate.try_lock().is_ok()
                && !current.accepted.load(Ordering::Acquire)
        });
    }
}

/// First-turn bootstrap path. Concurrent /turn requests for the same thread
/// race through the `OnceCell`; only one wins the bootstrap fetch and task
/// spawn. Later turns reuse the warm driver but queue their own opaque bearer.
pub async fn ensure_thread(
    host: &Arc<RuntimeHost>,
    thread_id: &str,
    bootstrap: ThreadBootstrap,
    tokens: TokenRegistry,
) -> Result<Arc<ConfiguredThread>, RunnerError> {
    let cell = host
        .threads
        .entry(thread_id.to_string())
        .or_insert_with(|| Arc::new(OnceCell::new()))
        .clone();

    let thread = cell
        .get_or_try_init(|| async {
            // Reap skips busy threads and our own (still-uninitialized)
            // OnceCell, so worst case is a no-op.
            reap_oldest_idle(host);
            spawn_thread(host, thread_id.to_string(), bootstrap, tokens).await
        })
        .await?;

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
    let (inbox_tx, inbox_rx) = mpsc::channel::<QueuedTurn>(64);
    let (notice_tx, notice_rx) = mpsc::unbounded_channel::<RunnerContent>();
    let (mcp_cmd_tx, mcp_catalog) = spawn_mcp_actor(
        host.gram_client.clone(),
        host.mcp_http_client.clone(),
        &thread_id,
        &bootstrap.mcp_servers,
        &tokens,
        notice_tx.clone(),
    )?;

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
    if !bootstrap.mcp_servers.is_empty() {
        transcript.push(Item::text(
            ItemKind::System,
            mcp_disclosure_item(&bootstrap.mcp_servers),
        ));
    }
    transcript.extend(normalize_history(&bootstrap.history)?);

    let permissions = CompositePermissionChecker::new(PermissionDecision::Allow).with_policy(
        PathPolicy::new()
            .allow_root(ASSISTANT_WORKDIR)
            .read_only_root(format!("{ASSISTANT_WORKDIR}/node_modules"))
            .read_only_root(format!("{ASSISTANT_WORKDIR}/browser.ts"))
            .read_only_root(format!("{ASSISTANT_WORKDIR}/package.json")),
    );
    let fs_resources = FileSystemToolResources::new()
        .with_policy(FileSystemToolPolicy::new().require_read_before_write(true));

    let native_tools = ToolRegistry::new()
        .with(tools::bun_run::bun_run)
        .with(tools::tool_search::ToolSearchTool::new(
            mcp_catalog.clone(),
            mcp_cmd_tx.clone(),
        ))
        .with(tools::inspect_asset::InspectAssetTool::new(
            notice_tx.clone(),
        ));

    // MCP tools resolve by name but are never advertised: the declared tool
    // set stays frozen for the thread's lifetime so the provider prompt
    // cache survives catalog churn. UnknownToolSource sits last so a call
    // to an undiscovered or hallucinated name returns a recovery hint
    // instead of a bare not-found.
    let compose_source = agentkit_tool_compose::ComposeTool::wrap(HiddenCatalogSource::new(
        mcp_catalog,
        mcp_cmd_tx.clone(),
    ))
    .with_source(native_tools.merge(agentkit_tool_fs::registry()))
    .with_source(UnknownToolSource);
    let clipped_source = ClippedToolSource::new(compose_source, host.spill_root.clone());

    // One controller per thread: the interrupt route bumps it, and the loop —
    // model stream, mutators, and tool rounds alike — checks the handle it was
    // built with. Scoping it to the thread keeps one user's stop from touching
    // a sibling thread's turn on the same VM.
    let cancellation = CancellationController::new();

    let mut builder = Agent::builder()
        .model(adapter)
        .add_tool_source(clipped_source)
        .permissions(permissions)
        .resources(fs_resources)
        .cancellation(cancellation.handle())
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

    let idle_since = Arc::new(Mutex::new(None));
    let loop_idle = Arc::clone(&idle_since);
    let log_thread_id = thread_id.clone();
    let host_for_eviction = Arc::clone(host);
    let evict_thread_id = thread_id.clone();
    let loop_thread_id = thread_id.clone();
    let entry_idle = Arc::clone(&idle_since);

    let loop_mcp_cmd = mcp_cmd_tx.clone();
    let task_handle = tokio::spawn(async move {
        let outcome = AssertUnwindSafe(run_loop(
            driver,
            inbox_rx,
            notice_rx,
            loop_idle,
            turn_end_compactor,
            loop_thread_id,
            (tokens, loop_mcp_cmd),
        ))
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
        host_for_eviction
            .threads
            .remove_if(&evict_thread_id, |_, cell| {
                cell.get()
                    .is_some_and(|thread| Arc::ptr_eq(&thread.idle_since, &entry_idle))
            });
    });

    let configured = Arc::new(ConfiguredThread {
        thread_id,
        chat_id,
        idle_since,
        inbox_tx,
        task_handle: Mutex::new(Some(task_handle)),
        cancellation,
    });
    Ok(configured)
}

fn mcp_disclosure_item(servers: &[McpServer]) -> String {
    let ids: Vec<&str> = servers.iter().map(|s| s.id.as_str()).collect();
    format!(
        "<mcp-servers>\nAttached MCP servers: {ids}.\nTheir tools are not present in the \
         declared tool schema. Use the tool_search tool to discover tool schemas and \
         per-server connection status, including authorization links for servers that \
         require auth. Call a discovered tool by its exact name — directly, or from a \
         compose script via tool(name, input). Servers connect on first search, so an \
         empty result before any search only means discovery has not run yet.\n</mcp-servers>",
        ids = ids.join(", "),
    )
}

async fn activate_turn(
    tokens: &TokenRegistry,
    mcp_cmd_tx: &mpsc::Sender<McpCmd>,
    turn: &QueuedTurn,
) -> Result<Option<String>, RunnerError> {
    let (reply, response) = tokio::sync::oneshot::channel();
    // Disconnect credential-bearing MCP sessions before changing the token.
    mcp_cmd_tx
        .send(McpCmd::BeginTurn {
            desired: turn.mcp_servers.clone(),
            bearer: turn.bearer.clone(),
            reply,
        })
        .await
        .map_err(|_| RunnerError::Loop("mcp actor closed".into()))?;
    let notice = response
        .await
        .map_err(|_| RunnerError::Loop("mcp turn admission failed".into()))??;
    // The actor and model clients share this registry. No model work starts
    // until the actor has completed the credential transition.
    debug_assert!(tokens.current()? == turn.bearer);
    Ok(notice)
}

async fn run_loop<S>(
    mut driver: LoopDriver<S>,
    mut inbox: mpsc::Receiver<QueuedTurn>,
    mut notices: UnboundedReceiver<RunnerContent>,
    idle_since: Arc<Mutex<Option<Instant>>>,
    turn_end_compactor: Option<PersistingCompactor>,
    thread_id: String,
    credential: (TokenRegistry, mpsc::Sender<McpCmd>),
) -> Result<&'static str, RunnerError>
where
    S: ModelSession,
{
    loop {
        // A fresh root span per driver step (one model call plus its tool
        // executions) bounds traces and carries the thread id. Gram identity
        // rides the span processor registered on the tracer provider, which
        // stamps every exported span.
        let step_span = tracing::info_span!("agent.step", thread_id = %thread_id);
        match driver.next().instrument(step_span).await? {
            LoopStep::Finished(turn) => {
                // A cancelled turn skips turn-end compaction. Compaction is a
                // model call of its own, and spending one right after the user
                // asked the assistant to stop both delays the thread going idle
                // and bakes a half-written turn into the summary the next cold
                // bootstrap replays. Deferring it is safe rather than free:
                // compaction is what keeps the transcript inside the context
                // window, but the next turn compacts over the same history once
                // the user's prompt arrives, so skipping it here only moves the
                // work to a point where the turn it summarises is complete.
                if turn.finish_reason == FinishReason::Cancelled {
                    tracing::info!(thread_id = %thread_id, "turn cancelled by user");
                } else if let Some(compactor) = &turn_end_compactor {
                    compact_at_turn_end(compactor, &driver).await;
                }
                if let Ok(mut idle) = idle_since.lock() {
                    *idle = inbox.is_empty().then(Instant::now);
                }
            }
            LoopStep::Interrupt(LoopInterrupt::AwaitingInput(req)) => {
                if let Ok(mut idle) = idle_since.lock()
                    && inbox.is_empty()
                    && idle.is_none()
                {
                    *idle = Some(Instant::now());
                }
                let turn = match inbox.recv().await {
                    Some(turn) => turn,
                    None => return Ok("inbox closed"),
                };
                if turn
                    .admission
                    .as_ref()
                    .is_some_and(|admission| admission.reply.is_closed())
                {
                    continue; // The HTTP admission was cancelled; its durable event retries.
                }
                mark_busy(&idle_since);
                // Only AwaitingInput admits the next tuple. Finished ran all
                // turn-end authenticated work before we can reach this point.
                // Stale notices (including auth links) belong to the finished
                // tuple, never to the next message's delegator.
                let _ = drain(&mut notices);
                let notice = activate_turn(&credential.0, &credential.1, &turn).await?;
                if turn
                    .admission
                    .as_ref()
                    .is_some_and(|admission| admission.reply.is_closed())
                {
                    continue;
                }
                let input = invocation_content(turn.input, notice);
                let items = vec![user_content_item(&input)];
                req.submit(&mut driver, items)?;
                if let Some(admission) = turn.admission {
                    // Publish dedup acceptance before acknowledging, even if the
                    // HTTP response is cancelled or lost immediately afterwards.
                    admission.accepted.store(true, Ordering::Release);
                    let _ = admission.reply.send(());
                    drop(admission.event);
                }
            }
            LoopStep::Interrupt(LoopInterrupt::AfterToolResult(info)) => {
                let drained = drain(&mut notices);
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

fn invocation_content(input: RunnerContent, notice: Option<String>) -> RunnerContent {
    let Some(notice) = notice else { return input };
    let mut parts = vec![RunnerContentPart::Text { text: notice }];
    match input {
        RunnerContent::Text(text) => parts.push(RunnerContentPart::Text { text }),
        RunnerContent::Parts(content) => parts.extend(content),
    }
    RunnerContent::Parts(parts)
}

fn drained_into_items(drained: Vec<RunnerContent>) -> Vec<Item> {
    drained.iter().map(user_content_item).collect()
}

fn drain(inbox: &mut UnboundedReceiver<RunnerContent>) -> Vec<RunnerContent> {
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

/// Builds a user item from a content union. Text maps to `TextPart`s and
/// image parts map to agentkit `MediaPart`s, which the completions adapter
/// sends upstream as `image_url` content. The wire `detail` hint has no
/// agentkit slot and is dropped at this boundary.
fn user_content_item(content: &RunnerContent) -> Item {
    let parts = match content {
        RunnerContent::Text(text) => vec![Part::Text(TextPart::new(text.clone()))],
        RunnerContent::Parts(parts) => parts
            .iter()
            .map(|part| match part {
                RunnerContentPart::Text { text } => Part::Text(TextPart::new(text.clone())),
                RunnerContentPart::ImageUrl { image_url } => Part::Media(MediaPart::new(
                    Modality::Image,
                    image_mime_type(&image_url.url),
                    DataRef::Uri(image_url.url.clone()),
                )),
            })
            .collect(),
    };
    Item::new(ItemKind::User, parts)
}

/// Best-effort mime type for an image `MediaPart`. The completions adapter
/// passes URI data refs through untouched, so this is informational only.
fn image_mime_type(url: &str) -> String {
    url.strip_prefix("data:")
        .and_then(|rest| rest.split([';', ',']).next())
        .filter(|mime| !mime.is_empty())
        .map(str::to_string)
        .unwrap_or_else(|| "image/*".to_string())
}

/// Plain-text projection for roles whose outbound messages cannot carry
/// media (the completions adapter rejects `Media` parts outside user
/// items): text parts join with newlines and image parts leave a visible
/// placeholder instead of being silently dropped.
fn text_with_image_placeholders(content: &RunnerContent) -> String {
    match content {
        RunnerContent::Text(text) => text.clone(),
        RunnerContent::Parts(parts) => {
            let mut buf = String::new();
            for part in parts {
                let piece = match part {
                    RunnerContentPart::Text { text } => text.clone(),
                    RunnerContentPart::ImageUrl { image_url }
                        if image_url.url.starts_with("data:") =>
                    {
                        "[image: inline data]".to_string()
                    }
                    RunnerContentPart::ImageUrl { image_url } => {
                        format!("[image: {}]", image_url.url)
                    }
                };
                if !buf.is_empty() {
                    buf.push('\n');
                }
                buf.push_str(&piece);
            }
            buf
        }
    }
}

fn normalize_history(history: &[RunnerMessage]) -> Result<Vec<Item>, RunnerError> {
    let mut items = Vec::with_capacity(history.len());
    for message in history {
        match message.role.as_str() {
            "user" => {
                items.push(user_content_item(&message.content));
            }
            "assistant" => {
                let mut parts: Vec<Part> = Vec::new();
                let text = text_with_image_placeholders(&message.content);
                if !text.is_empty() {
                    parts.push(Part::Text(TextPart::new(text)));
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
                        ToolOutput::text(text_with_image_placeholders(&message.content)),
                    ))],
                ));
            }
            "system" => {
                items.push(Item::text(
                    ItemKind::System,
                    text_with_image_placeholders(&message.content),
                ));
            }
            other => {
                return Err(RunnerError::UnsupportedHistoryRole(other.to_string()));
            }
        }
    }
    Ok(items)
}

#[cfg(test)]
#[allow(clippy::unwrap_used, clippy::expect_used, clippy::panic)]
mod tests {
    use super::*;
    use crate::http_layer::{TokenRegistry, build_bootstrap_client};

    fn test_bootstrap() -> ThreadBootstrap {
        serde_json::from_str(
            r#"{"model":"test","completions_url":"http://localhost","chat_id":"chat"}"#,
        )
        .unwrap()
    }

    #[tokio::test]
    async fn failed_admission_slots_are_bounded_and_success_is_retained() {
        let host = empty_host();
        for n in 0..1000 {
            let admission = EventAdmission::new(host.clone(), format!("failed:{n}"));
            let guard = admission.slot.gate.lock().await;
            assert!(!admission.slot.accepted.load(Ordering::Acquire));
            drop(guard);
            drop(admission);
            assert!(host.seen.is_empty());
        }
        let retry = EventAdmission::new(host.clone(), "failed:0".into());
        retry.slot.accepted.store(true, Ordering::Release);
        drop(retry);
        let successful_retry = EventAdmission::new(host.clone(), "failed:0".into());
        assert!(successful_retry.slot.accepted.load(Ordering::Acquire));
        drop(successful_retry);
        assert!(host.seen.contains_key("failed:0"));
    }

    #[test]
    fn queued_slot_owner_keeps_dedup_atomic_when_http_admission_is_cancelled() {
        let host = empty_host();
        let http = EventAdmission::new(host.clone(), "thread:event".into());
        let queued = http.clone();
        drop(http);
        assert!(host.seen.contains_key("thread:event"));
        queued.slot.accepted.store(true, Ordering::Release);
        drop(queued);
        let retry = EventAdmission::new(host.clone(), "thread:event".into());
        assert!(retry.slot.accepted.load(Ordering::Acquire));
    }

    #[tokio::test]
    async fn cancelled_admission_keeps_waiters_on_the_same_slot() {
        let host = empty_host();
        let first = EventAdmission::new(host.clone(), "thread:event".into());
        let first_guard = first.slot.gate.lock().await;
        let waiting = EventAdmission::new(host.clone(), "thread:event".into());
        assert!(Arc::ptr_eq(&first.slot, &waiting.slot));
        // Cancellation releases the lock before its owner, but the waiter
        // prevents removal; fresh retries must still join that same slot.
        drop(first_guard);
        drop(first);
        let retry = EventAdmission::new(host.clone(), "thread:event".into());
        assert!(Arc::ptr_eq(&waiting.slot, &retry.slot));
        let waiter_task = tokio::spawn(async move {
            let _guard = waiting.slot.gate.lock().await;
            std::future::pending::<()>().await;
        });
        tokio::task::yield_now().await;
        waiter_task.abort();
        let _ = waiter_task.await;
        assert!(host.seen.contains_key("thread:event"));
        drop(retry);
        assert!(host.seen.is_empty());
    }

    #[tokio::test(flavor = "multi_thread", worker_threads = 2)]
    async fn concurrent_final_failed_owners_do_not_orphan_slots() {
        let host = empty_host();
        for _ in 0..100 {
            let a = EventAdmission::new(host.clone(), "thread:event".into());
            let b = EventAdmission::new(host.clone(), "thread:event".into());
            let gate = Arc::new(tokio::sync::Barrier::new(2));
            let other_gate = gate.clone();
            let task = tokio::spawn(async move {
                other_gate.wait().await;
                drop(a);
            });
            gate.wait().await;
            drop(b);
            task.await.unwrap();
            assert!(host.seen.is_empty());
        }
    }

    fn empty_host() -> Arc<RuntimeHost> {
        let http_client = reqwest::Client::new();
        let gram_client = GramBootstrapClient::new(
            "http://localhost".to_string(),
            build_bootstrap_client(http_client.clone()),
        );
        let identity = Arc::new(SpanIdentity::default());
        let _ = identity.assistant_id.set("asst".to_string());
        Arc::new(RuntimeHost {
            identity,
            started_at: Instant::now(),
            seen: DashMap::new(),
            turn_admissions: DashMap::new(),
            threads: DashMap::new(),
            gram_client,
            thread_idle_ttl: Duration::from_secs(60 * 30),
            mcp_http_client: reqwest::Client::builder()
                .redirect(reqwest::redirect::Policy::none())
                .build()
                .expect("MCP HTTP client should build"),
            spill_root: PathBuf::from("/tmp/runtime-test-spill"),
        })
    }

    fn insert_thread(host: &RuntimeHost, thread_id: &str, idle_since: Option<Instant>) {
        let (inbox_tx, _inbox_rx) = mpsc::channel::<QueuedTurn>(64);
        let handle = tokio::spawn(async {});
        let configured = Arc::new(ConfiguredThread {
            thread_id: thread_id.to_string(),
            chat_id: format!("chat-{thread_id}"),
            idle_since: Arc::new(Mutex::new(idle_since)),
            inbox_tx,
            task_handle: Mutex::new(Some(handle)),
            cancellation: CancellationController::new(),
        });
        let cell = Arc::new(OnceCell::new());
        cell.set(configured)
            .map_err(|_| ())
            .expect("OnceCell should accept first set");
        host.threads.insert(thread_id.to_string(), cell);
    }

    #[tokio::test]
    async fn admission_is_scoped_and_expired_locks_are_pruned() {
        let host = empty_host();
        let a = admission_lock(&host, "a");
        let _guard = a.lock().await;
        let again = admission_lock(&host, "a");
        assert!(Arc::ptr_eq(&a, &again));
        let b = admission_lock(&host, "b");
        assert!(
            b.try_lock().is_ok(),
            "unrelated admission must not wait for a"
        );
        drop(b);
        let _c = admission_lock(&host, "c");
        assert!(!host.turn_admissions.contains_key("b"));
    }

    #[test]
    fn reconciliation_notice_is_part_of_first_input_without_a_tool_call() {
        let content = invocation_content(
            RunnerContent::Text("prompt".into()),
            Some("server update".into()),
        );
        let RunnerContent::Parts(parts) = content else {
            panic!("expected content parts")
        };
        assert_eq!(parts.len(), 2);
        assert!(matches!(&parts[0], RunnerContentPart::Text { text } if text == "server update"));
        assert!(matches!(&parts[1], RunnerContentPart::Text { text } if text == "prompt"));
    }

    #[tokio::test]
    async fn queued_invocations_reuse_the_warm_thread() {
        let host = empty_host();
        insert_thread(&host, "shared-thread", None);
        let original = lookup_thread(&host, "shared-thread").unwrap();
        let (a, b) = tokio::join!(
            ensure_thread(
                &host,
                "shared-thread",
                test_bootstrap(),
                TokenRegistry::new("user-a")
            ),
            ensure_thread(
                &host,
                "shared-thread",
                test_bootstrap(),
                TokenRegistry::new("user-b")
            ),
        );
        assert!(Arc::ptr_eq(&original, &a.unwrap()));
        assert!(Arc::ptr_eq(&original, &b.unwrap()));
        assert!(Arc::ptr_eq(
            &original,
            &lookup_thread(&host, "shared-thread").unwrap()
        ));
    }

    #[derive(Clone)]
    struct QueueTestModel {
        tokens: TokenRegistry,
        seen: mpsc::UnboundedSender<(String, Vec<String>)>,
        finish: Arc<tokio::sync::Semaphore>,
    }

    #[async_trait::async_trait]
    impl agentkit_loop::ModelAdapter for QueueTestModel {
        type Session = Self;
        async fn start_session(&self, _: SessionConfig) -> Result<Self, agentkit_loop::LoopError> {
            Ok(self.clone())
        }
    }

    #[async_trait::async_trait]
    impl ModelSession for QueueTestModel {
        type Turn = QueueTestTurn;
        async fn begin_turn(
            &mut self,
            request: agentkit_loop::TurnRequest,
            _: Option<agentkit_core::TurnCancellation>,
        ) -> Result<Self::Turn, agentkit_loop::LoopError> {
            let messages = request
                .transcript
                .iter()
                .filter(|item| item.kind == ItemKind::User)
                .flat_map(|item| item.parts.iter())
                .filter_map(|part| match part {
                    Part::Text(text) => Some(text.text.clone()),
                    _ => None,
                })
                .collect();
            self.seen
                .send((self.tokens.current().unwrap(), messages))
                .unwrap();
            Ok(QueueTestTurn {
                finish: self.finish.clone(),
                emitted: false,
            })
        }
    }

    struct QueueTestTurn {
        finish: Arc<tokio::sync::Semaphore>,
        emitted: bool,
    }

    #[async_trait::async_trait]
    impl agentkit_loop::ModelTurn for QueueTestTurn {
        async fn next_event(
            &mut self,
            _: Option<agentkit_core::TurnCancellation>,
        ) -> Result<Option<agentkit_loop::ModelTurnEvent>, agentkit_loop::LoopError> {
            if self.emitted {
                return Ok(None);
            }
            self.finish.acquire().await.unwrap().forget();
            self.emitted = true;
            Ok(Some(agentkit_loop::ModelTurnEvent::Finished(
                agentkit_loop::ModelTurnResult {
                    model: None,
                    response_id: None,
                    finish_reason: FinishReason::Completed,
                    output_items: vec![],
                    usage: None,
                    metadata: Default::default(),
                },
            )))
        }
    }

    #[tokio::test]
    async fn loop_finishes_each_message_before_installing_the_next_token() {
        let tokens = TokenRegistry::new("initial");
        let (seen_tx, mut seen_rx) = mpsc::unbounded_channel();
        let finish = Arc::new(tokio::sync::Semaphore::new(0));
        let model = QueueTestModel {
            tokens: tokens.clone(),
            seen: seen_tx,
            finish: finish.clone(),
        };
        let agent = Agent::builder().model(model).build().unwrap();
        let driver = agent.start(SessionConfig::new("queue-test")).await.unwrap();
        let (tx, rx) = mpsc::channel(64);
        let (_notice_tx, notices) = mpsc::unbounded_channel();
        let (cmd, mut commands) = mpsc::channel(2);
        let actor_tokens = tokens.clone();
        let actor = tokio::spawn(async move {
            while let Some(McpCmd::BeginTurn { bearer, reply, .. }) = commands.recv().await {
                actor_tokens.rotate(bearer).unwrap();
                reply.send(Ok(None)).unwrap();
            }
        });
        let runner = tokio::spawn(run_loop(
            driver,
            rx,
            notices,
            Arc::new(Mutex::new(None)),
            None,
            "thread".into(),
            (tokens.clone(), cmd),
        ));
        tx.try_send(QueuedTurn {
            input: RunnerContent::Text("message-a".into()),
            bearer: "token-a".into(),
            mcp_servers: vec![],
            admission: None,
        })
        .unwrap();
        let first = tokio::time::timeout(Duration::from_secs(2), seen_rx.recv())
            .await
            .unwrap()
            .unwrap();
        assert_eq!(first, ("token-a".into(), vec!["message-a".into()]));
        let (reply, mut admitted) = tokio::sync::oneshot::channel();
        let accepted = Arc::new(AtomicBool::new(false));
        // A cancelled HTTP admission stays retryable and never starts model work.
        let (cancelled_reply, cancelled) = tokio::sync::oneshot::channel();
        drop(cancelled);
        tx.try_send(QueuedTurn {
            input: RunnerContent::Text("cancelled-message".into()),
            bearer: "cancelled-token".into(),
            mcp_servers: vec![],
            admission: Some(TurnAdmission {
                event: None,
                accepted: Arc::new(AtomicBool::new(false)),
                reply: cancelled_reply,
            }),
        })
        .unwrap();
        tx.try_send(QueuedTurn {
            input: RunnerContent::Text("message-b".into()),
            bearer: "token-b".into(),
            mcp_servers: vec![],
            admission: Some(TurnAdmission {
                event: None,
                accepted: accepted.clone(),
                reply,
            }),
        })
        .unwrap();
        tokio::task::yield_now().await;
        assert_eq!(tokens.current().unwrap(), "token-a");
        assert!(matches!(
            admitted.try_recv(),
            Err(tokio::sync::oneshot::error::TryRecvError::Empty)
        ));
        assert!(!accepted.load(Ordering::Acquire));
        assert!(
            seen_rx.try_recv().is_err(),
            "out-of-turn input must not start model work"
        );
        finish.add_permits(1);
        let second = tokio::time::timeout(Duration::from_secs(2), seen_rx.recv())
            .await
            .unwrap()
            .unwrap();
        admitted.await.unwrap();
        assert!(accepted.load(Ordering::Acquire));
        assert_eq!(second.0, "token-b");
        assert_eq!(second.1.last().unwrap(), "message-b");
        finish.add_permits(1);
        drop(tx);
        assert_eq!(
            tokio::time::timeout(Duration::from_secs(2), runner)
                .await
                .unwrap()
                .unwrap()
                .unwrap(),
            "inbox closed"
        );
        actor.await.unwrap();
    }

    #[test]
    fn full_queue_returns_retryable_backpressure_and_dropped_queue_never_acknowledges() {
        let (tx, rx) = mpsc::channel(1);
        let thread = ConfiguredThread {
            thread_id: "thread".into(),
            chat_id: "chat".into(),
            idle_since: Arc::new(Mutex::new(None)),
            inbox_tx: tx,
            task_handle: Mutex::new(None),
            cancellation: CancellationController::new(),
        };
        let (reply, mut admitted) = tokio::sync::oneshot::channel();
        let accepted = Arc::new(AtomicBool::new(false));
        thread
            .enqueue(QueuedTurn {
                input: RunnerContent::Text("first".into()),
                bearer: "one".into(),
                mcp_servers: vec![],
                admission: Some(TurnAdmission {
                    event: None,
                    accepted: accepted.clone(),
                    reply,
                }),
            })
            .unwrap();
        assert!(matches!(
            thread.enqueue(QueuedTurn {
                input: RunnerContent::Text("second".into()),
                bearer: "two".into(),
                mcp_servers: vec![],
                admission: None
            }),
            Err(RunnerError::InvocationBusy)
        ));
        assert!(matches!(
            admitted.try_recv(),
            Err(tokio::sync::oneshot::error::TryRecvError::Empty)
        ));
        drop(rx); // A failed loop drops pending receipts, never accepts them.
        assert!(matches!(
            admitted.try_recv(),
            Err(tokio::sync::oneshot::error::TryRecvError::Closed)
        ));
        assert!(!accepted.load(Ordering::Acquire));
    }

    #[tokio::test]
    async fn queued_tuple_keeps_its_token_until_the_turn_boundary() {
        let tokens = TokenRegistry::new("human-a");
        let model = tokens.clone();
        let (tx, mut rx) = mpsc::channel(64);
        tx.try_send(QueuedTurn {
            input: RunnerContent::Text("message-b".into()),
            bearer: "human-b".into(),
            mcp_servers: vec![],
            admission: None,
        })
        .unwrap();
        assert_eq!(
            model.current().unwrap(),
            "human-a",
            "enqueue must not rotate the active client"
        );
        let next = rx.recv().await.unwrap();
        let (cmd_tx, mut cmd_rx) = mpsc::channel(1);
        let actor_tokens = tokens.clone();
        let actor = tokio::spawn(async move {
            if let Some(McpCmd::BeginTurn { bearer, reply, .. }) = cmd_rx.recv().await {
                actor_tokens.rotate(bearer).unwrap();
                reply.send(Ok(None)).unwrap();
            } else {
                panic!("expected turn boundary command");
            }
        });
        activate_turn(&tokens, &cmd_tx, &next).await.unwrap();
        assert_eq!(model.current().unwrap(), "human-b");
        assert!(matches!(next.input, RunnerContent::Text(ref msg) if msg == "message-b"));
        actor.await.unwrap();
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
            Arc::new(EventSlot {
                accepted: Arc::new(AtomicBool::new(true)),
                gate: tokio::sync::Mutex::new(()),
            }),
        );
        host.seen.insert(
            "T:evt-2".to_string(),
            Arc::new(EventSlot {
                accepted: Arc::new(AtomicBool::new(true)),
                gate: tokio::sync::Mutex::new(()),
            }),
        );
        host.seen.insert(
            "other:evt-1".to_string(),
            Arc::new(EventSlot {
                accepted: Arc::new(AtomicBool::new(true)),
                gate: tokio::sync::Mutex::new(()),
            }),
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

    #[tokio::test]
    async fn lookup_thread_finds_configured_threads_only() {
        let host = empty_host();
        insert_thread(&host, "T", Some(Instant::now()));

        assert!(lookup_thread(&host, "T").is_some());
        assert!(
            lookup_thread(&host, "missing").is_none(),
            "an unconfigured thread must not be bootstrapped by a lookup"
        );
    }

    #[tokio::test]
    async fn interrupt_cancels_a_checkpoint_taken_before_it() {
        let host = empty_host();
        insert_thread(&host, "T", None);
        let thread = lookup_thread(&host, "T").expect("thread should be configured");

        // The checkpoint stands in for the one the driver captures at turn
        // start: it must read as cancelled only after the interrupt lands.
        let in_flight = thread.cancellation.handle().checkpoint();
        assert!(!in_flight.is_cancelled());

        assert!(
            thread.interrupt(),
            "a busy thread reports the turn it stopped"
        );
        assert!(in_flight.is_cancelled());

        // A turn that starts after the interrupt checkpoints the new
        // generation, so a stop never leaks into the next turn.
        assert!(!thread.cancellation.handle().checkpoint().is_cancelled());
    }

    // A warm thread between turns is the other half of the stop button: the
    // reply landed before the press, so there was nothing to cancel and the
    // caller must not be told it stopped a turn.
    #[tokio::test]
    async fn interrupt_reports_nothing_stopped_on_an_idle_thread() {
        let host = empty_host();
        insert_thread(&host, "T", Some(Instant::now()));
        let thread = lookup_thread(&host, "T").expect("thread should be configured");

        assert!(!thread.interrupt());
    }

    fn image_part(url: &str) -> RunnerContentPart {
        RunnerContentPart::ImageUrl {
            image_url: crate::wire::RunnerImageUrl {
                url: url.to_string(),
                detail: None,
            },
        }
    }

    #[test]
    fn normalize_history_text_content_matches_legacy_shape() {
        let history = vec![RunnerMessage {
            role: "user".to_string(),
            content: RunnerContent::Text("hello".to_string()),
            tool_calls: Vec::new(),
            tool_call_id: None,
        }];
        let items = normalize_history(&history).unwrap();
        assert_eq!(items.len(), 1);
        assert_eq!(items[0].kind, ItemKind::User);
        assert_eq!(
            items[0].parts,
            vec![Part::Text(TextPart::new("hello"))],
            "text-only content must normalize exactly as the plain-string wire did"
        );
    }

    #[test]
    fn normalize_history_user_image_parts_become_media() {
        let history = vec![RunnerMessage {
            role: "user".to_string(),
            content: RunnerContent::Parts(vec![
                RunnerContentPart::Text {
                    text: "look at this".to_string(),
                },
                image_part("https://example.com/cat.png"),
            ]),
            tool_calls: Vec::new(),
            tool_call_id: None,
        }];
        let items = normalize_history(&history).unwrap();
        assert_eq!(items[0].parts.len(), 2);
        assert_eq!(items[0].parts[0], Part::Text(TextPart::new("look at this")));
        let Part::Media(media) = &items[0].parts[1] else {
            panic!("expected media part, got {:?}", items[0].parts[1]);
        };
        assert_eq!(media.modality, Modality::Image);
        assert_eq!(
            media.data,
            DataRef::Uri("https://example.com/cat.png".to_string())
        );
    }

    #[test]
    fn normalize_history_assistant_image_parts_become_placeholder_text() {
        // The completions adapter rejects Media parts on assistant items, so
        // image parts surface as visible text placeholders instead.
        let history = vec![RunnerMessage {
            role: "assistant".to_string(),
            content: RunnerContent::Parts(vec![
                RunnerContentPart::Text {
                    text: "here you go".to_string(),
                },
                image_part("https://example.com/out.png"),
            ]),
            tool_calls: Vec::new(),
            tool_call_id: None,
        }];
        let items = normalize_history(&history).unwrap();
        assert_eq!(
            items[0].parts,
            vec![Part::Text(TextPart::new(
                "here you go\n[image: https://example.com/out.png]"
            ))]
        );
    }

    #[test]
    fn image_mime_type_reads_data_uri_header() {
        assert_eq!(image_mime_type("data:image/png;base64,AAAA"), "image/png");
        assert_eq!(image_mime_type("data:image/jpeg,raw"), "image/jpeg");
        assert_eq!(image_mime_type("https://example.com/a.png"), "image/*");
    }
}
