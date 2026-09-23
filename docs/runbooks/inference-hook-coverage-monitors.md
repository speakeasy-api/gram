# Anthropic inference hook scanning coverage

The Anthropic inference hook (`server/internal/anthropicinference`) turns a
delivered transcript into policy inputs before returning its allow/deny
verdict. It extracts scannable content from four content block types — `text`,
`attachment`, `tool_use`, and `tool_result` — and every other block type is
archived but contributes nothing to the scan. An image, a document, or a block
type Anthropic introduces later therefore reaches the model without any policy
seeing it.

`risk.enforcement.inference.unscanned_content_blocks` makes that gap countable.
Dashboards and monitors are managed in the Datadog UI, not as repository IaC.

## Metric contract

| Metric                                                | Type    | Meaning                                                                   |
| ----------------------------------------------------- | ------- | ------------------------------------------------------------------------- |
| `risk.enforcement.inference.unscanned_content_blocks` | counter | Content blocks a delivery evaluated but extracted no scannable text from. |

Tags:

- `gram.inference.content_block_type`: the block type. Restricted to the four
  extracted types, the documented Anthropic types the hook does not extract
  (`image`, `document`, `search_result`, `thinking`, `redacted_thinking`,
  `server_tool_use`, `web_search_tool_result`, `web_fetch_tool_result`,
  `code_execution_tool_result`, `mcp_tool_use`, `mcp_tool_result`,
  `container_upload`), and `other`.
- `gram.inference.content_block_skip_reason`: `unsupported_type` for a block
  the hook does not decode, `no_scannable_text` for a decoded block that
  carried neither text nor a tool name.

Block types arrive inside a customer transcript, so the tag is restricted to
the list above and anything else is counted as `other`. No block content,
organization, project, actor, or request identifier is a tag on this metric.

The counter covers the messages a delivery actually evaluates. History an
accepted checkpoint already covers is not rescanned and is not counted again,
so one image in a long conversation is one block, not one per later turn.

## Log line

Each recorded series also emits an info log, `inference content block not
scanned`, carrying the provider's exact block type (truncated at 64
characters), the skip reason, the per-delivery count, and the organization and
project ids. Block content is never logged. This is how a type counted as
`other` gets a name:

```text
service:gram-server "inference content block not scanned"
```

Group by `@gram.inference.content_block_type` to rank unnamed types by volume.
A type with enough traffic to be worth scanning is a follow-up ticket; add it
to `reportedBlockTypes` in `server/internal/anthropicinference/coverage.go` so
it charts by name.

## Dashboard

Add to the enforcement dashboard, scoped to `service:gram-server`:

1. A timeseries of
   `sum:risk.enforcement.inference.unscanned_content_blocks{*} by {gram.inference.content_block_type}`,
   which is the coverage gap by type and volume.
2. A toplist of the same metric filtered to
   `gram.inference.content_block_skip_reason:unsupported_type`, which is the
   set of types a policy has never seen.
3. A log stream widget for `"inference content block not scanned"`, grouped by
   `@gram.inference.content_block_type`, to resolve the `other` bucket.

Widget 1 next to the existing inference verdict volume gives the share of
delivered content the hook scanned nothing from.

## Monitors

No alert ships with this metric: unscanned blocks are an expected, tolerated
condition today. Once the dashboard has a baseline, a week-over-week increase
in `unsupported_type` volume, or the first appearance of a type not in the
list above, is worth a low-urgency notification rather than a page.

## Ownership

- **Owner:** Risk enforcement on-call
- **Service:** `gram-server`
