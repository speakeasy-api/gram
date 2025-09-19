---
title: Configure Environments
description: Configure environments inside the Gram platform.
sidebar:
  order: 3
---

An **environment** stores API keys, tokens, server URLs, and other runtime settings, keeping secrets separate from logic. This separation is crucial for managing different deployment contexts (for example, production and staging), multi-tenant APIs, or team-specific credentials.

## Why Environments Matter

APIs typically require authentication and a server URL before an AI agent can access them. Different configurations may be needed for:

- **Production and staging environments** with different API endpoints
- **Multi-tenant APIs**, where each customer has their own subdomain
- **Team-specific credentials**, where different groups use different API keys

When attaching an environment to a toolset, every API call automatically includes the correct authentication details and server configuration.

## Permission Scoping Example

For a toolset that includes GitHub tools for managing issues, create different environments with varying permission levels:

- A **`support-readonly` environment** that uses a GitHub token with read-only access, allowing agents to view issues and pull requests but not modify them
- A **`support-manager` environment** that uses a token with write permissions, enabling senior support staff's agents to close issues, add labels, and update milestones
- A **`development` environment** that uses a token with full repository access for development team agents

This ensures that each agent operates within its intended scope, preventing accidental modifications or unauthorized access.

## Creating Environments

To set up an environment:

1. Navigate to the **Environments** tab
2. Click **New Environment**
3. Provide a name for the environment (for example, `demo-environment`)
4. Configure environment variables using one of these methods:
   - **Manual setup**: Add variables individually
   - **Auto-populate**: Click **Fill for toolset** to automatically create placeholders for all required variables based on the selected toolset
5. Set values for the relevant variables and remove any that aren't needed
6. Click **Save** to create the environment

<video width="600" controls>
  <source src="/img/blog/concepts/creating-environment.mp4" type="video/mp4" />
    Your browser does not support the video tag.
</video>

## Auto-Population Feature

Gram can automatically populate environment variables based on selected toolsets. When clicking **Fill for toolset**, Gram analyzes the chosen toolset, identifies all required environment variables, and creates empty placeholders for each one. This streamlines the setup process and ensures no required variables are missed.
