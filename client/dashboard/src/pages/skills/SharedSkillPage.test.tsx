import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { SharedSkillPage } from "./SharedSkillPage";

const testState = vi.hoisted(() => ({
  query: {
    isPending: false,
    error: null as Error | null,
    data: undefined as { result: Record<string, unknown> } | undefined,
  },
  lastOptions: undefined as Record<string, unknown> | undefined,
}));

vi.mock("react-router", () => ({
  useParams: () => ({ token: "tok_test" }),
}));
vi.mock("@gram/client/react-query/sharedSkill.js", () => ({
  useSharedSkill: (
    _request: Record<string, unknown>,
    options?: Record<string, unknown>,
  ) => {
    testState.lastOptions = options;
    return testState.query;
  },
}));

describe("SharedSkillPage", () => {
  afterEach(() => {
    cleanup();
    testState.query = { isPending: false, error: null, data: undefined };
  });

  it("renders the friendly unavailable state on query error instead of throwing", () => {
    testState.query = {
      isPending: false,
      error: new Error("link not found or no longer available"),
      data: undefined,
    };

    render(<SharedSkillPage />);

    expect(screen.getByText("This skill isn't available")).toBeDefined();
    // The raw server error (and anything token-shaped) must not render.
    expect(screen.queryByText(/link not found/)).toBeNull();
    expect(screen.queryByText(/tok_test/)).toBeNull();
  });

  it("opts out of the global throwOnError default so a dead link never hits the crash boundary", () => {
    render(<SharedSkillPage />);

    expect(testState.lastOptions?.throwOnError).toBe(false);
  });

  it("renders the shared skill document on success", () => {
    testState.query = {
      isPending: false,
      error: null,
      data: {
        result: {
          name: "example",
          displayName: "Example Skill",
          summary: "A summary.",
          content: "---\nname: example\n---\n# Body",
          updatedAt: new Date("2026-07-16T00:00:00Z"),
        },
      },
    };

    render(<SharedSkillPage />);

    expect(screen.getByText("Example Skill")).toBeDefined();
  });
});
