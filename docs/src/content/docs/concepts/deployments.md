---
title: Deployments
description: The "Git commits" of Gram
sidebar:
  order: 5
---

Deployments represent a snapshot of your Gram project at a specific point in
time. This snapshot includes all the "inputs" for a Gram project (uploaded
assets), and the generated "outputs", such as logs and tool definitions. The
tools in use by your MCP servers are always based on the most recent successful
deployment.

A project's deployment history can be accessed from the "Deployments" page:

![Deployments page](/img/concepts/deployments/deployments-page.png)

## Creating Deployments

A Deployment is created whenever you upload a new asset, or update an existing
one. Once the deployment process is finished, tool definitions generated from
it can be used in [Toolsets](/build-mcp/custom-toolsets) and Custom Tools. If a
Deployment includes updates to existing assets, any dependent tool definitions,
toolsets, custom tools, and MCP servers are updated automatically.

:::tip[Fun fact]
Each Gram project is backed by its own deployment history. Every new release
tags a particular deployment with a semantic version.
:::

## Troubleshooting

Information about each deployment can be accessed from the "Deployments" page of
a Gram project. Logs, assets, and tool definitions can be viewed after clicking
on a specific deployment. Logs are especially useful for debugging issues with assets
that caused a deployment to fail, or discovering why a tool was skipped.

![Deployments logs](/img/concepts/deployments/failed-deployment-logs.png)
