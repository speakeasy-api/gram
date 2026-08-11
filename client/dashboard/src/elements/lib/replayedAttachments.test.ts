import { describe, expect, it } from "vitest";

import {
  parseReplayedAttachments,
  stripReplayedAttachmentText,
  toCompleteAttachments,
} from "@/elements/lib/replayedAttachments";

const REPLAYED_TURN = [
  "<message-context>",
  "EventID: event-1",
  "</message-context>",
  "",
  "<attachments-context>",
  "- skill-issue.gif (image/gif, 33347 bytes) id=asset-1 project=project-1",
  "- openapi.yaml (application/yaml, 167 bytes) id=asset-2 project=project-1",
  "</attachments-context>",
  "",
  "whats this image",
  "[image omitted: image/gif, ~33348 bytes]",
].join("\n");

describe("parseReplayedAttachments", () => {
  it("rebuilds every attachment the turn carried", () => {
    expect(parseReplayedAttachments(REPLAYED_TURN)).toEqual([
      {
        name: "skill-issue.gif",
        contentType: "image/gif",
        contentLength: 33347,
        id: "asset-1",
        projectId: "project-1",
      },
      {
        name: "openapi.yaml",
        contentType: "application/yaml",
        contentLength: 167,
        id: "asset-2",
        projectId: "project-1",
      },
    ]);
  });

  // Turns sent before the block carried asset ids must still clean up.
  it("parses the pre-rename block without asset references", () => {
    const legacy = [
      "<attachments>",
      "- skill-issue.gif (image/gif, 33347 bytes)",
      "</attachments>",
      "",
      "whats this image",
    ].join("\n");

    expect(parseReplayedAttachments(legacy)).toEqual([
      {
        name: "skill-issue.gif",
        contentType: "image/gif",
        contentLength: 33347,
        id: "",
        projectId: "",
      },
    ]);
    expect(stripReplayedAttachmentText(legacy)).toBe("whats this image");
    expect(
      toCompleteAttachments(parseReplayedAttachments(legacy), "https://x")[0]
        ?.content,
    ).toEqual([]);
  });

  it("returns nothing for a turn without attachments", () => {
    expect(parseReplayedAttachments("just a message")).toEqual([]);
  });
});

describe("stripReplayedAttachmentText", () => {
  // The user typed "whats this image" — everything else is machine text that
  // would otherwise reappear as their own words when the thread is reopened.
  it("leaves only what the user wrote", () => {
    const stripped = stripReplayedAttachmentText(REPLAYED_TURN);
    expect(stripped).toContain("whats this image");
    expect(stripped).not.toContain("attachments-context");
    expect(stripped).not.toContain("image omitted");
    expect(stripped).not.toContain("skill-issue.gif");
  });

  // The inlined file bytes and the signed download URL are model-facing; the
  // reader must never see a file dump or a JWT in the transcript.
  it("removes the inlined content and download blocks", () => {
    const turn = [
      "<attachment-context>",
      "name: openapi.yaml",
      "type: application/yaml",
      "",
      "openapi: 3.1.0",
      "</attachment-context>",
      "<attachment-downloads-context>",
      "- report.pdf (application/pdf): https://gram.example/rpc/assets.serveChatAttachmentSigned?token=eyJhbGciOi",
      "</attachment-downloads-context>",
      "read this",
    ].join("\n");

    const stripped = stripReplayedAttachmentText(turn);
    expect(stripped).toBe("read this");
    expect(stripped).not.toContain("openapi: 3.1.0");
    expect(stripped).not.toContain("token=");
  });

  it("leaves a plain message untouched", () => {
    expect(stripReplayedAttachmentText("hello there")).toBe("hello there");
  });
});

describe("toCompleteAttachments", () => {
  it("points image cards at the project-scoped serve URL", () => {
    const [image, file] = toCompleteAttachments(
      parseReplayedAttachments(REPLAYED_TURN),
      "https://app.getgram.ai",
    );

    expect(image?.type).toBe("image");
    expect(image?.name).toBe("skill-issue.gif");
    expect(image?.content[0]).toMatchObject({
      type: "file",
      mimeType: "image/gif",
      data: "https://app.getgram.ai/rpc/assets.serveChatAttachment?id=asset-1&project_id=project-1",
    });
    expect(file?.type).toBe("file");
  });
});
