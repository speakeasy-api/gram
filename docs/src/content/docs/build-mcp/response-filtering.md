---
title: Response Filtering
description: Learn how to filter API responses for LLMs using the x-gram extension
sidebar:
  order: 7
  label: Response Filtering (experimental)
---

The `x-gram` extension supports response filtering to help LLMs process API responses more effectively. This is especially useful for APIs that return paginated results ore very large sets of data. Use the `responseFilterType` property to specify how responses should be filtered:

- **`jq`**: Enables [jq](https://jqlang.org/) filtering on JSON responses. This allows the LLM to extract specific data from complex API responses using jq syntax, reducing noise and focusing on relevant information.

```yaml
x-gram:
  name: get_user_profile
  description: "Get detailed user profile information"
  responseFilterType: jq
```

When `responseFilterType` is set to `jq`, the LLM can apply jq filters to the response data, such as:

- `.user | {name, email, status}` to extract only specific user fields
- `.data[] | select(.active == true)` to filter for active items
- `.results | map({id, title})` to transform arrays of objects

Omitting `responseFilterType` or setting it to `none` disables response filtering.

More response filter types will be available in future releases.