use std::path::{Path, PathBuf};

use agentkit_tools_core::ToolError;

/// Persistent working directory for the assistant inside the guest VM. Backed
/// by a fixed-size loop-mounted ext4 image so disk usage is hard-capped.
pub const ASSISTANT_WORKDIR: &str = "/var/lib/gram-assistant/work";

/// Canonicalizes `path` and rejects anything that does not resolve to a
/// location inside [`ASSISTANT_WORKDIR`]. `bun_run` shells out and so bypasses
/// the agentkit `PathPolicy`; this resolves symlinks (which `PathPolicy` does
/// not) so an in-tree link pointing outside is caught before bun reads it.
pub fn canonicalize_inside_workdir(path: &Path) -> Result<PathBuf, ToolError> {
    canonicalize_inside(path, Path::new(ASSISTANT_WORKDIR))
}

fn canonicalize_inside(path: &Path, workdir: &Path) -> Result<PathBuf, ToolError> {
    let workdir = std::fs::canonicalize(workdir).map_err(|e| {
        ToolError::ExecutionFailed(format!(
            "canonicalize workdir {}: {e}",
            workdir.display()
        ))
    })?;
    let resolved = std::fs::canonicalize(path)
        .map_err(|e| ToolError::InvalidInput(format!("resolve {}: {e}", path.display())))?;
    if !resolved.starts_with(&workdir) {
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

    #[test]
    fn rejects_path_outside_workdir() {
        let workdir = tempfile::tempdir().expect("tempdir");
        let outside = tempfile::NamedTempFile::new().expect("outside file");

        let err = canonicalize_inside(outside.path(), workdir.path())
            .expect_err("path outside workdir must be rejected");
        match err {
            ToolError::InvalidInput(_) => {}
            other => panic!("expected InvalidInput, got {other:?}"),
        }
    }

    #[test]
    fn allows_path_inside_workdir() {
        let workdir = tempfile::tempdir().expect("tempdir");
        let inside = workdir.path().join("script.ts");
        std::fs::write(&inside, b"// hi").expect("write");

        let resolved =
            canonicalize_inside(&inside, workdir.path()).expect("inside path must be allowed");
        assert!(resolved.starts_with(std::fs::canonicalize(workdir.path()).expect("canonical")));
    }

    #[test]
    fn rejects_symlink_pointing_outside_workdir() {
        let workdir = tempfile::tempdir().expect("tempdir");
        let outside = tempfile::NamedTempFile::new().expect("outside");
        let link = workdir.path().join("escape");
        #[cfg(unix)]
        std::os::unix::fs::symlink(outside.path(), &link).expect("symlink");

        let err = canonicalize_inside(&link, workdir.path())
            .expect_err("symlink-to-outside must be rejected");
        match err {
            ToolError::InvalidInput(_) => {}
            other => panic!("expected InvalidInput, got {other:?}"),
        }
    }
}
