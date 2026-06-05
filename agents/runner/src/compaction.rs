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
    AgentCompactor, CompactionError, CompactionPipeline, CompactionReason, Compactor,
    DropFailedToolResultsStrategy, DropReasoningStrategy, StrategyCompactor,
    SummarizeOlderStrategy, TriggerFn, context_window_trigger,
};
use agentkit_core::{Item, ItemKind, Part, SessionId, ToolOutput, TurnCancellation};
use agentkit_loop::{Agent, MutationPoint};
use agentkit_provider_openrouter::OpenRouterProvider;
use agentkit_tools_core::{CompositePermissionChecker, PermissionDecision};
use async_trait::async_trait;
use serde::{Deserialize, Deserializer};

use crate::errors::RunnerError;
use crate::gram_client::GramBootstrapClient;
use crate::http_layer::TokenRegistry;
use crate::wire::{RunnerMessage, RunnerToolCall};

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

/// Wraps a [`StrategyCompactor`] and synchronously POSTs the post-
/// compaction transcript to the server inside `compact()`. Persisting
/// before `compact()` returns serialises with the agent loop: the next
/// turn cannot dispatch a captured `/chat/completions` request until the
/// new generation row is in `chat_messages`, so a follow-up capture
/// cannot race ahead and write a newer generation that the compaction
/// row would then overwrite. Without this in-line tap the in-memory
/// mutation alone wouldn't reach the DB (the compactor's adapter sends
/// `Gram-Skip-Capture: 1`), and a cold cron bootstrap would re-load the
/// un-compacted prior generation.
pub struct PersistingCompactor {
    inner: StrategyCompactor,
    client: GramBootstrapClient,
    tokens: TokenRegistry,
    thread_id: String,
}

impl PersistingCompactor {
    pub fn new(
        inner: StrategyCompactor,
        client: GramBootstrapClient,
        tokens: TokenRegistry,
        thread_id: String,
    ) -> Self {
        Self {
            inner,
            client,
            tokens,
            thread_id,
        }
    }
}

#[async_trait]
impl Compactor for PersistingCompactor {
    fn should_compact(
        &self,
        transcript: &[Item],
        point: MutationPoint,
    ) -> Option<CompactionReason> {
        self.inner.should_compact(transcript, point)
    }

    async fn compact(
        &self,
        transcript: &[Item],
        reason: CompactionReason,
        cancellation: Option<TurnCancellation>,
    ) -> Result<Vec<Item>, CompactionError> {
        // Only `Cancelled` should propagate — that's the loop tearing the
        // turn down deliberately. Backend/provider failures during the
        // summarisation call (timeouts, OpenRouter 5xx) would otherwise
        // surface through `agentkit_compaction::CompactorMutator` as a
        // `LoopError::Mutator` and kill the per-thread runner task even
        // though the user-visible turn already succeeded. Log and keep
        // the un-compacted transcript so the thread stays alive; the
        // trigger fires again on the next AfterTurnEnded and retries.
        let raw = match self.inner.compact(transcript, reason, cancellation).await {
            Ok(items) => items,
            Err(CompactionError::Cancelled) => return Err(CompactionError::Cancelled),
            Err(err) => {
                tracing::warn!(
                    thread_id = %self.thread_id,
                    error = %err,
                    "compaction summarisation failed; keeping un-compacted transcript and will retry on next trigger"
                );
                return Ok(transcript.to_vec());
            }
        };
        // AgentCompactor emits its summary as `ItemKind::Context`, which
        // agentkit-adapter-completions serialises as a `system` chat message.
        // We persist Context as `role="user"` so it survives loadChatHistory's
        // system-row drop on cold bootstrap — but if we left the in-memory
        // kind as Context, the *next* warm turn would send `system` upstream,
        // capture's matcher would diverge against our `user` row, and write a
        // newer generation containing the summary as `system` that the cold
        // bootstrap after THAT would silently drop. Rewriting Context to User
        // here keeps warm-outbound, captured row, persisted row, and cold-
        // reload all consistent at `user`.
        let compacted: Vec<Item> = raw
            .into_iter()
            .map(|mut item| {
                if item.kind == ItemKind::Context {
                    item.kind = ItemKind::User;
                }
                item
            })
            .collect();
        let messages = denormalize_transcript(&compacted);
        if messages.is_empty() {
            return Ok(compacted);
        }
        match self
            .client
            .record_compacted_generation(&self.thread_id, &self.tokens, &messages)
            .await
        {
            Ok(()) => {
                tracing::info!(
                    thread_id = %self.thread_id,
                    rows = messages.len(),
                    "compacted generation persisted"
                );
                Ok(compacted)
            }
            Err(err) => {
                // Persisting failed — keep the in-memory transcript unchanged so
                // it stays consistent with what a cold bootstrap will see, and
                // retry on the next trigger. Returning the compacted vec here
                // would diverge in-memory from the DB and silently mask the
                // failure until the next cold bootstrap dropped the summary.
                tracing::warn!(
                    thread_id = %self.thread_id,
                    error = %err,
                    "failed to persist compacted generation; keeping un-compacted transcript and will retry on next trigger"
                );
                Ok(transcript.to_vec())
            }
        }
    }
}

/// Builds the compactor to attach to the agent via
/// [`agentkit_compaction::AgentBuilderCompactorExt::compactor`]. Returns
/// `None` when [`build_trigger`] declines. The returned compactor POSTs
/// the post-compaction transcript to the server synchronously inside
/// `compact()` so the new chat_messages generation is durable before the
/// loop accepts the next turn.
pub fn build_compactor(
    policy: &CompactionPolicy,
    chat_id: &str,
    thread_id: &str,
    context_window: u64,
    compactor_adapter: CompletionsAdapter<OpenRouterProvider>,
    client: GramBootstrapClient,
    tokens: TokenRegistry,
) -> Result<Option<PersistingCompactor>, RunnerError> {
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
    let inner = StrategyCompactor::builder()
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
    Ok(Some(PersistingCompactor::new(
        inner,
        client,
        tokens,
        thread_id.to_string(),
    )))
}

/// Converts an agentkit transcript back into the wire shape the server
/// persists. Mirrors `runtime::normalize_history` in reverse. Callers
/// hitting this for compaction persistence should already have rewritten
/// any `ItemKind::Context` items to `User` (see
/// [`PersistingCompactor::compact`]) so warm-outbound and persisted-row
/// shapes stay consistent; Context arriving here unrewritten still
/// collapses to `role=user` for the same survival-across-bootstrap
/// reason, but that's a backstop rather than the supported entry
/// shape.
///
/// Specifically:
///
/// * `System`, `Developer` items → `role=system` (loadChatHistory drops
///   these on bootstrap; the server-composed system prompt replaces
///   them anyway).
/// * `Context`, `User`, `Notification` items → `role=user`.
/// * `Assistant` items → single row with concatenated text and any
///   tool_calls from `Part::ToolCall` parts.
/// * `Tool` items → one row per `Part::ToolResult` with its `call_id`.
///
/// Non-text content (media, file, structured, reasoning, custom) is
/// dropped — the strategy pipeline already strips reasoning, and the
/// other kinds don't round-trip through the runner today.
pub fn denormalize_transcript(items: &[Item]) -> Vec<RunnerMessage> {
    let mut out = Vec::with_capacity(items.len());
    for item in items {
        match item.kind {
            ItemKind::System | ItemKind::Developer => {
                out.push(RunnerMessage {
                    role: "system".to_string(),
                    content: concat_text(&item.parts),
                    tool_calls: Vec::new(),
                    tool_call_id: None,
                });
            }
            ItemKind::Context | ItemKind::User | ItemKind::Notification => {
                out.push(RunnerMessage {
                    role: "user".to_string(),
                    content: concat_text(&item.parts),
                    tool_calls: Vec::new(),
                    tool_call_id: None,
                });
            }
            ItemKind::Assistant => {
                let content = concat_text(&item.parts);
                let tool_calls: Vec<RunnerToolCall> = item
                    .parts
                    .iter()
                    .filter_map(|p| match p {
                        Part::ToolCall(call) => Some(RunnerToolCall {
                            id: call.id.to_string(),
                            name: call.name.clone(),
                            arguments: call.input.to_string(),
                        }),
                        _ => None,
                    })
                    .collect();
                out.push(RunnerMessage {
                    role: "assistant".to_string(),
                    content,
                    tool_calls,
                    tool_call_id: None,
                });
            }
            ItemKind::Tool => {
                for part in &item.parts {
                    if let Part::ToolResult(result) = part {
                        out.push(RunnerMessage {
                            role: "tool".to_string(),
                            content: tool_output_text(&result.output),
                            tool_calls: Vec::new(),
                            tool_call_id: Some(result.call_id.to_string()),
                        });
                    }
                }
            }
        }
    }
    out
}

fn concat_text(parts: &[Part]) -> String {
    let mut buf = String::new();
    for part in parts {
        if let Part::Text(t) = part {
            if !buf.is_empty() {
                buf.push('\n');
            }
            buf.push_str(&t.text);
        }
    }
    buf
}

// Mirrors `agentkit_adapter_completions::request::tool_output_to_string` so
// the persisted compaction row matches what the next outbound capture would
// have stored. `Parts` and `Files` get the same JSON-serialised shape the
// completions adapter sends upstream; collapsing them to text would drop
// structured/media/file payloads that the assistant relies on after a cold
// bootstrap.
fn tool_output_text(output: &ToolOutput) -> String {
    match output {
        ToolOutput::Text(s) => s.clone(),
        ToolOutput::Structured(v) => v.to_string(),
        ToolOutput::Parts(parts) => serde_json::to_string(parts).unwrap_or_else(|_| "[]".into()),
        ToolOutput::Files(files) => serde_json::to_string(files).unwrap_or_else(|_| "[]".into()),
    }
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

    #[test]
    fn denormalize_kinds_map_to_wire_roles() {
        use agentkit_core::{Part, ToolCallPart, ToolOutput, ToolResultPart};

        let items = vec![
            Item::text(ItemKind::System, "rules"),
            Item::text(ItemKind::Developer, "dev rules"),
            Item::text(ItemKind::Context, "ambient"),
            Item::text(ItemKind::User, "hello"),
            Item::text(ItemKind::Notification, "background done"),
            Item::new(
                ItemKind::Assistant,
                vec![
                    Part::text("calling"),
                    Part::ToolCall(ToolCallPart::new(
                        "call-1",
                        "fs_read",
                        serde_json::json!({"path": "/x"}),
                    )),
                ],
            ),
            Item::new(
                ItemKind::Tool,
                vec![Part::ToolResult(ToolResultPart::success(
                    "call-1",
                    ToolOutput::text("ok"),
                ))],
            ),
        ];
        let out = denormalize_transcript(&items);
        assert_eq!(out.len(), 7);
        assert_eq!(out[0].role, "system");
        assert_eq!(out[1].role, "system");
        // Context maps to "user" so loadChatHistory preserves the
        // AgentCompactor summary across cold bootstraps.
        assert_eq!(out[2].role, "user");
        assert_eq!(out[2].content, "ambient");
        assert_eq!(out[3].role, "user");
        assert_eq!(out[3].content, "hello");
        assert_eq!(out[4].role, "user");
        assert_eq!(out[4].content, "background done");
        assert_eq!(out[5].role, "assistant");
        assert_eq!(out[5].content, "calling");
        assert_eq!(out[5].tool_calls.len(), 1);
        assert_eq!(out[5].tool_calls[0].id, "call-1");
        assert_eq!(out[5].tool_calls[0].name, "fs_read");
        assert_eq!(out[6].role, "tool");
        assert_eq!(out[6].tool_call_id.as_deref(), Some("call-1"));
        assert_eq!(out[6].content, "ok");
    }

    #[test]
    fn denormalize_assistant_concatenates_multiple_text_parts() {
        use agentkit_core::Part;

        let item = Item::new(
            ItemKind::Assistant,
            vec![Part::text("line one"), Part::text("line two")],
        );
        let out = denormalize_transcript(&[item]);
        assert_eq!(out.len(), 1);
        assert_eq!(out[0].content, "line one\nline two");
    }
}
