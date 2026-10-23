//! Launches one OSS Monty worker with cross-process syscalls disabled.
//!
//! Runner pods are shared by every caller in an organization, so a worker
//! that escaped the interpreter must not reach its siblings. Before exec'ing
//! the `monty` binary beside this one, the shim sets `no_new_privs` and
//! installs a seccomp filter that fails ptrace, `process_vm_readv`/`writev`,
//! signals and pidfd calls with `EPERM`. Both survive exec and bind every
//! process the worker could start.

#[cfg(target_os = "linux")]
fn main() -> std::io::Result<()> {
    use std::os::unix::process::CommandExt;

    let monty = std::env::current_exe()?
        .parent()
        .ok_or_else(|| std::io::Error::other("worker shim has no directory"))?
        .join("monty");
    linux::restrict()?;
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
    use std::io::{Error, Result};

    // Classic BPF opcodes and seccomp constants from linux/filter.h and
    // linux/seccomp.h.
    const BPF_LD_W_ABS: u16 = 0x20;
    const BPF_JEQ_K: u16 = 0x15;
    const BPF_RET_K: u16 = 0x06;
    const SECCOMP_RET_ALLOW: u32 = 0x7fff_0000;
    const SECCOMP_RET_KILL_PROCESS: u32 = 0x8000_0000;
    const SECCOMP_RET_ERRNO: u32 = 0x0005_0000;
    // Offsets of `nr` and `arch` in struct seccomp_data.
    const SECCOMP_DATA_NR: u32 = 0;
    const SECCOMP_DATA_ARCH: u32 = 4;

    #[cfg(target_arch = "x86_64")]
    const AUDIT_ARCH: u32 = 0xc000_003e;
    #[cfg(target_arch = "aarch64")]
    const AUDIT_ARCH: u32 = 0xc000_00b7;

    /// Syscalls that act on another process.
    const DENIED: [libc::c_long; 9] = [
        libc::SYS_ptrace,
        libc::SYS_process_vm_readv,
        libc::SYS_process_vm_writev,
        libc::SYS_kill,
        libc::SYS_tkill,
        libc::SYS_tgkill,
        libc::SYS_pidfd_open,
        libc::SYS_pidfd_send_signal,
        libc::SYS_pidfd_getfd,
    ];

    fn stmt(code: u16, k: u32) -> libc::sock_filter {
        libc::sock_filter {
            code,
            jt: 0,
            jf: 0,
            k,
        }
    }

    fn jump(k: u32, jt: u8, jf: u8) -> libc::sock_filter {
        libc::sock_filter {
            code: BPF_JEQ_K,
            jt,
            jf,
            k,
        }
    }

    pub fn restrict() -> Result<()> {
        let mut program = vec![
            stmt(BPF_LD_W_ABS, SECCOMP_DATA_ARCH),
            // A foreign syscall ABI could bypass the numbers below.
            jump(AUDIT_ARCH, 1, 0),
            stmt(BPF_RET_K, SECCOMP_RET_KILL_PROCESS),
            stmt(BPF_LD_W_ABS, SECCOMP_DATA_NR),
        ];
        for (index, syscall) in DENIED.iter().enumerate() {
            let remaining = (DENIED.len() - index - 1) as u8;
            // Match: jump past the remaining checks and the allow to the deny.
            program.push(jump(*syscall as u32, remaining + 1, 0));
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
            if libc::prctl(libc::PR_SET_NO_NEW_PRIVS, 1, 0, 0, 0) != 0 {
                return Err(Error::last_os_error());
            }
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
