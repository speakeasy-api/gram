---
"dashboard": patch
---

The terse activity phrase the assistant emits before a batch of tool calls no longer flashes into the chat as prose before moving into the tool group's heading. The phrase was classified by looking at whether the next message part was a tool call, but that part does not exist until after the phrase has finished streaming, so it rendered as a paragraph and was then yanked into the group. A still-streaming phrase is now held back until the group opens, and ordinary prose is released as soon as its opening word rules out an activity phrase. Long answers streaming in after the tools finish are judged as a whole rather than by their last line, which previously blinked each new line out of the render as it arrived.
