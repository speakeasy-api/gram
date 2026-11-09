import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const switchProject = vi.fn();
const navigate = vi.fn();

type Project = { id: string; slug: string; name: string };
let projects: Project[] = [];
let organizations: Array<{ id: string }> = [];
let projectSlug: string | undefined;

vi.mock("@/contexts/Auth", () => ({
  useOrganization: () => ({
    id: "org_1",
    slug: "acme",
    name: "Acme",
    projects,
  }),
  useProject: () => ({ id: "p1", slug: "proj", name: "Proj", switchProject }),
  useSession: () => ({ organizations }),
}));
vi.mock("@/contexts/Sdk", () => ({
  useSlugs: () => ({ orgSlug: "acme", projectSlug }),
  useSdkClient: () => ({ projects: { create: vi.fn() } }),
}));
vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({ hasAnyScope: () => true }),
}));
vi.mock("react-router", () => ({
  Link: ({ children }: { children: React.ReactNode }) => <a>{children}</a>,
  useNavigate: () => navigate,
}));

import { WorkspaceSwitcher } from "./workspace-switcher";

const proj = { id: "p1", slug: "proj", name: "Proj" };
const alpha = { id: "p2", slug: "alpha", name: "Alpha" };

beforeEach(() => {
  projects = [proj];
  organizations = [{ id: "org_1" }];
  projectSlug = "proj";
  navigate.mockReset();
  switchProject.mockReset();
});
afterEach(cleanup);

describe("WorkspaceSwitcher", () => {
  it("shows the project name", () => {
    render(<WorkspaceSwitcher />);
    // The trigger shows the project's display name (not the org/project slug),
    // suffixed with "Project".
    expect(screen.getByText("Proj Project")).toBeTruthy();
    expect(screen.queryByRole("img", { name: "Proj" })).toBeNull();
    expect(screen.getByText("P").getAttribute("aria-hidden")).toBe("true");
  });

  describe("on an org-level page", () => {
    beforeEach(() => {
      projectSlug = undefined;
    });

    it("lists the projects the member can open", async () => {
      const user = userEvent.setup();
      projects = [proj, alpha];
      render(<WorkspaceSwitcher />);

      await user.click(screen.getByRole("button", { name: /Acme/ }));
      expect(screen.getByRole("option", { name: /Alpha/ })).toBeTruthy();
      expect(screen.getByRole("option", { name: /Proj/ })).toBeTruthy();

      await user.click(screen.getByRole("option", { name: /Alpha/ }));
      expect(navigate).toHaveBeenCalledWith("/acme/projects/alpha");
    });

    it("says who to ask when the member is in no projects", async () => {
      const user = userEvent.setup();
      projects = [];
      render(<WorkspaceSwitcher />);

      await user.click(screen.getByRole("button", { name: /Acme/ }));
      expect(
        screen.getByText(/Ask an admin to add you to one/).textContent,
      ).toContain("not in any projects in Acme");
      expect(
        screen.queryByRole("button", { name: "Switch organization" }),
      ).toBeNull();
    });

    it("switches organization for members of several", async () => {
      const user = userEvent.setup();
      organizations = [{ id: "org_1" }, { id: "org_2" }];
      render(<WorkspaceSwitcher />);

      await user.click(screen.getByRole("button", { name: /Acme/ }));
      await user.click(
        screen.getByRole("button", { name: "Switch organization" }),
      );
      expect(navigate).toHaveBeenCalledWith("/switch-org");
    });
  });
});
