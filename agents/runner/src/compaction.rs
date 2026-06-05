//! Token-aware compaction wiring for the assistant runtime.
//!
//! The server picks a [`CompactionPolicy`] per thread and sends it in the
//! bootstrap blob. Three variants today:
//!
//! * `Threshold { percent }` — fire compaction when the latest item's
//!   `usage.input_tokens` crosses `percent` of the model's context window.
//!   Window is supplied at bootstrap (`ThreadBootstrap::context_window`)
//!   so the runner does not parse provider metadata.
//! * `OnTurnEnd` — fire compaction at every `AfterTurnEnded` regardless of
//!   utilisation.
//! * `Off` — never compact.
//!
//! Strategy pipeline: drop reasoning, drop failed tool results, summarise
//! older items through a nested agent loop on the same model. System +
//! context items and the most recent few turns are preserved.
//!
//! The compactor's adapter sends `Gram-Chat-ID` (the server's
//! assistant-scope guard rejects any runner request without one) plus
//! `Gram-Skip-Capture: 1`, which the chat handler honours by zeroing the
//! ChatID on the downstream completion request. Capture sees no chat id
//! and skips, so the compactor's "summarise this transcript" turn does
//! not persist as divergence on the user's chat.

use std::num::NonZeroU8;
use std::sync::Arc;

use agentkit_adapter_completions::CompletionsAdapter;
use agentkit_compaction::{
    AgentCompactor, CompactionPipeline, CompactionReason, DropFailedToolResultsStrategy,
    DropReasoningStrategy, StrategyCompactor, SummarizeOlderStrategy, TriggerFn,
    context_window_trigger,
};
use agentkit_core::{Item, ItemKind, SessionId};
use agentkit_loop::{Agent, MutationPoint};
use agentkit_provider_openrouter::OpenRouterProvider;
use agentkit_tools_core::{CompositePermissionChecker, PermissionDecision};
use serde::{Deserialize, Deserializer};

use crate::errors::RunnerError;

const COMPACTION_SYSTEM_PROMPT: &str = "You are a compaction agent. Compress the transcript that follows into a durable context note for an assistant that has lost the original messages. Preserve every named person, every year and date, every place, every decision the assistant committed to, every tool the assistant invoked, and every actionable fact in the tool results. Drop chatter, narration, and chain-of-thought. Return only the compacted note as plain text.";

const KEEP_RECENT: usize = 4;

const FALLBACK_PERCENT: NonZeroU8 = match NonZeroU8::new(60) {
    Some(n) => n,
    None => unreachable!(),
};

/// Compaction policy received from the server in the bootstrap blob.
///
/// The variants mirror the Go-side sealed sum type
/// (`server/internal/assistants/compaction_policy.go::CompactionPolicy`).
/// Deserialisation is lenient: a bootstrap that omits the field or sends an
/// unknown variant falls back to [`CompactionPolicy::fallback`] so a newer
/// runner can read an older server's bootstrap and vice versa.
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum CompactionPolicy {
    Threshold { percent: NonZeroU8 },
    OnTurnEnd,
    Off,
}

impl CompactionPolicy {
    /// Applied when the bootstrap omits the field or sends an unknown
    /// variant. Kept conservative so an unknown variant never produces a
    /// runaway transcript.
    fn fallback() -> Self {
        Self::Threshold {
            percent: FALLBACK_PERCENT,
        }
    }
}

impl Default for CompactionPolicy {
    fn default() -> Self {
        Self::fallback()
    }
}

impl<'de> Deserialize<'de> for CompactionPolicy {
    fn deserialize<D: Deserializer<'de>>(d: D) -> Result<Self, D::Error> {
        #[derive(Deserialize)]
        #[serde(rename_all = "snake_case")]
        enum Strict {
            Threshold { percent: NonZeroU8 },
            OnTurnEnd {},
            Off {},
        }
        let raw = serde_json::Value::deserialize(d)?;
        Ok(match serde_json::from_value::<Strict>(raw) {
            Ok(Strict::Threshold { percent }) => CompactionPolicy::Threshold { percent },
            Ok(Strict::OnTurnEnd {}) => CompactionPolicy::OnTurnEnd,
            Ok(Strict::Off {}) => CompactionPolicy::Off,
            Err(err) => {
                tracing::warn!(
                    error = %err,
                    "compaction policy deserialise failed, falling back to Threshold(60)"
                );
                CompactionPolicy::fallback()
            }
        })
    }
}

/// Trigger that fires at every `AfterTurnEnded` regardless of transcript
/// size. Symmetric with [`context_window_trigger`] except for the
/// utilisation check.
fn on_turn_end_trigger() -> TriggerFn {
    Box::new(move |_transcript: &[Item], point: MutationPoint| {
        if point != MutationPoint::AfterTurnEnded {
            return None;
        }
        Some(CompactionReason::Custom("on_turn_end".to_string()))
    })
}

/// Returns the trigger closure for the requested policy, or `None` when
/// compaction should be disabled entirely (`Off`, or `Threshold` without a
/// known context window).
fn build_trigger(policy: &CompactionPolicy, context_window: u64) -> Option<TriggerFn> {
    match policy {
        CompactionPolicy::Off => None,
        CompactionPolicy::Threshold { percent } => {
            if context_window == 0 {
                return None;
            }
            Some(context_window_trigger(
                context_window,
                u32::from(percent.get()),
            ))
        }
        CompactionPolicy::OnTurnEnd => Some(on_turn_end_trigger()),
    }
}

/// Builds the [`StrategyCompactor`] to attach to the agent via
/// [`agentkit_compaction::AgentBuilderCompactorExt::compactor`]. Returns
/// `None` when [`build_trigger`] declines.
pub fn build_compactor(
    policy: &CompactionPolicy,
    chat_id: &str,
    context_window: u64,
    compactor_adapter: CompletionsAdapter<OpenRouterProvider>,
) -> Result<Option<StrategyCompactor>, RunnerError> {
    let Some(trigger) = build_trigger(policy, context_window) else {
        return Ok(None);
    };
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
        .trigger(trigger)
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

#[cfg(test)]
#[allow(clippy::unwrap_used, clippy::expect_used, clippy::panic)]
mod tests {
    use super::*;
    use agentkit_core::Item;
    use agentkit_loop::MutationPoint;

    fn parse(json: &str) -> CompactionPolicy {
        serde_json::from_str(json).expect("CompactionPolicy::deserialize never errors")
    }

    #[test]
    fn deserialize_threshold() {
        match parse(r#"{"threshold":{"percent":60}}"#) {
            CompactionPolicy::Threshold { percent } => assert_eq!(percent.get(), 60),
            other => panic!("expected Threshold(60), got {other:?}"),
        }
    }

    #[test]
    fn deserialize_on_turn_end() {
        assert_eq!(
            parse(r#"{"on_turn_end":{}}"#),
            CompactionPolicy::OnTurnEnd
        );
    }

    #[test]
    fn deserialize_off() {
        assert_eq!(parse(r#"{"off":{}}"#), CompactionPolicy::Off);
    }

    #[test]
    fn deserialize_zero_percent_falls_back_to_threshold_60() {
        match parse(r#"{"threshold":{"percent":0}}"#) {
            CompactionPolicy::Threshold { percent } => assert_eq!(percent.get(), 60),
            other => panic!("expected fallback Threshold(60), got {other:?}"),
        }
    }

    #[test]
    fn deserialize_unknown_variant_falls_back_to_threshold_60() {
        match parse(r#"{"future_mode":{}}"#) {
            CompactionPolicy::Threshold { percent } => assert_eq!(percent.get(), 60),
            other => panic!("expected fallback Threshold(60), got {other:?}"),
        }
    }

    #[test]
    fn default_is_threshold_60() {
        match CompactionPolicy::default() {
            CompactionPolicy::Threshold { percent } => assert_eq!(percent.get(), 60),
            other => panic!("expected Threshold(60), got {other:?}"),
        }
    }

    #[test]
    fn build_trigger_off_returns_none() {
        assert!(build_trigger(&CompactionPolicy::Off, 1_000_000).is_none());
    }

    #[test]
    fn build_trigger_on_turn_end_returns_some_even_without_window() {
        let trigger = build_trigger(&CompactionPolicy::OnTurnEnd, 0)
            .expect("OnTurnEnd must produce a trigger regardless of context window");
        let fired = trigger(&[] as &[Item], MutationPoint::AfterTurnEnded);
        assert!(
            fired.is_some(),
            "OnTurnEnd trigger must fire at AfterTurnEnded"
        );
        let not_fired = trigger(&[] as &[Item], MutationPoint::AfterToolResult);
        assert!(
            not_fired.is_none(),
            "OnTurnEnd trigger must not fire at other mutation points"
        );
    }

    #[test]
    fn build_trigger_threshold_without_window_returns_none() {
        let policy = CompactionPolicy::Threshold {
            percent: NonZeroU8::new(60).unwrap(),
        };
        assert!(
            build_trigger(&policy, 0).is_none(),
            "Threshold without a known context window cannot compute a budget"
        );
    }

    #[test]
    fn build_trigger_threshold_with_window_returns_some() {
        let policy = CompactionPolicy::Threshold {
            percent: NonZeroU8::new(60).unwrap(),
        };
        assert!(
            build_trigger(&policy, 1_000_000).is_some(),
            "Threshold + known window must produce a trigger"
        );
    }
}
