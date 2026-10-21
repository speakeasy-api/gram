---
"server": patch
"dashboard": patch
---

Chat attachments can no longer run script on the Speakeasy API or dashboard origin. `assets.serveChatAttachment` and `assets.serveChatAttachmentSigned` now send `Content-Security-Policy: default-src 'none'; style-src 'unsafe-inline'; sandbox` and `X-Content-Type-Options: nosniff` with every attachment, and a `Content-Disposition` that displays raster images, audio, plain text, CSV, Markdown and JSON inline and downloads everything else, including HTML, SVG, XML and PDF. Fetching an attachment or embedding it as an image or audio source works as before. In chat, opening an HTML, SVG, XML or other active attachment now downloads it instead of opening it in a new tab, and replayed SVG images show the file icon instead of a preview.
