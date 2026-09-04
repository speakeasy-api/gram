import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { ReactNode } from "react";

vi.mock("@/components/page-layout", () => {
  const Wrapper = ({ children }: { children?: ReactNode }) => <>{children}</>;
  const Header = Object.assign(Wrapper, { Breadcrumbs: () => null });
  const Section = Object.assign(Wrapper, {
    Title: Wrapper,
    Description: Wrapper,
    Body: Wrapper,
  });
  const Page = Object.assign(Wrapper, { Header, Body: Wrapper, Section });
  return { Page };
});

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

import PlatformAdminOverview, { OrgOverrideSection } from "./Overview";

afterEach(cleanup);

describe("platform admin overview", () => {
  it("keeps organization and support controls without Dashboard activity", () => {
    render(<PlatformAdminOverview />);

    expect(screen.getByText("current")).toBeTruthy();
    expect(screen.getByText("org-current")).toBeTruthy();
    expect(screen.getByRole("button", { name: "Go to org" })).toBeTruthy();
    expect(
      screen.getByRole("switch", { name: "Toggle platform admin" }),
    ).toBeTruthy();
    expect(screen.queryByText("Activity")).toBeNull();
  });
});

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
