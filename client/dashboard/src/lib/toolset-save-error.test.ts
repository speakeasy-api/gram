import { describe, expect, it } from "vitest";

import {
  TOOLSET_CHANGED_MESSAGE,
  isToolsetVersionConflict,
  toolsetSaveErrorMessage,
} from "./toolset-save-error";

describe("toolsetSaveErrorMessage", () => {
  it("explains a stale-version conflict instead of echoing the raw error", () => {
    const conflict = Object.assign(new Error("conflict"), { statusCode: 409 });
    expect(isToolsetVersionConflict(conflict)).toBe(true);
    expect(toolsetSaveErrorMessage(conflict, "Failed to remove tools")).toBe(
      TOOLSET_CHANGED_MESSAGE,
    );
    expect(TOOLSET_CHANGED_MESSAGE).toMatch(/someone else changed/i);
    expect(TOOLSET_CHANGED_MESSAGE).toMatch(/reload/i);
  });

  it("keeps the API message for other failures", () => {
    const badRequest = Object.assign(new Error("invalid tool urn"), {
      statusCode: 400,
    });
    expect(isToolsetVersionConflict(badRequest)).toBe(false);
    expect(toolsetSaveErrorMessage(badRequest, "Failed to add tools")).toBe(
      "invalid tool urn",
    );
  });

  it("falls back when the error carries no message", () => {
    expect(toolsetSaveErrorMessage(undefined, "Failed to add tools")).toBe(
      "Failed to add tools",
    );
  });
});
