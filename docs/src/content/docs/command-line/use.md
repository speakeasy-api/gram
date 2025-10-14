---
title: Use
description: How to use the Gram CLI for deployments
---

## About

The Gram CLI helps you manage [Deployments](concepts/deployments) without leaving the command line.

It can also integrate with CI/CD pipelines. See our [Deploy from GitHub Actions](examples/deploy-from-github-actions) guide.

## Authentication

Create and save a [Producer key](concepts/api-keys#producer-keys) from your Gram dashboard.

Expose this key as an environment variable called `$GRAM_API_KEY`.

```bash
export GRAM_API_KEY="$YOUR_PRODUCER_KEY"
```

## Usage

To get started, check out your current deployment status on Gram:

```bash
gram status --json
```

Explore the full feature set:

```bash
gram --help
```
