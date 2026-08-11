import type { CompleteAttachment } from "@assistant-ui/react";

/**
 * A reopened thread replays the persisted turn text, which carries the
 * machine-facing `<attachments-context>` block the server wrote and — where an
 * image was inlined — the sanitizer's `[image omitted: …]` placeholder (image
 * bytes are deliberately not kept at rest).
 *
 * Rendering that verbatim shows the user a wall of metadata instead of the
 * file cards they attached. These helpers turn the block back into attachment
 * metadata and take both artefacts out of the displayed text, so a reloaded
 * chat reads exactly like it did when sent.
 */
// `<attachments>` is the pre-rename tag: turns sent before the block was
// folded into the `*-context` convention keep it, so both are recognised.
const ATTACHMENTS_BLOCK_RE =
  /<(attachments-context|attachments)>\n([\s\S]*?)<\/\1>\n?/;
// `- name.ext (mime/type, 1234 bytes)`, optionally followed by the asset
// reference. Older turns carry no ids, so their cards render without a preview.
const ATTACHMENT_LINE_RE =
  /^-\s+(.*?)\s+\(([^,]+),\s*(\d+)\s*bytes\)(?:\s+id=(\S+)\s+project=(\S+))?$/;
const IMAGE_PLACEHOLDER_RE = /^\[image omitted:[^\]]*\]$/gm;
// The turn also carries the file's inlined bytes and, for anything the model
// cannot read inline, a signed download URL. Both are written for the model
// only — rendered, they fill the transcript with file dumps and long tokens.
const MACHINE_BLOCK_RE =
  /<(attachment-context|attachment-downloads-context|attachment-downloads)>\n?([\s\S]*?)<\/\1>\n?/g;

export interface ReplayedAttachment {
  /** Empty for turns sent before the asset reference was recorded. */
  id: string;
  projectId: string;
  name: string;
  contentType: string;
  contentLength: number;
}

/** Parses the persisted block; returns [] when the turn carried no files. */
export function parseReplayedAttachments(text: string): ReplayedAttachment[] {
  const block = ATTACHMENTS_BLOCK_RE.exec(text);
  if (!block?.[2]) return [];

  const attachments: ReplayedAttachment[] = [];
  for (const line of block[2].split("\n")) {
    const match = ATTACHMENT_LINE_RE.exec(line.trim());
    if (!match) continue;
    attachments.push({
      name: match[1]!,
      contentType: match[2]!,
      contentLength: Number(match[3]),
      id: match[4] ?? "",
      projectId: match[5] ?? "",
    });
  }
  return attachments;
}

/** Removes the attachment block and image placeholders from displayed text. */
export function stripReplayedAttachmentText(text: string): string {
  return text
    .replace(ATTACHMENTS_BLOCK_RE, "")
    .replace(MACHINE_BLOCK_RE, "")
    .replace(IMAGE_PLACEHOLDER_RE, "")
    .replace(/\n{3,}/g, "\n\n")
    .trim();
}

/**
 * Builds the attachment metadata assistant-ui renders as cards. The file part
 * carries the asset's serve URL, which the preview resolves through an
 * authenticated fetch — the same shape the composer's upload adapter produces.
 */
export function toCompleteAttachments(
  attachments: ReplayedAttachment[],
  apiUrl: string,
): CompleteAttachment[] {
  return attachments.map((attachment, index) => {
    // No asset reference (an older turn): the card still shows the name and
    // type, it just has nothing to preview.
    const content =
      attachment.id && attachment.projectId
        ? [
            {
              type: "file" as const,
              filename: attachment.name,
              mimeType: attachment.contentType,
              data: `${apiUrl}/rpc/assets.serveChatAttachment?${new URLSearchParams(
                { id: attachment.id, project_id: attachment.projectId },
              ).toString()}`,
            },
          ]
        : [];
    return {
      id: attachment.id || `${attachment.name}-${index}`,
      type: attachment.contentType.startsWith("image/")
        ? ("image" as const)
        : ("file" as const),
      name: attachment.name,
      contentType: attachment.contentType,
      status: { type: "complete" as const },
      content,
    };
  });
}
