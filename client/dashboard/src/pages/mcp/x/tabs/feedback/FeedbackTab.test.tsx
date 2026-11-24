import type { ListMcpFeedbackResult } from "@gram/client/models/components/listmcpfeedbackresult.js";
import type { McpFeedback } from "@gram/client/models/components/mcpfeedback.js";
import type { McpFeedbackSettings } from "@gram/client/models/components/mcpfeedbacksettings.js";
import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import { GramError } from "@gram/client/models/errors/gramerror.js";
import { invalidateAllMcpFeedbackSettings } from "@gram/client/react-query/mcpFeedbackSettings.js";
import { unrecognized } from "@gram/client/types/unrecognized.js";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import { TooltipProvider } from "@/components/ui/Tooltip";
import { MemoryRouter } from "react-router";
import { toast } from "sonner";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { FeedbackTab } from "./FeedbackTab";

const state = vi.hoisted(() => ({
  settings: undefined as unknown,
  canEdit: true,
  canOpenOrganizationPages: true,
  update: vi.fn(),
  refetchReports: vi.fn(),
  // Every request the report list made, newest last.
  listRequests: [] as Array<Record<string, unknown>>,
  // undefined with listFailed false is a list still loading.
  result: undefined as unknown,
  listFailed: false,
}));

vi.mock("@tanstack/react-query", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@tanstack/react-query")>()),
  useQueryClient: () => ({}),
}));
vi.mock("sonner", () => ({
  toast: { success: vi.fn(), error: vi.fn() },
}));
vi.mock("@/hooks/useRBAC", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/hooks/useRBAC")>()),
  useRBAC: () => ({
    hasScope: (scope: string) => scope !== "mcp:write" || state.canEdit,
    hasAnyScope: () => state.canOpenOrganizationPages,
  }),
}));
vi.mock("@/contexts/Auth", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/contexts/Auth")>()),
  useOrganization: () => ({ id: "org-1", slug: "acme" }),
}));
vi.mock("@/routes", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/routes")>()),
  useOrgRoutes: () => ({
    dataExports: { href: () => "/acme/data/exports" },
    webhooks: { href: () => "/acme/webhooks" },
  }),
}));
vi.mock("@gram/client/react-query/mcpFeedbackSettings.js", () => ({
  useMcpFeedbackSettings: () => ({
    data: state.settings,
    isPending: false,
    refetch: vi.fn(),
  }),
  setMcpFeedbackSettingsData: vi.fn(),
  invalidateAllMcpFeedbackSettings: vi.fn(),
}));
vi.mock("@gram/client/react-query/updateMcpFeedbackSettings.js", () => ({
  useUpdateMcpFeedbackSettingsMutation: () => ({
    mutateAsync: state.update,
    isPending: false,
  }),
}));
vi.mock("@gram/client/react-query/mcpFeedback.js", () => ({
  useMcpFeedbackInfinite: (request: Record<string, unknown>) => {
    state.listRequests.push(request);
    return {
      data: state.result ? { pages: [{ result: state.result }] } : undefined,
      isPending: state.result === undefined && !state.listFailed,
      isFetching: false,
      isFetchingNextPage: false,
      isFetchNextPageError: false,
      hasNextPage: false,
      refetch: state.refetchReports,
      fetchNextPage: vi.fn(),
    };
  },
}));

const DEFAULT_NAME = "submit_feedback";
const DEFAULT_INSTRUCTIONS = "Report a concrete problem with this server.";
const INPUT_SCHEMA = JSON.stringify({
  type: "object",
  properties: {
    content: { type: "string", description: "A short note." },
    severity: { type: "string", description: "Impact on your task." },
  },
  required: ["content"],
});

const STORED_VERSION = "2026-11-16T09:00:00.123456Z";

function settings(
  overrides: Partial<McpFeedbackSettings> = {},
): McpFeedbackSettings {
  const toolName = overrides.toolName ?? DEFAULT_NAME;
  const toolDescription = overrides.toolDescription ?? DEFAULT_INSTRUCTIONS;
  return {
    mcpServerId: "server-1",
    enabled: false,
    defaultToolName: DEFAULT_NAME,
    defaultToolDescription: DEFAULT_INSTRUCTIONS,
    tool: {
      name: toolName,
      title: "Submit feedback",
      description: toolDescription,
      inputSchema: INPUT_SCHEMA,
    },
    // Stored settings carry a version finer than a Date can hold.
    version: overrides.updatedAt ? STORED_VERSION : "",
    ...overrides,
  };
}

function report(overrides: Partial<McpFeedback> = {}): McpFeedback {
  return {
    id: "report-1",
    mcpServerId: "server-1",
    content: "process_refund rejects every amount.",
    createdAt: new Date("2026-11-19T10:00:00Z"),
    ...overrides,
  };
}

function result(
  overrides: Partial<ListMcpFeedbackResult> = {},
): ListMcpFeedbackResult {
  return {
    counts: {
      total: 3,
      blocked: 1,
      degraded: 1,
      cosmetic: 0,
      unspecified: 1,
      windowStart: new Date("2026-10-21T00:00:00Z"),
      windowEnd: new Date("2026-11-19T12:00:00Z"),
    },
    tools: [
      { toolName: "search_logs", feedbackCount: 2, blockedCount: 0 },
      { toolName: "process_refund", feedbackCount: 1, blockedCount: 1 },
    ],
    feedback: [
      report({
        severity: "blocked",
        toolName: "process_refund",
        intention: "Refund an order in full.",
        workaround: "Told the user to use the billing console.",
        userEmail: "hana@example.com",
        clientName: "claude-code",
        clientVersion: "2.1.34",
        externalUserId: "idp|hana",
      }),
      report({
        id: "report-2",
        content: "fetch_traces is a pleasure to use.",
        toolName: "fetch_traces",
      }),
    ],
    ...overrides,
  };
}

const server = { id: "server-1", name: "Example server" } as McpServer;

function renderTab(initialEntry = "/") {
  return render(
    <MemoryRouter initialEntries={[initialEntry]}>
      <TooltipProvider>
        <FeedbackTab mcpServer={server} />
      </TooltipProvider>
    </MemoryRouter>,
  );
}

/** The stat tile with this label. */
function tile(label: string): HTMLElement {
  const found = screen
    .getAllByRole("group")
    .find((group) => within(group).queryByText(label) !== null);
  if (!found) throw new Error(`no tile labelled ${label}`);
  return found;
}

function lastListRequest(): Record<string, unknown> {
  const request = state.listRequests.at(-1);
  if (!request) throw new Error("the report list was never requested");
  return request;
}

beforeEach(() => {
  state.settings = settings();
  state.canEdit = true;
  state.canOpenOrganizationPages = true;
  state.update.mockReset();
  state.update.mockImplementation(
    async ({
      request,
    }: {
      request: { updateMcpFeedbackSettingsForm: { enabled: boolean } };
    }) => settings({ enabled: request.updateMcpFeedbackSettingsForm.enabled }),
  );
  state.refetchReports.mockReset();
  state.listRequests = [];
  state.result = undefined;
  state.listFailed = false;
});

afterEach(cleanup);

describe("a server feedback was never set up on", () => {
  it("invites the editor to turn it on and asks for no reports", () => {
    renderTab();

    expect(
      screen.getByRole("heading", { name: "Feedback is off" }),
    ).toBeTruthy();
    expect(
      screen.getByText(/Turn it on to hear from the agents using this server/),
    ).toBeTruthy();
    expect(
      screen.getByRole("button", { name: "Turn on feedback" }),
    ).toBeTruthy();
    expect(state.listRequests).toHaveLength(0);
  });

  it("turns feedback on with nothing to fill in", async () => {
    renderTab();

    fireEvent.click(screen.getByRole("button", { name: "Turn on feedback" }));

    await waitFor(() => expect(state.update).toHaveBeenCalledOnce());
    expect(state.update).toHaveBeenCalledWith({
      request: {
        updateMcpFeedbackSettingsForm: {
          mcpServerId: "server-1",
          enabled: true,
          toolName: undefined,
          toolDescription: undefined,
          expectedVersion: "",
        },
      },
    });
  });

  it("shows a reader the invitation without the controls", () => {
    state.canEdit = false;
    renderTab();

    expect(
      screen.queryByRole("button", { name: "Turn on feedback" }),
    ).toBeNull();
    expect(screen.queryByRole("button", { name: "Customize" })).toBeNull();
    expect(
      screen.getByText("Only people who can edit this server can change this."),
    ).toBeTruthy();
  });
});

describe("a server with feedback on", () => {
  beforeEach(() => {
    state.settings = settings({
      enabled: true,
      toolName: "report_a_problem",
      toolDescription: "Tell the platform team what went wrong.",
      updatedAt: new Date("2026-11-16T09:00:00Z"),
      updatedByEmail: "lucas@example.com",
    });
    state.result = result();
  });

  it("names the tool agents see and who changed it last", () => {
    renderTab();

    expect(
      screen.getByRole("heading", { name: "Feedback is on" }),
    ).toBeTruthy();
    expect(screen.getByText("report_a_problem")).toBeTruthy();
    expect(screen.getByText(/Changed by lucas@example\.com/)).toBeTruthy();
  });

  it("keeps the tool's own name and instructions when it is switched off", async () => {
    renderTab();

    fireEvent.click(screen.getByRole("button", { name: "Turn off" }));

    await waitFor(() => expect(state.update).toHaveBeenCalledOnce());
    expect(state.update).toHaveBeenCalledWith({
      request: {
        updateMcpFeedbackSettingsForm: {
          mcpServerId: "server-1",
          enabled: false,
          toolName: "report_a_problem",
          toolDescription: "Tell the platform team what went wrong.",
          expectedVersion: STORED_VERSION,
        },
      },
    });
  });

  it("shows the last 30 days, both rankings and each report in full", () => {
    renderTab();

    expect(screen.getByText("Last 30 days")).toBeTruthy();
    expect(within(tile("Reports")).getByText("3")).toBeTruthy();
    expect(within(tile("Blocked")).getByText("1")).toBeTruthy();
    // Severity is optional, so the total says where the rest of it went.
    expect(
      within(tile("Reports")).getByText("1 of them gave no severity"),
    ).toBeTruthy();
    const ranking = screen
      .getByText("Tools agents reported most")
      .closest("div");
    if (!ranking) throw new Error("ranking card not found");
    expect(within(ranking).getByTitle("1 blocked")).toBeTruthy();

    // The list, and what opening a report adds to it.
    expect(
      screen.getAllByText("process_refund rejects every amount.").length,
    ).toBeGreaterThan(0);
    expect(screen.getByText("Refund an order in full.")).toBeTruthy();
    expect(
      screen.getByText("Told the user to use the billing console."),
    ).toBeTruthy();
    expect(
      screen.getByText("hana@example.com, through claude-code 2.1.34"),
    ).toBeTruthy();
    // Identifiers stay folded away until asked for.
    expect(screen.queryByText("idp|hana")).toBeNull();
    const [technicalDetails] = screen.getAllByRole("button", {
      name: "Technical details",
    });
    if (!technicalDetails) throw new Error("no technical details to open");
    fireEvent.click(technicalDetails);
    expect(screen.getByText("idp|hana")).toBeTruthy();
    // A report with no severity carries no badge of one.
    expect(screen.getAllByText("Blocked")).toHaveLength(2);
  });

  it("narrows the list to a tool picked from the ranking", () => {
    renderTab();
    expect(lastListRequest()).toMatchObject({
      mcpServerId: "server-1",
      severity: undefined,
      toolName: undefined,
    });

    const ranking = screen
      .getByText("Tools agents reported most")
      .closest("div");
    if (!ranking) throw new Error("ranking card not found");
    fireEvent.click(
      within(ranking).getByRole("button", { name: "search_logs" }),
    );

    expect(lastListRequest()).toMatchObject({
      toolName: "search_logs",
      severity: undefined,
    });
  });

  it("offers a way back when the filters match nothing", () => {
    state.result = result({ feedback: [] });
    renderTab("/?severity=cosmetic&tool=process_refund");
    expect(lastListRequest()).toMatchObject({
      severity: "cosmetic",
      toolName: "process_refund",
    });
    expect(screen.getByText("No reports match these filters.")).toBeTruthy();

    fireEvent.click(screen.getByRole("button", { name: "Clear filters" }));

    expect(lastListRequest()).toMatchObject({
      severity: undefined,
      toolName: undefined,
    });
  });

  it("ignores a severity in the link that it does not know", () => {
    renderTab("/?severity=catastrophic");

    expect(lastListRequest()).toMatchObject({ severity: undefined });
  });

  it("shows a severity it does not know as it arrived", () => {
    state.result = result({
      feedback: [report({ severity: unrecognized("catastrophic") })],
    });
    renderTab();

    expect(screen.getByText("catastrophic")).toBeTruthy();
  });

  it("shows an agent's name without the control characters it sent", () => {
    state.result = result({
      feedback: [
        report({ clientName: "good\u202Eagent\u200B", clientVersion: "1.0" }),
      ],
    });
    renderTab();

    expect(screen.getAllByText("good agent 1.0").length).toBeGreaterThan(0);
  });

  it("links to exports and webhooks beside the reports", () => {
    renderTab();

    expect(
      screen
        .getByRole("link", { name: "your observability stack" })
        .getAttribute("href"),
    ).toBe("/acme/data/exports");
    expect(
      screen.getByRole("link", { name: "a webhook" }).getAttribute("href"),
    ).toBe("/acme/webhooks");
  });

  it("leaves those links out for people who cannot open the pages", () => {
    state.canOpenOrganizationPages = false;
    renderTab();

    expect(
      screen.queryByRole("link", { name: "your observability stack" }),
    ).toBeNull();
    expect(screen.queryByRole("link", { name: "a webhook" })).toBeNull();
  });

  it("says there is nothing yet when no agent has sent a report", () => {
    state.result = result({
      counts: {
        total: 0,
        blocked: 0,
        degraded: 0,
        cosmetic: 0,
        unspecified: 0,
        windowStart: new Date("2026-10-21T00:00:00Z"),
        windowEnd: new Date("2026-11-19T12:00:00Z"),
      },
      tools: [],
      feedback: [],
    });
    renderTab();

    expect(
      screen.getByRole("heading", { name: "No reports yet" }),
    ).toBeTruthy();
    expect(screen.queryByText("Last 30 days")).toBeNull();
    // Where reports can also go is the one thing left to set up.
    expect(
      screen.getByRole("link", { name: "your observability stack" }),
    ).toBeTruthy();
  });

  it("offers a retry when the reports cannot be loaded", () => {
    state.result = undefined;
    state.listFailed = true;
    renderTab();

    fireEvent.click(screen.getByRole("button", { name: "Retry" }));

    expect(
      screen.getByRole("heading", { name: "Couldn't load the reports" }),
    ).toBeTruthy();
    expect(state.refetchReports).toHaveBeenCalledOnce();
  });
});

describe("a server with feedback switched off", () => {
  it("keeps showing the reports it collected", () => {
    state.settings = settings({
      enabled: false,
      updatedAt: new Date("2026-11-16T09:00:00Z"),
    });
    state.result = result();
    renderTab();

    expect(
      screen.getByRole("heading", { name: "Feedback is off" }),
    ).toBeTruthy();
    expect(screen.getByText("Tools agents reported most")).toBeTruthy();
    // Nothing new will arrive, so there is nowhere new to send it.
    expect(
      screen.queryByRole("link", { name: "your observability stack" }),
    ).toBeNull();
  });

  it("shows nothing under the switch when it never collected any", () => {
    state.settings = settings({
      enabled: false,
      updatedAt: new Date("2026-11-16T09:00:00Z"),
    });
    state.result = result({
      counts: {
        total: 0,
        blocked: 0,
        degraded: 0,
        cosmetic: 0,
        unspecified: 0,
        windowStart: new Date("2026-10-21T00:00:00Z"),
        windowEnd: new Date("2026-11-19T12:00:00Z"),
      },
      tools: [],
      feedback: [],
    });
    renderTab();

    expect(
      screen.queryByRole("heading", { name: "No reports yet" }),
    ).toBeNull();
    expect(screen.queryByText("Last 30 days")).toBeNull();
  });
});

describe("customizing the feedback tool", () => {
  beforeEach(() => {
    state.settings = settings({
      enabled: true,
      updatedAt: new Date("2026-11-16T09:00:00Z"),
    });
    state.result = result();
  });

  function openSheet() {
    renderTab();
    fireEvent.click(screen.getByRole("button", { name: "Customize" }));
    return {
      name: screen.getByLabelText("Tool name"),
      instructions: screen.getByLabelText("Instructions for agents"),
      save: screen.getByRole("button", { name: "Save" }),
    };
  }

  it("starts from the defaults and has nothing to save until something changes", () => {
    const { name, instructions, save } = openSheet();

    expect(name).toHaveProperty("value", DEFAULT_NAME);
    expect(instructions).toHaveProperty("value", DEFAULT_INSTRUCTIONS);
    expect(save).toHaveProperty("disabled", true);
  });

  it("refuses a name agents could not call", () => {
    const { name, save } = openSheet();

    fireEvent.change(name, { target: { value: "report a problem!" } });

    expect(
      screen.getByText("Use only letters, numbers, underscores, and hyphens."),
    ).toBeTruthy();
    expect(save).toHaveProperty("disabled", true);
  });

  it("refuses a name Speakeasy already uses on every server", () => {
    const { name, save } = openSheet();

    fireEvent.change(name, { target: { value: "search_tools" } });

    expect(
      screen.getByText(
        "Speakeasy already uses this name on every server. Pick another.",
      ),
    ).toBeTruthy();
    expect(save).toHaveProperty("disabled", true);
  });

  it("says so and closes when someone else changed the settings first", async () => {
    state.update.mockRejectedValueOnce(
      new GramError("conflict", {
        response: new Response(null, { status: 409 }),
        request: new Request(
          "https://app.getgram.ai/rpc/mcpFeedback.updateSettings",
        ),
        body: "",
      }),
    );
    const { name, save } = openSheet();

    fireEvent.change(name, { target: { value: "report_a_problem" } });
    fireEvent.click(save);

    await waitFor(() => expect(toast.error).toHaveBeenCalledOnce());
    expect(vi.mocked(toast.error).mock.calls[0]?.[1]).toMatchObject({
      description: expect.stringContaining(
        "Someone else changed these settings",
      ),
    });
    expect(invalidateAllMcpFeedbackSettings).toHaveBeenCalled();
    await waitFor(() =>
      expect(screen.queryByRole("button", { name: "Save" })).toBeNull(),
    );
  });

  it("saves a new name and leaves the instructions at their default", async () => {
    const { name, save } = openSheet();

    fireEvent.change(name, { target: { value: "report_a_problem" } });
    fireEvent.click(save);

    await waitFor(() => expect(state.update).toHaveBeenCalledOnce());
    expect(state.update).toHaveBeenCalledWith({
      request: {
        updateMcpFeedbackSettingsForm: {
          mcpServerId: "server-1",
          enabled: true,
          toolName: "report_a_problem",
          toolDescription: undefined,
          expectedVersion: STORED_VERSION,
        },
      },
    });
  });

  it("goes back to the default name on request", () => {
    state.settings = settings({
      enabled: true,
      toolName: "report_a_problem",
      updatedAt: new Date("2026-11-16T09:00:00Z"),
    });
    const { name } = openSheet();
    expect(name).toHaveProperty("value", "report_a_problem");

    fireEvent.click(screen.getByRole("button", { name: "Use the default" }));

    expect(name).toHaveProperty("value", DEFAULT_NAME);
  });

  it("counts instructions in characters, as the server does", () => {
    const { instructions, save } = openSheet();

    // 1,500 emoji are 3,000 UTF-16 units but 1,500 characters.
    fireEvent.change(instructions, { target: { value: "🙂".repeat(1500) } });

    expect(screen.getByText(/1,500 of 2,000 characters/)).toBeTruthy();
    expect(save).toHaveProperty("disabled", false);
  });

  it("asks before throwing away an edit", () => {
    const { name } = openSheet();
    fireEvent.change(name, { target: { value: "report_a_problem" } });

    fireEvent.click(screen.getByRole("button", { name: "Cancel" }));

    expect(
      screen.getByRole("heading", { name: "Discard your changes?" }),
    ).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Keep editing" }));
    expect(screen.getByLabelText("Tool name")).toHaveProperty(
      "value",
      "report_a_problem",
    );
  });

  it("lists what a report can hold, from the tool's schema", () => {
    openSheet();

    fireEvent.click(screen.getByRole("button", { name: "Technical details" }));

    expect(screen.getByText("A short note.")).toBeTruthy();
    expect(screen.getByText("Impact on your task.")).toBeTruthy();
  });
});
