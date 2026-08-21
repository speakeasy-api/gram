---
"dashboard": patch
---

The Windows device-agent setup walkthrough now installs from the signed MSI instead of raw binaries. The install step downloads the installer from a stable link that always resolves to the current signed version, and `msiexec` registers the machine-wide service itself — no separate service-registration step. The raw-binary PowerShell script remains available as an alternative for scripted installs, an Intune Win32/LOB note covers fleet deployment, and the enroll/verify commands now invoke the CLI by its install path (the MSI does not add it to PATH).
