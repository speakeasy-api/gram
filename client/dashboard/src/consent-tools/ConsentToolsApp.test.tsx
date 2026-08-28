import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { ConsentToolsApp, type ConsentToolsAppProps } from "./ConsentToolsApp";
import { parsePrefillBootstrap } from "./prefill";

const listTools = vi.fn();
const connect = vi.fn();
const close = vi.fn();
const terminateSession = vi.fn();
const transportCtor = vi.fn();

vi.mock("@modelcontextprotocol/sdk/client/index.js", () => ({
  Client: class {
    connect = connect;
    listTools = listTools;
    close = close;
  },
}));

vi.mock("@modelcontextprotocol/sdk/client/streamableHttp.js", () => ({
  StreamableHTTPClientTransport: class {
    terminateSession = terminateSession;
    constructor(url: URL, opts: unknown) {
      transportCtor(url, opts);
    }
  },
  StreamableHTTPError: class extends Error {
    code: number | undefined;
    constructor(code: number | undefined, message: string | undefined) {
      super(message);
      this.code = code;
    }
  },
}));

function renderApp(overrides: Partial<ConsentToolsAppProps> = {}) {
  const button = document.createElement("button");
  button.id = "approve-btn";
  button.disabled = true;
  document.body.appendChild(button);
  const props: ConsentToolsAppProps = {
    toolsUrl: "/mcp/example/connect/mcp",
    state: "state-1",
    csrfToken: "csrf-1",
    formId: "consent-approve-form",
    approveButtonId: "approve-btn",
    consentEnabled: true,
    serverName: "example",
    prefill: null,
    ...overrides,
  };
  return { ...render(<ConsentToolsApp {...props} />), button };
}

type User = ReturnType<typeof userEvent.setup>;

/** Reveal the picker, which sits behind the "Specific tools" mode. */
async function openPicker(user: User): Promise<void> {
  await user.click(
    await screen.findByRole("radio", { name: /Specific tools/ }),
  );
  await screen.findByRole("tab", { name: /All/ });
}

function formFieldValues(name: string): string[] {
  return [...document.querySelectorAll('input[form="consent-approve-form"]')]
    .map((f) => f as HTMLInputElement)
    .filter((f) => f.name === name)
    .map((f) => f.value);
}

beforeEach(() => {
  connect.mockResolvedValue(undefined);
  close.mockResolvedValue(undefined);
  terminateSession.mockResolvedValue(undefined);
  listTools.mockResolvedValue({
    tools: [
      { name: "get_thing", annotations: { readOnlyHint: true } },
      { name: "delete_thing", annotations: { destructiveHint: true } },
      { name: "plain_tool" },
    ],
    nextCursor: undefined,
  });
});

afterEach(() => {
  cleanup();
  document.body.innerHTML = "";
  vi.clearAllMocks();
});

describe("ConsentToolsApp", () => {
  it("performs an MCP session with the consent headers and enables approval", async () => {
    const { button } = renderApp();
    await waitFor(() => expect(button.disabled).toBe(false));

    expect(connect).toHaveBeenCalledOnce();
    expect(terminateSession).toHaveBeenCalledOnce();
    const [url, opts] = transportCtor.mock.calls[0]!;
    expect((url as URL).pathname).toBe("/mcp/example/connect/mcp");
    const requestInit = (
      opts as {
        requestInit: {
          credentials: RequestCredentials;
          headers: Record<string, string>;
        };
      }
    ).requestInit;
    expect(requestInit.credentials).toBe("omit");
    const headers = requestInit.headers;
    expect(headers["Gram-Consent-State"]).toBe("state-1");
    expect(headers["Gram-Consent-Csrf"]).toBe("csrf-1");
    expect(headers["Gram-Consent-Inventory-Attempt"]).toMatch(
      /^[0-9a-f-]{36}$/,
    );
  });

  it("exhausts pagination before becoming ready", async () => {
    listTools
      .mockResolvedValueOnce({ tools: [{ name: "a" }], nextCursor: "next-1" })
      .mockResolvedValueOnce({ tools: [{ name: "b" }], nextCursor: undefined });
    const { button } = renderApp();
    await waitFor(() => expect(button.disabled).toBe(false));
    expect(listTools).toHaveBeenCalledTimes(2);
    expect(listTools).toHaveBeenNthCalledWith(2, { cursor: "next-1" });
  });

  it("keeps approval disabled while consentEnabled is false", async () => {
    const { button } = renderApp({ consentEnabled: false });
    await screen.findByRole("radio", { name: /All tools/ });
    expect(button.disabled).toBe(true);
  });

  it("keeps approval disabled and offers retry on failure", async () => {
    listTools.mockRejectedValue(new Error("boom"));
    const { button } = renderApp();
    await screen.findByText("Retry");
    expect(button.disabled).toBe(true);
  });

  it("defaults to the unrestricted all-tools grant with the picker collapsed", async () => {
    renderApp();
    await screen.findByRole("radio", { name: /All tools/ });
    expect(formFieldValues("tool_filtering")).toEqual(["off"]);
    expect(formFieldValues("tools")).toEqual([]);
    expect(formFieldValues("tool_inventory_id")).toHaveLength(1);
    expect(
      screen.getByText("All 3 tools, plus any the server adds later."),
    ).toBeTruthy();
    expect(screen.queryByText("get_thing")).toBeNull();
  });

  it("returning to all tools drops the narrowed grant", async () => {
    const user = userEvent.setup();
    renderApp();
    await openPicker(user);
    await user.click(screen.getByRole("checkbox", { name: /delete_thing/ }));
    expect(formFieldValues("tool_filtering")).toEqual(["on"]);

    await user.click(screen.getByRole("radio", { name: /All tools/ }));
    expect(formFieldValues("tool_filtering")).toEqual(["off"]);
    expect(formFieldValues("tools")).toEqual([]);
    expect(screen.queryByText("get_thing")).toBeNull();
  });

  it("navigating groups filters the list without granting anything", async () => {
    const user = userEvent.setup();
    renderApp();
    await openPicker(user);
    await user.click(screen.getByRole("tab", { name: /Read/ }));
    expect(screen.queryByText("delete_thing")).toBeNull();
    // Narrowing is on the moment the mode is "specific", but browsing a group
    // grants nothing: the scope is still empty.
    expect(formFieldValues("tool_filtering")).toEqual(["on"]);
    expect(formFieldValues("tools")).toEqual([]);
    expect(formFieldValues("tool_annotations")).toEqual([]);
    expect(formFieldValues("tool_annotations_live")).toEqual([]);
  });

  it("granting a group's annotation submits live intent by default", async () => {
    const user = userEvent.setup();
    renderApp();
    await openPicker(user);
    await user.click(screen.getByRole("tab", { name: /Read/ }));
    await user.click(screen.getByRole("checkbox", { name: /Select all 1/ }));

    expect(formFieldValues("tool_filtering")).toEqual(["on"]);
    expect(formFieldValues("tool_annotations_live")).toEqual(["read_only"]);
    expect(formFieldValues("tool_annotations")).toEqual([]);
    expect(formFieldValues("tools")).toEqual([]);
    expect(
      screen.getByText("Live grants include future matching tools"),
    ).toBeTruthy();
    expect(screen.getByText(/read-only \(live\)/)).toBeTruthy();
  });

  it("freezing a granted annotation submits snapshot intent", async () => {
    const user = userEvent.setup();
    renderApp();
    await openPicker(user);
    await user.click(screen.getByRole("tab", { name: /Read/ }));
    await user.click(screen.getByRole("checkbox", { name: /Select all 1/ }));
    await user.click(
      screen.getByRole("checkbox", { name: /Include future matching tools/ }),
    );

    expect(formFieldValues("tool_annotations")).toEqual(["read_only"]);
    expect(formFieldValues("tool_annotations_live")).toEqual([]);
    expect(screen.getByText("New tools require approval")).toBeTruthy();
    expect(screen.getByText(/read-only \(frozen\)/)).toBeTruthy();
  });

  it("ticking a tool row is a manual pick unioned with annotation grants", async () => {
    const user = userEvent.setup();
    renderApp();
    await openPicker(user);
    await user.click(screen.getByRole("tab", { name: /Read/ }));
    await user.click(screen.getByRole("checkbox", { name: /Select all 1/ }));
    await user.click(screen.getByRole("tab", { name: /All/ }));
    await user.click(screen.getByRole("checkbox", { name: /delete_thing/ }));

    expect(formFieldValues("tool_filtering")).toEqual(["on"]);
    expect(formFieldValues("tool_annotations_live")).toEqual(["read_only"]);
    expect(formFieldValues("tools")).toEqual(["delete_thing"]);
    expect(screen.getByText(/1 picked/)).toBeTruthy();
  });

  it("locks rows covered by a granted annotation", async () => {
    const user = userEvent.setup();
    renderApp();
    await openPicker(user);
    await user.click(screen.getByRole("tab", { name: /Read/ }));
    await user.click(screen.getByRole("checkbox", { name: /Select all 1/ }));
    const row = screen.getByRole("checkbox", { name: /get_thing/ });
    expect(row.getAttribute("aria-disabled")).toBe("true");
    await user.click(row);
    expect(formFieldValues("tool_annotations_live")).toEqual(["read_only"]);
    expect(formFieldValues("tools")).toEqual([]);
  });

  it("clears an all-tools group that an annotation grant fully covers", async () => {
    // Every tool is read-only, so granting that annotation covers the whole
    // inventory. The All tools tick must then be able to turn it back off.
    listTools.mockResolvedValue({
      tools: [
        { name: "get_thing", annotations: { readOnlyHint: true } },
        { name: "list_things", annotations: { readOnlyHint: true } },
      ],
      nextCursor: undefined,
    });
    const user = userEvent.setup();
    renderApp();
    await openPicker(user);
    await user.click(screen.getByRole("tab", { name: /Read/ }));
    await user.click(screen.getByRole("checkbox", { name: /Select all 2/ }));
    expect(formFieldValues("tool_annotations_live")).toEqual(["read_only"]);

    await user.click(screen.getByRole("tab", { name: /All/ }));
    const allTick = screen.getByRole("checkbox", { name: /Select all 2/ });
    expect(allTick.getAttribute("aria-checked")).toBe("true");
    await user.click(allTick);

    expect(formFieldValues("tool_annotations_live")).toEqual([]);
    expect(formFieldValues("tools")).toEqual([]);
  });

  it("bulk-picks the no-annotation group from its header tick", async () => {
    const user = userEvent.setup();
    renderApp();
    await openPicker(user);
    await user.click(screen.getByRole("tab", { name: /Other/ }));
    await user.click(screen.getByRole("checkbox", { name: /Select all 1/ }));

    expect(formFieldValues("tool_filtering")).toEqual(["on"]);
    expect(formFieldValues("tools")).toEqual(["plain_tool"]);
  });

  it("applies a stored-name prefill as picks intersected with the inventory", async () => {
    renderApp({
      prefill: {
        annotations: [{ name: "destructive", mode: "snapshot" }],
        tools: ["get_thing", "vanished"],
      },
    });
    await screen.findByText("get_thing");
    await waitFor(() => {
      expect(formFieldValues("tool_filtering")).toEqual(["on"]);
      expect(formFieldValues("tools")).toEqual(["get_thing"]);
      expect(formFieldValues("tool_annotations")).toEqual(["destructive"]);
    });
  });

  it("shows a role-hidden note listing hidden tool names", async () => {
    listTools.mockResolvedValue({
      tools: [{ name: "get_thing", annotations: { readOnlyHint: true } }],
      nextCursor: undefined,
      _meta: {
        "gram.dev/roleHiddenTools": {
          count: 4,
          names: ["drop_thing", "purge_thing"],
        },
      },
    });
    renderApp();
    expect(
      await screen.findByText(
        "4 tools are hidden by your role and cannot be granted here.",
      ),
    ).toBeTruthy();
    expect(screen.getByRole("tooltip")).toBeTruthy();
    expect(screen.getByText("drop_thing")).toBeTruthy();
    expect(screen.getByText("purge_thing")).toBeTruthy();
    expect(screen.getByText("and 2 more")).toBeTruthy();
  });

  it("filters the viewed group with the search box", async () => {
    const user = userEvent.setup();
    renderApp();
    await openPicker(user);
    await user.type(
      screen.getByRole("searchbox", { name: "Search tools" }),
      "delete",
    );
    expect(screen.queryByText("get_thing")).toBeNull();
    expect(screen.getByText("delete_thing")).toBeTruthy();
    await user.clear(screen.getByRole("searchbox", { name: "Search tools" }));
    expect(await screen.findByText("get_thing")).toBeTruthy();
  });

  it("discards prefilled grants that no longer match any tool, with notice", async () => {
    renderApp({
      prefill: {
        annotations: [
          { name: "read_only", mode: "live" },
          { name: "open_world", mode: "live" },
        ],
        tools: [],
      },
    });
    await screen.findByText("get_thing");
    expect(formFieldValues("tool_annotations_live")).toEqual(["read_only"]);
    expect(
      screen.getByText(
        "One previously granted annotation no longer matches any tool and was removed.",
      ),
    ).toBeTruthy();
  });

  it("renders hostile tool names as text", async () => {
    const hostile = '<img src=x onerror="window.pwned=true">';
    listTools.mockResolvedValue({
      tools: [{ name: hostile }],
      nextCursor: undefined,
    });
    const user = userEvent.setup();
    renderApp();
    await user.click(
      await screen.findByRole("radio", { name: /Specific tools/ }),
    );
    expect(await screen.findByText(hostile)).toBeTruthy();
    expect(
      (window as unknown as Record<string, unknown>)["pwned"],
    ).toBeUndefined();
  });
});

describe("parsePrefillBootstrap", () => {
  it("parses stored grants and rejects junk", () => {
    expect(parsePrefillBootstrap(undefined)).toBeNull();
    expect(parsePrefillBootstrap("not json")).toBeNull();
    expect(
      parsePrefillBootstrap(
        '{"annotations":[{"name":"read_only","mode":"live"}],"tools":["a"]}',
      ),
    ).toEqual({
      annotations: [{ name: "read_only", mode: "live" }],
      tools: ["a"],
    });
    expect(parsePrefillBootstrap('{"annotations":[],"tools":[1]}')).toBeNull();
    expect(
      parsePrefillBootstrap(
        '{"annotations":[{"name":"bogus","mode":"live"}],"tools":[]}',
      ),
    ).toBeNull();
    expect(
      parsePrefillBootstrap(
        '{"annotations":[{"name":"read_only","mode":"maybe"}],"tools":[]}',
      ),
    ).toBeNull();
    expect(parsePrefillBootstrap('{"tools":["a"]}')).toBeNull();
  });
});
