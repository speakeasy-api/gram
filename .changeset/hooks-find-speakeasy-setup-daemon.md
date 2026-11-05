---
---

The Speakeasy hooks now find `speakeasyd` where `speakeasy setup` installs it: `~/.local/state/speakeasy/speakeasyd` on Linux (or under `$XDG_STATE_HOME`), `~/Library/Application Support/Speakeasy/speakeasyd` on macOS, and `%LOCALAPPDATA%\Speakeasy\speakeasyd.exe` on Windows. Before, they reached a daemon installed that way only through the socket fallback.
