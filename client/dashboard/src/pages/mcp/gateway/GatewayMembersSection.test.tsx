import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { toast } from "sonner";
import { MemoryRouter } from "react-router";
import type { ToolsetEntry } from "@gram/client/models/components/toolsetentry.js";
import type { McpServer } from "@gram/client/models/components/mcpserver.js";

vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));

const permissions = vi.hoisted(() => ({
  allowed: true,
  project: true,
  flags: true,
  navigate: vi.fn(),
}));
vi.mock("react-router", async (importOriginal) => ({
  ...(await importOriginal<typeof import("react-router")>()),
  useNavigate: () => permissions.navigate,
}));
vi.mock("@/components/ui/Icon", () => ({ Icon: () => <span /> }));
vi.mock("@/components/sources/SourceCard", () => ({
  SourceMcpIcon: ({ mcpServerId }: { mcpServerId?: string }) => (
    <span data-testid="server-icon" data-server-id={mcpServerId} />
  ),
}));
vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({
    hasScope: (_scope: string, resource: string) =>
      permissions.allowed && (resource !== "project" || permissions.project),
    isLoading: false,
  }),
}));
vi.mock("@/contexts/Telemetry", () => ({
  useTelemetry: () => ({ isFeatureEnabled: () => permissions.flags }),
}));
vi.mock("@/routes", () => ({
  useRoutes: () => ({
    mcp: {
      catalog: { href: () => "/catalog" },
      add: Object.fromEntries(
        ["remote", "tunneled", "openapi", "fromSource", "function"].map(
          (key) => [key, { href: () => "/" + key }],
        ),
      ),
    },
  }),
}));
import { AddServersSheet } from "./GatewayMembersSection";

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
  permissions.allowed = true;
  permissions.project = true;
  permissions.flags = true;
  permissions.navigate.mockClear();
});
const servers = [
  { id: "a", name: "Alpha", slug: "alpha", remoteMcpServerId: "remote-a" },
  { id: "b", name: "Beta", slug: "beta", remoteMcpServerId: "remote-b" },
] as McpServer[];
function setup(
  overrides: Partial<React.ComponentProps<typeof AddServersSheet>> = {},
) {
  const onAdd = vi.fn(async (_candidates: unknown[]) => [
    { key: "server-a", name: "Alpha" },
    { key: "server-b", name: "Beta", error: "Try again" },
  ]);
  render(
    <MemoryRouter>
      <AddServersSheet
        open
        onOpenChange={() => {}}
        servers={servers}
        toolsets={[]}
        isLoading={false}
        loadFailed={false}
        onRetryLoad={() => {}}
        memberServerIds={new Set()}
        onAdd={onAdd}
        adding={false}
        projectId="project"
        gatewayId="gateway"
        {...overrides}
      />
    </MemoryRouter>,
  );
  return { onAdd };
}

describe("Add servers sheet", () => {
  it("uses one primary trigger with grouped creation choices and matching icons", () => {
    setup();
    const triggers = screen.getAllByRole("button", { name: "Add new" });
    expect(triggers).toHaveLength(1);
    const trigger = triggers[0]!;
    expect(trigger.classList.contains("bg-btn-primary")).toBe(true);
    fireEvent.pointerDown(trigger, {
      button: 0,
      ctrlKey: false,
      pointerType: "mouse",
    });
    const menu = screen.getByRole("menu");
    const groups = within(menu).getAllByRole("group");
    expect(groups.map((group) => group.getAttribute("aria-label"))).toEqual([
      "Recommended",
      "Advanced",
    ]);
    const expected = [
      [
        [
          "From the catalog",
          "blocks",
          "Pick a reviewed third-party server — Salesforce, Datadog, Linear, Slack, Okta and more.",
        ],
        [
          "Hosted remotely",
          "cloud",
          "Add a server that already runs elsewhere by its URL, proxied or not.",
        ],
        [
          "Reachable through a tunnel",
          "cable",
          "Connect a server running inside your own network through a tunnel.",
        ],
      ],
      [
        [
          "From your API",
          "file-code",
          "Upload an OpenAPI document to generate tools.",
        ],
        [
          "From an existing source",
          "boxes",
          "Build a server from an OpenAPI document or function this project already has.",
        ],
        [
          "Write custom code",
          "code",
          "Create tools with TypeScript functions.",
        ],
      ],
    ] as const;
    groups.forEach((group, index) => {
      const items = within(group).getAllByRole("menuitem");
      expect(items.map((item) => item.textContent)).toEqual(
        expected[index]!.map(([label, , description]) => label + description),
      );
      items.forEach((item, itemIndex) => {
        expect(
          item.firstElementChild?.classList.contains(
            `lucide-${expected[index]![itemIndex]![1]}`,
          ),
        ).toBe(true);
      });
    });
    const separator = within(menu).getByRole("separator");
    expect(separator.previousElementSibling).toBe(groups[0]);
    expect(separator.nextElementSibling).toBe(groups[1]);
  });

  it("dismisses only the dropdown on the first outside click, then the sheet", async () => {
    const onOpenChange = vi.fn();
    setup({ onOpenChange });
    await new Promise((resolve) => setTimeout(resolve, 0));
    fireEvent.pointerDown(screen.getByRole("button", { name: "Add new" }), {
      button: 0,
      ctrlKey: false,
      pointerType: "mouse",
    });
    expect(screen.getByRole("menu")).toBeTruthy();
    // Radix registers its document pointer listener on the next task.
    await new Promise((resolve) => setTimeout(resolve, 0));
    const overlay = document.querySelector('[data-slot="sheet-overlay"]')!;
    fireEvent.pointerDown(overlay, { button: 0, pointerType: "mouse" });
    fireEvent.click(overlay);
    await waitFor(() => expect(screen.queryByRole("menu")).toBeNull());
    expect(onOpenChange).not.toHaveBeenCalled();
    expect(screen.getByRole("dialog")).toBeTruthy();
    fireEvent.pointerDown(overlay, { button: 0, pointerType: "mouse" });
    fireEvent.click(overlay);
    expect(onOpenChange).toHaveBeenCalledWith(false);
  });

  it("clears focused search on Escape before allowing the sheet to close", () => {
    const onOpenChange = vi.fn();
    setup({ onOpenChange });
    const search = screen.getByRole("textbox", {
      name: "Search existing servers",
    });
    search.focus();
    fireEvent.change(search, { target: { value: "Alpha" } });
    fireEvent.keyDown(search, { key: "Escape" });
    expect((search as HTMLInputElement).value).toBe("");
    expect(onOpenChange).not.toHaveBeenCalled();
    fireEvent.keyDown(search, { key: "Escape" });
    expect(onOpenChange).toHaveBeenCalledWith(false);
  });

  it("allows Escape outside a nonempty search to close the sheet", () => {
    const onOpenChange = vi.fn();
    setup({ onOpenChange });
    fireEvent.change(screen.getByRole("textbox"), {
      target: { value: "Alpha" },
    });
    const button = screen.getByRole("button", { name: "Add new" });
    button.focus();
    fireEvent.keyDown(button, { key: "Escape" });
    expect(onOpenChange).toHaveBeenCalledWith(false);
  });

  it("closes only the dropdown on Escape while search is nonempty", async () => {
    const onOpenChange = vi.fn();
    setup({ onOpenChange });
    fireEvent.change(screen.getByRole("textbox"), {
      target: { value: "Alpha" },
    });
    const button = screen.getByRole("button", { name: "Add new" });
    button.focus();
    fireEvent.keyDown(button, { key: "ArrowDown" });
    const menu = await screen.findByRole("menu");
    fireEvent.keyDown(menu, { key: "Escape" });
    await waitFor(() => expect(screen.queryByRole("menu")).toBeNull());
    expect(onOpenChange).not.toHaveBeenCalled();
    expect((screen.getByRole("textbox") as HTMLInputElement).value).toBe(
      "Alpha",
    );
  });

  it("shows both sections and existing checkboxes immediately", () => {
    setup();
    expect(
      screen.getByRole("heading", { name: "Add servers to gateway" }),
    ).toBeTruthy();
    expect(
      screen.getByText(
        "Choose existing servers to add, or get started with a new one.",
      ),
    ).toBeTruthy();
    expect(
      screen.getByRole("heading", { name: "Create a new server" }),
    ).toBeTruthy();
    expect(screen.getByRole("button", { name: "Add new" })).toBeTruthy();
    expect(
      screen.getByRole("heading", { name: "Add existing servers" }),
    ).toBeTruthy();
    expect(screen.getByRole("list", { name: "Existing servers" })).toBeTruthy();
    expect(screen.getAllByRole("checkbox")).toHaveLength(2);
    const alpha = screen.getByRole("checkbox", { name: "Alpha" });
    expect(alpha.getAttribute("aria-checked")).toBe("false");
    fireEvent.click(alpha);
    expect(alpha.getAttribute("aria-checked")).toBe("true");
    fireEvent.click(alpha);
    expect(alpha.getAttribute("aria-checked")).toBe("false");
  });

  it("restores logos, slugs, classifications and exclusion badges", () => {
    setup({
      servers: [servers[0]!, { ...servers[1]!, visibility: "disabled" }],
      toolsets: [
        {
          id: "tools",
          name: "Local tools",
          slug: "local-tools",
          mcpEnabled: false,
        },
      ] as ToolsetEntry[],
    });
    const alpha = within(
      screen.getByRole("checkbox", { name: "Alpha" }).closest("li")!,
    );
    expect(alpha.getByText("alpha")).toBeTruthy();
    expect(alpha.getByText("Proxied")).toBeTruthy();
    expect(
      alpha.getByTestId("server-icon").getAttribute("data-server-id"),
    ).toBe("a");
    expect(alpha.queryByText("Excluded")).toBeNull();
    const beta = within(
      screen.getByRole("checkbox", { name: "Beta" }).closest("li")!,
    );
    expect(beta.getByText("Disabled")).toBeTruthy();
    expect(beta.getByText("Excluded")).toBeTruthy();
    const tools = within(
      screen.getByRole("checkbox", { name: "Local tools" }).closest("li")!,
    );
    expect(tools.getByText("local-tools")).toBeTruthy();
    expect(tools.getByText("Hosted")).toBeTruthy();
    expect(tools.getByText("MCP off")).toBeTruthy();
    expect(tools.getByTestId("server-icon")).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Add" })).toBeNull();
  });

  it("toggles the entire row and checkbox both ways without adding until submit", async () => {
    const { onAdd } = setup();
    const checkbox = screen.getByRole("checkbox", { name: "Alpha" });
    const row = checkbox.closest("label")!;
    for (const target of [row, screen.getByText("alpha"), checkbox, checkbox]) {
      const wasChecked = checkbox.getAttribute("aria-checked") === "true";
      fireEvent.click(target);
      expect(checkbox.getAttribute("aria-checked")).toBe(String(!wasChecked));
      expect(onAdd).not.toHaveBeenCalled();
    }
    fireEvent.click(row);
    fireEvent.click(
      screen.getByRole("button", { name: "Add selected servers" }),
    );
    await waitFor(() => expect(onAdd).toHaveBeenCalledOnce());
  });

  it.each(["ALPHA", " unique-slug "])(
    "searches names and slugs: %s",
    (query) => {
      setup({
        servers: [{ ...servers[0]!, slug: "unique-slug" }, servers[1]!],
      });
      fireEvent.click(screen.getByRole("checkbox", { name: "Beta" }));
      fireEvent.change(
        screen.getByRole("textbox", { name: "Search existing servers" }),
        {
          target: { value: query },
        },
      );
      expect(screen.getByRole("checkbox", { name: "Alpha" })).toBeTruthy();
      expect(screen.queryByRole("checkbox", { name: "Beta" })).toBeNull();
      expect(
        screen.getByRole("button", { name: "Add selected servers" }),
      ).toBeTruthy();
      fireEvent.click(screen.getByRole("button", { name: "Clear search" }));
      expect(
        screen
          .getByRole("checkbox", { name: "Beta" })
          .getAttribute("aria-checked"),
      ).toBe("true");
    },
  );

  it("retries failed list loading", () => {
    const onRetryLoad = vi.fn();
    setup({ loadFailed: true, onRetryLoad });
    fireEvent.click(screen.getByRole("button", { name: "Retry loading" }));
    expect(onRetryLoad).toHaveBeenCalledOnce();
    expect(screen.queryByRole("checkbox")).toBeNull();
  });

  it("selects existing wrappers sharing a toolset independently", async () => {
    const { onAdd } = setup({
      servers: servers.map((server) => ({ ...server, toolsetId: "shared" })),
    });
    fireEvent.click(screen.getByText("Beta"));
    fireEvent.click(
      screen.getByRole("button", { name: "Add selected servers" }),
    );
    await waitFor(() => expect(onAdd).toHaveBeenCalledTimes(1));
    expect(onAdd.mock.calls[0]?.[0]).toEqual([
      expect.objectContaining({ server: expect.objectContaining({ id: "b" }) }),
    ]);
  });

  it("retains a failed toolset selection only for its batch-created wrapper", async () => {
    const wrappers = new Map<string, string>();
    const hosted = {
      id: "tools",
      name: "Hosted",
      slug: "hosted",
    } as ToolsetEntry;
    const onAdd = vi.fn(async () => {
      wrappers.set("toolset-tools", "created");
      return [{ key: "toolset-tools", name: "Hosted", error: "Try again" }];
    });
    const props = {
      open: true,
      onOpenChange: () => {},
      toolsets: [hosted],
      isLoading: false,
      loadFailed: false,
      onRetryLoad: () => {},
      memberServerIds: new Set<string>(),
      onAdd,
      adding: false,
      projectId: "project",
      gatewayId: "gateway",
      wrappers,
    };
    const view = render(
      <MemoryRouter>
        <AddServersSheet {...props} servers={[]} />
      </MemoryRouter>,
    );
    fireEvent.click(screen.getByRole("checkbox", { name: "Hosted" }));
    fireEvent.click(
      screen.getByRole("button", { name: "Add selected servers" }),
    );
    await screen.findByText("Hosted: Try again");
    view.rerender(
      <MemoryRouter>
        <AddServersSheet
          {...props}
          servers={
            [
              { id: "other", name: "Other", slug: "other", toolsetId: "tools" },
              {
                id: "created",
                name: "Hosted",
                slug: "hosted",
                toolsetId: "tools",
              },
            ] as McpServer[]
          }
        />
      </MemoryRouter>,
    );
    fireEvent.click(
      screen.getByRole("button", { name: "Add selected servers" }),
    );
    await waitFor(() => expect(onAdd).toHaveBeenCalledTimes(2));
    expect(onAdd).toHaveBeenLastCalledWith([
      expect.objectContaining({
        server: expect.objectContaining({ id: "created" }),
      }),
    ]);
  });

  it("submits multiple servers, shows inline errors and retains only failures", async () => {
    const { onAdd } = setup();
    fireEvent.click(screen.getByText("Alpha"));
    fireEvent.click(screen.getByText("Beta"));
    fireEvent.click(
      screen.getByRole("button", { name: "Add selected servers" }),
    );
    await waitFor(() => expect(onAdd).toHaveBeenCalledTimes(1));
    expect(onAdd.mock.calls[0]?.[0]).toHaveLength(2);
    await waitFor(() =>
      expect(screen.getByText("Beta: Try again")).toBeTruthy(),
    );
    expect(screen.queryByText("Alpha: Added")).toBeNull();
    expect(toast.success).not.toHaveBeenCalled();
    expect(screen.getByRole("alert").closest("li")).toBeTruthy();
    expect(
      screen
        .getByRole("checkbox", { name: "Alpha" })
        .getAttribute("aria-checked"),
    ).toBe("false");
    expect(
      screen
        .getByRole("checkbox", { name: "Beta" })
        .getAttribute("aria-checked"),
    ).toBe("true");
    fireEvent.click(
      screen.getByRole("button", { name: "Add selected servers" }),
    );
    await waitFor(() => expect(onAdd).toHaveBeenCalledTimes(2));
    expect(onAdd).toHaveBeenLastCalledWith([
      expect.objectContaining({ server: expect.objectContaining({ id: "b" }) }),
    ]);
    expect(
      screen.getByRole("button", { name: "Add selected servers" }),
    ).toBeTruthy();
  });

  it.each([
    [{ isLoading: true }, "Loading servers…"],
    [
      { servers: [] },
      "No existing servers yet. Create a new server to get started.",
    ],
    [
      { memberServerIds: new Set(["a", "b"]) },
      "All existing servers have already been added.",
    ],
    [
      { loadFailed: true },
      "Couldn't load all servers. Retry to refresh the list.",
    ],
  ])("keeps creation discoverable in each list state", (props, message) => {
    setup(props);
    expect(screen.getByText(message)).toBeTruthy();
    expect(screen.getByRole("button", { name: "Add new" })).toBeTruthy();
  });

  it.each([{ servers: [] }, { memberServerIds: new Set(["a", "b"]) }])(
    "hides the bulk action when no candidates remain",
    (props) => {
      setup(props);
      expect(
        screen.queryByRole("button", { name: "Add selected servers" }),
      ).toBeNull();
    },
  );

  it("centers the empty-state copy in a shaded box without extra controls", () => {
    setup({ servers: [] });
    const panel = screen.getByText(
      "No existing servers yet. Create a new server to get started.",
    ).parentElement!;
    expect(panel.classList.contains("bg-muted/20")).toBe(true);
    expect(panel.classList.contains("text-center")).toBe(true);
    expect(panel.querySelector("button, svg")).toBeNull();
  });

  it("searches existing candidates without hiding creation", () => {
    setup();
    fireEvent.change(screen.getByPlaceholderText("Search by name or slug"), {
      target: { value: "no-match" },
    });
    expect(
      screen.getByText(
        "No matching servers. Try another search or add a new server.",
      ),
    ).toBeTruthy();
  });
  it("blocks direct-connect and slugless servers and explains hosted permission requirements", () => {
    setup({
      servers: [
        ...servers,
        {
          id: "direct",
          name: "Direct",
          slug: "direct",
          unproxiedMcpServerId: "direct-id",
        } as McpServer,
        { id: "slugless", name: "Slugless" } as McpServer,
      ],
    });
    expect(
      screen.getByRole("checkbox", { name: "Direct" }).hasAttribute("disabled"),
    ).toBe(true);
    expect(
      screen
        .getByRole("checkbox", { name: "Slugless" })
        .hasAttribute("disabled"),
    ).toBe(true);
    expect(
      screen.getByText(/Direct-connect servers cannot be added/),
    ).toBeTruthy();
    expect(screen.getByText(/A server slug is required/)).toBeTruthy();
    for (const name of ["Direct", "Slugless"]) {
      const checkbox = screen.getByRole("checkbox", { name });
      fireEvent.click(checkbox.closest("label")!);
      fireEvent.click(checkbox);
      expect(checkbox.getAttribute("aria-checked")).toBe("false");
    }
    expect(
      screen.getByRole("button", { name: "Add selected servers" }),
    ).toBeTruthy();
  });

  it("keeps disabled servers selectable with readiness explained beside the server", () => {
    setup({ servers: [{ ...servers[0]!, visibility: "disabled" }] });
    const checkbox = screen.getByRole("checkbox", { name: "Alpha" });
    expect(checkbox.hasAttribute("disabled")).toBe(false);
    expect(screen.getByText(/Disabled: excluded until enabled/)).toBeTruthy();
    fireEvent.click(checkbox);
    expect(checkbox.getAttribute("aria-checked")).toBe("true");
  });

  it("gates hosted wrapper creation separately from gateway membership", () => {
    permissions.project = false;
    setup({
      toolsets: [
        { id: "tools", name: "Hosted", slug: "hosted", mcpEnabled: false },
      ] as ToolsetEntry[],
    });
    expect(
      screen.getByRole("checkbox", { name: "Hosted" }).hasAttribute("disabled"),
    ).toBe(true);
    expect(
      screen.getByRole("checkbox", { name: "Alpha" }).hasAttribute("disabled"),
    ).not.toBe(true);
    expect(
      screen.getByText(
        /MCP off: excluded until enabled.*Requires project-level/,
      ),
    ).toBeTruthy();
  });

  it("prevents submitting without gateway write permission", () => {
    permissions.allowed = false;
    setup();
    expect(
      (
        screen.getByRole("button", {
          name: "Add selected servers",
        }) as HTMLButtonElement
      ).disabled,
    ).toBe(true);
  });

  it.each([
    ["From the catalog", "/catalog?attachToGateway=gateway"],
    ["Hosted remotely", "/remote"],
    ["Reachable through a tunnel", "/tunneled"],
    ["From your API", "/openapi"],
    ["From an existing source", "/fromSource"],
    ["Write custom code", "/function"],
  ])("opens %s directly without an all-route attach handoff", (label, href) => {
    setup();
    fireEvent.pointerDown(screen.getByRole("button", { name: "Add new" }), {
      button: 0,
      ctrlKey: false,
      pointerType: "mouse",
    });
    expect(screen.queryByRole("menuitem", { name: "New gateway" })).toBeNull();
    const item = screen.getByRole("menuitem", {
      name: new RegExp(`^${label}`),
    });
    fireEvent.click(item.querySelector(".text-muted-foreground")!);
    expect(permissions.navigate).toHaveBeenCalledWith(href);
  });

  it("hides flagged creation routes when disabled", () => {
    permissions.flags = false;
    setup();
    fireEvent.pointerDown(screen.getByRole("button", { name: "Add new" }), {
      button: 0,
      ctrlKey: false,
      pointerType: "mouse",
    });
    expect(
      screen.queryByRole("menuitem", { name: /^Reachable through a tunnel/ }),
    ).toBeNull();
    expect(
      screen.queryByRole("menuitem", { name: /^Write custom code/ }),
    ).toBeNull();
    expect(screen.getAllByRole("menuitem")).toHaveLength(4);
  });

  it("guards rapid duplicate submits while a batch is pending", async () => {
    let finish!: (results: []) => void;
    const onAdd = vi.fn(
      () =>
        new Promise<[]>((resolve) => {
          finish = resolve;
        }),
    );
    const onOpenChange = vi.fn();
    setup({ onAdd, onOpenChange });
    fireEvent.click(screen.getByText("Alpha"));
    const button = screen.getByRole("button", {
      name: "Add selected servers",
    });
    fireEvent.click(button);
    fireEvent.click(button);
    expect(onAdd).toHaveBeenCalledTimes(1);
    expect((button as HTMLButtonElement).disabled).toBe(true);
    expect(button.getAttribute("aria-busy")).toBe("true");
    expect(button.querySelector(".animate-spin")).toBeTruthy();
    fireEvent.keyDown(button, { key: "Escape" });
    expect(onOpenChange).not.toHaveBeenCalled();
    finish([]);
    await waitFor(() =>
      expect(screen.queryByText("Adding selected servers…")).toBeNull(),
    );
  });
  it("closes after full success with one counted toast and no success list", async () => {
    const onOpenChange = vi.fn();
    const onAdd = vi.fn(async () => [
      { key: "server-a", name: "Alpha" },
      { key: "server-b", name: "Beta" },
    ]);
    setup({ onAdd, onOpenChange });
    fireEvent.click(screen.getByText("Alpha"));
    fireEvent.click(screen.getByText("Beta"));
    fireEvent.click(
      screen.getByRole("button", { name: "Add selected servers" }),
    );
    await waitFor(() => expect(onOpenChange).toHaveBeenCalledWith(false));
    expect(toast.success).toHaveBeenCalledExactlyOnceWith("2 servers added");
    expect(screen.queryByText(/: Added/)).toBeNull();
    expect(screen.queryByRole("alert")).toBeNull();
  });

  it.each(["partial", "full", "rejected", "missing"] as const)(
    "keeps %s failures open and selected, then closes on successful retry",
    async (mode) => {
      const onOpenChange = vi.fn();
      const onAdd = vi.fn(async () => {
        if (mode === "rejected") throw new Error("Connection failed. Retry.");
        if (mode === "missing") return [];
        return [
          {
            key: "server-a",
            name: "Alpha",
            ...(mode === "full" ? { error: "Enable access and retry" } : {}),
          },
          { key: "server-b", name: "Beta", error: "Enable access and retry" },
        ];
      });
      setup({ onAdd, onOpenChange });
      fireEvent.click(screen.getByText("Alpha"));
      fireEvent.click(screen.getByText("Beta"));
      fireEvent.click(
        screen.getByRole("button", { name: "Add selected servers" }),
      );
      const count = mode === "partial" ? 1 : 2;
      await waitFor(() =>
        expect(screen.getAllByRole("alert")).toHaveLength(count),
      );
      expect(onOpenChange).not.toHaveBeenCalled();
      expect(toast.success).not.toHaveBeenCalled();
      expect(
        screen
          .getByRole("checkbox", { name: "Beta" })
          .getAttribute("aria-checked"),
      ).toBe("true");
      onAdd.mockResolvedValueOnce(
        mode === "partial"
          ? [{ key: "server-b", name: "Beta" }]
          : [
              { key: "server-a", name: "Alpha" },
              { key: "server-b", name: "Beta" },
            ],
      );
      fireEvent.click(
        screen.getByRole("button", { name: "Add selected servers" }),
      );
      await waitFor(() => expect(onOpenChange).toHaveBeenCalledWith(false));
      expect(onAdd.mock.calls).toHaveLength(2);
      expect(onAdd).toHaveBeenLastCalledWith(
        mode === "partial"
          ? [
              expect.objectContaining({
                server: expect.objectContaining({ id: "b" }),
              }),
            ]
          : [
              expect.objectContaining({
                server: expect.objectContaining({ id: "a" }),
              }),
              expect.objectContaining({
                server: expect.objectContaining({ id: "b" }),
              }),
            ],
      );
      expect(toast.success).toHaveBeenCalledExactlyOnceWith(
        `${count} ${count === 1 ? "server" : "servers"} added`,
      );
      expect(screen.queryByRole("alert")).toBeNull();
      expect(screen.queryByText(/: Added/)).toBeNull();
    },
  );
});
