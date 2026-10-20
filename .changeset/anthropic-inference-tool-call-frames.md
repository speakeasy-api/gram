---
"server": minor
---

Anthropic inference hooks now enforce security policies on tool call frames. With Validate tool calls on in Claude, each tool call Claude produces is scanned as a tool request with the same policies as prompt frames, and a block, warn, or quarantine match denies the frame before any of its tools run. Previously these frames were allowed unscanned, so a policy-violating tool call was only caught in the next prompt, after it had run. Tool call frames are not stored: the next prompt frame archives the same calls once. Tool-using responses now wait on the verdict within the same timeout as prompts, and a call that cannot be scanned in time is denied.
