---
"server": minor
---

Fetch Slack images server-side and inject them as vision content in assistant turns. Image attachments on a triggering Slack message (up to 4 files, 8 MiB total per turn) are downloaded through the authenticated Slack API, validated by magic-byte sniffing against an image allowlist (png/jpeg/gif/webp, 10 MiB per file), and attached to the turn as `image_url` input parts with `data:` URIs. A new `platform_slack_inspect_file` tool lets the assistant look at any image referenced later in a thread: the runner strips the image payload from the tool result and re-injects it as a user message before the next model call. Image bytes live only in the live inference path — persistence continues to sanitize `data:` URIs to text placeholders at rest.
