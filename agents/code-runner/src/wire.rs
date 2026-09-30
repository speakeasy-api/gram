use anyhow::{Result, bail, ensure};
use serde::{Deserialize, Serialize};
use serde_json::Value;
use tokio::io::{AsyncRead, AsyncReadExt, AsyncWrite, AsyncWriteExt};

pub const VERSION: u32 = 1;
pub const UPGRADE: &str = "gram-code-yamux";
pub const MAX_FRAME: usize = (1 << 20) + 4096;
pub const MAX_CALLBACK: usize = 1 << 20;
pub const MAX_CUMULATIVE: usize = 8 << 20;

#[derive(Debug, Deserialize, Serialize)]
#[serde(tag = "type", rename_all = "snake_case", deny_unknown_fields)]
pub enum ClientFrame {
    Start {
        version: u32,
        execution_id: String,
        code: String,
        wall_ms: u64,
    },
    CallbackResult {
        id: u32,
        #[serde(default)]
        value: Value,
        #[serde(default)]
        error: Option<String>,
    },
    Cancel,
}

#[derive(Debug, Deserialize, Serialize)]
#[serde(tag = "type", rename_all = "snake_case", deny_unknown_fields)]
pub enum ServerFrame {
    Started {
        execution_id: String,
    },
    Callback {
        id: u32,
        method: String,
        arguments: Value,
    },
    Complete {
        value: Value,
        output: String,
        output_truncated: bool,
    },
    Error {
        code: String,
        message: String,
    },
}

pub async fn read_frame<R, T>(reader: &mut R) -> Result<(T, usize)>
where
    R: AsyncRead + Unpin,
    T: serde::de::DeserializeOwned,
{
    let size = reader.read_u32().await? as usize;
    ensure!(size > 0 && size <= MAX_FRAME, "invalid frame size");
    let mut bytes = vec![0; size];
    reader.read_exact(&mut bytes).await?;
    Ok((serde_json::from_slice(&bytes)?, size))
}

pub async fn write_frame<W, T>(writer: &mut W, frame: &T) -> Result<()>
where
    W: AsyncWrite + Unpin,
    T: Serialize,
{
    let bytes = serde_json::to_vec(frame)?;
    if bytes.len() > MAX_FRAME {
        bail!("frame exceeds limit");
    }
    writer.write_u32(bytes.len() as u32).await?;
    writer.write_all(&bytes).await?;
    writer.flush().await?;
    Ok(())
}
