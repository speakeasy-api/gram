use std::sync::Arc;
use std::time::Duration;

use agentkit_adapter_completions::CompletionsAdapter;
use agentkit_core::{Item, ItemKind, Part, TextPart, ToolCallPart, ToolOutput, ToolResultPart};
use agentkit_loop::{
    Agent, LoopDriver, LoopInterrupt, LoopStep, PromptCacheRequest, PromptCacheRetention,
    SessionConfig, TurnResult,
};
use agentkit_mcp::{
    McpServerConfig, McpServerManager, McpTransportBinding, StreamableHttpTransportConfig,
};
use agentkit_provider_openrouter::{OpenRouterConfig, OpenRouterProvider};
use agentkit_reporting::TracingReporter;
use agentkit_tools_core::PermissionChecker;
use serde_json::Value;
use tokio::sync::Mutex;

use crate::errors::RunnerError;
use crate::http_layer::{build_http, build_http_with_static, TokenRegistry};
use crate::idempotency::IdempotencyCache;
use crate::tools;
use crate::wire::{McpServer, RunnerConfig, RunnerMessage, RunnerRequest, RunnerResponse};

const MCP_CONNECT_TIMEOUT: Duration = Duration::from_secs(10);
const AGENT_START_TIMEOUT: Duration = Duration::from_secs(10);
const DEFAULT_COMPLETIONS_URL: &str = "http://127.0.0.1:8080/chat/completions";

pub type AppState = Arc<Mutex<RuntimeHost>>;

#[derive(Default)]
pub struct RuntimeHost {
    runtime: Option<ConfiguredRuntime>,
    pub configured: bool,
    pub seen: IdempotencyCache,
}

impl RuntimeHost {
    pub fn set_runtime(&mut self, runtime: ConfiguredRuntime) {
        self.runtime = Some(runtime);
        self.configured = true;
    }

    pub fn runtime_mut(&mut self) -> Option<&mut ConfiguredRuntime> {
        self.runtime.as_mut()
    }
}

pub struct ConfiguredRuntime {
    tokens: TokenRegistry,
    driver: LoopDriver<agentkit_adapter_completions::CompletionsSession<OpenRouterProvider>>,
    hydrated: bool,
    instructions: Option<String>,
    // Held so MCP transports outlive the session; dropping the manager would
    // disconnect the streamable-http transports the tool registry references.
    _mcp_manager: McpServerManager,
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

pub async fn build_runtime(config: &RunnerConfig) -> Result<ConfiguredRuntime, RunnerError> {
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
        http::HeaderValue::from_str(&config.chat_id).map_err(|source| RunnerError::HeaderValue { source })?,
    );

    let http_client = reqwest::Client::builder()
        .user_agent(concat!("gram-assistant-runner/", env!("CARGO_PKG_VERSION")))
        .default_headers(default_headers)
        .build()?;

    let manager = connect_mcp_servers(&config.mcp_servers, http_client.clone(), tokens.clone()).await?;

    let base_url = config
        .completions_url
        .clone()
        .unwrap_or_else(|| DEFAULT_COMPLETIONS_URL.to_string());
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

    let session = SessionConfig::new(config.chat_id.clone()).with_cache(
        PromptCacheRequest::automatic().with_retention(PromptCacheRetention::Short),
    );
    let driver = match tokio::time::timeout(AGENT_START_TIMEOUT, agent.start(session)).await {
        Ok(Ok(driver)) => driver,
        Ok(Err(e)) => return Err(RunnerError::AgentStart(e.to_string())),
        Err(_) => return Err(RunnerError::AgentStartTimeout(AGENT_START_TIMEOUT)),
    };

    tracing::info!("build_runtime ok");

    Ok(ConfiguredRuntime {
        tokens,
        driver,
        hydrated: false,
        instructions: config.instructions.clone(),
        _mcp_manager: manager,
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

pub async fn handle_turn(
    runtime: &mut ConfiguredRuntime,
    request: RunnerRequest,
) -> Result<RunnerResponse, RunnerError> {
    if let Some(token) = request.auth_token.as_deref()
        && !token.is_empty()
    {
        runtime.tokens.rotate(token)?;
    }

    let mut items = Vec::new();
    if !runtime.hydrated {
        if let Some(instructions) = &runtime.instructions {
            items.push(Item::text(ItemKind::System, instructions));
        }
        items.extend(normalize_history(&request.history)?);
        runtime.hydrated = true;
    }
    items.push(Item::text(ItemKind::User, &request.input));
    runtime
        .driver
        .submit_input(items)
        .map_err(|e| RunnerError::SubmitInput(e.to_string()))?;

    let turn = run_loop(&mut runtime.driver).await?;
    Ok(RunnerResponse {
        finish_reason: format!("{:?}", turn.finish_reason),
        final_text: extract_final_text(&turn.items),
        items: turn.items,
        usage: turn.usage,
        error: None,
    })
}

async fn run_loop<S>(driver: &mut LoopDriver<S>) -> Result<TurnResult, RunnerError>
where
    S: agentkit_loop::ModelSession,
{
    loop {
        match driver.next().await? {
            LoopStep::Finished(turn) => return Ok(turn),
            LoopStep::Interrupt(LoopInterrupt::ApprovalRequest(_req)) => {
                tracing::warn!(
                    "unexpected approval request — assistant runner auto-approves; \
                     tools should not require approval in this environment"
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
