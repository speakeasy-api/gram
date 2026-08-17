---
"dashboard": patch
---

Fleet configuration now reports a managed tool that is absent from the
`platforms` map as `User` rather than `Off`. The device agent's `platforms`
map is opt-out — a tool with no entry is managed at the user layer, and only
an explicit `false` disables it — but this page defaulted three of the four
tools to `Off`, so an organization whose configuration predated a tool being
added here saw it reported as unmanaged while every enrolled device was
enforcing it. Saving the page then wrote that incorrect reading back, turning
the tool off for the fleet as a side effect of editing an unrelated field.
The per-tool default is now a single constant that mirrors the agent's own
resolution, so a tool added to this list later cannot reintroduce the skew.
