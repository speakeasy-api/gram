pub mod runtime;
pub mod values;
pub mod wire;

use std::{
    collections::HashMap,
    convert::Infallible,
    sync::{Arc, Mutex},
    time::{Duration, Instant},
};

use anyhow::{Result, ensure};
use bytes::Bytes;
use futures_util::future::poll_fn;
use http_body_util::Full;
use hyper::{
    Request, Response, StatusCode, body::Incoming, server::conn::http1, service::service_fn,
};
use hyper_util::rt::{TokioIo, TokioTimer};
use monty_pool::Pool;
use subtle::ConstantTimeEq;
use tokio::{
    io::{AsyncRead, AsyncWrite, AsyncWriteExt},
    net::TcpListener,
    sync::{OwnedSemaphorePermit, Semaphore, mpsc},
    task::JoinSet,
};
use tokio_util::{
    compat::{FuturesAsyncReadCompatExt, TokioAsyncReadCompatExt},
    sync::CancellationToken,
    task::{AbortOnDropHandle, TaskTracker},
};
use uuid::Uuid;

use wire::{ClientFrame, ServerFrame, UPGRADE, VERSION, read_frame, write_frame};

pub struct State {
    pool: Pool,
    token: String,
    capacity: Semaphore,
    seen: Mutex<HashMap<String, Instant>>,
}

impl State {
    pub async fn new(binary: &str, token: String, capacity: usize) -> Result<Arc<Self>> {
        ensure!(
            token.len() >= 32 && token.len() <= 4096,
            "runner token must contain 32 to 4096 bytes"
        );
        Ok(Arc::new(Self {
            pool: runtime::new_pool(binary, capacity).await?,
            token,
            capacity: Semaphore::new(capacity),
            seen: Mutex::new(HashMap::new()),
        }))
    }

    fn admit(&self, id: &str) -> Result<()> {
        let parsed = Uuid::parse_str(id)?;
        ensure!(
            parsed.to_string() == id,
            "execution ID must be a canonical UUID"
        );
        let now = Instant::now();
        let mut seen = self.seen.lock().unwrap();
        seen.retain(|_, at| now.duration_since(*at) < Duration::from_secs(120));
        ensure!(
            !seen.contains_key(id),
            "duplicate execution ID; executions are never replayed"
        );
        ensure!(seen.len() < 4096, "runner admission capacity exhausted");
        seen.insert(id.to_owned(), now);
        Ok(())
    }
}

pub async fn serve(
    listener: TcpListener,
    state: Arc<State>,
    shutdown: CancellationToken,
) -> Result<()> {
    let connections = Arc::new(Semaphore::new(32));
    let mut tasks = JoinSet::new();
    let upgrades = TaskTracker::new();
    let force = CancellationToken::new();
    loop {
        tokio::select! {
            _ = shutdown.cancelled() => break,
            Some(_) = tasks.join_next(), if !tasks.is_empty() => {},
            accepted = listener.accept() => {
                let (socket, _) = match accepted {
                    Ok(socket) => socket,
                    Err(error) => {
                        eprintln!("code runner accept failed: {error}");
                        tokio::select! {
                            _ = shutdown.cancelled() => break,
                            _ = tokio::time::sleep(Duration::from_millis(100)) => {},
                        }
                        continue;
                    }
                };
                let Ok(permit) = connections.clone().try_acquire_owned() else { continue };
                let permit = Arc::new(permit);
                let state = state.clone();
                let shutdown = shutdown.clone();
                let force = force.clone();
                let upgrades = upgrades.clone();
                tasks.spawn(async move {
                    let handler = service_fn(move |request| handle_http(request, state.clone(), shutdown.clone(), force.clone(), upgrades.clone(), permit.clone()));
                    let _ = http1::Builder::new()
                        .timer(TokioTimer::new())
                        .header_read_timeout(Duration::from_secs(5))
                        .max_buf_size(16 << 10)
                        .keep_alive(false)
                        .serve_connection(TokioIo::new(socket), handler)
                        .with_upgrades().await;
                });
            }
        }
    }
    tasks.abort_all();
    while tasks.join_next().await.is_some() {}
    upgrades.close();
    if tokio::time::timeout(Duration::from_secs(31), upgrades.wait())
        .await
        .is_err()
    {
        force.cancel();
        let _ = tokio::time::timeout(Duration::from_secs(1), upgrades.wait()).await;
    }
    state.pool.close().await;
    Ok(())
}

async fn handle_http(
    mut request: Request<Incoming>,
    state: Arc<State>,
    shutdown: CancellationToken,
    force: CancellationToken,
    upgrades: TaskTracker,
    permit: Arc<OwnedSemaphorePermit>,
) -> Result<Response<Full<Bytes>>, Infallible> {
    if request.method() != hyper::Method::GET {
        return Ok(response(StatusCode::METHOD_NOT_ALLOWED, "GET required"));
    }
    if request.uri().path() == "/health" {
        return Ok(response(
            StatusCode::OK,
            "{\"protocol\":1,\"runtime\":\"monty\"}",
        ));
    }
    if request.uri().path() != "/v1/connect" {
        return Ok(response(StatusCode::NOT_FOUND, "not found"));
    }
    let supplied = request
        .headers()
        .get("authorization")
        .and_then(|v| v.to_str().ok())
        .and_then(|v| v.strip_prefix("Bearer "))
        .unwrap_or("");
    if !bool::from(supplied.as_bytes().ct_eq(state.token.as_bytes())) {
        return Ok(response(StatusCode::UNAUTHORIZED, "unauthorized"));
    }
    let upgrade = request
        .headers()
        .get("upgrade")
        .and_then(|v| v.to_str().ok());
    let version = request
        .headers()
        .get("gram-code-protocol")
        .and_then(|v| v.to_str().ok());
    let connection = request
        .headers()
        .get("connection")
        .and_then(|v| v.to_str().ok())
        .unwrap_or("");
    if upgrade != Some(UPGRADE)
        || version != Some("1")
        || !connection
            .split(',')
            .any(|v| v.trim().eq_ignore_ascii_case("upgrade"))
    {
        return Ok(response(
            StatusCode::UPGRADE_REQUIRED,
            "gram-code-yamux protocol 1 required",
        ));
    }
    let upgrade = hyper::upgrade::on(&mut request);
    upgrades.spawn(async move {
        let _permit = permit;
        let Ok(Ok(socket)) = tokio::time::timeout(Duration::from_secs(5), upgrade).await else {
            return;
        };
        let mut config = yamux::Config::default();
        config.set_max_num_streams(16);
        config.set_max_connection_receive_window(Some(4 << 20));
        let mut connection =
            yamux::Connection::new(TokioIo::new(socket).compat(), config, yamux::Mode::Server);
        let mut streams = JoinSet::new();
        let mut draining = shutdown.is_cancelled();
        loop {
            if draining && streams.is_empty() { break; }
            tokio::select! {
                _ = force.cancelled() => break,
                _ = shutdown.cancelled(), if !draining => { draining = true; },
                Some(_) = streams.join_next(), if !streams.is_empty() => {},
                stream = poll_fn(|cx| connection.poll_next_inbound(cx)) => {
                    match stream {
                        Some(Ok(stream)) => {
                            let state = state.clone();
                            let draining = draining || shutdown.is_cancelled();
                            let shutdown = shutdown.clone();
                            streams.spawn(async move {
                                if draining {
                                    let mut stream = stream.compat();
                                    let _ = finish_stream(&mut stream, &ServerFrame::Error { code: "admission_refused".into(), message: "runner draining".into() }).await;
                                } else {
                                    let _ = serve_stream(stream.compat(), state, shutdown).await;
                                }
                            });
                        }
                        _ => break,
                    }
                }
            }
        }
        streams.abort_all();
        while streams.join_next().await.is_some() {}
        let _ = tokio::time::timeout(Duration::from_secs(1), poll_fn(|cx| connection.poll_close(cx))).await;
    });
    let mut accepted = response(StatusCode::SWITCHING_PROTOCOLS, "");
    accepted
        .headers_mut()
        .insert("connection", "upgrade".parse().unwrap());
    accepted
        .headers_mut()
        .insert("upgrade", UPGRADE.parse().unwrap());
    accepted
        .headers_mut()
        .insert("gram-code-protocol", "1".parse().unwrap());
    Ok(accepted)
}

fn response(status: StatusCode, body: &'static str) -> Response<Full<Bytes>> {
    let mut response = Response::new(Full::new(Bytes::from_static(body.as_bytes())));
    *response.status_mut() = status;
    response
}

// The stream is the execution ownership boundary. EOF, cancel, connection loss
// and deadline all drop the running checkout; none can resume or replay it.
pub async fn serve_stream<S: AsyncRead + AsyncWrite + Unpin + Send + 'static>(
    mut stream: S,
    state: Arc<State>,
    shutdown: CancellationToken,
) -> Result<()> {
    let (start, _) = tokio::time::timeout(
        Duration::from_secs(5),
        read_frame::<_, ClientFrame>(&mut stream),
    )
    .await??;
    let ClientFrame::Start {
        version,
        execution_id,
        code,
        wall_ms,
    } = start
    else {
        anyhow::bail!("first frame must be start")
    };
    let admission = (|| {
        ensure!(!shutdown.is_cancelled(), "runner draining");
        ensure!(version == VERSION, "unsupported protocol version");
        ensure!(
            wall_ms > 0 && wall_ms <= 30_000,
            "wall deadline must be 1 to 30000ms"
        );
        ensure!(code.len() <= runtime::MAX_CODE, "code exceeds 64 KiB");
        // Refused capacity attempts must not consume the replay guard budget.
        let permit = state.capacity.try_acquire()?;
        state.admit(&execution_id)?;
        Ok::<_, anyhow::Error>(permit)
    })();
    let _capacity = match admission {
        Ok(permit) => permit,
        Err(error) => {
            finish_stream(
                &mut stream,
                &ServerFrame::Error {
                    code: "admission_refused".into(),
                    message: error.to_string(),
                },
            )
            .await?;
            return Ok(());
        }
    };
    write_frame(&mut stream, &ServerFrame::Started { execution_id }).await?;
    let (mut reader, mut writer) = tokio::io::split(stream);
    let (frames, mut outbound) = mpsc::channel(1);
    let terminal = CancellationToken::new();
    let writer_terminal = terminal.clone();
    // This task owns framing through cancellation: a terminal frame can only
    // follow whole callback frames, never a cancelled prefix or partial payload.
    let writer_task = AbortOnDropHandle::new(tokio::spawn(async move {
        while let Some(frame) = outbound.recv().await {
            if writer_terminal.is_cancelled() && matches!(frame, ServerFrame::Callback { .. }) {
                continue;
            }
            write_frame(&mut writer, &frame).await?;
        }
        writer.shutdown().await?;
        Ok::<_, anyhow::Error>(())
    }));
    let (send, mut replies) = mpsc::channel(8);
    let cancelled = CancellationToken::new();
    let reader_cancel = cancelled.clone();
    let reader_task = AbortOnDropHandle::new(tokio::spawn(async move {
        loop {
            match read_frame::<_, ClientFrame>(&mut reader).await {
                Ok((ClientFrame::Cancel, _)) | Err(_) => break,
                Ok(frame @ (ClientFrame::CallbackResult { .. }, _)) => {
                    if send.try_send(frame).is_err() {
                        break;
                    }
                }
                _ => break,
            }
        }
        reader_cancel.cancel();
    }));
    let mut result = tokio::select! {
        biased;
        _ = cancelled.cancelled() => cancelled_error(),
        result = tokio::time::timeout(Duration::from_millis(wall_ms), runtime::execute(&state.pool, code, &mut replies, &frames)) => {
            match result {
                Ok(Ok(result)) => result,
                Ok(Err(error)) => runtime::public_error(&error),
                Err(_) => ServerFrame::Error { code: "deadline_exceeded".into(), message: "execution deadline exceeded; dispatched tool outcomes may be unknown".into() },
            }
        }
    };
    // Closing the reply channel and cancelling can make both futures ready
    // during one poll. Cancellation also wins after polling the runtime.
    if cancelled.is_cancelled() {
        result = cancelled_error();
    }
    terminal.cancel();
    drop(reader_task);
    // A disconnected peer must not hold an execution slot while refusing reads.
    tokio::time::timeout(Duration::from_secs(1), async {
        frames.send(result).await?;
        drop(frames);
        writer_task.await??;
        Ok::<_, anyhow::Error>(())
    })
    .await??;
    Ok(())
}

fn cancelled_error() -> ServerFrame {
    ServerFrame::Error {
        code: "cancelled".into(),
        message: "execution cancelled; dispatched tool outcomes may be unknown".into(),
    }
}

async fn finish_stream<W: AsyncWrite + Unpin>(stream: &mut W, result: &ServerFrame) -> Result<()> {
    tokio::time::timeout(Duration::from_secs(1), async {
        write_frame(stream, result).await?;
        stream.shutdown().await?;
        Ok::<_, anyhow::Error>(())
    })
    .await??;
    Ok(())
}
