#![cfg(target_os = "linux")]

use std::{fs, os::unix::fs::PermissionsExt, path::Path, process::Command};

/// Runs `script` as the worker shim's `monty`, targeting a sibling process.
fn signal_sibling(through_shim: bool) -> String {
    let dir = std::env::temp_dir().join(format!("gram-code-worker-{}", uuid::Uuid::new_v4()));
    fs::create_dir_all(&dir).unwrap();
    let shim = dir.join("gram-code-worker");
    fs::copy(env!("CARGO_BIN_EXE_gram-code-worker"), &shim).unwrap();
    let monty = dir.join("monty");
    fs::write(
        &monty,
        "#!/bin/sh\nif kill -0 \"$1\" 2>/dev/null; then echo signalled; else echo refused; fi\n",
    )
    .unwrap();
    fs::set_permissions(&monty, fs::Permissions::from_mode(0o755)).unwrap();
    let mut sibling = Command::new("sleep").arg("30").spawn().unwrap();
    let program: &Path = if through_shim { &shim } else { &monty };
    let output = Command::new(program)
        .arg(sibling.id().to_string())
        .output()
        .unwrap();
    sibling.kill().unwrap();
    sibling.wait().unwrap();
    fs::remove_dir_all(&dir).unwrap();
    String::from_utf8(output.stdout).unwrap().trim().to_owned()
}

#[test]
fn worker_cannot_signal_another_process() {
    assert_eq!(
        signal_sibling(false),
        "signalled",
        "the probe works unconfined"
    );
    assert_eq!(
        signal_sibling(true),
        "refused",
        "a confined worker cannot signal a sibling"
    );
}
