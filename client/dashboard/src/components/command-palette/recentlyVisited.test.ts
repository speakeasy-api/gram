import { describe, expect, it } from "vitest";
import { pageLabel } from "./recentlyVisited";

const BASE = "/acme/projects/default";

describe("pageLabel", () => {
  it("uses the route title for the section itself", () => {
    expect(pageLabel("Project Assistant", `${BASE}/chat`, `${BASE}/chat`)).toBe(
      "Project Assistant",
    );
  });

  it("uses a human slug on detail pages", () => {
    expect(
      pageLabel(
        "Sources",
        `${BASE}/sources`,
        `${BASE}/sources/externalmcp/notion`,
      ),
    ).toBe("notion");
  });

  it("does not show a UUID as the label", () => {
    expect(
      pageLabel(
        "Project Assistant",
        `${BASE}/chat`,
        `${BASE}/chat/b4b1d55f-1234-5678-9abc-def012345678`,
      ),
    ).toBe("Project Assistant");
  });

  it("does not show other opaque ids as the label", () => {
    expect(
      pageLabel(
        "Assistants",
        `${BASE}/assistants`,
        `${BASE}/assistants/${"a".repeat(32)}`,
      ),
    ).toBe("Assistants");
  });
});
