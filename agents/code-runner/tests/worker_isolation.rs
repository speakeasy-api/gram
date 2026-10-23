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

    fn run(&self, confined: bool, args: &[String]) -> String {
        let child = {
            let _executables = EXECUTABLES.lock().unwrap();
            Command::new(self.program(confined))
                .args(args)
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

/// Runs a probe against a sibling worker that holds a secret. Probe scripts
/// leave stderr alone: a confined worker cannot open `/dev/null` for writing,
/// so redirecting there would make every probe report a refusal.
fn probe(script: &str, confined: bool) -> String {
    let secret = format!("in-flight-{}", uuid::Uuid::new_v4());
    let sibling = Worker::new("exec sleep 30");
    let mut target = sibling.spawn(confined, &secret);
    // Wait until the sibling has exec'd and its environment is in place.
    let environ = Path::new("/proc")
        .join(target.id().to_string())
        .join("cmdline");
    let ready = (0..100).any(|_| {
        let started = fs::read(&environ).is_ok_and(|cmdline| cmdline.starts_with(b"sleep"));
        if !started {
            std::thread::sleep(std::time::Duration::from_millis(10));
        }
        started
    });
    // A dead sibling would make every refusal below pass vacuously.
    assert!(ready, "the sibling did not exec sleep");
    let reader = Worker::new(&script.replace("SECRET", &secret));
    let result = reader.run(confined, &[target.id().to_string()]);
    assert!(
        target.try_wait().unwrap().is_none(),
        "the sibling was still running when probed"
    );
    target.kill().unwrap();
    target.wait().unwrap();
    result
}

#[test]
fn worker_cannot_signal_another_process() {
    let script = r#"if kill -0 "$1"; then echo reached; else echo refused; fi"#;
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
fn worker_cannot_make_any_cross_process_syscall() {
    // Arguments name a process or descriptor that does not exist, so an
    // unconfined call fails with ESRCH, EBADF or similar, or succeeds;
    // only the seccomp filter answers EPERM.
    const NO_PID: libc::c_long = 999_999_999;
    let calls: [(&str, libc::c_long, &[libc::c_long]); 14] = [
        ("ptrace", libc::SYS_ptrace, &[16, NO_PID, 0, 0]),
        (
            "process_vm_readv",
            libc::SYS_process_vm_readv,
            &[NO_PID, 0, 0, 0, 0, 0],
        ),
        (
            "process_vm_writev",
            libc::SYS_process_vm_writev,
            &[NO_PID, 0, 0, 0, 0, 0],
        ),
        (
            "process_madvise",
            libc::SYS_process_madvise,
            &[-1, 0, 0, 0, 0],
        ),
        ("kill", libc::SYS_kill, &[NO_PID, 0]),
        ("tkill", libc::SYS_tkill, &[NO_PID, 0]),
        ("tgkill", libc::SYS_tgkill, &[NO_PID, NO_PID, 0]),
        (
            "rt_sigqueueinfo",
            libc::SYS_rt_sigqueueinfo,
            &[NO_PID, 0, 0],
        ),
        (
            "rt_tgsigqueueinfo",
            libc::SYS_rt_tgsigqueueinfo,
            &[NO_PID, NO_PID, 0, 0],
        ),
        ("pidfd_open", libc::SYS_pidfd_open, &[NO_PID, 0]),
        (
            "pidfd_send_signal",
            libc::SYS_pidfd_send_signal,
            &[-1, 0, 0, 0],
        ),
        ("pidfd_getfd", libc::SYS_pidfd_getfd, &[-1, 0, 0]),
        ("prlimit64", libc::SYS_prlimit64, &[NO_PID, 0, 0, 0]),
        ("kcmp", libc::SYS_kcmp, &[NO_PID, NO_PID, 0, 0, 0]),
    ];
    // Perl's syscall passes numeric arguments as integers and sets $! on failure.
    let worker = Worker::new(
        r#"exec perl -e '@a = map { $_ + 0 } @ARGV; $r = syscall(shift(@a), @a); print $r == -1 ? $! + 0 : "ok"' "$@""#,
    );
    let eperm = libc::EPERM.to_string();
    for (name, number, args) in calls {
        let args: Vec<String> = std::iter::once(number)
            .chain(args.iter().copied())
            .map(|value| value.to_string())
            .collect();
        // Docker's default seccomp profile already refuses kcmp, pidfd_getfd
        // and process_madvise, which leaves nothing to compare.
        if worker.run(false, &args) == eperm {
            eprintln!("skipping {name}: refused outside the worker too");
            continue;
        }
        assert_eq!(
            worker.run(true, &args),
            eperm,
            "a confined worker cannot call {name}"
        );
    }
}

#[test]
fn worker_cannot_truncate_a_file() {
    // SAFETY: a null attribute with the version flag only queries the ABI.
    let abi = unsafe {
        libc::syscall(
            libc::SYS_landlock_create_ruleset,
            std::ptr::null::<u8>(),
            0usize,
            1u32,
        )
    };
    if abi < 3 {
        eprintln!("skipping: Landlock ABI {abi} cannot restrict truncation");
        return;
    }
    // truncate(2) by path needs only TRUNCATE, which Landlock leaves
    // unrestricted unless the ruleset handles it.
    let worker = Worker::new(
        r#"if perl -e 'truncate($ARGV[0], 0) or exit 1' "$1"; then echo reached; else echo refused; fi"#,
    );
    let victim = worker.dir.join("victim");
    for (confined, expected) in [(true, "refused"), (false, "reached")] {
        fs::write(&victim, "kept").unwrap();
        let path = victim.to_str().unwrap().to_owned();
        assert_eq!(
            worker.run(confined, &[path]),
            expected,
            "confined: {confined}"
        );
        let remaining = fs::read_to_string(&victim).unwrap();
        assert_eq!(remaining.is_empty(), !confined, "confined: {confined}");
    }
}

#[test]
fn worker_cannot_read_another_workers_memory() {
    // environ uses the same ptrace access check as mem but is readable
    // without attaching, so it proves a sibling's memory is out of reach
    // even where Yama would stop an attach.
    let script = r#"if tr '\0' '\n' < /proc/"$1"/environ | grep -q SECRET; then echo reached; else echo refused; fi"#;
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
