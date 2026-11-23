use anyhow::{Result, bail, ensure};
use serde::{Deserialize, Serialize};
use serde_json::Value;
use tokio::io::{AsyncRead, AsyncReadExt, AsyncWrite, AsyncWriteExt};

pub const VERSION: u32 = 2;
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
        #[serde(
            default,
            deserialize_with = "present",
            skip_serializing_if = "Option::is_none"
        )]
        value: Option<Value>,
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
        // Output printed before the failure. Omitted when empty, so refusals
        // before admission keep their encoding and a frame without it decodes.
        #[serde(default, skip_serializing_if = "String::is_empty")]
        output: String,
        #[serde(default, skip_serializing_if = "std::ops::Not::not")]
        output_truncated: bool,
    },
}

impl ServerFrame {
    /// An error that carries no printed output, such as a refusal before the
    /// program started.
    pub fn error(code: impl Into<String>, message: impl Into<String>) -> Self {
        Self::Error {
            code: code.into(),
            message: message.into(),
            output: String::new(),
            output_truncated: false,
        }
    }
}

// Distinguishes an explicit `"value": null` from a missing value.
fn present<'de, D: serde::Deserializer<'de>>(deserializer: D) -> Result<Option<Value>, D::Error> {
    Value::deserialize(deserializer).map(Some)
}

pub async fn read_frame<R, T>(reader: &mut R) -> Result<(T, usize)>
where
    R: AsyncRead + Unpin,
    T: serde::de::DeserializeOwned,
{
    let size = reader.read_u32().await? as usize;
    ensure!(size > 0 && size <= MAX_FRAME, "invalid frame size");
    // Grow with the bytes that actually arrive: a declared size alone must not
    // reserve a full frame for a peer that stalls before sending it.
    let mut bytes = Vec::with_capacity(size.min(64 << 10));
    reader.take(size as u64).read_to_end(&mut bytes).await?;
    ensure!(bytes.len() == size, "truncated frame");
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
