import { describe, expect, it } from "vitest";
import {
  attachmentTypeLabel,
  isPassiveImageType,
  opensInBrowserTab,
} from "./attachment.helpers";

describe("attachmentTypeLabel", () => {
  it("uses a readable fallback for custom attachment types", () => {
    expect(attachmentTypeLabel("spreadsheet")).toBe("Spreadsheet");
    expect(attachmentTypeLabel("custom-data")).toBe("Custom data");
  });

  it("formats snake_case custom attachment types", () => {
    expect(attachmentTypeLabel("custom_data")).toBe("Custom data");
  });
});

describe("isPassiveImageType", () => {
  it("accepts raster images", () => {
    expect(isPassiveImageType("image/png")).toBe(true);
    expect(isPassiveImageType("IMAGE/JPEG")).toBe(true);
  });

  it("rejects SVG, which can carry script", () => {
    expect(isPassiveImageType("image/svg+xml")).toBe(false);
  });
});

describe("opensInBrowserTab", () => {
  it("opens passive media and documents", () => {
    expect(opensInBrowserTab("image/webp")).toBe(true);
    expect(opensInBrowserTab("audio/mpeg")).toBe(true);
    expect(opensInBrowserTab("text/plain; charset=utf-8")).toBe(true);
    expect(opensInBrowserTab("application/pdf")).toBe(true);
    expect(opensInBrowserTab("application/json")).toBe(true);
  });

  it("downloads types that can run script as the page", () => {
    expect(opensInBrowserTab("text/html")).toBe(false);
    expect(opensInBrowserTab("image/svg+xml")).toBe(false);
    expect(opensInBrowserTab("text/xml")).toBe(false);
    expect(opensInBrowserTab("application/xhtml+xml")).toBe(false);
    expect(opensInBrowserTab("")).toBe(false);
  });
});
