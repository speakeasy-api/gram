import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { afterEach, describe, expect, it, vi } from "vitest";
import { PluginDistributionLine } from "./PluginDistributionLine";

const state = vi.hoisted(() => ({
  canWrite: true,
  canReadOrg: true,
  plugins: undefined as
    | {
        plugins: {
          id: string;
          name: string;
          servers?: { mcpServerId?: string }[];
        }[];
      }
    | undefined,
  usePlugins: vi.fn(),
}));

vi.mock("@/hooks/usePluginWriteAccess", () => ({
  usePluginWriteAccess: () => state.canWrite,
}));
vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({ hasAnyScope: () => state.canReadOrg, isLoading: false }),
}));
vi.mock("@/contexts/Auth", () => ({
  useOrganization: () => ({ id: "org" }),
  useProject: () => ({ id: "project" }),
}));
// Stands in for the greyed-out wrapper RequireScope renders when denied.
vi.mock("@/components/require-scope", () => ({
  RequireScope: ({
    scope,
    children,
  }: {
    scope: string;
    children: React.ReactNode;
  }) => (
    <div data-testid="scope-disabled" data-scope={scope}>
      {children}
    </div>
  ),
}));
vi.mock("@/pages/plugins/usePluginQueryScope", () => ({
  usePluginQueryScope: () => ({ gramProject: "p", gramSession: "s" }),
}));
vi.mock("@/routes", () => ({
  useRoutes: () => ({
    plugins: { detail: { href: (id: string) => `/plugins/${id}` } },
  }),
}));
vi.mock("@gram/client/react-query/plugins", () => ({
  usePlugins: (...args: unknown[]) => {
    state.usePlugins(...args);
    return { data: state.plugins };
  },
}));

const server = {
  kind: "mcp-server" as const,
  id: "server-a",
  slug: "server-a",
  name: "Server",
};

function renderLine() {
  return render(
    <PluginDistributionLine server={server} settingsHref="/settings#plugins" />,
    { wrapper: MemoryRouter },
  );
}

afterEach(() => {
  cleanup();
  state.canWrite = true;
  state.canReadOrg = true;
  state.plugins = undefined;
  state.usePlugins.mockReset();
});

describe("PluginDistributionLine", () => {
  it("lists the server's plugins alphabetically as links, behind the count", () => {
    state.plugins = {
      plugins: [
        { id: "z", name: "zeta", servers: [{ mcpServerId: "server-a" }] },
        { id: "o", name: "Other", servers: [{ mcpServerId: "server-b" }] },
        { id: "a", name: "Alpha", servers: [{ mcpServerId: "server-a" }] },
      ],
    };
    renderLine();
    expect(screen.getByText("Distributed in")).toBeTruthy();
    expect(
      screen.getByRole("link", { name: "Manage plugins" }).getAttribute("href"),
    ).toBe("/settings#plugins");

    fireEvent.click(screen.getByRole("button", { name: "2 plugins" }));
    const links = screen
      .getByRole("list", { name: "Plugins this server is in" })
      .querySelectorAll("a");
    expect(
      [...links].map((a) => [a.textContent, a.getAttribute("href")]),
    ).toEqual([
      ["Alpha", "/plugins/a"],
      ["zeta", "/plugins/z"],
    ]);
  });

  it("says one plugin in the singular", () => {
    state.plugins = {
      plugins: [
        { id: "a", name: "Alpha", servers: [{ mcpServerId: "server-a" }] },
      ],
    };
    renderLine();
    expect(screen.getByRole("button", { name: "1 plugin" })).toBeTruthy();
  });

  it("offers to add the server to a plugin when it is in none", () => {
    state.plugins = { plugins: [] };
    renderLine();
    expect(screen.getByText("Not in a plugin yet")).toBeTruthy();
    expect(screen.queryByRole("button")).toBeNull();
    expect(
      screen
        .getByRole("link", { name: "Add to a plugin" })
        .getAttribute("href"),
    ).toBe("/settings#plugins");
  });

  it("renders nothing until plugins load", () => {
    const { container } = renderLine();
    expect(container.innerHTML).toBe("");
  });

  it("greys the line out, with nothing to click, for people who cannot edit plugins", () => {
    state.canWrite = false;
    state.plugins = {
      plugins: [
        { id: "a", name: "Alpha", servers: [{ mcpServerId: "server-a" }] },
      ],
    };
    renderLine();
    const disabled = screen.getByTestId("scope-disabled");
    expect(disabled.getAttribute("data-scope")).toBe("plugin:write");
    expect(disabled.textContent).toBe("Distributed in1 plugin·Manage plugins");
    expect(screen.queryByRole("link")).toBeNull();
    expect(screen.queryByRole("button")).toBeNull();
  });

  it("shows the greyed line without a count, and reads nothing, for people who cannot read plugins", () => {
    state.canWrite = false;
    state.canReadOrg = false;
    state.plugins = { plugins: [] };
    renderLine();
    expect(screen.getByTestId("scope-disabled").textContent).toBe(
      "Manage plugins",
    );
    expect(state.usePlugins).not.toHaveBeenCalled();
  });
});
