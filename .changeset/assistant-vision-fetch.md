---
"server": minor
---

Fetch Slack images server-side and inject them as vision content in assistant turns. Image attachments on a triggering Slack message (up to 4 files, 8 MiB total per turn) are downloaded concurrently through the authenticated Slack API, validated by magic-byte sniffing against an image allowlist (png/jpeg/gif/webp, 10 MiB per file), and attached to the turn as `image_url` input parts with `data:` URIs. For images referenced later in a thread, the assistant gains a generic `inspect_asset` runner tool that fetches any directly reachable image URL, validates it, and attaches it to the conversation as a user message; a new `platform_slack_get_file_url` tool bridges Slack's credentialed downloads by minting a short-lived, sealed download URL served by the Gram server's Slack file proxy. Image bytes live only in the live inference path — persistence continues to sanitize `data:` URIs to text placeholders at rest.
