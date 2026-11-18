import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { getRBACScopeOverrideHeader } from "@/components/dev-toolbar-utils";
import { loadState, saveState, type CatalogScope } from "./rbac-override-state";

const mocks = vi.hoisted(() => ({ listScopes: vi.fn(), invalidate: vi.fn() }));
vi.mock("@gram/client/react-query/listScopes.js", () => ({
  useListScopes: mocks.listScopes,
}));
vi.mock("@gram/client/react-query/listToolsetsForOrg.js", () => ({
  useListToolsetsForOrg: () => ({ data: undefined }),
}));
vi.mock("@tanstack/react-query", () => {
  const client = { invalidateQueries: mocks.invalidate };
  return { useQueryClient: () => client };
});
vi.mock("@/contexts/Auth", () => ({
  useOrganization: () => ({
    id: "org-test",
    projects: [{ id: "project-a", slug: "example-project" }],
  }),
  useIsPlatformAdmin: () => true,
}));
vi.mock("@/components/page-layout", () => ({ Page: () => null }));
vi.mock("@/components/ui/MultiSelect", () => ({
  MultiSelect: ({
    options,
    onValueChange,
  }: {
    options: { label: string; value: string }[];
    onValueChange: (values: string[]) => void;
  }) => (
    <select
      aria-label="Scope resources"
      onChange={(event) => onValueChange([event.target.value])}
    >
      <option value="">All resources</option>
      {options.map((option) => (
        <option key={option.value} value={option.value}>
          {option.label}
        </option>
      ))}
    </select>
  ),
}));

import { RbacOverrideEditor } from "./RbacOverride";

const plugin: CatalogScope = {
  slug: "plugin:install",
  resourceType: "project",
  description: "Install plugins in projects",
  visibility: "user_visible",
};

beforeEach(() => {
  localStorage.clear();
  vi.resetAllMocks();
  mocks.invalidate.mockResolvedValue(undefined);
});
afterEach(cleanup);

describe("RBAC override catalog", () => {
  it("renders catalog groups and narrowing, persisting before refresh and retaining cached choices on failure", async () => {
    saveState({
      enabled: true,
      scopes: { "org:read": { enabled: false, resources: null } },
    });
    mocks.listScopes.mockReturnValue({
      data: {
        scopes: [
          plugin,
          { ...plugin, slug: "widget:read", resourceType: "widget" },
        ],
      },
    });
    let persisted: string | null = null;
    mocks.invalidate.mockImplementation(() => {
      persisted = localStorage.getItem("gram-rbac-dev-override");
    });
    const view = render(<RbacOverrideEditor />);
    expect(JSON.parse(persisted ?? "null").scopes[plugin.slug].enabled).toBe(
      true,
    );
    expect(screen.getByRole("heading", { name: "widget" })).toBeDefined();
    expect(
      screen.getByLabelText("Toggle org:read").getAttribute("aria-checked"),
    ).toBe("false");
    const projectSection = screen.getByText(plugin.slug).closest("section")!;
    fireEvent.change(within(projectSection).getByRole("combobox"), {
      target: { value: "project-a" },
    });
    await waitFor(() => expect(mocks.invalidate).toHaveBeenCalledTimes(2));
    expect(getRBACScopeOverrideHeader(true)).toContain(
      "plugin:install=project-a",
    );
    mocks.listScopes.mockReturnValue({ isError: true });
    view.rerender(<RbacOverrideEditor />);
    expect(screen.getByLabelText("Toggle plugin:install")).toBeDefined();
    expect(loadState().scopes[plugin.slug]?.resources).toEqual(["project-a"]);
  });

  it("preserves saved state during cold loading and denial, and allows org:read recovery or removal", async () => {
    const saved = {
      enabled: true,
      scopes: {
        "org:read": { enabled: false, resources: ["stale-org"] },
        "saved:read": { enabled: true, resources: null },
      },
    };
    saveState(saved);
    mocks.listScopes.mockReturnValue({ isPending: true });
    const view = render(<RbacOverrideEditor />);
    expect(screen.getByRole("status").textContent).toContain(
      "Loading scope catalog",
    );
    expect(loadState()).toEqual(saved);
    mocks.listScopes.mockReturnValue({ isError: true });
    view.rerender(<RbacOverrideEditor />);
    expect(screen.getByRole("status").textContent).toContain("Enable org:read");
    expect(getRBACScopeOverrideHeader(true)).toBe("saved:read");
    let persisted: string | null = null;
    mocks.invalidate.mockImplementation(() => {
      persisted = localStorage.getItem("gram-rbac-dev-override");
    });
    fireEvent.click(screen.getByLabelText("Toggle org:read"));
    await waitFor(() => expect(mocks.invalidate).toHaveBeenCalledTimes(1));
    expect(JSON.parse(persisted ?? "null").scopes["org:read"]).toEqual({
      enabled: true,
      resources: null,
    });
    fireEvent.click(screen.getByLabelText("Toggle org:read"));
    await waitFor(() => expect(mocks.invalidate).toHaveBeenCalledTimes(2));
    expect(getRBACScopeOverrideHeader(true)).toBe("saved:read");
  });
});
