use std::time::Duration;

use futures_util::future::poll_fn;
use gram_code_runner::{
    State, serve,
    wire::{ClientFrame, ServerFrame, read_frame, write_frame},
};
use tokio::{
    io::{AsyncReadExt, AsyncWriteExt},
    net::{TcpListener, TcpStream},
};
use tokio_util::{
    compat::{FuturesAsyncReadCompatExt, TokioAsyncReadCompatExt},
    sync::CancellationToken,
    task::AbortOnDropHandle,
};
use uuid::Uuid;

const TOKEN: &str = "local-fixture-token-with-at-least-32-bytes";

async fn fixture() -> (
    std::net::SocketAddr,
    CancellationToken,
    AbortOnDropHandle<()>,
) {
    let binary = std::env::var("GRAM_MONTY_TEST_BIN")
        .unwrap_or_else(|_| format!("{}/target/monty/bin/monty", env!("CARGO_MANIFEST_DIR")));
    let state = State::new(&binary, TOKEN.into(), 2).await.unwrap();
    let listener = TcpListener::bind("127.0.0.1:0").await.unwrap();
    let addr = listener.local_addr().unwrap();
    let cancel = CancellationToken::new();
    let stopped = cancel.clone();
    let task = AbortOnDropHandle::new(tokio::spawn(async move {
        serve(listener, state, stopped).await.unwrap();
    }));
    (addr, cancel, task)
}

async fn upgrade(addr: std::net::SocketAddr, token: &str, version: u32) -> (TcpStream, String) {
    let mut socket = TcpStream::connect(addr).await.unwrap();
    socket.write_all(format!("GET /v1/connect HTTP/1.1\r\nHost: localhost\r\nConnection: upgrade\r\nUpgrade: gram-code-yamux\r\nGram-Code-Protocol: {version}\r\nAuthorization: Bearer {token}\r\n\r\n").as_bytes()).await.unwrap();
    let mut response = Vec::new();
    tokio::time::timeout(Duration::from_secs(3), async {
        while !response.ends_with(b"\r\n\r\n") {
            response.push(socket.read_u8().await.unwrap());
            assert!(response.len() < 8192);
        }
    })
    .await
    .unwrap();
    (socket, String::from_utf8(response).unwrap())
}

#[tokio::test(flavor = "multi_thread")]
async fn authenticates_and_negotiates_before_accepting_yamux() {
    let (addr, cancel, task) = fixture().await;
    let (_, response) = upgrade(addr, "invalid", 1).await;
    assert!(response.starts_with("HTTP/1.1 401"));
    let (_, response) = upgrade(addr, TOKEN, 2).await;
    assert!(response.starts_with("HTTP/1.1 426"));
    cancel.cancel();
    task.await.unwrap();
}

#[tokio::test(flavor = "multi_thread")]
async fn multiplexes_fresh_executions_on_one_authenticated_connection() {
    let (addr, cancel, task) = fixture().await;
    let (socket, response) = upgrade(addr, TOKEN, 1).await;
    assert!(response.starts_with("HTTP/1.1 101"), "{response}");
    let mut connection = yamux::Connection::new(
        socket.compat(),
        yamux::Config::default(),
        yamux::Mode::Client,
    );
    let first = poll_fn(|cx| connection.poll_new_outbound(cx))
        .await
        .unwrap();
    let second = poll_fn(|cx| connection.poll_new_outbound(cx))
        .await
        .unwrap();
    let driver = AbortOnDropHandle::new(tokio::spawn(async move {
        while let Some(Ok(_)) = poll_fn(|cx| connection.poll_next_inbound(cx)).await {}
    }));
    let mut jobs = Vec::new();
    for stream in [first, second] {
        jobs.push(tokio::spawn(async move {
            let mut stream = stream.compat();
            write_frame(&mut stream, &ClientFrame::Start { version: 1, execution_id: Uuid::new_v4().to_string(), code: "sum(range(10))".into(), wall_ms: 5000 }).await.unwrap();
            assert!(matches!(read_frame::<_, ServerFrame>(&mut stream).await.unwrap().0, ServerFrame::Started { .. }));
            assert!(matches!(read_frame::<_, ServerFrame>(&mut stream).await.unwrap().0, ServerFrame::Complete { value, .. } if value == serde_json::json!(45)));
        }));
    }
    for job in jobs {
        tokio::time::timeout(Duration::from_secs(8), job)
            .await
            .unwrap()
            .unwrap();
    }
    drop(driver);
    cancel.cancel();
    task.await.unwrap();
}

#[tokio::test(flavor = "multi_thread")]
async fn shutdown_drains_an_active_callback() {
    let (addr, cancel, task) = fixture().await;
    let (socket, response) = upgrade(addr, TOKEN, 1).await;
    assert!(response.starts_with("HTTP/1.1 101"));
    let mut connection = yamux::Connection::new(
        socket.compat(),
        yamux::Config::default(),
        yamux::Mode::Client,
    );
    let stream = poll_fn(|cx| connection.poll_new_outbound(cx))
        .await
        .unwrap();
    let driver = AbortOnDropHandle::new(tokio::spawn(async move {
        while let Some(Ok(_)) = poll_fn(|cx| connection.poll_next_inbound(cx)).await {}
    }));
    let mut stream = stream.compat();
    write_frame(
        &mut stream,
        &ClientFrame::Start {
            version: 1,
            execution_id: Uuid::new_v4().to_string(),
            code: "await tools.call('a--write', {})".into(),
            wall_ms: 5000,
        },
    )
    .await
    .unwrap();
    assert!(matches!(
        read_frame::<_, ServerFrame>(&mut stream).await.unwrap().0,
        ServerFrame::Started { .. }
    ));
    let ServerFrame::Callback { id, .. } =
        read_frame::<_, ServerFrame>(&mut stream).await.unwrap().0
    else {
        panic!("expected callback")
    };
    cancel.cancel();
    write_frame(
        &mut stream,
        &ClientFrame::CallbackResult {
            id,
            value: serde_json::json!({"committed":true}),
            error: None,
        },
    )
    .await
    .unwrap();
    assert!(
        matches!(read_frame::<_, ServerFrame>(&mut stream).await.unwrap().0, ServerFrame::Complete { value, .. } if value["committed"] == true)
    );
    drop(stream);
    tokio::time::timeout(Duration::from_secs(3), task)
        .await
        .unwrap()
        .unwrap();
    drop(driver);
}
