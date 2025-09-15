# @gram/server

## 0.1.5

### Patch Changes

- 635a012: Avoid a nil pointer dereference on API-based requests to create deployments.
- 94c0009: Clear tools from previous deployment attempts when retrying deployments
- c270b33: fix implement hardcoded limit for tool calls until polar max can be trusted
- 7b65af4: Fill in project id and openapi document id when creating http security records during deployment processing
- bb6393f: handle subscription downgrade in polar webhook
- 0158ef8: Fall back to free tier for orgs with canceled subscriptions
- f150c54: correct openrouter threshold for pro tier
- fbcbeee: start checking tool call usage in free tier

## 0.1.4

### Patch Changes

- ef1eff3: fix a bug updating account type from polar

## 0.1.3

### Patch Changes

- a160361: update openrouter playground credits on account upgrade/downgrade

## 0.1.2

### Patch Changes

- dd769ee: update proxy parsing to better handle large numbers in params

## 0.1.1

### Patch Changes

- acf6726: Expose the kind of prompt templates, and do not count higher order tools as prompts in the dashboard.

## 0.1.0

### Minor Changes

- d4dbddd: Manage versioning and changelog with [changesets](https://github.com/changesets/changesets)
