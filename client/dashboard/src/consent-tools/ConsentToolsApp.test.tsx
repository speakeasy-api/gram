import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { StreamableHTTPError } from "@modelcontextprotocol/sdk/client/streamableHttp.js";

import {
  CHAIN_REJECTED_EVENT,
  ConsentToolsApp,
  type ConsentToolsAppProps,
} from "./ConsentToolsApp";
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

function renderApp(
  overrides: Partial<ConsentToolsAppProps> = {},
  agentSelected = false,
) {
  const button = document.createElement("button");
  button.id = "approve-btn";
  button.disabled = true;
  button.dataset["agentSelected"] = agentSelected ? "true" : "false";
  document.body.appendChild(button);
  const props: ConsentToolsAppProps = {
    toolsUrl: "/mcp/example/connect/mcp",
    state: "state-1",
    csrfToken: "csrf-1",
    formId: "consent-approve-form",
    approveButtonId: "approve-btn",
    consentEnabled: true,
    inventoryDeferred: false,
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
  await screen.findByRole("button", { name: /All/ });
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
  document.documentElement.removeAttribute("data-consent-chain-rejected");
  vi.clearAllMocks();
});

describe("ConsentToolsApp", () => {
  it("performs an MCP session with the consent headers and enables approval", async () => {
    const { button } = renderApp();
    await waitFor(() => expect(button.disabled).toBe(false));
    expect(button.dataset["consentSelfReady"]).toBe("true");

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

  it("does not override connection readiness while an agent is selected", async () => {
    listTools.mockReturnValue(new Promise(() => {}));

    const { button } = renderApp({}, true);

    await waitFor(() =>
      expect(button.dataset["consentSelfReady"]).toBe("false"),
    );
    expect(button.disabled).toBe(true);
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

  it("falls back to all tools when the inventory cannot be captured", async () => {
    connect.mockRejectedValue(new StreamableHTTPError(422, "too many tools"));
    const { button } = renderApp();
    await screen.findByText(/too many tools to choose from here/);
    expect(button.disabled).toBe(false);
    expect(formFieldValues("tool_filtering")).toEqual(["off"]);
    expect(formFieldValues("tool_inventory_id")).toEqual([]);
  });

  it("asks before widening a stored selection the inventory cannot honor", async () => {
    connect.mockRejectedValue(new StreamableHTTPError(422, "too many tools"));
    const user = userEvent.setup();
    const { button } = renderApp({
      prefill: { annotations: [], tools: ["get_thing"] },
    });
    await screen.findByText(/too many tools to choose from here/);
    expect(button.disabled).toBe(true);
    expect(formFieldValues("tool_filtering")).toEqual([]);

    await user.click(
      screen.getByRole("button", { name: "Give access to all tools" }),
    );
    expect(button.disabled).toBe(false);
    expect(formFieldValues("tool_filtering")).toEqual(["off"]);
  });

  it("lets the user give access with all tools after a failed load", async () => {
    listTools.mockRejectedValue(new Error("boom"));
    const user = userEvent.setup();
    const { button } = renderApp();
    await user.click(
      await screen.findByRole("button", { name: "Give access to all tools" }),
    );
    expect(button.disabled).toBe(false);
    expect(formFieldValues("tool_filtering")).toEqual(["off"]);
    expect(formFieldValues("tool_inventory_id")).toEqual([]);
  });

  it("gives access with all tools when a chained service defers the inventory", async () => {
    connect.mockRejectedValue(new StreamableHTTPError(409, "not connected"));
    const { button } = renderApp({ inventoryDeferred: true });
    await screen.findByText(/Tools load after you give access/);
    expect(button.disabled).toBe(false);
    expect(button.dataset["consentSelfReady"]).toBe("true");
    expect(screen.getByRole("button", { name: "Load tools" })).toBeTruthy();
    expect(screen.queryByText("Retry")).toBeNull();
    expect(formFieldValues("tool_filtering")).toEqual(["off"]);
    expect(formFieldValues("tool_inventory_id")).toEqual([]);
  });

  it("never defers over a stored narrower selection", async () => {
    connect.mockRejectedValue(new StreamableHTTPError(409, "not connected"));
    const { button } = renderApp({
      inventoryDeferred: true,
      prefill: { annotations: [], tools: ["get_thing"] },
    });
    await screen.findByText("The upstream service is not connected.");
    expect(button.disabled).toBe(true);
    expect(formFieldValues("tool_filtering")).toEqual([]);
  });

  it("blocks approval once every chained service is refused", async () => {
    connect.mockRejectedValue(new StreamableHTTPError(409, "not connected"));
    const { button } = renderApp({ inventoryDeferred: true });
    await screen.findByText(/Tools load after you give access/);
    expect(button.disabled).toBe(false);

    document.documentElement.setAttribute(
      "data-consent-chain-rejected",
      "true",
    );
    document.dispatchEvent(new CustomEvent(CHAIN_REJECTED_EVENT));

    await screen.findByText(/couldn't connect this service/);
    expect(button.disabled).toBe(true);
    expect(formFieldValues("tool_filtering")).toEqual([]);
  });

  it("starts blocked when the refusal settled before the island mounted", async () => {
    document.documentElement.setAttribute(
      "data-consent-chain-rejected",
      "true",
    );
    connect.mockRejectedValue(new StreamableHTTPError(409, "not connected"));
    const { button } = renderApp({ inventoryDeferred: true });
    await screen.findByText(/couldn't connect this service/);
    expect(button.disabled).toBe(true);
  });

  it("keeps a not-connected inventory blocking when nothing is chained", async () => {
    connect.mockRejectedValue(new StreamableHTTPError(409, "not connected"));
    const { button } = renderApp();
    await screen.findByText("The upstream service is not connected.");
    expect(button.disabled).toBe(true);
    expect(formFieldValues("tool_filtering")).toEqual([]);
  });

  it("keeps other failures blocking even when the inventory may be deferred", async () => {
    connect.mockRejectedValue(new StreamableHTTPError(503, "unavailable"));
    const { button } = renderApp({ inventoryDeferred: true });
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

  it("warns, without blocking, when specific tools grants none", async () => {
    const user = userEvent.setup();
    const { button } = renderApp();
    await openPicker(user);
    const note = /No tools selected/;
    expect(screen.getByText(note)).toBeTruthy();
    expect(button.disabled).toBe(false);

    await user.click(screen.getByRole("checkbox", { name: /get_thing/ }));
    expect(screen.queryByText(note)).toBeNull();
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

  it("a group covered by picks shows checked and clears without a grant", async () => {
    const user = userEvent.setup();
    renderApp();
    await openPicker(user);
    await user.click(screen.getByRole("checkbox", { name: /get_thing/ }));
    await user.click(screen.getByRole("button", { name: /Read/ }));
    const selectAll = screen.getByRole("checkbox", { name: /Select all 1/ });
    expect(selectAll.getAttribute("aria-checked")).toBe("true");

    await user.click(selectAll);
    expect(formFieldValues("tools")).toEqual([]);
    expect(formFieldValues("tool_annotations")).toEqual([]);
    expect(formFieldValues("tool_annotations_live")).toEqual([]);
  });

  it("navigating groups filters the list without granting anything", async () => {
    const user = userEvent.setup();
    renderApp();
    await openPicker(user);
    await user.click(screen.getByRole("button", { name: /Read/ }));
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
    await user.click(screen.getByRole("button", { name: /Read/ }));
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
    await user.click(screen.getByRole("button", { name: /Read/ }));
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
    await user.click(screen.getByRole("button", { name: /Read/ }));
    await user.click(screen.getByRole("checkbox", { name: /Select all 1/ }));
    await user.click(screen.getByRole("button", { name: /All/ }));
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
    await user.click(screen.getByRole("button", { name: /Read/ }));
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
    await user.click(screen.getByRole("button", { name: /Read/ }));
    await user.click(screen.getByRole("checkbox", { name: /Select all 2/ }));
    expect(formFieldValues("tool_annotations_live")).toEqual(["read_only"]);

    await user.click(screen.getByRole("button", { name: /All/ }));
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
    await user.click(screen.getByRole("button", { name: /Unannotated/ }));
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
  it("groups a gateway's tools by source and selects a source at once", async () => {
    listTools.mockResolvedValue({
      tools: [
        {
          name: "github--create_issue",
          _meta: { "gram.dev/source": "GitHub" },
        },
        {
          name: "github--list_issues",
          annotations: { readOnlyHint: true },
          _meta: { "gram.dev/source": "GitHub" },
        },
        { name: "docs--search", _meta: { "gram.dev/source": "Docs" } },
      ],
      _meta: { "gram.dev/unavailableSources": ["Linear", "Slack"] },
      nextCursor: undefined,
    });
    const user = userEvent.setup();
    renderApp({ serverName: "gateway" });
    await openPicker(user);

    expect(
      screen.getByText(/Linear and Slack aren't connected yet/),
    ).toBeTruthy();
    expect(screen.getByRole("group", { name: "GitHub" })).toBeTruthy();
    expect(screen.getByRole("group", { name: "Docs" })).toBeTruthy();
    expect(screen.getByText("create_issue")).toBeTruthy();
    expect(screen.queryByText("github--create_issue")).toBeNull();

    await user.click(screen.getByRole("checkbox", { name: /GitHub/ }));
    expect(formFieldValues("tools")).toEqual([
      "github--create_issue",
      "github--list_issues",
    ]);

    await user.type(screen.getByRole("searchbox"), "docs");
    expect(screen.queryByRole("group", { name: "GitHub" })).toBeNull();
    await user.click(screen.getByText("search"));
    expect(formFieldValues("tools")).toEqual([
      "docs--search",
      "github--create_issue",
      "github--list_issues",
    ]);
  });
  it("shows partially selected sources as mixed and counts scope once", async () => {
    listTools.mockResolvedValue({
      tools: [
        {
          name: "github--create_issue",
          _meta: { "gram.dev/source": "GitHub" },
        },
        {
          name: "github--list_issues",
          _meta: { "gram.dev/source": "GitHub" },
        },
        { name: "docs--search", _meta: { "gram.dev/source": "Docs" } },
      ],
      nextCursor: undefined,
    });
    const user = userEvent.setup();
    renderApp({ serverName: "gateway" });
    await openPicker(user);

    await user.click(screen.getByText("create_issue"));
    expect(
      screen
        .getByRole("checkbox", { name: /GitHub/ })
        .getAttribute("aria-checked"),
    ).toBe("mixed");
    expect(
      screen
        .getByRole("checkbox", { name: /Docs/ })
        .getAttribute("aria-checked"),
    ).toBe("false");
    expect(
      screen
        .getByRole("checkbox", { name: /Select all 3/ })
        .getAttribute("aria-checked"),
    ).toBe("mixed");
    const footer = screen.getByText(
      /of 3 tools in scope on gateway/,
    ).textContent;
    expect(footer).toContain("1 of 3 tools in scope on gateway");
    expect(footer).not.toMatch(/picked/);
  });
  it("keeps another source's tools when clearing a source covered by a grant", async () => {
    listTools.mockResolvedValue({
      tools: [
        {
          name: "github--list_issues",
          annotations: { readOnlyHint: true },
          _meta: { "gram.dev/source": "GitHub" },
        },
        {
          name: "docs--search",
          annotations: { readOnlyHint: true },
          _meta: { "gram.dev/source": "Docs" },
        },
        { name: "docs--write", _meta: { "gram.dev/source": "Docs" } },
      ],
      nextCursor: undefined,
    });
    const user = userEvent.setup();
    renderApp({ serverName: "gateway" });
    await openPicker(user);
    await user.click(screen.getByRole("button", { name: /Read/ }));
    await user.click(screen.getByRole("checkbox", { name: /Select all 2/ }));
    expect(formFieldValues("tool_annotations_live")).toEqual(["read_only"]);

    await user.click(screen.getByRole("checkbox", { name: /GitHub/ }));
    expect(formFieldValues("tool_annotations_live")).toEqual([]);
    expect(formFieldValues("tools")).toEqual(["docs--search"]);
  });

  it("clearing one annotation group keeps an overlapping grant", async () => {
    listTools.mockResolvedValue({
      tools: [
        {
          name: "get_thing",
          annotations: { readOnlyHint: true, idempotentHint: true },
        },
        { name: "put_thing", annotations: { idempotentHint: true } },
      ],
      nextCursor: undefined,
    });
    const user = userEvent.setup();
    renderApp({
      prefill: {
        annotations: [
          { name: "idempotent", mode: "live" },
          { name: "read_only", mode: "live" },
        ],
        tools: [],
      },
    });
    await user.click(await screen.findByRole("button", { name: /Read/ }));

    await user.click(screen.getByRole("checkbox", { name: /Select all 1/ }));
    expect(formFieldValues("tool_annotations_live")).toEqual(["idempotent"]);
  });

  it("keeps members that share a display name in separate sections", async () => {
    listTools.mockResolvedValue({
      tools: [
        {
          name: "github-prod--create_issue",
          _meta: { "gram.dev/source": "GitHub" },
        },
        {
          name: "github-sandbox--create_issue",
          _meta: { "gram.dev/source": "GitHub" },
        },
        { name: "docs--search", _meta: { "gram.dev/source": "Docs" } },
      ],
      nextCursor: undefined,
    });
    const user = userEvent.setup();
    renderApp({ serverName: "gateway" });
    await openPicker(user);

    expect(
      screen.getByRole("group", { name: "GitHub (github-prod)" }),
    ).toBeTruthy();
    expect(screen.getByRole("group", { name: "Docs" })).toBeTruthy();
    await user.click(
      screen.getByRole("checkbox", { name: /GitHub \(github-sandbox\)/ }),
    );
    expect(formFieldValues("tools")).toEqual(["github-sandbox--create_issue"]);
  });
});
