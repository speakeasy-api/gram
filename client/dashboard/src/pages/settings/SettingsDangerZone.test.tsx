import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const mocks = vi.hoisted(() => ({
  mutate: vi.fn(),
  navigate: vi.fn(),
  invalidateListProjects: vi.fn(),
  toastSuccess: vi.fn(),
  project: {
    id: "proj-1",
    name: "Sandbox",
    slug: "sandbox",
  },
}));

vi.mock("@/contexts/Auth", () => ({
  useOrganization: () => ({ id: "org-1", slug: "acme" }),
  useProject: () => mocks.project,
}));

vi.mock("@/routes", () => ({
  useOrgRoutes: () => ({
    home: { href: () => "/acme" },
  }),
}));

vi.mock("react-router", () => ({
  useNavigate: () => mocks.navigate,
}));

vi.mock("@gram/client/react-query/deleteProject", () => ({
  useDeleteProjectMutation: (options?: {
    onSuccess?: () => Promise<void>;
  }) => ({
    mutate: (variables: unknown) => {
      mocks.mutate(variables);
      void options?.onSuccess?.();
    },
    isPending: false,
  }),
}));

vi.mock("@gram/client/react-query/listProjects", () => ({
  invalidateListProjects: mocks.invalidateListProjects,
}));

vi.mock("@tanstack/react-query", () => ({
  useQueryClient: () => ({}),
}));

vi.mock("sonner", () => ({
  toast: {
    success: mocks.toastSuccess,
    error: vi.fn(),
  },
}));

import { SettingsDangerZone } from "./SettingsDangerZone";

beforeEach(() => {
  mocks.mutate.mockReset();
  mocks.navigate.mockReset();
  mocks.invalidateListProjects.mockReset();
  mocks.invalidateListProjects.mockResolvedValue(undefined);
  mocks.toastSuccess.mockReset();
  mocks.project.id = "proj-1";
  mocks.project.name = "Sandbox";
  mocks.project.slug = "sandbox";
});

afterEach(cleanup);

async function confirmDelete() {
  fireEvent.click(screen.getByRole("button", { name: "Delete Project" }));
  fireEvent.change(screen.getByLabelText("Type the project name to confirm:"), {
    target: { value: mocks.project.name },
  });
  fireEvent.click(
    screen.getAllByRole("button", { name: "Delete Project" }).at(-1)!,
  );
  await waitFor(() => expect(mocks.mutate).toHaveBeenCalled());
}

describe("SettingsDangerZone", () => {
  it("sends the user to org home after deleting a project", async () => {
    render(<SettingsDangerZone />);

    await confirmDelete();

    await waitFor(() =>
      expect(mocks.navigate).toHaveBeenCalledWith("/acme", { replace: true }),
    );
    expect(mocks.mutate).toHaveBeenCalledWith({
      request: { id: "proj-1" },
    });
    expect(mocks.invalidateListProjects).toHaveBeenCalled();
    const toastMessage = mocks.toastSuccess.mock.calls[0]?.[0] as ReactNode;
    const { container } = render(<>{toastMessage}</>);
    expect(container.textContent).toBe("Project Sandbox deleted successfully");
    expect(container.querySelector("strong")?.textContent).toBe("Sandbox");
  });
});
