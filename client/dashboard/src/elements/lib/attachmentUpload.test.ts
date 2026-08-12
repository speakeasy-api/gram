import { describe, expect, it } from "vitest";

import {
  chatAttachmentAssetId,
  chatAttachmentContentType,
} from "@/elements/lib/attachmentUpload";

describe("chatAttachmentContentType", () => {
  it("prefers what the browser reports", () => {
    expect(
      chatAttachmentContentType(
        new File(["{}"], "openapi.json", { type: "application/json" }),
      ),
    ).toBe("application/json");
  });

  // Windows and Linux report "" for YAML, which would upload as
  // application/octet-stream and be rejected by the server.
  it("infers a type from the extension when the browser reports none", () => {
    expect(chatAttachmentContentType(new File(["a: 1"], "openapi.yaml"))).toBe(
      "application/yaml",
    );
    expect(chatAttachmentContentType(new File(["a: 1"], "spec.YML"))).toBe(
      "application/yaml",
    );
    expect(chatAttachmentContentType(new File(["# hi"], "notes.md"))).toBe(
      "text/markdown",
    );
  });

  it("falls back to octet-stream for unknown extensions", () => {
    expect(chatAttachmentContentType(new File([""], "archive.bin"))).toBe(
      "application/octet-stream",
    );
  });
});

describe("chatAttachmentAssetId", () => {
  it("reads the asset id out of a serve URL", () => {
    expect(
      chatAttachmentAssetId(
        "https://app.getgram.ai/rpc/assets.serveChatAttachment?project_id=p&id=asset-1",
      ),
    ).toBe("asset-1");
  });

  it("returns null for URLs that carry no asset id", () => {
    expect(chatAttachmentAssetId("data:image/png;base64,AAAA")).toBeNull();
    expect(chatAttachmentAssetId("")).toBeNull();
  });
});
