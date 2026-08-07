import { TooltipProvider } from "@/components/ui/Tooltip";
import type { Plugin } from "@gram/client/models/components/plugin.js";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import type { ReactNode } from "react";
import { MemoryRouter } from "react-router";
import { afterEach, describe, expect, it, vi } from "vitest";
import { PluginCard } from "./PluginCard";

const { client, downloadPluginPackage, navigate } = vi.hoisted(() => ({
  client: {},
  downloadPluginPackage: vi.fn(),
  navigate: vi.fn(),
}));

vi.mock("@/components/card-context-menu", () => ({
  CardContextMenu: ({ children }: { children: ReactNode }) => <>{children}</>,
}));
vi.mock("@/components/ui/Dropdown", () => ({
  DropdownMenu: ({ children }: { children: ReactNode }) => <>{children}</>,
  DropdownMenuContent: ({ children }: { children: ReactNode }) => (
    <div>{children}</div>
  ),
  DropdownMenuItem: ({
    children,
    disabled,
    onClick,
  }: {
    children: ReactNode;
    disabled?: boolean;
    onClick?: () => void;
  }) => (
    <button type="button" disabled={disabled} onClick={onClick}>
      {children}
    </button>
  ),
  DropdownMenuSeparator: () => <hr />,
  DropdownMenuTrigger: ({ children }: { children: ReactNode }) => (
    <>{children}</>
  ),
}));
vi.mock("@/contexts/Sdk", () => ({ useSdkClient: () => client }));
vi.mock("react-router", async (importOriginal) => ({
  ...(await importOriginal<typeof import("react-router")>()),
  useNavigate: () => navigate,
}));
vi.mock("@/routes", () => ({
  useRoutes: () => ({
    plugins: { detail: { href: (id: string) => `/plugins/${id}` } },
  }),
}));
vi.mock("./downloadPluginPackage", () => ({
  usePluginPackageDownload: (
    sdkClient: unknown,
    pluginId: string,
  ): {
    isDownloading: boolean;
    download: (platform: string) => Promise<void>;
  } => ({
    isDownloading: false,
    download: async (platform) => {
      await downloadPluginPackage(sdkClient, pluginId, platform);
    },
  }),
}));

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

function plugin(agentPluginsV1Compatible: boolean): Plugin {
  return {
    agentPluginsV1Compatible,
    createdAt: new Date("2026-08-07T00:00:00Z"),
    id: "plugin-id",
    name: "Portable plugin",
    slug: "portable-plugin",
    updatedAt: new Date("2026-08-07T00:00:00Z"),
  };
}

function renderCard(agentPluginsV1Compatible: boolean): void {
  render(
    <MemoryRouter>
      <TooltipProvider>
        <PluginCard
          plugin={plugin(agentPluginsV1Compatible)}
          publishStatus={undefined}
        />
      </TooltipProvider>
    </MemoryRouter>,
  );
}

describe("PluginCard Agent Plugins actions", () => {
  it("offers the portable ZIP once and requests agent-plugin", async () => {
    renderCard(true);

    const action = screen.getByRole("button", {
      name: "Download Agent Plugins ZIP",
    });
    fireEvent.click(action);

    await waitFor(() => {
      expect(downloadPluginPackage).toHaveBeenCalledWith(
        client,
        "plugin-id",
        "agent-plugin",
      );
    });
    expect(screen.getAllByText("Download Agent Plugins ZIP")).toHaveLength(1);
    const status = screen.getByRole("img", {
      name: "Agent Plugin standard compatible",
    });
    fireEvent.keyDown(status, { key: "Enter" });
    fireEvent.keyDown(status, { key: " " });
    expect(navigate).not.toHaveBeenCalled();
    fireEvent.focus(status);
    const tooltip = await screen.findByRole("tooltip");
    expect(tooltip.textContent).toBe(
      "Agent Plugin standard compatible. Additional harnesses can use this plugin: https://agent-plugins.org/compatible-clients",
    );
  });

  it("shows neutral compatibility status while retaining native downloads", async () => {
    renderCard(false);

    expect(screen.queryByText("Download Agent Plugins ZIP")).toBeNull();
    for (const platform of ["Claude", "Cursor", "Codex"]) {
      const action = screen.getByRole("button", {
        name: `Download as zip — ${platform}`,
      });
      expect(action).toBeTruthy();
      expect((action as HTMLButtonElement).disabled).toBe(false);
    }

    const status = screen.getByLabelText(
      "Not Agent Plugin standard compatible",
    );
    fireEvent.focus(status);
    const tooltip = await screen.findByRole("tooltip");
    expect(tooltip.textContent).toBe(
      "Not Agent Plugin standard compatible. Plugin works normally in our supported harnesses.",
    );
  });
});
