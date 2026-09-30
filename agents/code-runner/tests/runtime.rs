use std::{sync::Arc, time::Duration};

use gram_code_runner::{
    State, serve_stream,
    wire::{ClientFrame, MAX_FRAME, ServerFrame, read_frame, write_frame},
};
use serde_json::{Value, json};
use tokio::{
    io::{AsyncReadExt, AsyncWriteExt, DuplexStream},
    task::JoinHandle,
};
use tokio_util::sync::CancellationToken;
use uuid::Uuid;

async fn state(capacity: usize) -> Arc<State> {
    let binary = std::env::var("GRAM_MONTY_TEST_BIN")
        .unwrap_or_else(|_| format!("{}/target/monty/bin/monty", env!("CARGO_MANIFEST_DIR")));
    assert!(
        std::path::Path::new(&binary).exists(),
        "build OSS Monty with mise run build:monty or set GRAM_MONTY_TEST_BIN"
    );
    State::new(
        &binary,
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

    async fn close(self) {
        drop(self.stream);
        let _ = tokio::time::timeout(Duration::from_secs(3), self.task)
            .await
            .unwrap();
    }
}

async fn run(state: Arc<State>, code: &str) -> ServerFrame {
    let mut run = Run::new(state, Uuid::new_v4().to_string(), code, 5000).await;
    run.started().await;
    let result = run.next().await;
    run.close().await;
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
    for (method, response) in [
        ("search", json!({"items":[{"path":"example--list.issues"}]})),
        (
            "describe",
            json!({"path":"example--list.issues","definition":{"inputSchema":{"type":"object"}}}),
        ),
        (
            "call",
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
        if method != "search" {
            assert_eq!(arguments["path"], "example--list.issues");
        }
        write_frame(
            &mut run.stream,
            &ClientFrame::CallbackResult {
                id,
                value: response,
                error: None,
            },
        )
        .await
        .unwrap();
    }
    assert!(
        matches!(run.next().await, ServerFrame::Complete { value, .. } if value["data"] == json!({}))
    );
    run.close().await;
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
                value: json!(index),
                error: None,
            },
        )
        .await
        .unwrap();
    }
    assert!(
        matches!(run.next().await, ServerFrame::Complete { value, .. } if value == json!([0,1]))
    );
    run.close().await;
}

#[tokio::test(flavor = "multi_thread")]
async fn filesystem_and_unknown_external_functions_are_denied() {
    let state = state(1).await;
    for code in [
        "from pathlib import Path\nPath('/etc/passwd').read_text()",
        "await arbitrary_host_function()",
    ] {
        assert!(matches!(
            run(state.clone(), code).await,
            ServerFrame::Error { .. }
        ));
    }
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
    for code in ["'x' * (80 * 1024 * 1024)", "while True:\n    pass"] {
        assert!(matches!(
            run(state.clone(), code).await,
            ServerFrame::Error { .. }
        ));
    }
    assert!(
        matches!(run(state, "42").await, ServerFrame::Complete { value, .. } if value == json!(42))
    );
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
        pending.close().await;
        assert!(
            matches!(run(state.clone(), "3").await, ServerFrame::Complete { value, .. } if value == json!(3))
        );
    }
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
    pending.close().await;
}

#[tokio::test(flavor = "multi_thread")]
async fn execution_ids_cannot_be_replayed() {
    let state = state(1).await;
    let id = Uuid::new_v4().to_string();
    let mut first = Run::new(state.clone(), id.clone(), "1", 5000).await;
    first.started().await;
    assert!(matches!(first.next().await, ServerFrame::Complete { .. }));
    first.close().await;
    let mut second = Run::new(state, id, "2", 5000).await;
    assert!(
        matches!(second.next().await, ServerFrame::Error { code, .. } if code == "admission_refused")
    );
    second.close().await;
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
            value: Value::Null,
            error: None,
        },
    )
    .await
    .unwrap();
    assert!(
        matches!(pending.next().await, ServerFrame::Error { message, .. } if message.contains("unknown or duplicate"))
    );
    pending.close().await;
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
                value: Value::Null,
                error: None,
            },
        )
        .await
        .unwrap();
    }
    assert!(
        matches!(pending.next().await, ServerFrame::Error { message, .. } if message.contains("attempt limit"))
    );
    pending.close().await;
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
    pending.close().await;
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
            value: value.clone(),
            error: None,
        },
    )
    .await
    .unwrap();
    assert!(
        matches!(pending.next().await, ServerFrame::Complete { value: actual, .. } if actual == value)
    );
    pending.close().await;
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
