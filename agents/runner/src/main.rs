mod tools;

use std::collections::{BTreeMap, HashSet, VecDeque};
use std::net::SocketAddr;
use std::sync::Arc;

use agentkit_adapter_completions::{CompletionsAdapter, CompletionsProvider, CompletionsSession};
use agentkit_core::{
    Item, ItemKind, Part, TextPart, ToolCallPart, ToolOutput, ToolResultPart, Usage,
};
use agentkit_http::{Http, HttpRequestBuilder};
use agentkit_loop::{
    Agent, AgentEvent, LoopDriver, LoopError, LoopInterrupt, LoopObserver, LoopStep,
    PromptCacheRequest, PromptCacheRetention, SessionConfig, TurnRequest,
};
use agentkit_reporting::TracingReporter;
use agentkit_mcp::{
    McpServerConfig, McpServerManager, McpTransportBinding, StreamableHttpTransportConfig,
};
use agentkit_tools_core::PermissionChecker;
use async_trait::async_trait;
use axum::extract::State;
use axum::http::{HeaderMap, StatusCode};
use axum::routing::{get, post};
use axum::{Json, Router};
use reqwest_middleware::{ClientBuilder, Middleware, Next};
use std::sync::RwLock;

const IDEMPOTENCY_HEADER: &str = "x-idempotency-key";
const IDEMPOTENCY_CAPACITY: usize = 1024;

// Bounded FIFO set of idempotency keys already processed by /turn. Dedup is
// best-effort memory-only: when the Go side redelivers the same event (activity
// retry, coordinator re-signal, reaper requeue), we skip re-running the turn
// and reply with the canned ack below. Capacity bounds memory; oldest keys age
// out once the cap is hit.
#[derive(Default)]
struct IdempotencyCache {
    set: HashSet<String>,
    order: VecDeque<String>,
}

impl IdempotencyCache {
    fn contains(&self, key: &str) -> bool {
        self.set.contains(key)
    }

    fn insert(&mut self, key: String) {
        if self.set.contains(&key) {
            return;
        }
        if self.order.len() >= IDEMPOTENCY_CAPACITY {
            if let Some(evicted) = self.order.pop_front() {
                self.set.remove(&evicted);
            }
        }
        self.set.insert(key.clone());
        self.order.push_back(key);
    }
}
use serde::{Deserialize, Serialize};
use serde_json::Value;
use tokio::net::TcpListener;
use tokio::sync::Mutex;

#[derive(Debug, Deserialize, Serialize, Clone)]
struct RunnerConfig {
    model: String,
    instructions: Option<String>,
    auth_token: String,
    completions_url: Option<String>,
    chat_id: String,
    #[serde(default)]
    mcp_servers: Vec<McpServer>,
}

#[derive(Debug, Deserialize, Serialize, Clone)]
struct McpServer {
    id: String,
    url: String,
    #[serde(default)]
    headers: BTreeMap<String, String>,
}

#[derive(Debug, Deserialize)]
struct RunnerRequest {
    #[serde(default)]
    history: Vec<RunnerMessage>,
    input: String,
    #[serde(default)]
    auth_token: Option<String>,
}

// RunnerMessage is the wire shape used to rehydrate transcript items on cold
// start. It mirrors server/internal/assistants/runtime.go's runtimeMessage one
// field at a time — keep them in sync.
#[derive(Debug, Deserialize)]
struct RunnerMessage {
    role: String,
    #[serde(default)]
    content: String,
    #[serde(default)]
    tool_calls: Vec<RunnerToolCall>,
    #[serde(default)]
    tool_call_id: Option<String>,
}

#[derive(Debug, Deserialize)]
struct RunnerToolCall {
    id: String,
    name: String,
    // JSON-encoded string matching the OpenAI tool-call arguments shape; stored
    // verbatim in the DB so the bytes we persist equal the bytes we replay.
    arguments: String,
}

#[derive(Debug, Serialize)]
struct RunnerResponse {
    finish_reason: String,
    final_text: String,
    items: Vec<Item>,
    usage: Option<Usage>,
    #[serde(skip_serializing_if = "Option::is_none")]
    error: Option<String>,
}

#[derive(Debug, Serialize)]
struct RunnerStateResponse {
    configured: bool,
}

#[derive(Clone, Debug, Serialize)]
struct GramRequestConfig {
    model: String,
}

#[derive(Clone, Debug)]
struct TokenRegistry {
    inner: Arc<RwLock<String>>,
}

impl TokenRegistry {
    fn new(initial: impl Into<String>) -> Self {
        Self {
            inner: Arc::new(RwLock::new(initial.into())),
        }
    }

    fn current(&self) -> String {
        self.inner
            .read()
            .expect("token registry read lock poisoned")
            .clone()
    }

    fn rotate(&self, next: impl Into<String>) {
        let mut slot = self
            .inner
            .write()
            .expect("token registry write lock poisoned");
        *slot = next.into();
    }
}

struct RequestDecorator {
    registry: TokenRegistry,
    static_headers: Vec<(http::HeaderName, http::HeaderValue)>,
}

impl RequestDecorator {
    fn new(
        registry: TokenRegistry,
        static_headers: Vec<(http::HeaderName, http::HeaderValue)>,
    ) -> Self {
        Self {
            registry,
            static_headers,
        }
    }
}

#[async_trait]
impl Middleware for RequestDecorator {
    async fn handle(
        &self,
        mut req: reqwest::Request,
        extensions: &mut http::Extensions,
        next: Next<'_>,
    ) -> reqwest_middleware::Result<reqwest::Response> {
        for (name, value) in &self.static_headers {
            req.headers_mut().insert(name, value.clone());
        }
        let token = self.registry.current();
        let value = http::HeaderValue::try_from(format!("Bearer {token}"))
            .map_err(|error| reqwest_middleware::Error::Middleware(error.into()))?;
        req.headers_mut().insert(http::header::AUTHORIZATION, value);
        next.run(req, extensions).await
    }
}

fn build_decorated_http(
    client: reqwest::Client,
    registry: TokenRegistry,
    static_headers: Vec<(http::HeaderName, http::HeaderValue)>,
) -> Http {
    let client = ClientBuilder::new(client)
        .with(RequestDecorator::new(registry, static_headers))
        .build();
    Http::new(client)
}

#[derive(Clone)]
struct GramProvider {
    base_url: String,
    chat_id: String,
    config: GramRequestConfig,
}

impl GramProvider {
    fn new(config: &RunnerConfig) -> Self {
        Self {
            base_url: config
                .completions_url
                .clone()
                .unwrap_or_else(|| "http://127.0.0.1:8080/chat/completions".to_string()),
            chat_id: config.chat_id.clone(),
            config: GramRequestConfig {
                model: config.model.clone(),
            },
        }
    }
}

impl CompletionsProvider for GramProvider {
    type Config = GramRequestConfig;

    fn provider_name(&self) -> &str {
        "Gram"
    }

    fn endpoint_url(&self) -> &str {
        &self.base_url
    }

    fn config(&self) -> &Self::Config {
        &self.config
    }

    // Authorization is stamped by the RequestDecorator middleware wired into
    // CompletionsAdapter's Http client. Here we only add request-specific
    // headers the middleware shouldn't know about.
    fn preprocess_request(&self, builder: HttpRequestBuilder) -> HttpRequestBuilder {
        builder.header("Gram-Chat-ID", &self.chat_id)
    }

    fn apply_prompt_cache(
        &self,
        _body: &mut serde_json::Map<String, Value>,
        _request: &TurnRequest,
    ) -> Result<(), LoopError> {
        Ok(())
    }
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

type GramLoopDriver = LoopDriver<CompletionsSession<GramProvider>>;

struct ConfiguredRuntime {
    _config: RunnerConfig,
    _manager: McpServerManager,
    tokens: TokenRegistry,
    driver: GramLoopDriver,
    hydrated: bool,
}

#[derive(Default)]
struct RuntimeHost {
    runtime: Option<ConfiguredRuntime>,
    configured: bool,
    seen: IdempotencyCache,
}

type AppState = Arc<Mutex<RuntimeHost>>;

#[tokio::main]
async fn main() -> Result<(), Box<dyn std::error::Error>> {
    // Route tracing events (including agentkit's TracingReporter output) to
    // stderr so they flow through firecracker's serial console into the
    // `assistant-runtime` log. RUST_LOG tunes the filter; default shows info+.
    tracing_subscriber::fmt()
        .with_env_filter(
            tracing_subscriber::EnvFilter::try_from_default_env()
                .unwrap_or_else(|_| tracing_subscriber::EnvFilter::new("info,agentkit=trace")),
        )
        .with_writer(std::io::stderr)
        .with_target(true)
        .init();

    let mode = std::env::args()
        .nth(1)
        .unwrap_or_else(|| "serve".to_string());

    match mode.as_str() {
        "serve" => {
            let addr = std::env::args()
                .skip(2)
                .collect::<Vec<_>>()
                .windows(2)
                .find_map(|pair| {
                    if pair[0] == "--addr" {
                        Some(pair[1].clone())
                    } else {
                        None
                    }
                })
                .unwrap_or_else(|| "0.0.0.0:8081".to_string());
            serve(addr.parse()?).await?;
            Ok(())
        }
        other => Err(format!("unsupported runner mode: {other}").into()),
    }
}

async fn serve(addr: SocketAddr) -> Result<(), Box<dyn std::error::Error>> {
    let state = Arc::new(Mutex::new(RuntimeHost::default()));

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
        .map_err(|err| (StatusCode::BAD_REQUEST, err.to_string()))?;

    let mut guard = state.lock().await;
    guard.runtime = Some(runtime);
    guard.configured = true;
    Ok(StatusCode::NO_CONTENT)
}

async fn turn(
    State(state): State<AppState>,
    headers: HeaderMap,
    Json(request): Json<RunnerRequest>,
) -> Result<Json<RunnerResponse>, (StatusCode, String)> {
    let idempotency_key = headers
        .get(IDEMPOTENCY_HEADER)
        .and_then(|v| v.to_str().ok())
        .map(|s| s.to_string());

    let mut guard = state.lock().await;

    if let Some(ref key) = idempotency_key {
        if guard.seen.contains(key) {
            eprintln!("[runner] dedup: skipping already-processed turn key={key}");
            return Ok(Json(RunnerResponse {
                finish_reason: "deduped".to_string(),
                final_text: String::new(),
                items: Vec::new(),
                usage: None,
                error: None,
            }));
        }
    }

    let configured = guard
        .runtime
        .as_mut()
        .ok_or_else(|| (StatusCode::CONFLICT, "runner is not configured".to_string()))?;

    let response = handle_turn(configured, request)
        .await
        .map_err(|err| (StatusCode::INTERNAL_SERVER_ERROR, err.to_string()))?;

    if let Some(key) = idempotency_key {
        guard.seen.insert(key);
    }

    Ok(Json(response))
}

async fn build_runtime(
    config: &RunnerConfig,
) -> Result<ConfiguredRuntime, Box<dyn std::error::Error>> {
    eprintln!(
        "[runner] build_runtime: model={} mcp_servers={} chat_id={}",
        config.model,
        config.mcp_servers.len(),
        config.chat_id
    );
    for server in &config.mcp_servers {
        let header_names: Vec<&String> = server.headers.keys().collect();
        eprintln!(
            "[runner] mcp server id={} url={} header_names={:?}",
            server.id, server.url, header_names
        );
    }

    // Independent sanity probe: hit each MCP endpoint with a raw reqwest POST
    // to verify the URL + auth header. If this works but agentkit_mcp hangs,
    // the bug is in the MCP client library, not in the token/URL.
    for server in &config.mcp_servers {
        let client = reqwest::Client::builder()
            .timeout(std::time::Duration::from_secs(5))
            .build();
        match client {
            Err(err) => eprintln!("[runner] probe: client build failed: {err}"),
            Ok(client) => {
                let mut req = client.post(&server.url)
                    .header("Content-Type", "application/json")
                    .header("Accept", "application/json, text/event-stream")
                    .body(r#"{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"gram-runner-probe","version":"0"}}}"#);
                for (k, v) in &server.headers {
                    req = req.header(k, v);
                }
                match req.send().await {
                    Ok(resp) => {
                        let status = resp.status();
                        let body = resp.text().await.unwrap_or_default();
                        let snippet: String = body.chars().take(300).collect();
                        eprintln!(
                            "[runner] probe id={} status={} body_len={} body_preview={}",
                            server.id, status, body.len(), snippet
                        );
                    }
                    Err(err) => {
                        eprintln!("[runner] probe id={} request error: {err}", server.id);
                        let mut source: &dyn std::error::Error = &err;
                        while let Some(next) = source.source() {
                            eprintln!("[runner] probe cause: {next}");
                            source = next;
                        }
                    }
                }
            }
        }
    }

    let tokens = TokenRegistry::new(config.auth_token.clone());
    let http_client = reqwest::Client::builder()
        .user_agent(concat!("gram-assistant-runner/", env!("CARGO_PKG_VERSION")))
        .build()?;

    let mut manager = McpServerManager::new();
    for server in &config.mcp_servers {
        let static_headers = server
            .headers
            .iter()
            .map(|(k, v)| {
                let name = http::HeaderName::from_bytes(k.as_bytes())
                    .map_err(|err| format!("mcp server {} header name {k}: {err}", server.id))?;
                let value = http::HeaderValue::from_str(v)
                    .map_err(|err| format!("mcp server {} header value for {k}: {err}", server.id))?;
                Ok::<_, String>((name, value))
            })
            .collect::<Result<Vec<_>, _>>()?;
        let http = build_decorated_http(http_client.clone(), tokens.clone(), static_headers);
        let transport = StreamableHttpTransportConfig::new(&server.url).with_client(http);
        manager = manager.with_server(McpServerConfig::new(
            &server.id,
            McpTransportBinding::StreamableHttp(transport),
        ));
    }

    // Bound the MCP connect so a misconfigured or unreachable MCP server surfaces
    // as a concrete 400 from /configure instead of the Go-side curl timing out at
    // 30s with no detail. Without this, agentkit's streamable HTTP transport can
    // block indefinitely on handshakes.
    match tokio::time::timeout(std::time::Duration::from_secs(10), manager.connect_all()).await {
        Ok(Ok(handles)) => {
            eprintln!("[runner] connect_all ok ({} servers)", handles.len());
        }
        Ok(Err(err)) => {
            eprintln!("[runner] connect_all failed: {err}");
            return Err(format!("connect_all: {err}").into());
        }
        Err(_) => {
            eprintln!("[runner] connect_all timed out after 10s");
            return Err("connect_all timed out after 10s".into());
        }
    }

    let completions_http = build_decorated_http(http_client.clone(), tokens.clone(), Vec::new());
    let adapter = CompletionsAdapter::with_client(GramProvider::new(config), completions_http);
    let combined = tools::bun_run::registry().merge(manager.tool_registry());
    let agent = Agent::builder()
        .model(adapter)
        .tools(combined)
        .permissions(AllowAll)
        .observer(VerboseReporter::new())
        .build()?;

    let driver = match tokio::time::timeout(
        std::time::Duration::from_secs(10),
        agent.start(SessionConfig::new(config.chat_id.clone()).with_cache(
            PromptCacheRequest::automatic().with_retention(PromptCacheRetention::Short),
        )),
    )
    .await
    {
        Ok(Ok(driver)) => driver,
        Ok(Err(err)) => {
            eprintln!("[runner] agent.start failed: {err}");
            return Err(format!("agent.start: {err}").into());
        }
        Err(_) => {
            eprintln!("[runner] agent.start timed out after 10s");
            return Err("agent.start timed out after 10s".into());
        }
    };
    eprintln!("[runner] build_runtime ok");

    Ok(ConfiguredRuntime {
        _config: config.clone(),
        _manager: manager,
        tokens,
        driver,
        hydrated: false,
    })
}

async fn handle_turn(
    runtime: &mut ConfiguredRuntime,
    request: RunnerRequest,
) -> Result<RunnerResponse, Box<dyn std::error::Error>> {
    if let Some(token) = request.auth_token.as_deref() {
        if !token.is_empty() {
            runtime.tokens.rotate(token);
        }
    }
    let mut items = Vec::new();
    if !runtime.hydrated {
        items.push(Item::text(
            ItemKind::System,
            &runtime._config.instructions.clone().unwrap_or_else(|| {
                format!(
                    "You are a Gram assistant. Current date/time: {}",
                    chrono::Utc::now().format("%Y-%m-%d %H:%M:%S UTC")
                )
            }),
        ));
        items.extend(normalize_history(&request.history)?);
        runtime.hydrated = true;
    }
    items.push(Item::text(ItemKind::User, &request.input));
    runtime.driver.submit_input(items)?;

    let turn = run_loop(&mut runtime.driver).await?;
    Ok(RunnerResponse {
        finish_reason: format!("{:?}", turn.finish_reason),
        final_text: extract_final_text(&turn.items),
        items: turn.items,
        usage: turn.usage,
        error: None,
    })
}

async fn run_loop<S>(
    driver: &mut LoopDriver<S>,
) -> Result<agentkit_loop::TurnResult, Box<dyn std::error::Error>>
where
    S: agentkit_loop::ModelSession,
{
    loop {
        match driver.next().await? {
            LoopStep::Finished(turn) => return Ok(turn),
            LoopStep::Interrupt(LoopInterrupt::ApprovalRequest(_req)) => {
                driver.resolve_approval(agentkit_tools_core::ApprovalDecision::Approve)?;
            }
            LoopStep::Interrupt(LoopInterrupt::AuthRequest(req)) => {
                driver.resolve_auth(agentkit_tools_core::AuthResolution::cancelled(req.request))?;
            }
            LoopStep::Interrupt(LoopInterrupt::AwaitingInput(_)) => {}
        }
    }
}

fn normalize_history(history: &[RunnerMessage]) -> Result<Vec<Item>, Box<dyn std::error::Error>> {
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
                        serde_json::from_str(&call.arguments).map_err(|err| {
                            format!(
                                "decode tool_call arguments for id {id}: {err}",
                                id = call.id
                            )
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
                    .ok_or("tool history message missing tool_call_id")?;
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
                return Err(format!("unsupported history role: {other}").into());
            }
        }
    }
    Ok(items)
}

fn extract_final_text(items: &[Item]) -> String {
    let mut out = String::new();
    for item in items {
        if !matches!(item.kind, ItemKind::Assistant) {
            continue;
        }
        for part in &item.parts {
            if let Part::Text(text) = part {
                out.push_str(&text.text);
            }
        }
    }
    out.trim().to_string()
}


struct VerboseReporter {
    inner: TracingReporter,
}

impl VerboseReporter {
    fn new() -> Self {
        Self {
            inner: TracingReporter::new(),
        }
    }
}

impl LoopObserver for VerboseReporter {
    fn handle_event(&mut self, event: AgentEvent) {
        match &event {
            AgentEvent::InputAccepted { items, .. } => {
                for item in items {
                    let text = item_text(item);
                    if !text.is_empty() {
                        tracing::info!(
                            target: "agentkit.content",
                            kind = ?item.kind,
                            text = %truncate(&text, 800),
                            "input item"
                        );
                    }
                }
            }
            AgentEvent::ContentDelta(delta) => {
                let rendered = format!("{delta:?}");
                tracing::trace!(target: "agentkit.content", delta = %truncate(&rendered, 800), "content delta");
            }
            AgentEvent::ToolCallRequested(call) => {
                let input = serde_json::to_string(&call.input).unwrap_or_default();
                tracing::info!(
                    target: "agentkit.content",
                    tool = %call.name,
                    input = %truncate(&input, 1200),
                    "tool call"
                );
            }
            AgentEvent::TurnFinished(result) => {
                for item in &result.items {
                    if !matches!(item.kind, ItemKind::Assistant) {
                        continue;
                    }
                    let text = item_text(item);
                    if !text.is_empty() {
                        tracing::info!(
                            target: "agentkit.content",
                            finish_reason = ?result.finish_reason,
                            text = %truncate(&text, 2000),
                            "assistant message"
                        );
                    }
                }
            }
            _ => {}
        }
        self.inner.handle_event(event);
    }
}

fn item_text(item: &Item) -> String {
    let mut out = String::new();
    for part in &item.parts {
        if let Part::Text(t) = part {
            out.push_str(&t.text);
        }
    }
    out
}

fn truncate(s: &str, max: usize) -> String {
    if s.chars().count() <= max {
        return s.to_string();
    }
    let mut out: String = s.chars().take(max).collect();
    out.push_str("…");
    out
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

    #[tokio::test]
    async fn state_reports_configured_after_configure() {
        let state_value = Arc::new(Mutex::new(RuntimeHost::default()));

        let status = configure(
            State(state_value.clone()),
            Json(RunnerConfig {
                model: "openai/gpt-4o-mini".to_string(),
                instructions: Some("test instructions".to_string()),
                auth_token: "test-token".to_string(),
                completions_url: Some("https://example.com/chat/completions".to_string()),
                chat_id: "test-chat".to_string(),
                mcp_servers: Vec::new(),
            }),
        )
        .await
        .expect("configure should succeed");
        assert_eq!(status, StatusCode::NO_CONTENT);

        let Json(response) = state_handler(State(state_value)).await;

        assert!(response.configured);
    }
}
