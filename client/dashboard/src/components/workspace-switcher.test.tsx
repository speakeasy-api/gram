import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

const switchProject = vi.fn();

vi.mock("@/contexts/Auth", () => ({
  useOrganization: () => ({
    id: "org_1",
    slug: "acme",
    name: "Acme",
    projects: [{ id: "p1", slug: "proj", name: "Proj" }],
  }),
  useProject: () => ({ id: "p1", slug: "proj", name: "Proj", switchProject }),
}));
vi.mock("@/contexts/Sdk", () => ({
  useSlugs: () => ({ orgSlug: "acme", projectSlug: "proj" }),
  useSdkClient: () => ({ projects: { create: vi.fn() } }),
}));
vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({ hasAnyScope: () => true }),
}));
vi.mock("react-router", () => ({
  Link: ({ children }: { children: React.ReactNode }) => <a>{children}</a>,
}));
vi.mock("./project-menu", () => ({
  ProjectAvatar: () => <span data-testid="project-avatar" />,
}));
vi.mock("./brand-gradient-rail", () => ({
  BrandGradientRail: () => <div data-testid="ws-rail" />,
}));

import { WorkspaceSwitcher } from "./workspace-switcher";

afterEach(cleanup);

describe("WorkspaceSwitcher", () => {
  it("shows the project name and the gradient rail", () => {
    render(<WorkspaceSwitcher />);
    // The trigger shows the project's display name (not the org/project slug).
    expect(screen.getByText("Proj")).toBeTruthy();
    expect(screen.getByTestId("ws-rail")).toBeTruthy();
  });
});
