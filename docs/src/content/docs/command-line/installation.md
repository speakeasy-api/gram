---
title: Installation
description: Install the Gram CLI to deploy MCP servers programmatically
---

The Gram CLI is available as a beta release via `curl | bash` and `Homebrew`.

**Using `curl | bash`:**

```
curl -fsSL https://go.getgram.ai/cli.sh | bash
```

**Using Homebrew:**

```bash
brew update
brew install speakeasy-api/homebrew-tap/gram
```

After installation, verify that the Gram CLI is installed correctly by running:

```bash
gram --version
```

Then authenticate with your Gram account:

```bash
gram auth
```
