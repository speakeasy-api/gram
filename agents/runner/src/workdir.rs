use std::path::{Path, PathBuf};
use std::sync::OnceLock;

use agentkit_tools_core::ToolError;

/// Persistent working directory for the assistant inside the guest VM. Backed
/// by a fixed-size loop-mounted ext4 image so disk usage is hard-capped.
pub const ASSISTANT_WORKDIR: &str = "/var/lib/gram-assistant/work";

/// Canonicalizes `path` and rejects anything that does not resolve to a
/// location inside [`ASSISTANT_WORKDIR`]. `bun_run` shells out and so bypasses
/// the agentkit `PathPolicy`; this resolves symlinks (which `PathPolicy` does
/// not) so an in-tree link pointing outside is caught before bun reads it.
pub fn canonicalize_inside_workdir(path: &Path) -> Result<PathBuf, ToolError> {
    static CANONICAL_WORKDIR: OnceLock<PathBuf> = OnceLock::new();
    let workdir = match CANONICAL_WORKDIR.get() {
        Some(p) => p,
        None => {
            let p = std::fs::canonicalize(ASSISTANT_WORKDIR).map_err(|e| {
                ToolError::ExecutionFailed(format!(
                    "canonicalize workdir {ASSISTANT_WORKDIR}: {e}"
                ))
            })?;
            CANONICAL_WORKDIR.get_or_init(|| p)
        }
    };
    canonicalize_inside(path, workdir)
}

fn canonicalize_inside(path: &Path, canonical_workdir: &Path) -> Result<PathBuf, ToolError> {
    let resolved = std::fs::canonicalize(path)
        .map_err(|e| ToolError::InvalidInput(format!("resolve {}: {e}", path.display())))?;
    if !resolved.starts_with(canonical_workdir) {
        return Err(ToolError::InvalidInput(format!(
            "path {} resolves outside the assistant workdir",
            path.display()
        )));
    }
    Ok(resolved)
}

#[cfg(test)]
mod tests {
    use super::*;

    fn canonical_tempdir() -> (tempfile::TempDir, PathBuf) {
        let dir = tempfile::tempdir().expect("tempdir");
        let canonical = std::fs::canonicalize(dir.path()).expect("canonical");
        (dir, canonical)
    }

    #[test]
    fn rejects_path_outside_workdir() {
        let (_workdir, canonical) = canonical_tempdir();
        let outside = tempfile::NamedTempFile::new().expect("outside file");

        let err = canonicalize_inside(outside.path(), &canonical)
            .expect_err("path outside workdir must be rejected");
        match err {
            ToolError::InvalidInput(_) => {}
            other => panic!("expected InvalidInput, got {other:?}"),
        }
    }

    #[test]
    fn allows_path_inside_workdir() {
        let (workdir, canonical) = canonical_tempdir();
        let inside = workdir.path().join("script.ts");
        std::fs::write(&inside, b"// hi").expect("write");

        let resolved =
            canonicalize_inside(&inside, &canonical).expect("inside path must be allowed");
        assert!(resolved.starts_with(&canonical));
    }

    #[test]
    fn rejects_symlink_pointing_outside_workdir() {
        let (workdir, canonical) = canonical_tempdir();
        let outside = tempfile::NamedTempFile::new().expect("outside");
        let link = workdir.path().join("escape");
        #[cfg(unix)]
        std::os::unix::fs::symlink(outside.path(), &link).expect("symlink");

        let err = canonicalize_inside(&link, &canonical)
            .expect_err("symlink-to-outside must be rejected");
        match err {
            ToolError::InvalidInput(_) => {}
            other => panic!("expected InvalidInput, got {other:?}"),
        }
    }
}
