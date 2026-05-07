//! Token-aware compaction wiring for the assistant runtime.
//!
//! The model's context window is supplied at `/configure` time
//! (`RunnerConfig::context_window`) — the gram backend resolves it via
//! `openrouter.ContextWindowResolver` so the runner doesn't need to parse
//! provider-specific metadata or make a fresh OpenRouter round-trip. A
//! [`ContextWindowTrigger`] fires once `usage.input_tokens` (read from
//! `AgentEvent::UsageUpdated`) crosses a configurable percentage of the window.
//!
//! Strategy pipeline: drop reasoning, drop failed tool results, summarise older
//! items through a nested agent loop on the same model. System + context items
//! and the most recent few turns are preserved.
//!
//! The compactor's adapter is built without the `Gram-Chat-ID` default header
//! so its calls bypass chat capture — capture runs a divergence check per
//! chat_id and would otherwise persist the compactor's "summarise this
//! transcript" turn, polluting the next replay.

use std::sync::Arc;
use std::sync::atomic::{AtomicU64, Ordering};

use agentkit_adapter_completions::CompletionsAdapter;
use agentkit_compaction::{
    CompactionBackend, CompactionConfig, CompactionError, CompactionPipeline, CompactionReason,
    CompactionTrigger, DropFailedToolResultsStrategy, DropReasoningStrategy,
    SummarizeOlderStrategy, SummaryRequest, SummaryResult,
};
use agentkit_core::{Item, ItemKind, MetadataMap, Part, SessionId, TurnCancellation, TurnId};
use agentkit_loop::{
    Agent, AgentEvent, LoopInterrupt, LoopObserver, LoopStep, ModelSession, SessionConfig,
};
use agentkit_provider_openrouter::OpenRouterProvider;
use agentkit_tools_core::{PermissionChecker, PermissionDecision, PermissionRequest};
use async_trait::async_trait;

const COMPACTION_SYSTEM_PROMPT: &str = "You are a compaction agent. Compress the transcript that follows into a durable context note for an assistant that has lost the original messages. Preserve every named person, every year and date, every place, every decision the assistant committed to, every tool the assistant invoked, and every actionable fact in the tool results. Drop chatter, narration, and chain-of-thought. Return only the compacted note as plain text.";

const DEFAULT_PERCENTAGE: u32 = 80;
const KEEP_RECENT: usize = 4;

/// Fires compaction once the provider-reported `input_tokens` reaches a
/// percentage of the configured context window. The window is static for a
/// given runtime — the model never changes mid-session — so it lives here as
/// a plain `u64` rather than an atomic.
#[derive(Clone)]
pub struct ContextWindowTrigger {
    context_window: u64,
    last_input_tokens: Arc<AtomicU64>,
    percentage: u32,
}

impl ContextWindowTrigger {
    pub fn new(context_window: u64, percentage: u32) -> Self {
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
        self.context_window
            .saturating_mul(self.percentage as u64)
            / 100
    }
}

impl CompactionTrigger for ContextWindowTrigger {
    fn should_compact(
        &self,
        _session_id: &SessionId,
        _turn_id: Option<&TurnId>,
        _transcript: &[Item],
    ) -> Option<CompactionReason> {
        if self.context_window == 0 {
            return None;
        }
        let last = self.last_input_tokens.load(Ordering::Acquire);
        let threshold = self.threshold();
        if last >= threshold {
            Some(CompactionReason::Custom(format!(
                "input_tokens={last} >= threshold={threshold} (window={}, {}%)",
                self.context_window, self.percentage
            )))
        } else {
            None
        }
    }
}

/// Mirrors [`AgentEvent::UsageUpdated`] into the trigger's `last_input_tokens`
/// atomic; resets to zero on `CompactionFinished` so a subsequent
/// `should_compact` call (defensive, in case the loop ever evaluates the
/// trigger more than once per turn) doesn't fire on the pre-compaction count.
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
        match event {
            AgentEvent::UsageUpdated(usage) => {
                if let Some(tokens) = usage.tokens {
                    self.last_input_tokens
                        .store(tokens.input_tokens, Ordering::Release);
                }
            }
            AgentEvent::CompactionFinished { .. } => {
                self.last_input_tokens.store(0, Ordering::Release);
            }
            _ => {}
        }
    }
}

/// Runs a nested [`Agent`] loop on a sibling adapter to summarise older items.
pub struct NestedLoopCompactionBackend {
    adapter: CompletionsAdapter<OpenRouterProvider>,
}

impl NestedLoopCompactionBackend {
    pub fn new(adapter: CompletionsAdapter<OpenRouterProvider>) -> Self {
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

async fn run_to_completion<S>(
    driver: &mut agentkit_loop::LoopDriver<S>,
) -> Result<String, String>
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
/// registered on the same agent so the trigger sees `input_tokens`. Returns
/// `None` when the model's context window is unknown — without a budget the
/// trigger has nothing to compare against.
pub fn build_compaction(
    context_window: u64,
    compactor_adapter: CompletionsAdapter<OpenRouterProvider>,
    percentage: u32,
) -> Option<(CompactionConfig, InputTokenObserver)> {
    if context_window == 0 {
        return None;
    }
    let trigger = ContextWindowTrigger::new(context_window, percentage);
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
    Some((config, observer))
}

pub fn percentage_from_env() -> u32 {
    std::env::var("ASSISTANT_CONTEXT_PERCENTAGE")
        .ok()
        .and_then(|s| s.parse::<u32>().ok())
        .map(|v| v.clamp(1, 100))
        .unwrap_or(DEFAULT_PERCENTAGE)
}
