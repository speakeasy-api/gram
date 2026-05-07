//! Token-aware compaction wiring for the assistant runtime.
//!
//! The runner reads `gram_metadata.context_window` from completion responses
//! (decorated by the gram chat service when the URL carries
//! `?includeContextWindow=1`) and a [`ContextWindowTrigger`] fires once the
//! provider-reported `usage.input_tokens` crosses a configurable percentage of
//! that window. The strategy pipeline drops reasoning + failed tool results
//! and summarises older items through a nested agent loop on the same model.
//!
//! The compactor's adapter is built without the `Gram-Chat-ID` default header
//! so its calls bypass the chat capture path — capture runs a divergence check
//! per chat_id and would otherwise persist the compactor's "summarise the
//! transcript" turn, polluting the next replay.

use std::sync::Arc;
use std::sync::atomic::{AtomicU64, Ordering};

use agentkit_adapter_completions::{CompletionsAdapter, CompletionsProvider};
use agentkit_compaction::{
    CompactionBackend, CompactionConfig, CompactionError, CompactionPipeline, CompactionReason,
    CompactionTrigger, DropFailedToolResultsStrategy, DropReasoningStrategy,
    SummarizeOlderStrategy, SummaryRequest, SummaryResult,
};
use agentkit_core::{
    Item, ItemKind, MetadataMap, Part, SessionId, TurnCancellation, TurnId, Usage,
};
use agentkit_http::{HttpRequestBuilder, StatusCode};
use agentkit_loop::{
    Agent, AgentEvent, LoopError, LoopInterrupt, LoopObserver, LoopStep, ModelSession,
    SessionConfig, TurnRequest,
};
use agentkit_provider_openrouter::OpenRouterProvider;
use agentkit_tools_core::{PermissionChecker, PermissionDecision, PermissionRequest};
use async_trait::async_trait;
use serde_json::{Map, Value};

const COMPACTION_SYSTEM_PROMPT: &str = "You are a compaction agent. Compress the transcript that follows into a durable context note for an assistant that has lost the original messages. Preserve every named person, every year and date, every place, every decision the assistant committed to, every tool the assistant invoked, and every actionable fact in the tool results. Drop chatter, narration, and chain-of-thought. Return only the compacted note as plain text.";

const DEFAULT_PERCENTAGE: u32 = 80;
const KEEP_RECENT: usize = 4;

/// Wraps [`OpenRouterProvider`] and folds `gram_metadata.context_window` from
/// the raw response into a shared [`AtomicU64`]. The trigger reads the same
/// atomic, so the budget tracks whatever the most recent response advertised
/// (the value is stable per model but cheap to keep refreshing).
#[derive(Clone)]
pub struct GramCompletionsProvider {
    inner: OpenRouterProvider,
    context_window: Arc<AtomicU64>,
}

impl GramCompletionsProvider {
    pub fn new(inner: OpenRouterProvider) -> Self {
        Self {
            inner,
            context_window: Arc::new(AtomicU64::new(0)),
        }
    }

    pub fn context_window_handle(&self) -> Arc<AtomicU64> {
        Arc::clone(&self.context_window)
    }
}

impl CompletionsProvider for GramCompletionsProvider {
    type Config = <OpenRouterProvider as CompletionsProvider>::Config;

    fn provider_name(&self) -> &str {
        self.inner.provider_name()
    }

    fn endpoint_url(&self) -> &str {
        self.inner.endpoint_url()
    }

    fn config(&self) -> &Self::Config {
        self.inner.config()
    }

    fn preprocess_request(&self, builder: HttpRequestBuilder) -> HttpRequestBuilder {
        self.inner.preprocess_request(builder)
    }

    fn apply_prompt_cache(
        &self,
        body: &mut Map<String, Value>,
        request: &TurnRequest,
    ) -> Result<(), LoopError> {
        self.inner.apply_prompt_cache(body, request)
    }

    fn requires_alternating_roles(&self) -> bool {
        self.inner.requires_alternating_roles()
    }

    fn preprocess_response(&self, status: StatusCode, body: &str) -> Result<(), LoopError> {
        self.inner.preprocess_response(status, body)
    }

    fn postprocess_response(
        &self,
        usage: &mut Option<Usage>,
        metadata: &mut MetadataMap,
        raw: &Value,
    ) {
        self.inner.postprocess_response(usage, metadata, raw);
        if let Some(cw) = raw
            .pointer("/gram_metadata/context_window")
            .and_then(Value::as_u64)
            && cw > 0
        {
            self.context_window.store(cw, Ordering::Release);
        }
    }
}

/// Fires compaction once the provider-reported `input_tokens` reaches a
/// percentage of the model's context window. The window itself is read from
/// the same atomic that [`GramCompletionsProvider::postprocess_response`]
/// writes to; until the first response arrives the trigger is dormant.
#[derive(Clone)]
pub struct ContextWindowTrigger {
    context_window: Arc<AtomicU64>,
    last_input_tokens: Arc<AtomicU64>,
    percentage: u32,
}

impl ContextWindowTrigger {
    pub fn new(context_window: Arc<AtomicU64>, percentage: u32) -> Self {
        Self {
            context_window,
            last_input_tokens: Arc::new(AtomicU64::new(0)),
            percentage: percentage.clamp(1, 100),
        }
    }

    pub fn input_tokens_handle(&self) -> Arc<AtomicU64> {
        Arc::clone(&self.last_input_tokens)
    }

    fn threshold(&self) -> u64 {
        let win = self.context_window.load(Ordering::Acquire);
        win.saturating_mul(self.percentage as u64) / 100
    }
}

impl CompactionTrigger for ContextWindowTrigger {
    fn should_compact(
        &self,
        _session_id: &SessionId,
        _turn_id: Option<&TurnId>,
        _transcript: &[Item],
    ) -> Option<CompactionReason> {
        let win = self.context_window.load(Ordering::Acquire);
        if win == 0 {
            return None;
        }
        let last = self.last_input_tokens.load(Ordering::Acquire);
        let threshold = self.threshold();
        if last >= threshold {
            Some(CompactionReason::Custom(format!(
                "input_tokens={last} >= threshold={threshold} (window={win}, {}%)",
                self.percentage
            )))
        } else {
            None
        }
    }
}

/// Mirrors [`AgentEvent::UsageUpdated`] into the trigger's `last_input_tokens`
/// atomic so [`ContextWindowTrigger::should_compact`] sees the freshest count.
#[derive(Clone)]
pub struct InputTokenObserver {
    last_input_tokens: Arc<AtomicU64>,
}

impl InputTokenObserver {
    pub fn new(handle: Arc<AtomicU64>) -> Self {
        Self {
            last_input_tokens: handle,
        }
    }
}

impl LoopObserver for InputTokenObserver {
    fn handle_event(&mut self, event: AgentEvent) {
        if let AgentEvent::UsageUpdated(usage) = event
            && let Some(tokens) = usage.tokens
        {
            self.last_input_tokens
                .store(tokens.input_tokens, Ordering::Release);
        }
    }
}

/// Runs a nested [`Agent`] loop on a sibling adapter to summarise older items.
///
/// The adapter must be built without the `Gram-Chat-ID` default header so the
/// compactor's request bypasses chat capture.
pub struct NestedLoopCompactionBackend {
    adapter: CompletionsAdapter<GramCompletionsProvider>,
}

impl NestedLoopCompactionBackend {
    pub fn new(adapter: CompletionsAdapter<GramCompletionsProvider>) -> Self {
        Self { adapter }
    }
}

struct AllowAll;

impl PermissionChecker for AllowAll {
    fn evaluate(&self, _request: &dyn PermissionRequest) -> PermissionDecision {
        PermissionDecision::Allow
    }
}

#[async_trait]
impl CompactionBackend for NestedLoopCompactionBackend {
    async fn summarize(
        &self,
        request: SummaryRequest,
        cancellation: Option<TurnCancellation>,
    ) -> Result<SummaryResult, CompactionError> {
        if cancellation
            .as_ref()
            .is_some_and(TurnCancellation::is_cancelled)
        {
            return Err(CompactionError::Cancelled);
        }

        let rendered = render_items(&request.items);
        let mut builder = Agent::builder()
            .model(self.adapter.clone())
            .permissions(AllowAll)
            .transcript(vec![Item::text(ItemKind::System, COMPACTION_SYSTEM_PROMPT)])
            .input(vec![Item::text(
                ItemKind::User,
                format!(
                    "Compress the transcript below into a durable context note. Preserve names, places, dates, decisions, and tool outcomes.\n\n{rendered}"
                ),
            )]);
        if let Some(c) = cancellation.as_ref() {
            builder = builder.cancellation(c.handle().clone());
        }
        let agent = builder
            .build()
            .map_err(|e| CompactionError::Failed(e.to_string()))?;

        let mut driver = agent
            .start(SessionConfig::new(format!(
                "{}-compactor",
                request.session_id
            )))
            .await
            .map_err(|e| CompactionError::Failed(e.to_string()))?;

        let summary = run_to_completion(&mut driver)
            .await
            .map_err(CompactionError::Failed)?;

        Ok(SummaryResult {
            items: vec![Item::text(ItemKind::Context, summary)],
            metadata: MetadataMap::new(),
        })
    }
}

async fn run_to_completion<S>(driver: &mut agentkit_loop::LoopDriver<S>) -> Result<String, String>
where
    S: ModelSession,
{
    loop {
        let step = driver.next().await.map_err(|e| e.to_string())?;
        match step {
            LoopStep::Finished(result) => {
                let mut sections = Vec::new();
                for item in result.items {
                    if item.kind != ItemKind::Assistant {
                        continue;
                    }
                    for part in item.parts {
                        if let Part::Text(t) = part {
                            sections.push(t.text);
                        }
                    }
                }
                return Ok(sections.join("\n"));
            }
            LoopStep::Interrupt(LoopInterrupt::AfterToolResult(_)) => continue,
            LoopStep::Interrupt(LoopInterrupt::AwaitingInput(_)) => {
                return Err("compaction sub-agent unexpectedly awaiting input".into());
            }
            LoopStep::Interrupt(LoopInterrupt::ApprovalRequest(_)) => {
                return Err("compaction sub-agent unexpectedly required approval".into());
            }
        }
    }
}

fn render_items(items: &[Item]) -> String {
    items
        .iter()
        .map(|item| {
            let kind = match item.kind {
                ItemKind::User => "USER",
                ItemKind::Assistant => "ASSISTANT",
                ItemKind::System => "SYSTEM",
                ItemKind::Developer => "DEVELOPER",
                ItemKind::Tool => "TOOL",
                ItemKind::Context => "CONTEXT",
                ItemKind::Notification => "NOTIFICATION",
            };
            let body = item
                .parts
                .iter()
                .filter_map(|p| match p {
                    Part::Text(t) => Some(t.text.clone()),
                    Part::Structured(v) => Some(v.value.to_string()),
                    _ => None,
                })
                .collect::<Vec<_>>()
                .join("\n");
            format!("[{kind}]\n{body}")
        })
        .collect::<Vec<_>>()
        .join("\n\n")
}

/// Builds the trigger + pipeline + backend, returning the [`CompactionConfig`]
/// to attach to the agent and the [`InputTokenObserver`] that must be
/// registered on the same agent so the trigger sees `input_tokens`.
pub fn build_compaction(
    main_provider: &GramCompletionsProvider,
    compactor_adapter: CompletionsAdapter<GramCompletionsProvider>,
    percentage: u32,
) -> (CompactionConfig, InputTokenObserver) {
    let trigger = ContextWindowTrigger::new(main_provider.context_window_handle(), percentage);
    let observer = InputTokenObserver::new(trigger.input_tokens_handle());
    let pipeline = CompactionPipeline::new()
        .with_strategy(DropReasoningStrategy::new())
        .with_strategy(DropFailedToolResultsStrategy::new())
        .with_strategy(
            SummarizeOlderStrategy::new(KEEP_RECENT)
                .preserve_kind(ItemKind::System)
                .preserve_kind(ItemKind::Context),
        );
    let backend = NestedLoopCompactionBackend::new(compactor_adapter);
    let config = CompactionConfig::new(trigger, pipeline).with_backend(backend);
    (config, observer)
}

pub fn percentage_from_env() -> u32 {
    std::env::var("ASSISTANT_CONTEXT_PERCENTAGE")
        .ok()
        .and_then(|s| s.parse::<u32>().ok())
        .map(|v| v.clamp(1, 100))
        .unwrap_or(DEFAULT_PERCENTAGE)
}
