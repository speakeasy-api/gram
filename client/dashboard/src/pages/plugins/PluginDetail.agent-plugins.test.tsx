import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { PluginInstallControl } from "./PluginDetail";
import type { PluginPackagePlatform } from "./downloadPluginPackage";

vi.mock("@/contexts/Sdk", () => ({ useSdkClient: () => ({}) }));
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

afterEach(cleanup);

function renderControl(
  agentPluginsV1Compatible: boolean,
  onDownload: (platform: PluginPackagePlatform) => void = vi.fn(),
  isDownloading = false,
): (platform: PluginPackagePlatform) => void {
  render(
    <PluginInstallControl
      plugin={{
        name: "Portable plugin",
        slug: "portable-plugin",
        agentPluginsV1Compatible,
      }}
      publishStatus={undefined}
      isDownloadMenuOpen
      onDownloadMenuOpenChange={() => {}}
      onDownload={onDownload}
      isDownloading={isDownloading}
      isInstallSheetOpen={false}
      onInstallSheetOpenChange={() => {}}
    />,
  );
  return onDownload;
}

describe("Plugin detail Agent Plugins controls", () => {
  it("offers one portable ZIP action for compatible plugins", () => {
    const onDownload = renderControl(true);

    const action = screen.getByRole("button", {
      name: "Download Agent Plugins ZIP",
    });
    expect(screen.getAllByText("Download Agent Plugins ZIP")).toHaveLength(1);
    fireEvent.click(action);
    expect(onDownload).toHaveBeenCalledWith("agent-plugin");
  });

  it("omits only the portable action for incompatible plugins", () => {
    renderControl(false);

    expect(screen.queryByText("Download Agent Plugins ZIP")).toBeNull();
    for (const platform of ["Claude", "Cursor", "Codex"]) {
      const action = screen.getByRole("button", {
        name: `Download as zip — ${platform}`,
      });
      expect(action).toBeTruthy();
      expect((action as HTMLButtonElement).disabled).toBe(false);
    }
  });

  it("announces loading without disabling the menu trigger", () => {
    renderControl(
      true,
      vi.fn<(platform: PluginPackagePlatform) => void>(),
      true,
    );

    const trigger = screen.getByRole("button", { name: "Downloading..." });
    expect(trigger.getAttribute("aria-busy")).toBe("true");
    expect((trigger as HTMLButtonElement).disabled).toBe(false);
  });
});
