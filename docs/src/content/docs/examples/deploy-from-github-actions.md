---
title: Deploy from GitHub Actions
description: How to deploy your Gram toolsets using GitHub Actions
sidebar:
  order: 3
---

## Overview

You can automate Gram deployments using GitHub Actions by setting up a workflow that runs the Gram CLI in your CI/CD pipeline.

This allows you to deploy changes automatically whenever you push to your repository.

## Prerequisites

Before setting up GitHub Actions deployment, ensure you have:

- A Gram [Producer key](concepts/api-keys#producer-keys)
- A repository with your OpenAPI specification or toolset configuration

## Setup

### 1. Add API Key to Repository Secrets

1. Go to your repository's **Settings** > **Secrets and variables** > **Actions**
2. Click **New repository secret**
3. Name: `GRAM_API_KEY`
4. Value: Your Producer API key
5. Click **Add secret**

### 2. Create a deployment config

The deployment config will replace any existing Gram deployment in your project.

Config files can refer to local and remote files. Relative paths resolve from the config file's directory.

Example:

```json
// fixtures/gram.json
{
  "schema_version": "1.0.0",
  "type": "deployment",
  "sources": [
    {
      "type": "openapiv3",
      "location": "./petstore.yaml",
      "name": "My Petstore",
      "slug": "my-petstore"
    },
    {
      "type": "openapiv3",
      "location": "./symphony.json",
      "name": "Night at the Symphony",
      "slug": "symphony"
    },
    {
      "type": "openapiv3",
      "location": "https://raw.githubusercontent.com/speakeasy-api/gram/refs/heads/main/server/gen/http/openapi3.yaml",
      "name": "Gram Official API",
      "slug": "gram-official"
    }
  ]
}
```

### 3. Create GitHub Actions Workflow

Create `.github/workflows/deploy.yml` in your repository.

Force the deployment to be processed by passing a random value to `--idempotency-key`.

```yaml
# .github/workflows/deploy.yml
name: Deploy to gram (staging)

on:
  pull_request:
    branches:
      - main

jobs:
  push-deployment:
    name: Push deployment to staging project
    runs-on: ubuntu-latest
    steps:
      - name: Checkout code
        uses: actions/checkout@v4

      # Alternatively: `brew install speakeasy-api/homebrew-tap/gram`
      - name: Install CLI
        run: |
          curl -fsSL -o gram.zip https://github.com/speakeasy-api/gram/releases/download/0.2.0/gram_linux_amd64.zip
          unzip gram.zip gram
          chmod +x gram
          sudo mv gram /usr/local/bin/

      - name: Push to gram
        env:
          GRAM_API_KEY: ${{ secrets.GRAM_API_KEY }}
        run: |
          gram push --project staging --config ./fixtures/gram.json --idempotency-key="$(uuidgen)"
```

## Workflow Customization

### Deploy on Specific Branches

To deploy only from specific branches:

```yaml
on:
  push:
    branches: [main, production]
```

### Add Validation Step

Include validation before deployment:

```yaml
- name: Validate OpenAPI Spec
  run: |
    # Add your validation commands here
    npm run validate-spec ./openapi.yaml

- name: Deploy to Gram
  if: success()
  env:
    GRAM_API_KEY: ${{ secrets.GRAM_API_KEY }}
  run: gram push --project default --config ./openapi.yaml --idempotency-key="$(uuidgen)"
```

## Troubleshooting

### Authentication Errors

If you encounter authentication errors:

1. Verify your `GRAM_API_KEY` secret is set correctly
2. Ensure you're using a Producer key, not a Consumer key
