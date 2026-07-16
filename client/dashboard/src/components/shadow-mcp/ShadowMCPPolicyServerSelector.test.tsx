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
  originalSelection = [],
  servers = [githubServer, linearServer],
}: {
  initialSelection?: string[];
  originalSelection?: string[];
  servers?: ShadowMCPInventoryServer[];
}) {
  const [selectedURLs, setSelectedURLs] = useState(
    () => new Set(initialSelection),
  );
  const [originalURLs] = useState(() => new Set(originalSelection));

  return (
    <ShadowMCPPolicyServerSelector
      servers={servers}
      originalURLs={originalURLs}
      selectedURLs={selectedURLs}
      onSelectionChange={setSelectedURLs}
      isLoading={false}
      error={null}
      onRetry={() => {}}
    />
  );
}

function openSelector() {
  const trigger =
    screen.queryByRole("button", { name: "Select servers" }) ??
    screen.getByRole("button", { name: "Manage servers" });

  fireEvent.click(trigger);
  return screen.getByRole("dialog", { name: "Select allowed servers" });
}

function expectCheckboxState(element: HTMLElement, checked: boolean) {
  expect(element.getAttribute("data-state")).toBe(
    checked ? "checked" : "unchecked",
  );
}

function rowLabels(dialog: HTMLElement): string[] {
  return within(dialog)
    .getAllByRole("row")
    .slice(1)
    .map((row) =>
      within(row)
        .getByRole("checkbox")
        .getAttribute("aria-label")!
        .replace("Select ", ""),
    );
}

describe("ShadowMCPPolicyServerSelector", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  afterEach(cleanup);

  it("renders an actionable empty selection", () => {
    render(<ControlledSelector />);

    const section = screen.getByRole("region", {
      name: "Servers allowed by this policy",
    });
    expect(
      within(section).getByText(
        "These Shadow MCP servers remain available when the policy blocks access.",
      ),
    ).toBeTruthy();
    expect(within(section).getByText("No servers allowed yet")).toBeTruthy();
    expect(
      within(section).getByText(
        "Select any Shadow MCP servers that should remain available when this policy blocks access.",
      ),
    ).toBeTruthy();
    expect(
      within(section).getByRole("button", { name: "Select servers" }),
    ).toBeTruthy();
    expect(
      within(section).queryByRole("button", { name: "Manage servers" }),
    ).toBeNull();
    expect(within(section).queryByText("0 servers selected")).toBeNull();
  });

  it("shows an action-first table for a new policy selection", () => {
    render(
      <ControlledSelector
        initialSelection={[githubServer.canonicalServerUrl]}
      />,
    );

    const section = screen.getByRole("region", {
      name: "Servers allowed by this policy",
    });
    const table = within(section).getByRole("table");

    expect(
      within(table)
        .getAllByRole("columnheader")
        .map((header) => header.textContent?.trim()),
    ).toEqual(["Action", "Server", "URL"]);
    const row = within(table).getByRole("row", { name: /GitHub MCP/ });
    expect(within(row).getByText("Add")).toBeTruthy();
    expect(within(row).getByText("GitHub MCP")).toBeTruthy();
    expect(within(row).getByText(githubServer.canonicalServerUrl)).toBeTruthy();

    const rowGroups = within(table).getAllByRole("rowgroup");
    const body = rowGroups[1];
    if (!body) throw new Error("Expected a table body");
    expect(body.className).toContain("max-h-[200px]");
    expect(body.className).toContain("overflow-y-auto");
  });

  it("keeps complete server values accessible in the applied table", () => {
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
      name: "Servers allowed by this policy",
    });
    expect(
      within(section).getByRole("button", { name: "Manage servers" }),
    ).toBeTruthy();
    expect(
      within(section).queryByRole("button", { name: "Select servers" }),
    ).toBeNull();
    const table = within(section).getByRole("table");
    const row = within(table).getByRole("row", {
      name: new RegExp(longServerName),
    });
    const name = within(row).getByText(longServerName);
    expect(name.getAttribute("title")).toBe(longServerName);
    expect(name.className).toContain("truncate");

    const url = within(row).getByText(longServerURL);
    expect(url.getAttribute("title")).toBe(longServerURL);
    expect(url.className).toContain("truncate");
    expect(url.className).toContain("font-mono");
  });

  it("shows add, remove, and no-change actions while editing", () => {
    const notionServer = inventoryServer("https://notion.example.com/mcp", {
      serverName: "Notion MCP",
    });
    render(
      <ControlledSelector
        servers={[githubServer, linearServer, notionServer]}
        originalSelection={[
          githubServer.canonicalServerUrl,
          linearServer.canonicalServerUrl,
        ]}
        initialSelection={[
          githubServer.canonicalServerUrl,
          notionServer.canonicalServerUrl,
        ]}
      />,
    );

    const table = within(
      screen.getByRole("region", { name: "Servers allowed by this policy" }),
    ).getByRole("table");
    expect(
      within(within(table).getByRole("row", { name: /GitHub MCP/ })).getByText(
        "No change",
      ),
    ).toBeTruthy();
    expect(
      within(
        within(table).getByRole("row", { name: /linear\.example\.com/ }),
      ).getByText("Remove"),
    ).toBeTruthy();
    expect(
      within(within(table).getByRole("row", { name: /Notion MCP/ })).getByText(
        "Add",
      ),
    ).toBeTruthy();
  });

  it("keeps pending removals visible when every saved server is deselected", () => {
    render(
      <ControlledSelector
        originalSelection={[githubServer.canonicalServerUrl]}
      />,
    );

    const section = screen.getByRole("region", {
      name: "Servers allowed by this policy",
    });
    expect(within(section).getByText("Remove")).toBeTruthy();
    expect(within(section).getByText("0 servers selected")).toBeTruthy();
    expect(
      within(section).getByRole("button", { name: "Manage servers" }),
    ).toBeTruthy();
    expect(within(section).queryByText("No servers allowed yet")).toBeNull();
  });

  it("matches inventory columns and request badge presentation", () => {
    render(<ControlledSelector />);

    const dialog = openSelector();
    expect(
      within(dialog)
        .getAllByRole("columnheader")
        .map((header) => header.textContent?.replace(/\s+/g, " ").trim()),
    ).toEqual(["Selected", "Server", "Status", "Last called", "Usage"]);
    for (const header of ["Server", "Status", "Last called", "Usage"]) {
      expect(
        within(dialog).getByRole("button", { name: new RegExp(header) }),
      ).toBeTruthy();
    }
    expect(within(dialog).queryByText("Last seen", { exact: true })).toBeNull();
    expect(within(dialog).queryByText("Access", { exact: true })).toBeNull();

    const githubRow = within(dialog).getByRole("row", { name: /GitHub MCP/ });
    expect(within(githubRow).getByText("2 Access Requests")).toBeTruthy();
    expect(within(githubRow).getByText("8 calls")).toBeTruthy();
    expect(within(githubRow).getByText("3 users")).toBeTruthy();
    expect(within(githubRow).queryByText("2 requests")).toBeNull();
    expect(within(githubRow).getByText("Allowed")).toBeTruthy();
    expect(
      within(githubRow).getByText(formatShortDate(githubServer.lastCalled)),
    ).toBeTruthy();

    const linearRow = within(dialog).getByRole("row", {
      name: /linear\.example\.com/,
    });
    expect(within(linearRow).queryByText(/Access Request/)).toBeNull();
    expect(within(linearRow).getByText("Never")).toBeTruthy();
  });

  it("sorts the same columns as inventory", () => {
    const servers = [
      inventoryServer("https://zulu.example.com/mcp", {
        access: "blocked",
        lastCalled: new Date("2026-01-03T10:00:00Z"),
        observedUseCount: 30,
        serverName: "Zulu MCP",
      }),
      inventoryServer("https://alpha.example.com/mcp", {
        access: "allowed",
        lastCalled: new Date("2026-01-01T10:00:00Z"),
        observedUseCount: 10,
        serverName: "Alpha MCP",
      }),
      inventoryServer("https://middle.example.com/mcp", {
        access: "none",
        lastCalled: new Date("2026-01-02T10:00:00Z"),
        observedUseCount: 20,
        serverName: "Middle MCP",
      }),
    ];
    render(<ControlledSelector servers={servers} />);
    const dialog = openSelector();

    expect(rowLabels(dialog)).toEqual(["Zulu MCP", "Middle MCP", "Alpha MCP"]);

    fireEvent.click(within(dialog).getByRole("button", { name: /Server/ }));
    expect(rowLabels(dialog)).toEqual(["Alpha MCP", "Middle MCP", "Zulu MCP"]);

    fireEvent.click(within(dialog).getByRole("button", { name: /Status/ }));
    expect(rowLabels(dialog)).toEqual(["Alpha MCP", "Zulu MCP", "Middle MCP"]);

    fireEvent.click(
      within(dialog).getByRole("button", { name: /Last called/ }),
    );
    expect(rowLabels(dialog)).toEqual(["Alpha MCP", "Middle MCP", "Zulu MCP"]);

    fireEvent.click(within(dialog).getByRole("button", { name: /Usage/ }));
    expect(rowLabels(dialog)).toEqual(["Alpha MCP", "Middle MCP", "Zulu MCP"]);
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
      name: "Servers allowed by this policy",
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
      name: "Servers allowed by this policy",
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
        originalURLs={new Set([githubServer.canonicalServerUrl])}
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
        originalURLs={new Set()}
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
        originalURLs={new Set()}
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
