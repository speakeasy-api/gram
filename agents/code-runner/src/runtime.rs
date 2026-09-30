use std::{
    collections::HashSet,
    sync::{Arc, Mutex},
    time::Duration,
};

use anyhow::{Result, bail, ensure};
use monty_pool::{
    Persistence, Pool, PoolConfig, PoolError, ReplConfig, ResumeValue, TurnEvent, on_print_sync,
};
use monty_types::{ExcType, MontyException, MontyObject, ResourceLimits};
use tokio::sync::mpsc;

use crate::{
    values::{from_json, to_json},
    wire::{ClientFrame, MAX_CALLBACK, MAX_CUMULATIVE, ServerFrame},
};

pub const MAX_CODE: usize = 64 << 10;
pub const MAX_CALLS: usize = 64;
pub const MAX_PENDING: usize = 8;
const MAX_OUTPUT: usize = 16 << 10;
const MAX_FINAL: usize = 256 << 10;
const PRELUDE: &str = include_str!("prelude.py");

pub async fn new_pool(binary: &str, capacity: usize) -> Result<Pool> {
    ensure!(
        (1..=16).contains(&capacity),
        "capacity must be between 1 and 16"
    );
    let mut config = PoolConfig::subprocess(binary);
    config.min_processes = 1;
    config.max_processes = capacity;
    config.checkout_timeout = Some(Duration::from_secs(2));
    config.request_timeout = Some(Duration::from_secs(4));
    config.max_checkouts_per_worker = Some(1);
    config.auto_resume = false;
    Ok(Pool::new(config).await?)
}

#[derive(Default)]
struct Output {
    text: String,
    truncated: bool,
}

pub async fn execute(
    pool: &Pool,
    code: String,
    replies: &mut mpsc::Receiver<(ClientFrame, usize)>,
    frames: &mpsc::Sender<ServerFrame>,
) -> Result<ServerFrame> {
    ensure!(code.len() <= MAX_CODE, "code exceeds 64 KiB");
    let config = ReplConfig {
        script_name: "execute.py".into(),
        limits: Some(
            ResourceLimits::default()
                .max_feed_duration(Duration::from_secs(2))
                .max_memory(64 << 20)
                .max_recursion_depth(128)
                .max_suspensions(256),
        ),
        persistence: Persistence::Ephemeral,
        ..Default::default()
    };
    // Dropping an unfinished checkout kills its worker, including on cancellation.
    let mut session = pool.checkout(&config).await?;
    let output = Arc::new(Mutex::new(Output::default()));
    let captured = output.clone();
    let mut print = on_print_sync(move |_, text| {
        let mut output = captured.lock().unwrap();
        let remaining = MAX_OUTPUT.saturating_sub(output.text.len());
        let mut end = remaining.min(text.len());
        while !text.is_char_boundary(end) {
            end -= 1;
        }
        output.text.push_str(&text[..end]);
        output.truncated |= end < text.len();
    });
    ensure!(
        matches!(
            session
                .feed(PRELUDE, vec![], vec![], true, &mut print)
                .await?,
            TurnEvent::Complete(_)
        ),
        "invalid tools prelude"
    );
    let mut event = session.feed(code, vec![], vec![], true, &mut print).await?;
    let mut pending = HashSet::new();
    let mut issued = HashSet::new();
    let mut callbacks = 0;
    let mut turns = 0;
    let mut cumulative = 0;
    loop {
        turns += 1;
        ensure!(turns <= 256, "suspension limit exceeded");
        event = match event {
            TurnEvent::Complete(value) => {
                ensure!(
                    pending.is_empty(),
                    "execution completed with unresolved tool calls"
                );
                let value = to_json(value.as_ref(), MAX_FINAL)?;
                // Cleanup cannot replace a computed result with a reset error.
                // Kill the process as well as discarding its interpreter state.
                drop(session);
                let mut output = output.lock().unwrap();
                return Ok(ServerFrame::Complete {
                    value,
                    output: std::mem::take(&mut output.text),
                    output_truncated: output.truncated,
                });
            }
            TurnEvent::FunctionCall {
                function_name,
                args,
                call_id,
                object_id,
                ..
            } => {
                if function_name != "_gram_host" || object_id.is_some() {
                    session.resume(ResumeValue::NotFound, &mut print).await?
                } else {
                    callbacks += 1;
                    ensure!(callbacks <= MAX_CALLS, "callback attempt limit exceeded");
                    ensure!(
                        pending.len() < MAX_PENDING,
                        "at most eight callbacks may run concurrently"
                    );
                    ensure!(issued.insert(call_id), "duplicate runtime callback ID");
                    ensure!(
                        args.args().len() == 2 && args.kwargs().len() == 0,
                        "invalid host callback arguments"
                    );
                    let method = args
                        .arg(0)
                        .and_then(|v| v.as_str())
                        .ok_or_else(|| anyhow::anyhow!("invalid host method"))?
                        .to_owned();
                    ensure!(
                        matches!(method.as_str(), "search" | "describe" | "call" | "servers"),
                        "unknown host method"
                    );
                    let arguments = to_json(args.arg(1).unwrap(), MAX_CALLBACK)?;
                    ensure!(
                        arguments.is_object(),
                        "callback arguments must be an object"
                    );
                    pending.insert(call_id);
                    frames
                        .send(ServerFrame::Callback {
                            id: call_id,
                            method,
                            arguments,
                        })
                        .await?;
                    session.resume(ResumeValue::Future, &mut print).await?
                }
            }
            TurnEvent::ResolveFutures {
                pending_call_ids, ..
            } => {
                ensure!(
                    pending_call_ids.iter().all(|id| pending.contains(id)),
                    "unknown runtime future"
                );
                let (reply, size) = replies
                    .recv()
                    .await
                    .ok_or_else(|| anyhow::anyhow!("host disconnected"))?;
                cumulative += size;
                ensure!(
                    size <= MAX_CALLBACK + 4096 && cumulative <= MAX_CUMULATIVE,
                    "callback result budget exceeded"
                );
                let ClientFrame::CallbackResult { id, value, error } = reply else {
                    bail!("unexpected frame during execution")
                };
                ensure!(
                    pending.remove(&id),
                    "unknown or duplicate callback result ID"
                );
                let result = if let Some(error) = error {
                    ensure!(error.len() <= 4096, "callback error exceeds limit");
                    ResumeValue::Error(MontyException::new(ExcType::RuntimeError, Some(error)))
                } else {
                    ensure!(
                        serde_json::to_vec(&value)?.len() <= MAX_CALLBACK,
                        "callback result exceeds limit"
                    );
                    ResumeValue::Return(from_json(value)?)
                };
                session
                    .resume_futures(vec![(id, result)], &mut print)
                    .await?
            }
            TurnEvent::OsCall { .. } => session.resume(ResumeValue::NotHandled, &mut print).await?,
            TurnEvent::NameLookup { .. } => {
                session
                    .resume_name_lookup(None::<MontyObject>, &mut print)
                    .await?
            }
        };
    }
}

pub fn public_error(error: &anyhow::Error) -> ServerFrame {
    let message = match error.downcast_ref::<PoolError>() {
        Some(PoolError::Runtime(exception)) => exception.to_string(),
        Some(PoolError::Timeout { .. }) => "Python execution time limit exceeded".into(),
        Some(_) => "Python runtime unavailable; execution was not replayed".into(),
        None => error.to_string(),
    };
    let mut end = message.len().min(4096);
    while !message.is_char_boundary(end) {
        end -= 1;
    }
    ServerFrame::Error {
        code: "execution_failed".into(),
        message: message[..end].to_owned(),
    }
}
