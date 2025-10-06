---
title: Deployments
description: The "Git commits" of Gram
sidebar:
  order: 5
---

Each time you upload a new OpenAPI document or update a previously uploaded one, you create a new **deployment** in Gram.

For each deployment, Gram analyzes all related OpenAPI documents to generate or update the corresponding tool definitions.

Gram generates logs for every deployment, showing what was processed successfully and which operations or endpoints failed to convert into tools.

![Deployments logs](/img/concepts/deployments/failed-deployment-logs.png)

:::tip[Fun fact]
Each Gram project is backed by its own deployment history. Every new release tags a particular deployment with a semantic version.
:::
