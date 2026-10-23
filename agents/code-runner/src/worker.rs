//! Launches one OSS Monty worker confined away from its siblings.
//!
//! Runner pods are shared by every caller in an organization, so a worker
//! that escaped the interpreter must not reach its siblings. Before exec'ing
//! the `monty` binary beside this one, the shim sets `no_new_privs`, then:
//!
//! - enters its own Landlock domain allowing only reads and execution of
//!   system libraries and the binary's directory. Without `/proc` a worker
//!   cannot open a sibling's `mem`, `environ` or `maps`, and a separate
//!   domain per worker also denies ptrace between siblings;
//! - installs a seccomp filter that fails ptrace, `process_vm_readv`/`writev`,
//!   signals, pidfd and similar cross-process calls with `EPERM`;
//! - caps the worker's CPU time. Each worker serves one execution, so this is
//!   a per-execution budget that neighbours cannot spend: a busy pod slows a
//!   program down instead of failing it, as a wall-clock limit would.
//!
//! All of these survive exec and bind every process the worker could start.
//! Landlock is skipped on a kernel without it, such as gVisor, whose sandbox
//! then bounds the worker.

#[cfg(target_os = "linux")]
fn main() -> std::io::Result<()> {
    use std::os::unix::process::CommandExt;

    let exe = std::env::current_exe()?;
    let directory = exe
        .parent()
        .ok_or_else(|| std::io::Error::other("worker shim has no directory"))?;
    let monty = directory.join("monty");
    linux::restrict(directory)?;
    Err(std::process::Command::new(monty)
        .args(std::env::args_os().skip(1))
        .exec())
}

#[cfg(not(target_os = "linux"))]
fn main() {
    eprintln!("gram-code-worker only isolates workers on Linux");
    std::process::exit(1);
}

#[cfg(target_os = "linux")]
mod linux {
    use std::{
        ffi::CString,
        io::{Error, Result},
        os::unix::ffi::OsStrExt,
        path::Path,
    };

    /// CPU seconds one execution may use before `SIGXCPU` ends it. Twelve
    /// CPU-bound executions sharing about 1.3 effective CPUs still finish
    /// within the 30-second execution deadline (12 × 2 / 1.3 ≈ 18 s).
    pub const CPU_SECONDS: libc::rlim_t = 2;

    // Classic BPF opcodes and seccomp constants from linux/filter.h and
    // linux/seccomp.h.
    const BPF_LD_W_ABS: u16 = 0x20;
    const BPF_JEQ_K: u16 = 0x15;
    #[cfg(target_arch = "x86_64")]
    const BPF_JGE_K: u16 = 0x35;
    const BPF_RET_K: u16 = 0x06;
    const SECCOMP_RET_ALLOW: u32 = 0x7fff_0000;
    const SECCOMP_RET_KILL_PROCESS: u32 = 0x8000_0000;
    const SECCOMP_RET_ERRNO: u32 = 0x0005_0000;
    // Offsets of `nr` and `arch` in struct seccomp_data.
    const SECCOMP_DATA_NR: u32 = 0;
    const SECCOMP_DATA_ARCH: u32 = 4;

    #[cfg(target_arch = "x86_64")]
    const AUDIT_ARCH: u32 = 0xc000_003e;
    /// x32 syscalls share the x86_64 audit arch with this bit set in `nr`,
    /// so they would otherwise slip past the numbers below.
    #[cfg(target_arch = "x86_64")]
    const X32_SYSCALL_BIT: u32 = 0x4000_0000;
    #[cfg(target_arch = "aarch64")]
    const AUDIT_ARCH: u32 = 0xc000_00b7;

    /// Syscalls that act on another process.
    const DENIED: [libc::c_long; 14] = [
        libc::SYS_ptrace,
        libc::SYS_process_vm_readv,
        libc::SYS_process_vm_writev,
        libc::SYS_process_madvise,
        libc::SYS_kill,
        libc::SYS_tkill,
        libc::SYS_tgkill,
        libc::SYS_rt_sigqueueinfo,
        libc::SYS_rt_tgsigqueueinfo,
        libc::SYS_pidfd_open,
        libc::SYS_pidfd_send_signal,
        libc::SYS_pidfd_getfd,
        libc::SYS_prlimit64,
        libc::SYS_kcmp,
    ];

    // Landlock ABI from linux/landlock.h.
    const LANDLOCK_CREATE_RULESET_VERSION: libc::c_uint = 1;
    const LANDLOCK_RULE_PATH_BENEATH: libc::c_uint = 1;
    const LANDLOCK_ACCESS_FS_EXECUTE: u64 = 1 << 0;
    const LANDLOCK_ACCESS_FS_READ_FILE: u64 = 1 << 2;
    const LANDLOCK_ACCESS_FS_READ_DIR: u64 = 1 << 3;
    /// Every filesystem right of Landlock ABI 1, so anything not granted
    /// below is denied.
    const LANDLOCK_HANDLED_FS: u64 = (1 << 13) - 1;
    const LANDLOCK_READ_EXECUTE: u64 =
        LANDLOCK_ACCESS_FS_EXECUTE | LANDLOCK_ACCESS_FS_READ_FILE | LANDLOCK_ACCESS_FS_READ_DIR;
    /// System paths a dynamically linked worker reads. `/proc`, `/sys`,
    /// `/tmp` and home directories are deliberately absent.
    const SYSTEM_PATHS: [&str; 6] = ["/usr", "/lib", "/lib64", "/bin", "/etc", "/dev"];

    #[repr(C)]
    struct RulesetAttr {
        handled_access_fs: u64,
    }

    #[repr(C, packed)]
    struct PathBeneathAttr {
        allowed_access: u64,
        parent_fd: i32,
    }

    /// Confines the filesystem to read and execute beneath the system paths
    /// and `binary_dir`, in a new Landlock domain. Returns false when the
    /// kernel has no Landlock.
    fn confine_filesystem(binary_dir: &Path) -> Result<bool> {
        // SAFETY: a null attribute with the version flag only queries the ABI.
        let abi = unsafe {
            libc::syscall(
                libc::SYS_landlock_create_ruleset,
                std::ptr::null::<RulesetAttr>(),
                0usize,
                LANDLOCK_CREATE_RULESET_VERSION,
            )
        };
        if abi < 1 {
            let error = Error::last_os_error();
            return match error.raw_os_error() {
                Some(libc::ENOSYS) | Some(libc::EOPNOTSUPP) => Ok(false),
                _ => Err(error),
            };
        }
        let attr = RulesetAttr {
            handled_access_fs: LANDLOCK_HANDLED_FS,
        };
        // SAFETY: attr is a valid ruleset attribute of the given size.
        let ruleset = unsafe {
            libc::syscall(
                libc::SYS_landlock_create_ruleset,
                &attr as *const RulesetAttr,
                std::mem::size_of::<RulesetAttr>(),
                0u32,
            )
        } as libc::c_int;
        if ruleset < 0 {
            return Err(Error::last_os_error());
        }
        let result = (|| {
            let paths = SYSTEM_PATHS
                .iter()
                .map(Path::new)
                .chain(std::iter::once(binary_dir));
            for path in paths {
                let encoded = CString::new(path.as_os_str().as_bytes())
                    .map_err(|_| Error::other("worker path contains NUL"))?;
                // SAFETY: encoded is a valid C string; O_PATH opens no data.
                let fd = unsafe { libc::open(encoded.as_ptr(), libc::O_PATH | libc::O_CLOEXEC) };
                if fd < 0 {
                    // A distribution without one of these paths needs no rule.
                    continue;
                }
                let rule = PathBeneathAttr {
                    allowed_access: LANDLOCK_READ_EXECUTE,
                    parent_fd: fd,
                };
                // SAFETY: rule is a valid path-beneath attribute for the ruleset.
                let added = unsafe {
                    libc::syscall(
                        libc::SYS_landlock_add_rule,
                        ruleset,
                        LANDLOCK_RULE_PATH_BENEATH,
                        &rule as *const PathBeneathAttr,
                        0u32,
                    )
                };
                // SAFETY: fd was opened above and is not used again.
                unsafe { libc::close(fd) };
                if added != 0 {
                    return Err(Error::last_os_error());
                }
            }
            // SAFETY: ruleset is a valid Landlock ruleset descriptor and
            // no_new_privs is already set.
            if unsafe { libc::syscall(libc::SYS_landlock_restrict_self, ruleset, 0u32) } != 0 {
                return Err(Error::last_os_error());
            }
            Ok(true)
        })();
        // SAFETY: ruleset was created above and is not used again.
        unsafe { libc::close(ruleset) };
        result
    }

    fn stmt(code: u16, k: u32) -> libc::sock_filter {
        libc::sock_filter {
            code,
            jt: 0,
            jf: 0,
            k,
        }
    }

    fn jump(code: u16, k: u32, jt: u8, jf: u8) -> libc::sock_filter {
        libc::sock_filter { code, jt, jf, k }
    }

    pub fn restrict(binary_dir: &Path) -> Result<()> {
        // SAFETY: prctl takes integer flags.
        if unsafe { libc::prctl(libc::PR_SET_NO_NEW_PRIVS, 1, 0, 0, 0) } != 0 {
            return Err(Error::last_os_error());
        }
        confine_filesystem(binary_dir)?;
        // The hard limit kills a worker that raised its soft limit to it; the
        // seccomp filter below already denies prlimit64 on other processes.
        let cpu = libc::rlimit {
            rlim_cur: CPU_SECONDS,
            rlim_max: CPU_SECONDS + 1,
        };
        // SAFETY: setrlimit reads the struct, which outlives the call.
        if unsafe { libc::setrlimit(libc::RLIMIT_CPU, &cpu) } != 0 {
            return Err(Error::last_os_error());
        }
        let mut program = vec![
            stmt(BPF_LD_W_ABS, SECCOMP_DATA_ARCH),
            // A foreign syscall ABI could bypass the numbers below.
            jump(BPF_JEQ_K, AUDIT_ARCH, 1, 0),
            stmt(BPF_RET_K, SECCOMP_RET_KILL_PROCESS),
            stmt(BPF_LD_W_ABS, SECCOMP_DATA_NR),
        ];
        let mut checks: Vec<(u16, u32)> = Vec::with_capacity(DENIED.len() + 1);
        #[cfg(target_arch = "x86_64")]
        checks.push((BPF_JGE_K, X32_SYSCALL_BIT));
        checks.extend(DENIED.iter().map(|syscall| (BPF_JEQ_K, *syscall as u32)));
        for (index, (code, k)) in checks.iter().enumerate() {
            let remaining = (checks.len() - index - 1) as u8;
            // Match: jump past the remaining checks and the allow to the deny.
            program.push(jump(*code, *k, remaining + 1, 0));
        }
        program.push(stmt(BPF_RET_K, SECCOMP_RET_ALLOW));
        program.push(stmt(BPF_RET_K, SECCOMP_RET_ERRNO | libc::EPERM as u32));
        let filter = libc::sock_fprog {
            len: program.len() as u16,
            filter: program.as_mut_ptr(),
        };
        // SAFETY: prctl takes integer flags; the filter outlives the call,
        // which copies it into the kernel.
        unsafe {
            if libc::prctl(
                libc::PR_SET_SECCOMP,
                libc::SECCOMP_MODE_FILTER,
                &filter as *const libc::sock_fprog,
            ) != 0
            {
                return Err(Error::last_os_error());
            }
        }
        Ok(())
    }
}
