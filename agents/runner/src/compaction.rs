//! Token-aware compaction wiring for the assistant runtime.
//!
//! The model's context window is supplied at `/configure` time
//! (`RunnerConfig::context_window`) — the gram backend resolves it via
//! `openrouter.ContextWindowResolver` so the runner doesn't need to parse
//! provider-specific metadata or make a fresh OpenRouter round-trip.
//! [`agentkit_compaction::context_window_trigger`] fires once the latest
//! transcript item's reported `usage.input_tokens` crosses a configurable
//! percentage of the window.
//!
//! Strategy pipeline: drop reasoning, drop failed tool results, summarise older
//! items through a nested agent loop on the same model. System + context items
//! and the most recent few turns are preserved.
//!
//! The compactor's adapter sends `Gram-Chat-ID` (the server's
//! assistant-scope guard rejects any runner request without one) plus
//! `Gram-Skip-Capture: 1`, which the chat handler honours by zeroing the
//! ChatID on the downstream completion request. Capture sees no chat id
//! and skips, so the compactor's "summarise this transcript" turn does
//! not persist as divergence on the user's chat.

use std::sync::Arc;

use agentkit_adapter_completions::CompletionsAdapter;
use agentkit_compaction::{
    AgentCompactor, CompactionPipeline, DropFailedToolResultsStrategy, DropReasoningStrategy,
    StrategyCompactor, SummarizeOlderStrategy, context_window_trigger,
};
use agentkit_core::{ItemKind, SessionId};
use agentkit_loop::Agent;
use agentkit_provider_openrouter::OpenRouterProvider;
use agentkit_tools_core::{CompositePermissionChecker, PermissionDecision};

use crate::errors::RunnerError;

const COMPACTION_SYSTEM_PROMPT: &str = "You are a compaction agent. Compress the transcript that follows into a durable context note for an assistant that has lost the original messages. Preserve every named person, every year and date, every place, every decision the assistant committed to, every tool the assistant invoked, and every actionable fact in the tool results. Drop chatter, narration, and chain-of-thought. Return only the compacted note as plain text.";

const TRIGGER_PERCENTAGE: u32 = 80;
const KEEP_RECENT: usize = 4;

/// Builds the [`StrategyCompactor`] to attach to the agent via
/// [`agentkit_compaction::AgentBuilderCompactorExt::compactor`]. Returns
/// `None` when the model's context window is unknown — without a budget the
/// trigger has nothing to compare against.
pub fn build_compactor(
    chat_id: &str,
    context_window: u64,
    compactor_adapter: CompletionsAdapter<OpenRouterProvider>,
) -> Result<Option<StrategyCompactor>, RunnerError> {
    if context_window == 0 {
        return Ok(None);
    }
    let backend_agent = Arc::new(
        Agent::builder()
            .model(compactor_adapter)
            .permissions(CompositePermissionChecker::new(PermissionDecision::Allow))
            .build()
            .map_err(|e| RunnerError::AgentBuild(e.to_string()))?,
    );
    let backend = AgentCompactor::builder()
        .agent(backend_agent)
        .session_id(SessionId::from(format!("{chat_id}-compactor")))
        .system_prompt(COMPACTION_SYSTEM_PROMPT)
        .build()
        .map_err(|e| RunnerError::AgentBuild(e.to_string()))?;
    let compactor = StrategyCompactor::builder()
        .trigger(context_window_trigger(context_window, TRIGGER_PERCENTAGE))
        .strategy(
            CompactionPipeline::new()
                .with_strategy(DropReasoningStrategy::new())
                .with_strategy(DropFailedToolResultsStrategy::new())
                .with_strategy(
                    SummarizeOlderStrategy::new(KEEP_RECENT)
                        .preserve_kind(ItemKind::System)
                        .preserve_kind(ItemKind::Context),
                ),
        )
        .backend(backend)
        .build()
        .map_err(|e| RunnerError::AgentBuild(e.to_string()))?;
    Ok(Some(compactor))
}
