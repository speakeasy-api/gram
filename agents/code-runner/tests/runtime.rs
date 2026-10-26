use std::{sync::Arc, time::Duration};

use gram_code_runner::{
    MAX_START_READS, State, serve_stream,
    wire::{ClientFrame, MAX_FRAME, ServerFrame, read_frame, write_frame},
};
use serde_json::{Value, json};
use tokio::{
    io::{AsyncReadExt, AsyncWriteExt, DuplexStream},
    task::JoinHandle,
};
use tokio_util::sync::CancellationToken;
use uuid::Uuid;

fn monty_binary() -> String {
    std::env::var("GRAM_MONTY_TEST_BIN")
        .unwrap_or_else(|_| format!("{}/target/monty/bin/monty", env!("CARGO_MANIFEST_DIR")))
}

async fn state(capacity: usize) -> Arc<State> {
    state_for(&monty_binary(), capacity).await
}

async fn state_for(binary: &str, capacity: usize) -> Arc<State> {
    assert!(
        std::path::Path::new(&binary).exists(),
        "build OSS Monty with mise run build:monty or set GRAM_MONTY_TEST_BIN"
    );
    State::new(
        binary,
        "local-fixture-token-with-at-least-32-bytes".into(),
        capacity,
    )
    .await
    .unwrap()
}

struct Run {
    stream: DuplexStream,
    task: JoinHandle<anyhow::Result<()>>,
}

impl Run {
    async fn new(state: Arc<State>, id: String, code: &str, wall_ms: u64) -> Self {
        let (mut client, server) = tokio::io::duplex(2 << 20);
        let task = tokio::spawn(serve_stream(server, state, CancellationToken::new()));
        write_frame(
            &mut client,
            &ClientFrame::Start {
                version: 1,
                execution_id: id,
                code: code.into(),
                wall_ms,
            },
        )
        .await
        .unwrap();
        Self {
            stream: client,
            task,
        }
    }

    async fn next(&mut self) -> ServerFrame {
        tokio::time::timeout(Duration::from_secs(8), read_frame(&mut self.stream))
            .await
            .unwrap()
            .unwrap()
            .0
    }

    async fn started(&mut self) {
        assert!(matches!(self.next().await, ServerFrame::Started { .. }));
    }

    async fn close(self) -> anyhow::Result<()> {
        drop(self.stream);
        tokio::time::timeout(Duration::from_secs(3), self.task)
            .await
            .unwrap()
            .unwrap()
    }
}

async fn run(state: Arc<State>, code: &str) -> ServerFrame {
    let mut run = Run::new(state, Uuid::new_v4().to_string(), code, 5000).await;
    run.started().await;
    let result = run.next().await;
    run.close().await.unwrap();
    result
}

#[tokio::test(flavor = "multi_thread")]
async fn fresh_executions_do_not_leak_values() {
    let state = state(1).await;
    assert!(
        matches!(run(state.clone(), "secret = 'one run only'\nprint('hello')\n{'answer': 42}").await,
        ServerFrame::Complete { value, output, output_truncated: false } if value == json!({"answer":42}) && output == "hello\n")
    );
    assert!(
        matches!(run(state, "secret").await, ServerFrame::Error { message, .. } if message.contains("NameError"))
    );
}

#[tokio::test(flavor = "multi_thread")]
async fn discovery_and_call_preserve_exact_paths_and_structured_data() {
    let mut run = Run::new(state(1).await, Uuid::new_v4().to_string(), "page = await tools.search('issues', server='example')\npath = page['items'][0]['path']\ndetails = await tools.describe(path)\nawait tools.call(path, {'id': 3})", 5000).await;
    run.started().await;
    for (method, expected, response) in [
        (
            "search",
            json!({"query":"issues","server":"example","limit":10,"cursor":null}),
            json!({"items":[{"path":"example--list.issues"}]}),
        ),
        (
            "describe",
            json!({"path":"example--list.issues"}),
            json!({"path":"example--list.issues","definition":{"inputSchema":{"type":"object"}}}),
        ),
        (
            "call",
            json!({"path":"example--list.issues","arguments":{"id":3}}),
            json!({"ok":true,"outcome":"completed","data":{},"content":[]}),
        ),
    ] {
        let ServerFrame::Callback {
            id,
            method: actual,
            arguments,
        } = run.next().await
        else {
            panic!("expected callback")
        };
        assert_eq!(actual, method);
        assert_eq!(arguments, expected, "{method} wire arguments");
        write_frame(
            &mut run.stream,
            &ClientFrame::CallbackResult {
                id,
                value: Some(response),
                error: None,
            },
        )
        .await
        .unwrap();
    }
    assert!(
        matches!(run.next().await, ServerFrame::Complete { value, .. } if value["data"] == json!({}))
    );
    run.close().await.unwrap();
}

#[tokio::test(flavor = "multi_thread")]
async fn callbacks_can_finish_out_of_order() {
    let mut run = Run::new(
        state(1).await,
        Uuid::new_v4().to_string(),
        "import asyncio\nawait asyncio.gather(tools.call('a--one', {}), tools.call('a--two', {}))",
        5000,
    )
    .await;
    run.started().await;
    let mut ids = Vec::new();
    for _ in 0..2 {
        let ServerFrame::Callback { id, .. } = run.next().await else {
            panic!("expected concurrent callback")
        };
        ids.push(id);
    }
    for (index, id) in ids.into_iter().enumerate().rev() {
        write_frame(
            &mut run.stream,
            &ClientFrame::CallbackResult {
                id,
                value: Some(json!(index)),
                error: None,
            },
        )
        .await
        .unwrap();
    }
    assert!(
        matches!(run.next().await, ServerFrame::Complete { value, .. } if value == json!([0,1]))
    );
    run.close().await.unwrap();
}

#[tokio::test(flavor = "multi_thread")]
async fn filesystem_environment_and_unknown_external_functions_are_denied() {
    let state = state(1).await;
    // Each program reaches the host boundary (an OS call or a name lookup)
    // before failing, so the specific refusal proves the host denied it.
    for (code, denial) in [
        (
            "import os\nos.getenv('HOME')",
            "RuntimeError: 'os.getenv' is not supported in this environment",
        ),
        (
            "from pathlib import Path\nPath('/etc/passwd').read_text()",
            "PermissionError: Permission denied: '/etc/passwd'",
        ),
        (
            "await arbitrary_host_function()",
            "NameError: name 'arbitrary_host_function' is not defined",
        ),
    ] {
        let result = run(state.clone(), code).await;
        assert!(
            matches!(&result, ServerFrame::Error { code, message, .. } if code == "execution_failed" && message.contains(denial)),
            "{code}: {result:?}"
        );
    }
}

#[tokio::test(flavor = "multi_thread")]
async fn a_program_may_use_every_suspension() {
    let state = state(1).await;
    // Each failed name lookup suspends to the host once.
    let program = |count: usize| {
        format!(
            "n = 0\nfor i in range({count}):\n    try:\n        undefined_name\n    except NameError:\n        n += 1\nn"
        )
    };
    assert!(
        matches!(run(state.clone(), &program(256)).await, ServerFrame::Complete { value, .. } if value == json!(256))
    );
    assert!(
        matches!(run(state, &program(257)).await, ServerFrame::Error { message, .. } if message.contains("suspension limit"))
    );
}

#[tokio::test(flavor = "multi_thread")]
async fn output_is_bounded_without_corrupting_utf8() {
    let result = run(state(1).await, "print('🌍' * 10000)\n7").await;
    let ServerFrame::Complete {
        value,
        output,
        output_truncated,
    } = result
    else {
        panic!("expected result")
    };
    assert_eq!(value, json!(7));
    assert!(output_truncated);
    assert!(output.len() <= 16 << 10);
}

#[tokio::test(flavor = "multi_thread")]
async fn failed_runs_keep_printed_output() {
    let state = state(1).await;
    for (code, failure) in [
        ("print('before traceback')\n1 / 0", "ZeroDivisionError"),
        (
            "print('before traceback')\nlen('x' * (80 * 1024 * 1024))",
            "MemoryError",
        ),
        (
            "print('before traceback')\n'x' * 300000",
            "JSON result exceeds limit",
        ),
        (
            "print('before traceback')\nfor _ in range(65):\n    await tools.call('a--write', {})",
            "attempt limit",
        ),
    ] {
        let mut run = Run::new(state.clone(), Uuid::new_v4().to_string(), code, 5000).await;
        run.started().await;
        let result = loop {
            match run.next().await {
                ServerFrame::Callback { id, .. } => write_frame(
                    &mut run.stream,
                    &ClientFrame::CallbackResult {
                        id,
                        value: Some(Value::Null),
                        error: None,
                    },
                )
                .await
                .unwrap(),
                frame => break frame,
            }
        };
        run.close().await.unwrap();
        assert!(
            matches!(&result, ServerFrame::Error { code, message, output, output_truncated: false } if code == "execution_failed" && message.contains(failure) && output == "before traceback\n"),
            "{code}: {result:?}"
        );
    }
}

#[tokio::test(flavor = "multi_thread")]
async fn failed_run_output_is_bounded_without_corrupting_utf8() {
    let result = run(state(1).await, "print('🌍' * 10000)\n1 / 0").await;
    let ServerFrame::Error {
        output,
        output_truncated,
        ..
    } = result
    else {
        panic!("expected error")
    };
    assert!(output_truncated);
    assert!(output.len() <= 16 << 10 && output.len() > (16 << 10) - 4);
    assert!(output.chars().all(|c| c == '🌍'));
}

#[tokio::test(flavor = "multi_thread")]
async fn deadline_keeps_printed_output() {
    let mut pending = Run::new(
        state(1).await,
        Uuid::new_v4().to_string(),
        "print('waiting')\nawait tools.servers()",
        2000,
    )
    .await;
    pending.started().await;
    assert!(matches!(pending.next().await, ServerFrame::Callback { .. }));
    let result = pending.next().await;
    assert!(
        matches!(&result, ServerFrame::Error { code, output, output_truncated: false, .. } if code == "deadline_exceeded" && output == "waiting\n"),
        "{result:?}"
    );
    pending.close().await.unwrap();
}

#[test]
fn error_frames_without_output_decode() {
    let ServerFrame::Error {
        code,
        output,
        output_truncated,
        ..
    } = serde_json::from_value(
        json!({"type":"error","code":"admission_refused","message":"runner draining"}),
    )
    .unwrap()
    else {
        panic!("expected error")
    };
    assert_eq!(
        (code.as_str(), output.as_str(), output_truncated),
        ("admission_refused", "", false)
    );
    // Refusals before admission keep their original encoding.
    let refusal =
        serde_json::to_value(ServerFrame::error("admission_refused", "runner draining")).unwrap();
    assert_eq!(
        refusal,
        json!({"type":"error","code":"admission_refused","message":"runner draining"})
    );
}

#[tokio::test(flavor = "multi_thread")]
async fn non_json_and_cyclic_outputs_are_rejected() {
    let state = state(1).await;
    for code in [
        "{1,2,3}",
        "float('nan')",
        "a=[]\na.append(a)\na",
        "'x' * 300000",
    ] {
        assert!(matches!(
            run(state.clone(), code).await,
            ServerFrame::Error { .. }
        ));
    }
}

#[tokio::test(flavor = "multi_thread")]
async fn memory_and_active_execution_limits_are_enforced() {
    let state = state(1).await;
    // A small result, so only the allocator can reject the allocation.
    assert!(
        matches!(run(state.clone(), "len('x' * (80 * 1024 * 1024))").await, ServerFrame::Error { message, .. } if message.contains("MemoryError"))
    );
    assert!(matches!(
        run(state.clone(), "while True:\n    pass").await,
        ServerFrame::Error { .. }
    ));
    assert!(
        matches!(run(state, "42").await, ServerFrame::Complete { value, .. } if value == json!(42))
    );
}

/// A directory holding the worker shim beside a copy of Monty, as the image
/// lays them out, removed on drop. The copy keeps Monty inside the directory
/// the shim's Landlock domain allows.
#[cfg(target_os = "linux")]
struct WorkerShim(std::path::PathBuf);

#[cfg(target_os = "linux")]
impl WorkerShim {
    fn new() -> Self {
        let dir = std::env::temp_dir().join(format!("gram-code-worker-{}", Uuid::new_v4()));
        std::fs::create_dir_all(&dir).unwrap();
        // GRAM_MONTY_TEST_BIN may already name a shim with Monty beside it.
        let mut monty = std::path::PathBuf::from(monty_binary());
        if monty.ends_with("gram-code-worker") {
            monty.set_file_name("monty");
        }
        std::fs::copy(monty, dir.join("monty")).unwrap();
        std::fs::copy(
            env!("CARGO_BIN_EXE_gram-code-worker"),
            dir.join("gram-code-worker"),
        )
        .unwrap();
        Self(dir)
    }

    fn binary(&self) -> String {
        self.0.join("gram-code-worker").to_str().unwrap().to_owned()
    }
}

#[cfg(target_os = "linux")]
impl Drop for WorkerShim {
    fn drop(&mut self) {
        let _ = std::fs::remove_dir_all(&self.0);
    }
}

// Only the Linux worker shim applies the CPU budget.
#[cfg(target_os = "linux")]
#[tokio::test(flavor = "multi_thread")]
async fn the_worker_cpu_budget_ends_a_busy_program() {
    let shim = WorkerShim::new();
    let mut busy = Run::new(
        state_for(&shim.binary(), 1).await,
        Uuid::new_v4().to_string(),
        "print('busy')\nwhile True:\n    pass",
        20_000,
    )
    .await;
    busy.started().await;
    let started = std::time::Instant::now();
    let result = busy.next().await;
    assert!(
        matches!(&result, ServerFrame::Error { code, message, .. } if code == "execution_failed" && message == "Python CPU time limit exceeded"),
        "the CPU budget, not the wall deadline, ends the program"
    );
    // The worker flushes a print while it computes in silence, so the output
    // reached the runner before the budget ended the worker.
    assert!(
        matches!(&result, ServerFrame::Error { output, output_truncated: false, .. } if output == "busy\n"),
        "{result:?}"
    );
    assert!(started.elapsed() < Duration::from_secs(10));
    busy.close().await.unwrap();
}

#[tokio::test(flavor = "multi_thread")]
async fn cancelled_and_disconnected_executions_release_capacity() {
    let state = state(1).await;
    for explicit in [true, false] {
        let mut pending = Run::new(
            state.clone(),
            Uuid::new_v4().to_string(),
            "await tools.call('a--write', {})",
            5000,
        )
        .await;
        pending.started().await;
        assert!(matches!(pending.next().await, ServerFrame::Callback { .. }));
        if explicit {
            write_frame(&mut pending.stream, &ClientFrame::Cancel)
                .await
                .unwrap();
            assert!(
                matches!(pending.next().await, ServerFrame::Error { code, .. } if code == "cancelled")
            );
        }
        assert_eq!(pending.close().await.is_ok(), explicit);
        assert!(
            matches!(run(state.clone(), "3").await, ServerFrame::Complete { value, .. } if value == json!(3))
        );
    }
}

#[tokio::test(flavor = "multi_thread")]
async fn a_full_runner_refuses_as_capacity() {
    let state = state(1).await;
    let mut pending = Run::new(
        state.clone(),
        Uuid::new_v4().to_string(),
        "await tools.call('a--write', {})",
        5000,
    )
    .await;
    pending.started().await;
    assert!(matches!(pending.next().await, ServerFrame::Callback { .. }));
    let mut full = Run::new(state.clone(), Uuid::new_v4().to_string(), "1", 5000).await;
    assert!(
        matches!(full.next().await, ServerFrame::Error { code, .. } if code == "capacity_exceeded")
    );
    full.close().await.unwrap();
    write_frame(&mut pending.stream, &ClientFrame::Cancel)
        .await
        .unwrap();
    assert!(matches!(pending.next().await, ServerFrame::Error { code, .. } if code == "cancelled"));
    pending.close().await.unwrap();
}

#[tokio::test(flavor = "multi_thread")]
async fn deadline_includes_host_wait() {
    let mut pending = Run::new(
        state(1).await,
        Uuid::new_v4().to_string(),
        "await tools.servers()",
        2000,
    )
    .await;
    pending.started().await;
    assert!(matches!(pending.next().await, ServerFrame::Callback { .. }));
    assert!(
        matches!(pending.next().await, ServerFrame::Error { code, .. } if code == "deadline_exceeded")
    );
    pending.close().await.unwrap();
}

#[tokio::test(flavor = "multi_thread")]
async fn empty_code_is_refused_before_admission() {
    let state = state(1).await;
    let id = Uuid::new_v4().to_string();
    let mut empty = Run::new(state.clone(), id.clone(), "", 5000).await;
    assert!(
        matches!(empty.next().await, ServerFrame::Error { code, message, .. } if code == "admission_refused" && message.contains("empty"))
    );
    empty.close().await.unwrap();
    // A refused attempt consumes neither capacity nor its execution ID.
    let mut retry = Run::new(state, id, "1", 5000).await;
    retry.started().await;
    assert!(matches!(retry.next().await, ServerFrame::Complete { .. }));
    retry.close().await.unwrap();
}

#[tokio::test(flavor = "multi_thread")]
async fn execution_ids_cannot_be_replayed() {
    let state = state(1).await;
    let id = Uuid::new_v4().to_string();
    let mut first = Run::new(state.clone(), id.clone(), "1", 5000).await;
    first.started().await;
    assert!(matches!(first.next().await, ServerFrame::Complete { .. }));
    first.close().await.unwrap();
    let mut second = Run::new(state, id, "2", 5000).await;
    assert!(
        matches!(second.next().await, ServerFrame::Error { code, .. } if code == "admission_refused")
    );
    second.close().await.unwrap();
}

#[tokio::test(flavor = "multi_thread")]
async fn unknown_callback_reply_is_terminal() {
    let mut pending = Run::new(
        state(1).await,
        Uuid::new_v4().to_string(),
        "await tools.servers()",
        5000,
    )
    .await;
    pending.started().await;
    let ServerFrame::Callback { id, .. } = pending.next().await else {
        panic!("expected callback")
    };
    write_frame(
        &mut pending.stream,
        &ClientFrame::CallbackResult {
            id: id + 1,
            value: Some(Value::Null),
            error: None,
        },
    )
    .await
    .unwrap();
    assert!(
        matches!(pending.next().await, ServerFrame::Error { message, .. } if message.contains("unknown or duplicate"))
    );
    pending.close().await.unwrap();
}

#[tokio::test(flavor = "multi_thread")]
async fn callback_result_with_value_and_error_is_terminal() {
    let mut pending = Run::new(
        state(1).await,
        Uuid::new_v4().to_string(),
        "await tools.servers()",
        5000,
    )
    .await;
    pending.started().await;
    let ServerFrame::Callback { id, .. } = pending.next().await else {
        panic!("expected callback")
    };
    write_frame(
        &mut pending.stream,
        &ClientFrame::CallbackResult {
            id,
            value: Some(json!({"items":[]})),
            error: Some("denied".into()),
        },
    )
    .await
    .unwrap();
    assert!(
        matches!(pending.next().await, ServerFrame::Error { message, .. } if message.contains("exactly one"))
    );
    pending.close().await.unwrap();
}

#[tokio::test(flavor = "multi_thread")]
async fn unadmitted_first_frames_buffer_within_a_bounded_permit_pool() {
    let state = state(1).await;
    // Each staller declares a full-size first frame and never sends its body.
    let mut stallers = Vec::new();
    for _ in 0..MAX_START_READS {
        let (mut client, server) = tokio::io::duplex(64);
        let task = tokio::spawn(serve_stream(
            server,
            state.clone(),
            CancellationToken::new(),
        ));
        client.write_u32(MAX_FRAME as u32).await.unwrap();
        stallers.push((client, task));
    }
    // Spawned stallers take their read permits asynchronously; until they all
    // hold one, the waiting stream could win a permit and start straight away.
    tokio::time::timeout(Duration::from_secs(5), async {
        while state.idle_start_reads() > 0 {
            tokio::time::sleep(Duration::from_millis(5)).await;
        }
    })
    .await
    .expect("every staller takes a read permit");
    let mut waiting = Run::new(state, Uuid::new_v4().to_string(), "1", 5000).await;
    assert!(
        tokio::time::timeout(
            Duration::from_millis(300),
            read_frame::<_, ServerFrame>(&mut waiting.stream)
        )
        .await
        .is_err(),
        "a stream past the permit pool must wait before reading its frame"
    );
    let (client, task) = stallers.pop().unwrap();
    drop(client);
    assert!(task.await.unwrap().is_err());
    waiting.started().await;
    assert!(
        matches!(waiting.next().await, ServerFrame::Complete { value, .. } if value == json!(1))
    );
    waiting.close().await.unwrap();
    for (client, task) in stallers {
        drop(client);
        assert!(task.await.unwrap().is_err());
    }
}

#[tokio::test(flavor = "multi_thread")]
async fn a_start_frame_that_never_arrives_is_refused_admission() {
    let (mut client, server) = tokio::io::duplex(64);
    let task = tokio::spawn(serve_stream(
        server,
        state(1).await,
        CancellationToken::new(),
    ));
    // Declare a frame but stall before its body.
    client.write_u32(64).await.unwrap();
    let (frame, _) = tokio::time::timeout(Duration::from_secs(8), read_frame(&mut client))
        .await
        .unwrap()
        .unwrap();
    assert!(
        matches!(frame, ServerFrame::Error { code, .. } if code == "admission_refused"),
        "a first-frame timeout proves the code never ran"
    );
    task.await.unwrap().unwrap();
}

#[test]
fn callback_results_round_trip_without_a_value() {
    let error = ClientFrame::CallbackResult {
        id: 1,
        value: None,
        error: Some("denied".into()),
    };
    let encoded = serde_json::to_value(&error).unwrap();
    assert!(encoded.get("value").is_none(), "{encoded}");
    let ClientFrame::CallbackResult { value, error, .. } = serde_json::from_value(encoded).unwrap()
    else {
        panic!("expected callback result")
    };
    assert_eq!((value, error.as_deref()), (None, Some("denied")));
    let null = ClientFrame::CallbackResult {
        id: 1,
        value: Some(Value::Null),
        error: None,
    };
    let ClientFrame::CallbackResult { value, .. } =
        serde_json::from_value(serde_json::to_value(&null).unwrap()).unwrap()
    else {
        panic!("expected callback result")
    };
    assert_eq!(value, Some(Value::Null), "an explicit null stays a value");
}

#[tokio::test]
async fn oversized_frame_is_rejected_before_allocation() {
    let (mut client, mut server) = tokio::io::duplex(16);
    client.write_u32((MAX_FRAME + 1) as u32).await.unwrap();
    assert!(read_frame::<_, ClientFrame>(&mut server).await.is_err());
}

#[tokio::test(flavor = "multi_thread")]
async fn callback_attempt_budget_prevents_a_sixty_fifth_dispatch() {
    let mut pending = Run::new(
        state(1).await,
        Uuid::new_v4().to_string(),
        "for _ in range(65):\n    await tools.call('a--write', {})",
        5000,
    )
    .await;
    pending.started().await;
    for _ in 0..64 {
        let ServerFrame::Callback { id, .. } = pending.next().await else {
            panic!("expected callback within budget")
        };
        write_frame(
            &mut pending.stream,
            &ClientFrame::CallbackResult {
                id,
                value: Some(Value::Null),
                error: None,
            },
        )
        .await
        .unwrap();
    }
    assert!(
        matches!(pending.next().await, ServerFrame::Error { message, .. } if message.contains("attempt limit"))
    );
    pending.close().await.unwrap();
}

#[tokio::test(flavor = "multi_thread")]
async fn callback_concurrency_is_bounded() {
    let mut pending = Run::new(
        state(1).await,
        Uuid::new_v4().to_string(),
        "import asyncio\nawait asyncio.gather(*[tools.call('a--write', {}) for _ in range(9)])",
        5000,
    )
    .await;
    pending.started().await;
    for _ in 0..8 {
        assert!(matches!(pending.next().await, ServerFrame::Callback { .. }));
    }
    assert!(
        matches!(pending.next().await, ServerFrame::Error { message, .. } if message.contains("eight callbacks"))
    );
    pending.close().await.unwrap();
}

#[tokio::test(flavor = "multi_thread")]
async fn json_integer_precision_survives_callback_round_trip() {
    let value: Value =
        serde_json::from_str("{\"large\":18446744073709551616,\"empty\":{},\"absent\":null}")
            .unwrap();
    let mut pending = Run::new(
        state(1).await,
        Uuid::new_v4().to_string(),
        "await tools.call('a--read', {})",
        5000,
    )
    .await;
    pending.started().await;
    let ServerFrame::Callback { id, .. } = pending.next().await else {
        panic!("expected callback")
    };
    write_frame(
        &mut pending.stream,
        &ClientFrame::CallbackResult {
            id,
            value: Some(value.clone()),
            error: None,
        },
    )
    .await
    .unwrap();
    assert!(
        matches!(pending.next().await, ServerFrame::Complete { value: actual, .. } if actual == value)
    );
    pending.close().await.unwrap();
}

#[tokio::test(flavor = "multi_thread")]
async fn huge_integers_are_rejected_before_parent_decimal_conversion() {
    let state = state(1).await;
    let result = tokio::time::timeout(Duration::from_secs(3), run(state.clone(), "1 << 8000000"))
        .await
        .unwrap();
    assert!(matches!(result, ServerFrame::Error { message, .. } if message.contains("4096 bits")));
    assert!(matches!(
        run(state, "42").await,
        ServerFrame::Complete { .. }
    ));
    let value: Value = serde_json::from_str(&"1".repeat(20000)).unwrap();
    assert!(gram_code_runner::values::from_json(value).is_err());
}

#[tokio::test(flavor = "multi_thread")]
async fn cancellation_during_a_partial_write_keeps_frames_intact() {
    let (mut stream, server) = tokio::io::duplex(512);
    let task = tokio::spawn(serve_stream(
        server,
        state(1).await,
        CancellationToken::new(),
    ));
    write_frame(
        &mut stream,
        &ClientFrame::Start {
            version: 1,
            execution_id: Uuid::new_v4().to_string(),
            code: "await tools.call('a--write', {'payload': 'x' * 300000})".into(),
            wall_ms: 5000,
        },
    )
    .await
    .unwrap();
    assert!(matches!(
        read_frame::<_, ServerFrame>(&mut stream).await.unwrap().0,
        ServerFrame::Started { .. }
    ));
    let size = stream.read_u32().await.unwrap() as usize;
    assert!(size > 300000);
    write_frame(&mut stream, &ClientFrame::Cancel)
        .await
        .unwrap();
    let mut body = vec![0; size];
    tokio::time::timeout(Duration::from_secs(2), stream.read_exact(&mut body))
        .await
        .unwrap()
        .unwrap();
    assert!(matches!(
        serde_json::from_slice::<ServerFrame>(&body).unwrap(),
        ServerFrame::Callback { .. }
    ));
    assert!(
        matches!(read_frame::<_, ServerFrame>(&mut stream).await.unwrap().0, ServerFrame::Error { code, .. } if code == "cancelled")
    );
    drop(stream);
    task.await.unwrap().unwrap();
}

#[tokio::test(flavor = "multi_thread")]
async fn a_preopened_stream_cannot_start_execution_after_shutdown() {
    let (mut stream, server) = tokio::io::duplex(512);
    let shutdown = CancellationToken::new();
    let task = tokio::spawn(serve_stream(server, state(1).await, shutdown.clone()));
    let start = serde_json::to_vec(&ClientFrame::Start {
        version: 1,
        execution_id: Uuid::new_v4().to_string(),
        code: "await tools.call('a--write', {})".into(),
        wall_ms: 30000,
    })
    .unwrap();
    let prefix = (start.len() as u32).to_be_bytes();
    stream.write_all(&prefix[..3]).await.unwrap();
    shutdown.cancel();
    stream.write_all(&prefix[3..]).await.unwrap();
    stream.write_all(&start).await.unwrap();
    assert!(
        matches!(read_frame::<_, ServerFrame>(&mut stream).await.unwrap().0, ServerFrame::Error { code, .. } if code == "admission_refused")
    );
    drop(stream);
    task.await.unwrap().unwrap();
}
