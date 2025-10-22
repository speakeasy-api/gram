---
title: Use
description: How to use the Gram CLI for deployments
---

## About

The Gram CLI helps you manage [Deployments](concepts/deployments) without leaving the command line.

It can also integrate with CI/CD pipelines. See our [Deploy from GitHub Actions](examples/deploy-from-github-actions) guide.

## Authentication

Use `gram auth` to bootstrap your account:

```bash
gram auth
```

Then, inspect your current user information:

```bash
gram whoami
```

The first time you run `gram auth`, it will create and save a [Producer key](concepts/api-keys#producer-keys) in your Gram dashboard.

## Usage

To get started, check out your current deployment status on Gram:

```bash
gram status --json
```

Explore the full feature set:

```bash
gram --help
```
