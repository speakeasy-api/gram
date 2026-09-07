import { describe, expect, it } from "vitest";

import {
  formatActingSurfaceLabel,
  isNotableActingSurface,
} from "./audit-surfaces";

describe("formatActingSurfaceLabel", () => {
  it("labels every surface the server records", () => {
    expect(formatActingSurfaceLabel("dashboard")).toBe("Dashboard");
    expect(formatActingSurfaceLabel("api_key")).toBe("API key");
    expect(formatActingSurfaceLabel("platform_mcp")).toBe("Platform MCP");
    expect(formatActingSurfaceLabel("project_assistant")).toBe(
      "Project assistant",
    );
    expect(formatActingSurfaceLabel("unknown")).toBe("Unknown");
  });

  it("titles a surface this build has not been taught about", () => {
    expect(formatActingSurfaceLabel("some_new_surface")).toBe(
      "Some New Surface",
    );
  });

  it("survives an empty value", () => {
    expect(formatActingSurfaceLabel("")).toBe("");
  });
});

describe("isNotableActingSurface", () => {
  it("calls out surfaces that are not the assumed one", () => {
    expect(isNotableActingSurface("platform_mcp")).toBe(true);
    expect(isNotableActingSurface("project_assistant")).toBe(true);
    expect(isNotableActingSurface("api_key")).toBe(true);
  });

  it("stays quiet for the dashboard and for unrecorded surfaces", () => {
    expect(isNotableActingSurface("dashboard")).toBe(false);
    expect(isNotableActingSurface("unknown")).toBe(false);
    expect(isNotableActingSurface("")).toBe(false);
  });
});
