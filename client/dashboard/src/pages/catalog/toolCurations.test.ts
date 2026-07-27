import { describe, expect, it } from "vitest";
import {
  catalogToolCurations,
  GOOGLE_WORKSPACE_REGISTRY_SPECIFIER,
} from "./toolCurations";

describe("catalogToolCurations", () => {
  it("steers rich Google Doc creation to the HTML import tool", () => {
    const curations = catalogToolCurations(
      GOOGLE_WORKSPACE_REGISTRY_SPECIFIER,
      "019c97bd-067f-78c6-aac1-72bb82546f9b",
    );

    expect(curations).toEqual([
      expect.objectContaining({
        srcToolName: "import_to_google_doc",
        srcToolUrn:
          "tools:externalmcp:019c97bd-067f-78c6-aac1-72bb82546f9b:import_to_google_doc",
        name: "create_rich_doc",
      }),
      expect.objectContaining({
        srcToolName: "create_doc",
        srcToolUrn:
          "tools:externalmcp:019c97bd-067f-78c6-aac1-72bb82546f9b:create_doc",
      }),
    ]);
    expect(curations[0]?.description).toContain('source_format: "html"');
    expect(curations[0]?.description).toContain("tables");
    expect(curations[1]?.description).toContain("plain text only");
  });

  it("does not alter unrelated catalog servers", () => {
    expect(catalogToolCurations("example/server", "remote-id")).toEqual([]);
  });
});
