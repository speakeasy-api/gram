#![cfg(target_os = "linux")]

use std::{
    fs,
    os::unix::fs::PermissionsExt,
    path::{Path, PathBuf},
    process::{Child, Command},
    sync::Mutex,
};

/// Serialises writing fake executables with spawning them. A child forked
/// while another test holds a script open for writing inherits that handle
/// until it execs, so exec'ing the script meanwhile fails with `ETXTBSY`.
static EXECUTABLES: Mutex<()> = Mutex::new(());

/// A directory holding the worker shim beside a fake `monty` script, which
/// the shim confines and runs in place of the interpreter.
struct Worker {
    dir: PathBuf,
}

impl Worker {
    fn new(script: &str) -> Self {
        let _executables = EXECUTABLES.lock().unwrap();
        let dir = std::env::temp_dir().join(format!("gram-code-worker-{}", uuid::Uuid::new_v4()));
        fs::create_dir_all(&dir).unwrap();
        fs::copy(
            env!("CARGO_BIN_EXE_gram-code-worker"),
            dir.join("gram-code-worker"),
        )
        .unwrap();
        let monty = dir.join("monty");
        fs::write(&monty, format!("#!/bin/sh\n{script}\n")).unwrap();
        fs::set_permissions(&monty, fs::Permissions::from_mode(0o755)).unwrap();
        Self { dir }
    }

    fn program(&self, confined: bool) -> PathBuf {
        self.dir.join(if confined {
            "gram-code-worker"
        } else {
            "monty"
        })
    }

    fn run(&self, confined: bool, target: u32) -> String {
        let child = {
            let _executables = EXECUTABLES.lock().unwrap();
            Command::new(self.program(confined))
                .arg(target.to_string())
                .stdout(std::process::Stdio::piped())
                .spawn()
                .unwrap()
        };
        let output = child.wait_with_output().unwrap();
        String::from_utf8(output.stdout).unwrap().trim().to_owned()
    }

    fn spawn(&self, confined: bool, secret: &str) -> Child {
        let _executables = EXECUTABLES.lock().unwrap();
        Command::new(self.program(confined))
            .env("WORKER_SECRET", secret)
            .spawn()
            .unwrap()
    }
}

impl Drop for Worker {
    fn drop(&mut self) {
        let _ = fs::remove_dir_all(&self.dir);
    }
}

/// Runs a probe against a sibling worker that holds a secret.
fn probe(script: &str, confined: bool) -> String {
    let secret = format!("in-flight-{}", uuid::Uuid::new_v4());
    let sibling = Worker::new("exec sleep 30");
    let mut target = sibling.spawn(confined, &secret);
    // Wait until the sibling has exec'd and its environment is in place.
    let environ = Path::new("/proc")
        .join(target.id().to_string())
        .join("cmdline");
    for _ in 0..100 {
        if fs::read(&environ).is_ok_and(|cmdline| cmdline.starts_with(b"sleep")) {
            break;
        }
        std::thread::sleep(std::time::Duration::from_millis(10));
    }
    let reader = Worker::new(&script.replace("SECRET", &secret));
    let result = reader.run(confined, target.id());
    target.kill().unwrap();
    target.wait().unwrap();
    result
}

#[test]
fn worker_cannot_signal_another_process() {
    let script = r#"if kill -0 "$1" 2>/dev/null; then echo reached; else echo refused; fi"#;
    assert_eq!(
        probe(script, false),
        "reached",
        "the probe works unconfined"
    );
    assert_eq!(
        probe(script, true),
        "refused",
        "a confined worker cannot signal a sibling"
    );
}

#[test]
fn worker_cannot_read_another_workers_memory() {
    // environ uses the same ptrace access check as mem but is readable
    // without attaching, so it proves a sibling's memory is out of reach
    // even where Yama would stop an attach.
    let script = r#"if tr '\0' '\n' < /proc/"$1"/environ 2>/dev/null | grep -q SECRET; then echo reached; else echo refused; fi"#;
    assert_eq!(
        probe(script, false),
        "reached",
        "the probe works unconfined"
    );
    assert_eq!(
        probe(script, true),
        "refused",
        "a confined worker cannot read a sibling's memory"
    );
    let mem = r#"if dd if=/proc/"$1"/mem bs=1 count=1 skip=4194304 2>&1 | grep -q 'No such file\|Permission denied\|Operation not permitted'; then echo refused; else echo reached; fi"#;
    assert_eq!(
        probe(mem, true),
        "refused",
        "a confined worker cannot open a sibling's mem"
    );
}

#[test]
fn worker_cpu_time_is_capped() {
    use std::os::unix::process::ExitStatusExt;

    let worker = Worker::new("while :; do :; done");
    let started = std::time::Instant::now();
    let status = worker.spawn(true, "").wait().unwrap();
    assert_eq!(
        status.signal(),
        Some(libc::SIGXCPU),
        "a CPU-bound worker ends at its CPU budget: {status:?}"
    );
    assert!(
        started.elapsed() < std::time::Duration::from_secs(10),
        "the budget is seconds of CPU, not an unbounded run"
    );
}
