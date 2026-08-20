import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

vi.mock("@/contexts/Auth", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/contexts/Auth")>();
  return {
    ...actual,
    useIsPlatformAdmin: () => true,
    useOrganization: () => ({
      id: "org-current",
      name: "Current organization",
      slug: "current",
    }),
    useProject: () => ({ id: "project-current", slug: "default" }),
  };
});

vi.mock("@tanstack/react-query", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@tanstack/react-query")>();
  return {
    ...actual,
    useQueryClient: () => ({ invalidateQueries: vi.fn() }),
  };
});

import { OrgOverrideSection } from "./Overview";

describe("organization support override", () => {
  it("posts the target slug to the trusted support-session endpoint", () => {
    render(<OrgOverrideSection />);

    const input = screen.getByPlaceholderText("organization-slug");
    const submit = screen.getByRole("button", { name: "Go to org" });
    const form = submit.closest("form");

    expect(input.getAttribute("name")).toBe("organization_slug");
    expect(form?.getAttribute("method")).toBe("post");
    expect(form?.getAttribute("action")).toBe("/rpc/auth.startSupportSession");
  });
});
