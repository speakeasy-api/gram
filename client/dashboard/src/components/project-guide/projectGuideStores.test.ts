import { act, renderHook } from "@testing-library/react";
import { beforeEach, describe, expect, it } from "vitest";

import {
  markProjectGuideMcpServerSelected,
  markProjectGuideStarted,
  useProjectGuideMcpServerSelection,
  useProjectGuideStarted,
} from "./projectGuideStores";

describe("projectGuideStores", () => {
  beforeEach(() => {
    localStorage.clear();
    window.dispatchEvent(new StorageEvent("storage", { key: null }));
  });

  it("scopes the started flag by organization and project", () => {
    const orgOneProject = renderHook(() =>
      useProjectGuideStarted("org-one", "project"),
    );
    const orgTwoProject = renderHook(() =>
      useProjectGuideStarted("org-two", "project"),
    );

    act(() => markProjectGuideStarted("org-one", "project"));

    expect(orgOneProject.result.current).toBe(true);
    expect(orgTwoProject.result.current).toBe(false);
    expect(
      localStorage.getItem("gram-project-guide-started:org-one:project"),
    ).toBe("true");
  });

  it("persists the selected MCP server by organization and project", () => {
    const orgOneProject = renderHook(() =>
      useProjectGuideMcpServerSelection("org-one", "project"),
    );
    const orgTwoProject = renderHook(() =>
      useProjectGuideMcpServerSelection("org-two", "project"),
    );

    act(() =>
      markProjectGuideMcpServerSelected("org-one", "project", "linear/server"),
    );

    expect(orgOneProject.result.current).toBe("linear/server");
    expect(orgTwoProject.result.current).toBeUndefined();
    expect(
      localStorage.getItem("gram-project-guide-mcp-server:org-one:project"),
    ).toBe("linear/server");
  });
});
