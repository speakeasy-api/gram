import { describe, expect, it } from "vitest";
import { attachmentTypeLabel } from "./attachment.helpers";

describe("attachmentTypeLabel", () => {
  it("uses a readable fallback for custom attachment types", () => {
    expect(attachmentTypeLabel("spreadsheet")).toBe("Spreadsheet");
    expect(attachmentTypeLabel("custom-data")).toBe("Custom data");
  });

  it("formats snake_case custom attachment types", () => {
    expect(attachmentTypeLabel("custom_data")).toBe("Custom data");
  });
});
