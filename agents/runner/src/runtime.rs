use std::sync::{Arc, Mutex};
use std::time::{Duration, Instant};

use agentkit_adapter_completions::CompletionsAdapter;
use agentkit_core::{Item, ItemKind, Part, TextPart, ToolCallPart, ToolOutput, ToolResultPart};
use agentkit_loop::{
    Agent, LoopDriver, LoopInterrupt, LoopStep, PromptCacheRequest, PromptCacheRetention,
    SessionConfig,
};
use agentkit_mcp::{
    McpServerConfig, McpServerManager, McpTransportBinding, StreamableHttpTransportConfig,
};
use agentkit_provider_openrouter::{OpenRouterConfig, OpenRouterProvider};
use agentkit_reporting::TracingReporter;
use agentkit_tools_core::PermissionChecker;
use serde_json::Value;
use tokio::sync::mpsc::{self, UnboundedReceiver, UnboundedSender};
use tokio::sync::{Mutex as AsyncMutex, Notify};

use crate::errors::RunnerError;
use crate::http_layer::{TokenRegistry, build_http, build_http_with_static};
use crate::idempotency::IdempotencyCache;
use crate::tools;
use crate::wire::{McpServer, RunnerConfig, RunnerMessage, RunnerRequest};

const MCP_CONNECT_TIMEOUT: Duration = Duration::from_secs(30);
const AGENT_START_TIMEOUT: Duration = Duration::from_secs(30);
const SHUTDOWN_GRACE: Duration = Duration::from_secs(60);

pub type AppState = Arc<AsyncMutex<RuntimeHost>>;

pub struct RuntimeHost {
    pub runtime: Option<ConfiguredRuntime>,
    pub seen: IdempotencyCache,
    /// Signals the HTTP server to shut down. Loop task fires this when it
    /// exits (warm TTL / fatal error) so the process tears down immediately
    /// rather than lingering in a configured-but-dead state.
    pub shutdown: Arc<Notify>,
}

pub struct ConfiguredRuntime {
    tokens: TokenRegistry,
    inbox_tx: UnboundedSender<String>,
    last_active: Arc<Mutex<Instant>>,
    // Non-rotating fields of the RunnerConfig this runtime was built from.
    // `auth_token` is excluded — it rolls on every /turn — so a caller retrying
    // /configure with a refreshed token is still treated as an identical config.
    fingerprint: u64,
    // Held so MCP transports outlive the session; dropping the manager would
    // disconnect the streamable-http transports the tool registry references.
    _mcp_manager: McpServerManager,
    // Loop task handle; dropping aborts the task on runtime drop.
    _loop_handle: tokio::task::JoinHandle<()>,
}

impl ConfiguredRuntime {
    pub fn last_active_ago(&self) -> Option<Duration> {
        self.last_active
            .lock()
            .ok()
            .map(|last| Instant::now().saturating_duration_since(*last))
    }

    pub fn rotate_token(&self, token: &str) -> Result<(), RunnerError> {
        self.tokens.rotate(token)
    }

    pub fn matches(&self, config: &RunnerConfig) -> bool {
        self.fingerprint == fingerprint(config)
    }

    pub fn enqueue(&self, request: RunnerRequest) -> Result<(), RunnerError> {
        self.inbox_tx
            .send(request.input)
            .map_err(|_| RunnerError::SubmitInput("loop inbox closed".into()))
    }
}

fn fingerprint(config: &RunnerConfig) -> u64 {
    use std::hash::{DefaultHasher, Hash, Hasher};
    let mut hasher = DefaultHasher::new();
    config.model.hash(&mut hasher);
    config.instructions.hash(&mut hasher);
    config.completions_url.hash(&mut hasher);
    config.chat_id.hash(&mut hasher);
    config.mcp_servers.hash(&mut hasher);
    config.history.hash(&mut hasher);
    config.warm_ttl_seconds.hash(&mut hasher);
    hasher.finish()
}

struct AllowAll;

impl PermissionChecker for AllowAll {
    fn evaluate(
        &self,
        _request: &dyn agentkit_tools_core::PermissionRequest,
    ) -> agentkit_tools_core::PermissionDecision {
        agentkit_tools_core::PermissionDecision::Allow
    }
}

pub async fn build_runtime(
    config: &RunnerConfig,
    shutdown: Arc<Notify>,
) -> Result<ConfiguredRuntime, RunnerError> {
    tracing::info!(
        model = %config.model,
        mcp_servers = config.mcp_servers.len(),
        chat_id = %config.chat_id,
        "build_runtime"
    );

    let tokens = TokenRegistry::new(config.auth_token.clone());

    let mut default_headers = http::HeaderMap::new();
    default_headers.insert(
        http::HeaderName::from_static("gram-chat-id"),
        http::HeaderValue::from_str(&config.chat_id)
            .map_err(|source| RunnerError::HeaderValue { source })?,
    );

    let http_client = reqwest::Client::builder()
        .user_agent(concat!("gram-assistant-runner/", env!("CARGO_PKG_VERSION")))
        .default_headers(default_headers)
        .build()?;

    let manager =
        connect_mcp_servers(&config.mcp_servers, http_client.clone(), tokens.clone()).await?;

    let base_url = config
        .completions_url
        .clone()
        .ok_or_else(|| RunnerError::ConfigError {
            key: "completions_url".to_string(),
        })?;
    let openrouter_config =
        OpenRouterConfig::new(String::new(), config.model.clone()).with_base_url(base_url);
    let provider = OpenRouterProvider::from(openrouter_config);
    let completions_http = build_http(http_client, tokens.clone());
    let adapter = CompletionsAdapter::with_client(provider, completions_http);

    let combined = tools::bun_run::registry().merge(manager.tool_registry());
    let agent = Agent::builder()
        .model(adapter)
        .tools(combined)
        .permissions(AllowAll)
        .observer(TracingReporter::new())
        .build()
        .map_err(|e| RunnerError::AgentBuild(e.to_string()))?;

    let session = SessionConfig::new(config.chat_id.clone())
        .with_cache(PromptCacheRequest::automatic().with_retention(PromptCacheRetention::Short));
    let mut driver = match tokio::time::timeout(AGENT_START_TIMEOUT, agent.start(session)).await {
        Ok(Ok(driver)) => driver,
        Ok(Err(e)) => return Err(RunnerError::AgentStart(e.to_string())),
        Err(_) => return Err(RunnerError::AgentStartTimeout(AGENT_START_TIMEOUT)),
    };

    // Prime the driver with instructions + persisted transcript once, here.
    // submit_input only stages items — the model isn't called until the loop
    // calls next(), which happens after the first /turn arrives. So the loop
    // comes up already hydrated; /turn carries only new user input.
    let mut priming = Vec::new();
    if let Some(instructions) = &config.instructions {
        priming.push(Item::text(ItemKind::System, instructions));
    }
    priming.extend(normalize_history(&config.history)?);
    if !priming.is_empty() {
        driver
            .submit_input(priming)
            .map_err(|e| RunnerError::SubmitInput(e.to_string()))?;
    }

    let warm_ttl = config
        .warm_ttl_seconds
        .map(Duration::from_secs)
        .ok_or_else(|| RunnerError::ConfigError {
            key: "warm_ttl".to_string(),
        })?;
    let last_active = Arc::new(Mutex::new(Instant::now()));

    let (inbox_tx, inbox_rx) = mpsc::unbounded_channel::<String>();

    let loop_last_active = Arc::clone(&last_active);
    let loop_handle = tokio::spawn(async move {
        let outcome = run_loop(driver, inbox_rx, loop_last_active, warm_ttl).await;
        match outcome {
            Ok(reason) => tracing::info!(reason = %reason, "loop exited"),
            Err(err) => tracing::error!(error = %err, "loop exited with error"),
        }
        shutdown.notify_waiters();
    });

    tracing::info!("build_runtime ok");

    Ok(ConfiguredRuntime {
        tokens,
        inbox_tx,
        last_active,
        fingerprint: fingerprint(config),
        _mcp_manager: manager,
        _loop_handle: loop_handle,
    })
}

async fn connect_mcp_servers(
    servers: &[McpServer],
    http_client: reqwest::Client,
    tokens: TokenRegistry,
) -> Result<McpServerManager, RunnerError> {
    let mut manager = McpServerManager::new();
    for server in servers {
        let static_headers = server
            .headers
            .iter()
            .map(|(k, v)| {
                let name = http::HeaderName::from_bytes(k.as_bytes()).map_err(|source| {
                    RunnerError::McpHeaderName {
                        server: server.id.clone(),
                        name: k.clone(),
                        source,
                    }
                })?;
                let value = http::HeaderValue::from_str(v).map_err(|source| {
                    RunnerError::McpHeaderValue {
                        server: server.id.clone(),
                        name: k.clone(),
                        source,
                    }
                })?;
                Ok::<_, RunnerError>((name, value))
            })
            .collect::<Result<Vec<_>, _>>()?;

        let mut server_headers = http::HeaderMap::new();
        for (name, value) in static_headers {
            server_headers.insert(name, value);
        }
        let http = build_http_with_static(http_client.clone(), tokens.clone(), server_headers);
        let transport = StreamableHttpTransportConfig::new(&server.url).with_client(http);
        manager = manager.with_server(McpServerConfig::new(
            &server.id,
            McpTransportBinding::StreamableHttp(transport),
        ));
    }

    match tokio::time::timeout(MCP_CONNECT_TIMEOUT, manager.connect_all()).await {
        Ok(Ok(handles)) => {
            tracing::info!(servers = handles.len(), "mcp connect_all ok");
            Ok(manager)
        }
        Ok(Err(e)) => Err(RunnerError::McpConnect(e.to_string())),
        Err(_) => Err(RunnerError::McpConnectTimeout(MCP_CONNECT_TIMEOUT)),
    }
}

/// Runs the agent loop continuously until warm TTL + grace elapse idle, or
/// a fatal error occurs. Input arrives via `inbox`; `/turn` pushes there and
/// returns immediately. The driver is already primed with instructions and
/// history from `build_runtime`, so the loop just wraps each String as a user
/// Item and submits.
///
/// Agent loop events the runner cares about:
/// - `LoopStep::Finished`: turn ended. Drain any queued inputs and submit; if
///   the queue was empty, race a timer (warm_ttl + 60s grace) against the next
///   inbox arrival. Timer wins -> exit.
/// - `LoopInterrupt::AfterToolResult`: cooperative mid-turn yield (agentkit
///   0.4+). Drain any queued inputs and submit before the next model call.
/// - `LoopInterrupt::AuthRequest`: backend token rotation is the correct fix
///   for expired MCP auth; we cannot resolve it here. Warn and bail.
/// - `LoopInterrupt::ApprovalRequest`: tools in this environment should not
///   require approval. Warn and auto-approve so we don't deadlock.
async fn run_loop<S>(
    mut driver: LoopDriver<S>,
    mut inbox: UnboundedReceiver<String>,
    last_active: Arc<Mutex<Instant>>,
    warm_ttl: Duration,
) -> Result<&'static str, RunnerError>
where
    S: agentkit_loop::ModelSession,
{
    let Some(first) = inbox.recv().await else {
        return Ok("inbox closed before first input");
    };
    submit_user(&mut driver, first)?;
    bump(&last_active);

    loop {
        match driver.next().await? {
            LoopStep::Finished(_turn) => {
                bump(&last_active);
                let drained = drain(&mut inbox);
                if !drained.is_empty() {
                    for msg in drained {
                        submit_user(&mut driver, msg)?;
                    }
                    continue;
                }
                let wait_budget = warm_ttl + SHUTDOWN_GRACE;
                // Race: new input vs shutdown timer.
                tokio::select! {
                    maybe = inbox.recv() => {
                        match maybe {
                            Some(msg) => {
                                submit_user(&mut driver, msg)?;
                                bump(&last_active);
                            }
                            None => return Ok("inbox closed"),
                        }
                    }
                    _ = tokio::time::sleep(wait_budget) => {
                        return Ok("warm ttl elapsed");
                    }
                }
            }
            LoopStep::Interrupt(LoopInterrupt::AfterToolResult(_info)) => {
                bump(&last_active);
                let drained = drain(&mut inbox);
                for msg in drained {
                    submit_user(&mut driver, msg)?;
                }
            }
            LoopStep::Interrupt(LoopInterrupt::ApprovalRequest(_req)) => {
                tracing::warn!(
                    "unexpected approval request — runner auto-approves; tools should \
                     not require approval in this environment"
                );
                driver.resolve_approval(agentkit_tools_core::ApprovalDecision::Approve)?;
            }
            LoopStep::Interrupt(LoopInterrupt::AuthRequest(req)) => {
                tracing::warn!(
                    "unexpected mcp auth interrupt — token likely expired or backend returned 403"
                );
                driver.resolve_auth(agentkit_tools_core::AuthResolution::cancelled(req.request))?;
                return Err(RunnerError::McpAuthInterrupt);
            }
            LoopStep::Interrupt(LoopInterrupt::AwaitingInput(_)) => {}
        }
    }
}

fn submit_user<S>(driver: &mut LoopDriver<S>, input: String) -> Result<(), RunnerError>
where
    S: agentkit_loop::ModelSession,
{
    driver
        .submit_input(vec![Item::text(ItemKind::User, &input)])
        .map_err(|e| RunnerError::SubmitInput(e.to_string()))
}

fn drain(inbox: &mut UnboundedReceiver<String>) -> Vec<String> {
    let mut out = Vec::new();
    while let Ok(msg) = inbox.try_recv() {
        out.push(msg);
    }
    out
}

fn bump(last_active: &Arc<Mutex<Instant>>) {
    if let Ok(mut slot) = last_active.lock() {
        *slot = Instant::now();
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
