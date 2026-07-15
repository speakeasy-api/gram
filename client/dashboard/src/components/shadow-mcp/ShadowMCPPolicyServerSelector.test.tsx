import { formatShortDate } from "@/components/access/shadow-mcp-utils";
import type { ShadowMCPInventoryServer } from "@gram/client/models/components/shadowmcpinventoryserver.js";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import { useState } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { ShadowMCPPolicyServerSelector } from "./ShadowMCPPolicyServerSelector";

function inventoryServer(
  canonicalServerUrl: string,
  overrides: Partial<ShadowMCPInventoryServer> = {},
): ShadowMCPInventoryServer {
  return {
    access: "none",
    allowedPolicyIds: [],
    canonicalServerUrl,
    firstSeen: new Date("2026-01-01T10:00:00Z"),
    lastCalled: undefined,
    lastSeen: new Date("2026-01-02T10:00:00Z"),
    observedUseCount: 0,
    requestCount: 0,
    serverName: undefined,
    serverSlug: "inventory-server-d8860eea",
    topUsers: [],
    urlHost: new URL(canonicalServerUrl).host,
    userCount: 0,
    ...overrides,
  };
}

const githubServer = inventoryServer("https://github.example.com/mcp", {
  access: "allowed",
  lastCalled: new Date("2026-01-02T09:00:00Z"),
  lastSeen: new Date("2026-01-02T10:00:00Z"),
  observedUseCount: 8,
  requestCount: 2,
  serverName: "GitHub MCP",
  userCount: 3,
});

const linearServer = inventoryServer("https://linear.example.com/sse", {
  access: "none",
  lastCalled: undefined,
  lastSeen: new Date("2026-01-03T10:00:00Z"),
  observedUseCount: 0,
  requestCount: 0,
  userCount: 0,
});

function ControlledSelector({
  initialSelection = [],
  servers = [githubServer, linearServer],
}: {
  initialSelection?: string[];
  servers?: ShadowMCPInventoryServer[];
}) {
  const [selectedURLs, setSelectedURLs] = useState(
    () => new Set(initialSelection),
  );

  return (
    <ShadowMCPPolicyServerSelector
      servers={servers}
      selectedURLs={selectedURLs}
      onSelectionChange={setSelectedURLs}
      isLoading={false}
      error={null}
      onRetry={() => {}}
    />
  );
}

function openSelector() {
  fireEvent.click(screen.getByRole("button", { name: "Select servers" }));
  return screen.getByRole("dialog", { name: "Select allowed servers" });
}

function expectCheckboxState(element: HTMLElement, checked: boolean) {
  expect(element.getAttribute("data-state")).toBe(
    checked ? "checked" : "unchecked",
  );
}

describe("ShadowMCPPolicyServerSelector", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  afterEach(cleanup);

  it("renders the empty applied selection", () => {
    render(<ControlledSelector />);

    const section = screen.getByRole("region", {
      name: "Allowed Shadow MCP servers",
    });
    expect(within(section).getByText("No servers selected")).toBeTruthy();
    expect(within(section).getByText("0 servers selected")).toBeTruthy();
    expect(
      within(section).queryByRole("list", {
        name: "Selected Shadow MCP servers",
      }),
    ).toBeNull();
  });

  it("renders a compact scrollable applied summary with only names and URLs", () => {
    const longServerName =
      "Server 0 with a deliberately long display name that must truncate";
    const longServerURL =
      "https://server-0.example.com/mcp/a/deliberately/long/path/that/must/truncate";
    const servers = Array.from({ length: 6 }, (_, index) =>
      inventoryServer(
        index === 0 ? longServerURL : `https://server-${index}.example.com/mcp`,
        {
          access: index === 0 ? "allowed" : "none",
          serverName: index === 0 ? longServerName : `Server ${index}`,
        },
      ),
    );

    render(
      <ControlledSelector
        servers={servers}
        initialSelection={servers.map((server) => server.canonicalServerUrl)}
      />,
    );

    const section = screen.getByRole("region", {
      name: "Allowed Shadow MCP servers",
    });
    const list = within(section).getByRole("list", {
      name: "Selected Shadow MCP servers",
    });

    expect(list.getAttribute("tabindex")).toBe("0");
    expect(list.className).toContain("max-h-[200px]");
    expect(list.className).toContain("overflow-y-auto");
    expect(list.className).toContain("focus-visible:ring-ring");
    expect(list.className).toContain("focus-visible:ring-2");
    expect(list.className).toContain("focus-visible:outline-none");

    const rows = within(list).getAllByRole("listitem");
    expect(rows).toHaveLength(6);

    const firstRow = rows[0];
    if (!firstRow) throw new Error("Expected an applied server row");

    expect(firstRow.className).toContain("h-9");
    expect(firstRow.className).toContain(
      "grid-cols-[minmax(7rem,0.35fr)_minmax(0,1fr)]",
    );

    const name = within(firstRow).getByText(longServerName);
    expect(name.getAttribute("title")).toBe(longServerName);
    expect(name.className).toContain("truncate");

    const url = within(firstRow).getByText(longServerURL);
    expect(url.getAttribute("title")).toBe(longServerURL);
    expect(url.className).toContain("truncate");
    expect(url.className).toContain("font-mono");
    expect(url.className).toContain("text-muted-foreground");
    expect(within(section).queryByText("Allowed", { exact: true })).toBeNull();
  });

  it("renders every inventory field including optional and zero values", () => {
    render(<ControlledSelector />);

    const dialog = openSelector();
    const githubRow = within(dialog).getByRole("row", { name: /GitHub MCP/ });
    expect(
      within(githubRow).getByText(githubServer.canonicalServerUrl),
    ).toBeTruthy();
    expect(
      within(githubRow).getByText(formatShortDate(githubServer.lastSeen)),
    ).toBeTruthy();
    expect(
      within(githubRow).getByText(
        `Last called ${formatShortDate(githubServer.lastCalled)}`,
      ),
    ).toBeTruthy();
    expect(within(githubRow).getByText("8 calls")).toBeTruthy();
    expect(within(githubRow).getByText("3 users")).toBeTruthy();
    expect(within(githubRow).getByText("2 requests")).toBeTruthy();
    expect(within(githubRow).getByText("Allowed")).toBeTruthy();

    const linearRow = within(dialog).getByRole("row", {
      name: /linear\.example\.com/,
    });
    expect(within(linearRow).getByText("Last called Never")).toBeTruthy();
    expect(within(linearRow).getByText("0 calls")).toBeTruthy();
    expect(within(linearRow).getByText("0 users")).toBeTruthy();
    expect(within(linearRow).getByText("0 requests")).toBeTruthy();
    expect(within(linearRow).getByText("Observed")).toBeTruthy();
  });

  it("searches names and canonical URLs case-insensitively", async () => {
    render(<ControlledSelector />);
    const dialog = openSelector();
    const search = within(dialog).getByRole("searchbox", {
      name: "Search servers",
    });

    fireEvent.change(search, { target: { value: "GITHUB MCP" } });
    await waitFor(() => {
      expect(within(dialog).getByText("GitHub MCP")).toBeTruthy();
      expect(
        within(dialog).queryByText(linearServer.canonicalServerUrl),
      ).toBeNull();
    });

    fireEvent.change(search, { target: { value: "LINEAR.EXAMPLE.COM/SSE" } });
    await waitFor(() => {
      expect(
        within(dialog).getByText(linearServer.canonicalServerUrl),
      ).toBeTruthy();
      expect(within(dialog).queryByText("GitHub MCP")).toBeNull();
    });
  });

  it("toggles rows and checkboxes once while preserving hidden selections", async () => {
    render(<ControlledSelector />);
    const dialog = openSelector();
    const githubRow = within(dialog).getByRole("row", { name: /GitHub MCP/ });
    const githubCheckbox = within(githubRow).getByRole("checkbox", {
      name: "Select GitHub MCP",
    });

    fireEvent.click(githubRow);
    expectCheckboxState(githubCheckbox, true);
    fireEvent.click(githubCheckbox);
    expectCheckboxState(githubCheckbox, false);
    fireEvent.click(githubCheckbox);
    expectCheckboxState(githubCheckbox, true);

    fireEvent.change(
      within(dialog).getByRole("searchbox", { name: "Search servers" }),
      { target: { value: "linear" } },
    );
    await waitFor(() =>
      expect(within(dialog).queryByText("GitHub MCP")).toBeNull(),
    );
    fireEvent.click(
      within(dialog).getByRole("checkbox", {
        name: "Select linear.example.com",
      }),
    );
    expect(within(dialog).getByText("2 of 2 servers selected")).toBeTruthy();
  });

  it("discards local changes on Cancel and restores the controlled selection", () => {
    render(
      <ControlledSelector
        initialSelection={[githubServer.canonicalServerUrl]}
      />,
    );
    let dialog = openSelector();
    fireEvent.click(
      within(dialog).getByRole("checkbox", { name: "Select GitHub MCP" }),
    );
    fireEvent.click(
      within(dialog).getByRole("checkbox", {
        name: "Select linear.example.com",
      }),
    );
    fireEvent.click(within(dialog).getByRole("button", { name: "Cancel" }));

    const section = screen.getByRole("region", {
      name: "Allowed Shadow MCP servers",
    });
    expect(within(section).getByText("GitHub MCP")).toBeTruthy();
    expect(
      within(section).queryByText(linearServer.canonicalServerUrl),
    ).toBeNull();

    dialog = openSelector();
    expectCheckboxState(
      within(dialog).getByRole("checkbox", { name: "Select GitHub MCP" }),
      true,
    );
    expectCheckboxState(
      within(dialog).getByRole("checkbox", {
        name: "Select linear.example.com",
      }),
      false,
    );
  });

  it("discards local changes when the dialog is dismissed", () => {
    render(
      <ControlledSelector
        initialSelection={[githubServer.canonicalServerUrl]}
      />,
    );
    let dialog = openSelector();
    fireEvent.click(
      within(dialog).getByRole("checkbox", {
        name: "Select linear.example.com",
      }),
    );
    fireEvent.click(within(dialog).getByRole("button", { name: "Close" }));

    dialog = openSelector();
    expectCheckboxState(
      within(dialog).getByRole("checkbox", {
        name: "Select linear.example.com",
      }),
      false,
    );
  });

  it("applies the full local set and lists only applied servers", () => {
    render(
      <ControlledSelector
        initialSelection={[githubServer.canonicalServerUrl]}
      />,
    );
    const dialog = openSelector();
    fireEvent.click(
      within(dialog).getByRole("checkbox", {
        name: "Select linear.example.com",
      }),
    );
    fireEvent.click(
      within(dialog).getByRole("button", { name: "Apply selection" }),
    );

    const section = screen.getByRole("region", {
      name: "Allowed Shadow MCP servers",
    });
    expect(within(section).getByText("GitHub MCP")).toBeTruthy();
    expect(
      within(section).getByText(linearServer.canonicalServerUrl),
    ).toBeTruthy();
    expect(within(section).getByText("2 servers selected")).toBeTruthy();
  });

  it("emits a new Set only when selection is applied", () => {
    const onSelectionChange = vi.fn<(next: Set<string>) => void>();
    render(
      <ShadowMCPPolicyServerSelector
        servers={[githubServer, linearServer]}
        selectedURLs={new Set([githubServer.canonicalServerUrl])}
        onSelectionChange={onSelectionChange}
        isLoading={false}
        error={null}
        onRetry={() => {}}
      />,
    );
    const dialog = openSelector();
    fireEvent.click(
      within(dialog).getByRole("checkbox", {
        name: "Select linear.example.com",
      }),
    );
    expect(onSelectionChange).not.toHaveBeenCalled();
    fireEvent.click(
      within(dialog).getByRole("button", { name: "Apply selection" }),
    );

    expect(onSelectionChange).toHaveBeenCalledOnce();
    const applied = onSelectionChange.mock.calls[0]?.[0];
    expect(applied).toBeInstanceOf(Set);
    expect(applied).toEqual(
      new Set([
        githubServer.canonicalServerUrl,
        linearServer.canonicalServerUrl,
      ]),
    );
  });

  it("renders loading and retry states", () => {
    const onRetry = vi.fn<() => void>();
    const { rerender } = render(
      <ShadowMCPPolicyServerSelector
        servers={[]}
        selectedURLs={new Set()}
        onSelectionChange={() => {}}
        isLoading
        error={null}
        onRetry={onRetry}
      />,
    );
    expect(screen.getByText("Loading Shadow MCP inventory…")).toBeTruthy();
    expect(
      (
        screen.getByRole("button", {
          name: "Select servers",
        }) as HTMLButtonElement
      ).disabled,
    ).toBe(true);

    rerender(
      <ShadowMCPPolicyServerSelector
        servers={[]}
        selectedURLs={new Set()}
        onSelectionChange={() => {}}
        isLoading={false}
        error={new Error("inventory unavailable")}
        onRetry={onRetry}
      />,
    );
    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    expect(onRetry).toHaveBeenCalledOnce();
  });
});
