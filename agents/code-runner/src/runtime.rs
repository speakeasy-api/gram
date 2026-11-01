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
    // A pool backstop just above the 25-second active-execution limit: the
    // worker's CPU budget bounds work, so a slow turn on a busy pod is not
    // killed by wall time.
    config.request_timeout = Some(Duration::from_secs(26));
    config.max_checkouts_per_worker = Some(1);
    config.auto_resume = false;
    Ok(Pool::new(config).await?)
}

/// Print output captured from one execution, bounded to 16 KiB. The stream
/// holds it outside the execution, so a failure or the wall deadline can still
/// report what the program printed first.
#[derive(Clone, Default)]
pub struct Output(Arc<Mutex<Printed>>);

#[derive(Default)]
struct Printed {
    text: String,
    truncated: bool,
}

impl Output {
    fn push(&self, text: &str) {
        let mut printed = self.0.lock().unwrap();
        // Once a print is cut, later shorter prints must not fill the leftover
        // bytes: the output stays a prefix of what the program printed.
        if printed.truncated {
            return;
        }
        let remaining = MAX_OUTPUT.saturating_sub(printed.text.len());
        let mut end = remaining.min(text.len());
        while !text.is_char_boundary(end) {
            end -= 1;
        }
        printed.text.push_str(&text[..end]);
        printed.truncated |= end < text.len();
    }

    /// Takes the captured text and whether any of it was cut.
    pub fn take(&self) -> (String, bool) {
        let mut printed = self.0.lock().unwrap();
        (std::mem::take(&mut printed.text), printed.truncated)
    }
}

pub async fn execute(
    pool: &Pool,
    code: String,
    output: &Output,
    replies: &mut mpsc::Receiver<(ClientFrame, usize)>,
    frames: &mpsc::Sender<ServerFrame>,
) -> Result<ServerFrame> {
    ensure!(code.len() <= MAX_CODE, "code exceeds 64 KiB");
    let config = ReplConfig {
        script_name: "execute.py".into(),
        limits: Some(
            ResourceLimits::default()
                // The worker shim's CPU budget bounds work; this wall-clock
                // limit is only a backstop below the execution deadline, so
                // CPU contention on a busy pod does not fail a program.
                .max_feed_duration(Duration::from_secs(25))
                .max_memory(64 << 20)
                .max_recursion_depth(128)
                .max_suspensions(256),
        ),
        persistence: Persistence::Ephemeral,
        ..Default::default()
    };
    // Dropping an unfinished checkout kills its worker, including on cancellation.
    let mut session = pool.checkout(&config).await?;
    let captured = output.clone();
    let mut print = on_print_sync(move |_, text| captured.push(text));
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
    let mut suspensions = 0;
    let mut cumulative = 0;
    loop {
        // The final Complete event is not a suspension.
        if !matches!(event, TurnEvent::Complete(_)) {
            suspensions += 1;
            ensure!(suspensions <= 256, "suspension limit exceeded");
        }
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
                let (output, output_truncated) = output.take();
                return Ok(ServerFrame::Complete {
                    value,
                    output,
                    output_truncated,
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
                    // The callbacks already issued still reach the host ahead
                    // of this error, so it records each of those calls.
                    ensure!(
                        pending.len() < MAX_PENDING,
                        "at most eight callbacks may run concurrently; the call over the limit was not dispatched"
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
                    cumulative += serde_json::to_vec(&arguments)?.len();
                    ensure!(cumulative <= MAX_CUMULATIVE, "callback budget exceeded");
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
                let result = match (value, error) {
                    (Some(_), Some(_)) => {
                        bail!("callback result requires exactly one of a value or an error")
                    }
                    (None, Some(error)) => {
                        ensure!(error.len() <= 4096, "callback error exceeds limit");
                        ResumeValue::Error(MontyException::new(ExcType::RuntimeError, Some(error)))
                    }
                    (Some(value), None) => {
                        ensure!(
                            serde_json::to_vec(&value)?.len() <= MAX_CALLBACK,
                            "callback result exceeds limit"
                        );
                        ResumeValue::Return(from_json(value)?)
                    }
                    (None, None) => bail!("callback result requires a value or an error"),
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

/// The public failure for an execution error, with the output printed before it.
pub fn public_error(error: &anyhow::Error, output: &Output) -> ServerFrame {
    let message = match error.downcast_ref::<PoolError>() {
        Some(PoolError::Runtime(exception)) => exception.to_string(),
        Some(PoolError::Crashed {
            status: Some(status),
            ..
        }) if cpu_limit_exceeded(status) => "Python CPU time limit exceeded".into(),
        Some(PoolError::Timeout { .. }) => "Python execution time limit exceeded".into(),
        Some(_) => "Python runtime unavailable; execution was not replayed".into(),
        None => error.to_string(),
    };
    let mut end = message.len().min(4096);
    while !message.is_char_boundary(end) {
        end -= 1;
    }
    let (output, output_truncated) = output.take();
    ServerFrame::Error {
        code: "execution_failed".into(),
        message: message[..end].to_owned(),
        output,
        output_truncated,
    }
}

/// Whether a worker was ended by the shim's CPU budget (`SIGXCPU`).
#[cfg(target_os = "linux")]
fn cpu_limit_exceeded(status: &std::process::ExitStatus) -> bool {
    use std::os::unix::process::ExitStatusExt;
    status.signal() == Some(libc::SIGXCPU)
}

#[cfg(not(target_os = "linux"))]
fn cpu_limit_exceeded(_: &std::process::ExitStatus) -> bool {
    false
}
