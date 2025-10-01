---
title: Advanced tool curation
description: Understand the principles and practices for curating tools into effective toolsets.
sidebar:
  order: 2
---

Effective API design does not automatically create effective MCP servers. When uploading an OpenAPI document with hundreds of endpoints to Gram, avoid placing all tools into a single toolset. This approach overwhelms agents and reduces their effectiveness.

Tool curation involves thinking like an agent-UX designer. It requires understanding how AI agents navigate complex workflows, make decisions, and discover what they need to accomplish goals. Traditional APIs are flat and expose every endpoint equally. Effective MCP servers use multilayered approaches with progressive disclosure to surface the right tools at the right time.

## Understanding tools and toolsets

**[Tools](/concepts/tool-definitions)** are individual callable API actions. When uploading an OpenAPI document to Gram, each operation becomes a tool definition — a single endpoint that an agent can invoke. For example, a CRM API might generate tools like `search_users`, `create_deal`, and `list_pipelines`.

**[Toolsets](/concepts/toolsets)** are curated bundles of tools organized around specific use cases or workflows. Rather than giving an agent access to every available tool, select only the ones needed to accomplish particular tasks. Think of toolsets as specialized toolkits — bring only the necessary tools for each job.

## The tool curation challenge

When connecting a large API like Slack's to Gram, over 150 tools become available for operations from sending messages to managing workspace settings. Including all tools in a single toolset risks choice paralysis for the agent.

Consider this task: "Send a summary of today's GitHub pull requests to the #engineering channel." An agent facing 200 Slack tools might waste time considering irrelevant options, like `admin.conversations.setTeams` or `admin.usergroups.addChannels` instead of focusing on the core workflow:

1. **Discover** → Find the #engineering channel
2. **Compose** → Format the message with PR data
3. **Send** → Post to the channel

The curation challenge involves identifying these workflows and building toolsets that make them easier for agents to use.

## How agents approach tasks

Agents follow a progressive discovery pattern when handling requests. For example, when asked to "Create a new deal for George's Test Deal worth $25,000," an agent doesn't immediately call a `create_deal` tool. Instead, it follows this pattern:

### Step 1: Discover prerequisites

The agent realizes it needs specific IDs and calls `search_users` to find the SDR and Solution Engineer. It cannot create a deal without knowing who is responsible for it.

### Step 2: Understand structure

Next, the agent calls `list_pipelines` to understand the available sales pipelines and find the correct stage. The deal needs to be placed in the correct workflow stage.

### Step 3: Execute action

Only after gathering all prerequisites does the agent call `create_deal` with complete, validated information.

### Key curation principles

This workflow demonstrates three key principles:

- **Discovery tools come first** → Agents can find what they need
- **Validation tools provide context** → Agents understand the system structure
- **Action tools complete workflows** → Agents can accomplish their goals

## Workflow-based design

The best toolsets are designed around agent workflows, not API structures. Here's how to shift thinking:

### Traditional API approach (avoid)

```
✗ Include every endpoint:
  - create_deal, update_deal, delete_deal
  - create_contact, update_contact, delete_contact
  - create_company, update_company, delete_company
  - create_task, update_task, delete_task
  - list_pipelines, list_stages, list_users
  - get_deal_by_id, get_contact_by_id, get_company_by_id
  - ... 50+ more tools
```

### Workflow-based approach (recommended)

```
✓ "Deal Creation" toolset:
  - search_users (find responsible parties)
  - list_pipelines (understand deal stages)
  - create_deal (complete the action)
  - get_deal_by_id (verify creation)

✓ "Deal Management" toolset:
  - search_deals (find existing deals)
  - update_deal_stage (move through pipeline)
  - add_deal_note (record interactions)
  - list_deal_activities (see history)
```

Each toolset supports a complete workflow with only essential tools. An agent working on deal creation does not need deal deletion tools cluttering its context.

## Curation strategies

### Map tool dependencies

Start by identifying tool dependencies. Some tools rely on information from other tools to function properly. Map these relationships:

```
create_deal depends on:
├── User IDs (from search_users)
├── Pipeline info (from list_pipelines)
└── Company ID (from search_companies)

update_deal_stage depends on:
├── Deal ID (from search_deals)
└── Valid stage ID (from list_pipelines)
```

Include the dependency tools in toolsets, or agents will be blocked by incomplete information.

### Group tools for specific use cases

Create different toolsets for different personas and goals.

**Sales rep toolset:**
*Focus: deal progression*

- `search_deals`, `update_deal_stage`, `add_deal_note`
- `search_contacts`, `log_call_activity`
- `list_pipelines`, `get_pipeline_metrics`

**Sales manager toolset:**
*Focus: oversight*

- `list_team_deals`, `get_pipeline_report`
- `search_deals_by_rep`, `get_deal_forecast`
- `list_pipeline_stages`, `update_deal_owner`

**Support toolset:**
*Focus: customer issues*

- `search_contacts`, `get_contact_deals`
- `create_support_ticket`, `update_ticket_status`
- `search_companies`, `get_account_health`

### Provide rich context

Use Gram's `x-gram` extension directly in OpenAPI documents to provide agents with workflow context:

```yaml
x-gram:
  name: create_deal
  description: |
    <context>
      Creates a new sales opportunity in the CRM system.
      Deals must be associated with a valid pipeline and stage.
    </context>
    <prerequisites>
      - Use search_users to find the responsible SDR and Solution Engineer
      - Use list_pipelines to identify the correct pipeline and stage IDs
      - Ensure deal amount is provided as a number, not a string
    </prerequisites>
```

Embedding context and prerequisites helps agents understand not just what a tool does, but when and how to use it effectively.

## Anti-patterns to avoid

### The everything toolset

Avoid creating a single toolset with every available tool. This overwhelms agents and defeats the purpose of curation.

### The mirror-API approach

Avoid simply copying API structure into toolsets. APIs are organized for developers; toolsets should be organized for agent workflows.

### The missing-dependencies trap

Avoid including action tools without their required discovery tools. An agent that can `create_deal` but cannot `search_users` will struggle to complete workflows.

### The inconsistent-naming problem

Avoid mixing naming conventions within a toolset. If using `search_users`, stick with `search_*` patterns rather than mixing in `list_contacts` or `find_companies`.

## Testing curation decisions

The Gram playground is ideal for validating curation decisions. For a CRM toolset, test it with various agent prompts:

- **End-to-end workflows:** "Create a new deal for Acme Corp worth $50k in the Enterprise pipeline"
- **Discovery scenarios:** "Find all deals assigned to Sarah in the Discovery stage"
- **Error recovery:** "Update the deal amount to $75k" (without providing a deal ID)

Watch how the agent navigates the toolset. Does it get stuck? Does it waste time with irrelevant tools? Does it complete workflows smoothly? Use these insights to refine the toolset.

## Implementation approach

Tool curation is both an art and a science. It requires understanding agent workflows, mapping tool dependencies, and designing progressive disclosure experiences that feel natural and efficient.

The goal is not to restrict agents but to empower them. A well-curated toolset gives agents exactly what they need to succeed without overwhelming them with unnecessary choices.

Start with agent goals, work backward to identify essential tools and dependencies, and build toolsets that support complete workflows.
