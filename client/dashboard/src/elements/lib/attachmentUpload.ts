import type {
  AttachmentAdapter,
  CompleteAttachment,
  PendingAttachment,
} from "@assistant-ui/react";

/**
 * File types `assets.uploadChatAttachment` accepts. Kept in sync with the
 * server's validateChatAttachmentContentType allow-list — anything else is
 * rejected with a 415 after the user has already picked the file.
 */
export const CHAT_ATTACHMENT_ACCEPT =
  "image/*,text/*,audio/*,application/pdf,application/json,application/yaml,.yaml,.yml,.md,.csv";

/**
 * Content types for extensions browsers commonly report as "" (Windows and
 * Linux have no registry entry for YAML, and .md varies by platform). Without
 * this the upload would fall back to application/octet-stream and the server
 * would reject a perfectly supported file — e.g. an OpenAPI document.
 */
const EXTENSION_CONTENT_TYPES: Record<string, string> = {
  yaml: "application/yaml",
  yml: "application/yaml",
  json: "application/json",
  md: "text/markdown",
  markdown: "text/markdown",
  csv: "text/csv",
  txt: "text/plain",
  log: "text/plain",
};

/** Best-effort content type for a picked file, preferring what the browser says. */
export function chatAttachmentContentType(file: File): string {
  if (file.type) return file.type;
  const ext = file.name.split(".").pop()?.toLowerCase() ?? "";
  return EXTENSION_CONTENT_TYPES[ext] ?? "application/octet-stream";
}

/** Server-side cap on a single chat attachment (assets.MaxFileSizeChatAttachment). */
export const CHAT_ATTACHMENT_MAX_BYTES = 10 * 1024 * 1024;

export interface UploadedChatAttachment {
  assetId: string;
  /** Absolute URL that serves the attachment back to an authenticated caller. */
  url: string;
}

interface UploadChatAttachmentInit {
  apiUrl: string;
  headers: Record<string, string>;
  file: File;
  signal?: AbortSignal;
}

/**
 * Uploads one file to Gram and returns the asset it was stored as. The endpoint
 * takes the raw bytes as the request body, so no multipart encoding is
 * involved. `Content-Length` is a forbidden header name in browsers — it is
 * stripped and recomputed from the body, so it is not set here.
 */
export async function uploadChatAttachment({
  apiUrl,
  headers,
  file,
  signal,
}: UploadChatAttachmentInit): Promise<UploadedChatAttachment> {
  const response = await fetch(`${apiUrl}/rpc/assets.uploadChatAttachment`, {
    method: "POST",
    headers: {
      ...headers,
      "Content-Type": chatAttachmentContentType(file),
    },
    body: file,
    signal,
  });

  if (!response.ok) {
    throw new Error(
      `Upload failed (${response.status}): ${await response.text()}`,
    );
  }

  const result = (await response.json()) as {
    asset?: { id?: string };
    url?: string;
  };
  const assetId = result.asset?.id;
  if (!assetId) {
    throw new Error("Upload response did not include an asset id");
  }
  return { assetId, url: toAbsoluteUrl(apiUrl, result.url ?? "") };
}

function toAbsoluteUrl(apiUrl: string, path: string): string {
  if (!path) return "";
  return /^https?:\/\//.test(path) ? path : `${apiUrl}${path}`;
}

/**
 * Reads the Gram asset id back out of an attachment URL minted by
 * `uploadChatAttachment`. The URL is what survives assistant-ui's attachment →
 * message-part conversion, so it is how a sent attachment is traced back to the
 * asset the assistant runtime should read.
 */
export function chatAttachmentAssetId(rawUrl: string): string | null {
  try {
    return new URL(rawUrl, window.location.origin).searchParams.get("id");
  } catch {
    return null;
  }
}

interface AttachmentAdapterInit {
  apiUrl: string;
  /** Resolves auth headers, refreshing an expired chat session first. */
  getHeaders: () => Promise<Record<string, string>>;
  accept?: string;
  maxBytes?: number;
}

/**
 * An assistant-ui attachment adapter backed by Gram's chat attachment storage.
 *
 * Files upload as soon as they are attached rather than on send, so the user
 * sees failures (too large, unsupported type) while they are still composing.
 * The uploaded asset travels on the attachment's file part as its serve URL —
 * transports read the asset id back out of it with `chatAttachmentAssetId`.
 */
export function createChatAttachmentAdapter({
  apiUrl,
  getHeaders,
  accept = CHAT_ATTACHMENT_ACCEPT,
  maxBytes = CHAT_ATTACHMENT_MAX_BYTES,
}: AttachmentAdapterInit): AttachmentAdapter {
  return {
    accept,

    async *add({ file }) {
      const contentType = chatAttachmentContentType(file);
      const base = {
        id: crypto.randomUUID(),
        type: contentType.startsWith("image/")
          ? ("image" as const)
          : ("file" as const),
        name: file.name,
        contentType,
        file,
      };

      if (file.size > maxBytes) {
        yield {
          ...base,
          content: [],
          status: {
            type: "incomplete",
            reason: "error",
            message: `${file.name} is larger than the ${Math.round(maxBytes / 1024 / 1024)}MB limit`,
          },
        };
        return;
      }

      yield {
        ...base,
        content: [],
        status: { type: "running", reason: "uploading", progress: 0 },
      };

      try {
        const uploaded = await uploadChatAttachment({
          apiUrl,
          headers: await getHeaders(),
          file,
        });
        yield {
          ...base,
          content: [
            {
              type: "file",
              filename: file.name,
              mimeType: contentType,
              data: uploaded.url,
            },
          ],
          status: { type: "requires-action", reason: "composer-send" },
        };
      } catch (error) {
        yield {
          ...base,
          content: [],
          status: {
            type: "incomplete",
            reason: "error",
            message: error instanceof Error ? error.message : "Upload failed",
          },
        };
      }
    },

    async send(attachment: PendingAttachment): Promise<CompleteAttachment> {
      if (!attachment.content?.length) {
        throw new Error(`${attachment.name} is not ready to send`);
      }
      return {
        ...attachment,
        content: attachment.content,
        status: { type: "complete" },
      };
    },

    // Uploaded assets are content-addressed and shared across chats, so
    // removing an attachment from the composer leaves the asset in place.
    async remove() {},
  };
}
