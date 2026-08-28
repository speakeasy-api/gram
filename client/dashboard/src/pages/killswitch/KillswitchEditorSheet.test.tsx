import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter } from "react-router";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { KillswitchDetail } from "@gram/client/models/components/killswitchdetail.js";
import { KillswitchEditorSheet } from "./KillswitchEditorSheet";

vi.mock("@/components/FeatureRequestModal", () => ({
  FeatureRequestModal: ({ isOpen }: { isOpen: boolean }) =>
    isOpen ? <div>Capability request opened</div> : null,
}));

afterEach(cleanup);

const members = [
  {
    id: "user-1",
    name: "Alex Morgan",
    email: "alex@example.test",
    joinedAt: new Date(),
    principalUrn: "user:user-1",
    roleIds: [],
  },
];
const servers = [
  { id: "server-a", name: "Server A", projectId: "project-1" },
  { id: "server-b", name: "Server B", projectId: "project-2" },
  { id: "server-c", name: "Server C", projectId: "project-2" },
];
const capabilities = [
  { key: "mcp_tool_calls" as const, label: "MCP tool calls" },
];
const previewResult = { overlaps: [], truncated: false };

function renderEditor(
  overrides: Partial<React.ComponentProps<typeof KillswitchEditorSheet>> = {},
) {
  const props: React.ComponentProps<typeof KillswitchEditorSheet> = {
    open: true,
    onOpenChange: () => {},
    mode: "create",
    members,
    servers,
    capabilities,
    comingSoon: [{ label: "More capabilities" }],
    onPreview: vi.fn().mockResolvedValue(previewResult),
    onSubmit: vi
      .fn()
      .mockResolvedValue({ id: "ks-1", version: 1, replayed: false }),
    ...overrides,
  };
  const view = render(
    <MemoryRouter>
      <KillswitchEditorSheet {...props} />
    </MemoryRouter>,
  );
  return { ...props, componentProps: props, ...view };
}

describe("KillswitchEditorSheet", () => {
  it("has no default resource scope and shows focused validation errors", async () => {
    renderEditor();
    expect(
      (screen.getByLabelText(/All MCP servers/) as HTMLInputElement).checked,
    ).toBe(false);
    expect(
      (screen.getByLabelText(/Selected servers/) as HTMLInputElement).checked,
    ).toBe(false);

    await userEvent.click(
      screen.getByRole("button", { name: "Turn off MCP tool calls" }),
    );
    expect(await screen.findByText("Choose one team member.")).not.toBeNull();
    expect(screen.getByText("Choose one capability.")).not.toBeNull();
    expect(screen.getByText("Choose an MCP server scope.")).not.toBeNull();
    expect(screen.getAllByText("This note is required.")).toHaveLength(2);
    await waitFor(() =>
      expect(document.activeElement).toBe(screen.getByLabelText("Team member")),
    );
    expect(screen.getByText(/Shown exactly as plain text/)).not.toBeNull();
    expect(
      screen.getByText("Visible only to organization admins."),
    ).not.toBeNull();
  });

  it("keeps a contextual member fixed and applies a pending server only after Selected servers", async () => {
    const view = renderEditor({
      createContext: {
        userId: "user-1",
        capabilityKey: "mcp_tool_calls",
        originatingMcpServerId: "server-a",
      },
    });

    expect(screen.queryByLabelText("Team member")).toBeNull();
    expect(screen.getByText(/Alex Morgan — alex@example.test/)).not.toBeNull();
    expect(
      (screen.getByLabelText("MCP tool calls") as HTMLInputElement).checked,
    ).toBe(true);
    expect(
      (screen.getByLabelText(/All MCP servers/) as HTMLInputElement).checked,
    ).toBe(false);
    expect(
      (screen.getByLabelText(/Selected servers/) as HTMLInputElement).checked,
    ).toBe(false);
    expect(screen.queryByText("Server A")).toBeNull();

    await userEvent.click(screen.getByLabelText(/Selected servers/));
    expect(
      screen.getByRole("button", { name: "Choose servers (1)" }),
    ).not.toBeNull();
    expect(screen.getByText("Server A")).not.toBeNull();
    await userEvent.type(
      screen.getByLabelText("Public message shown to the member"),
      "Access is temporarily paused.",
    );
    await userEvent.type(
      screen.getByLabelText("Internal note"),
      "Incident response.",
    );
    await userEvent.click(
      screen.getByRole("button", { name: "Review impact" }),
    );
    expect(view.onPreview).toHaveBeenLastCalledWith(
      expect.objectContaining({
        userId: "user-1",
        capabilityKey: "mcp_tool_calls",
        scopeType: "selected_servers",
        serverIds: ["server-a"],
      }),
    );

    await userEvent.click(screen.getByLabelText(/All MCP servers/));
    await userEvent.click(
      screen.getByRole("button", { name: "Review impact" }),
    );
    expect(view.onPreview).toHaveBeenLastCalledWith(
      expect.objectContaining({
        scopeType: "all_servers",
        serverIds: [],
      }),
    );
  });

  it("keeps coming-soon capabilities non-selectable and opens the request path", async () => {
    renderEditor();
    expect(screen.getByText("More capabilities — Coming soon")).not.toBeNull();
    await userEvent.click(
      screen.getByRole("button", { name: "Request a capability" }),
    );
    expect(screen.getByText("Capability request opened")).not.toBeNull();
  });

  it("warns with added, unchanged, and removed servers when narrowing", async () => {
    const initial: KillswitchDetail = {
      id: "ks-1",
      userId: "user-1",
      capabilityKey: "mcp_tool_calls",
      capabilityLabel: "MCP tool calls",
      version: 1,
      status: "active",
      scope: {
        type: "selected_servers",
        serverIds: ["server-a", "server-b", "server-c"],
      },
      schedule: { start: "now", end: "until_lifted" },
      externalNote: "Access paused.",
      internalNote: "Incident response.",
      history: [],
      historyTruncated: false,
    };
    renderEditor({ mode: "edit", initial });
    await userEvent.click(
      screen.getByRole("button", { name: /Choose servers/ }),
    );
    await userEvent.click(screen.getByLabelText(/Server B/));
    await userEvent.click(screen.getByLabelText(/Server C/));
    await userEvent.click(
      screen.getByRole("button", { name: "Apply selection" }),
    );
    expect(
      screen.getByText(
        "Narrowing to selected servers reduces Killswitch coverage",
      ),
    ).not.toBeNull();
    expect(
      screen.getByText("Removed servers regain access immediately."),
    ).not.toBeNull();
    expect(screen.getByText("Unchanged: Server A")).not.toBeNull();
    expect(screen.getByText("Removed: Server B, Server C")).not.toBeNull();
  });

  it("warns when all-server scope narrows on a future schedule", async () => {
    renderEditor({
      mode: "edit",
      initial: {
        id: "ks-all",
        userId: "user-1",
        capabilityKey: "mcp_tool_calls",
        capabilityLabel: "MCP tool calls",
        version: 1,
        status: "scheduled",
        scope: { type: "all_servers" },
        schedule: {
          start: "scheduled",
          startsAt: new Date("2099-01-01T00:00:00Z"),
          end: "until_lifted",
        },
        externalNote: "Access paused.",
        internalNote: "Incident response.",
        history: [],
        historyTruncated: false,
      },
    });
    await userEvent.click(screen.getByLabelText(/Selected servers/));
    await userEvent.click(
      screen.getByRole("button", { name: /Choose servers/ }),
    );
    await userEvent.click(screen.getByLabelText(/Server A/));
    await userEvent.click(
      screen.getByRole("button", { name: "Apply selection" }),
    );
    expect(
      screen.getByText(
        "Removed servers remain available when this Killswitch starts.",
      ),
    ).not.toBeNull();
    expect(
      screen.getByText(/Future MCP servers will no longer be covered/),
    ).not.toBeNull();
    expect(screen.getByText("Added: None")).not.toBeNull();
    expect(screen.getByText("Unchanged: Server A")).not.toBeNull();
    expect(screen.getByText("Removed: Server B, Server C")).not.toBeNull();
  });

  it("warns about lost dynamic coverage when every current server is selected", async () => {
    renderEditor({
      mode: "edit",
      initial: {
        id: "ks-all-current",
        userId: "user-1",
        capabilityKey: "mcp_tool_calls",
        capabilityLabel: "MCP tool calls",
        version: 1,
        status: "active",
        scope: { type: "all_servers" },
        schedule: { start: "now", end: "until_lifted" },
        externalNote: "Access paused.",
        internalNote: "Incident response.",
        history: [],
        historyTruncated: false,
      },
    });
    await userEvent.click(screen.getByLabelText(/Selected servers/));
    await userEvent.click(
      screen.getByRole("button", { name: /Choose servers/ }),
    );
    for (const server of servers) {
      await userEvent.click(screen.getByLabelText(new RegExp(server.name)));
    }
    await userEvent.click(
      screen.getByRole("button", { name: "Apply selection" }),
    );
    expect(screen.getByText("Removed: None")).not.toBeNull();
    expect(
      screen.getByText(/even when every current server is selected/),
    ).not.toBeNull();
  });

  it("preserves the draft, rebases comparisons, and requires a fresh preview after a stale-version conflict", async () => {
    const conflict = Object.assign(new Error("stale"), {
      statusCode: 409,
      data$: { name: "version_conflict" },
    });
    const initial: KillswitchDetail = {
      id: "ks-1",
      userId: "user-1",
      capabilityKey: "mcp_tool_calls",
      capabilityLabel: "MCP tool calls",
      version: 1,
      status: "active",
      scope: { type: "all_servers" },
      schedule: { start: "now", end: "until_lifted" },
      externalNote: "Keep this draft",
      internalNote: "Incident response",
      history: [],
      historyTruncated: false,
    };
    const latest: KillswitchDetail = {
      ...initial,
      version: 2,
      scope: { type: "selected_servers", serverIds: ["server-b"] },
      schedule: {
        start: "now",
        end: "bounded",
        endsAt: new Date("2099-01-01T00:00:00Z"),
      },
      externalNote: "Concurrent public message",
      internalNote: "Concurrent internal note",
    };
    const onRefreshConflict = vi.fn().mockResolvedValue(latest);
    const onSubmit = vi
      .fn()
      .mockRejectedValueOnce(conflict)
      .mockResolvedValueOnce({ id: "ks-1", version: 3, replayed: false });
    const view = renderEditor({
      mode: "edit",
      initial,
      onSubmit,
      onRefreshConflict,
    });

    await userEvent.click(
      screen.getByRole("button", { name: "Save new version" }),
    );
    await userEvent.click(
      screen.getByRole("button", { name: "Save new version" }),
    );
    expect(
      screen.getByText("This killswitch changed while you were editing"),
    ).not.toBeNull();
    await userEvent.click(
      screen.getByRole("button", { name: "Review latest version" }),
    );
    expect(onRefreshConflict).toHaveBeenCalledTimes(1);
    expect(
      screen.getByText(
        /Changed concurrently: MCP server scope, schedule, public message, internal note/,
      ),
    ).not.toBeNull();
    expect(
      (screen.getAllByRole("textbox")[0] as HTMLTextAreaElement).value,
    ).toBe("Keep this draft");

    await userEvent.click(
      screen.getByRole("button", { name: "Save new version" }),
    );
    expect(view.onPreview).toHaveBeenCalledTimes(2);
    expect(onSubmit).toHaveBeenCalledTimes(1);
    await userEvent.click(
      screen.getByRole("button", { name: "Save new version" }),
    );
    expect(onSubmit).toHaveBeenLastCalledWith(
      expect.objectContaining({ externalNote: "Keep this draft" }),
      expect.any(String),
      2,
    );
  });

  it("lets admins remove a deleted selected server without exposing its ID", async () => {
    renderEditor({
      mode: "edit",
      initial: {
        id: "ks-deleted-server",
        userId: "user-1",
        capabilityKey: "mcp_tool_calls",
        capabilityLabel: "MCP tool calls",
        version: 1,
        status: "active",
        scope: { type: "selected_servers", serverIds: ["opaque-gone-id"] },
        schedule: { start: "now", end: "until_lifted" },
        externalNote: "Access paused.",
        internalNote: "Incident response.",
        history: [],
        historyTruncated: false,
      },
    });
    expect(screen.queryByText(/opaque-gone-id/)).toBeNull();
    await userEvent.click(
      screen.getByRole("button", { name: "Remove deleted MCP server" }),
    );
    expect(
      screen.getByRole("button", { name: "Choose servers (0)" }),
    ).not.toBeNull();
  });

  it("keeps picker changes temporary until Apply and discards them on Cancel", async () => {
    const initial: KillswitchDetail = {
      id: "ks-picker",
      userId: "user-1",
      capabilityKey: "mcp_tool_calls",
      capabilityLabel: "MCP tool calls",
      version: 1,
      status: "active",
      scope: { type: "selected_servers", serverIds: ["server-a"] },
      schedule: { start: "now", end: "until_lifted" },
      externalNote: "Access paused.",
      internalNote: "Incident response.",
      history: [],
      historyTruncated: false,
    };
    renderEditor({ mode: "edit", initial });
    await userEvent.click(
      screen.getByRole("button", { name: /Choose servers/ }),
    );
    await userEvent.click(screen.getByLabelText(/Server B/));
    await userEvent.click(screen.getByRole("button", { name: "Cancel" }));
    expect(
      screen.getByRole("button", { name: "Choose servers (1)" }),
    ).not.toBeNull();
  });

  it("starts formerly scheduled active edits now", () => {
    renderEditor({
      mode: "edit",
      initial: {
        id: "ks-active",
        userId: "user-1",
        capabilityKey: "mcp_tool_calls",
        capabilityLabel: "MCP tool calls",
        version: 2,
        status: "active",
        scope: { type: "all_servers" },
        schedule: {
          start: "scheduled",
          startsAt: new Date("2020-01-01T00:00:00Z"),
          end: "until_lifted",
        },
        externalNote: "Access paused.",
        internalNote: "Incident response.",
        history: [],
        historyTruncated: false,
      },
    });
    expect(
      (screen.getByLabelText("Start timing") as HTMLSelectElement).value,
    ).toBe("now");
  });

  it("distinguishes operation conflicts and prepares a fresh replay ID", async () => {
    const operationConflict = Object.assign(new Error("collision"), {
      statusCode: 409,
      data$: { name: "operation_conflict" },
    });
    const onSubmit = vi
      .fn()
      .mockRejectedValueOnce(operationConflict)
      .mockResolvedValueOnce({ id: "ks-1", version: 2, replayed: false });
    renderEditor({
      mode: "edit",
      initial: {
        id: "ks-conflict",
        userId: "user-1",
        capabilityKey: "mcp_tool_calls",
        capabilityLabel: "MCP tool calls",
        version: 1,
        status: "active",
        scope: { type: "all_servers" },
        schedule: { start: "now", end: "until_lifted" },
        externalNote: "Access paused.",
        internalNote: "Incident response.",
        history: [],
        historyTruncated: false,
      },
      onSubmit,
    });
    await userEvent.click(
      screen.getByRole("button", { name: "Save new version" }),
    );
    await userEvent.click(
      screen.getByRole("button", { name: "Save new version" }),
    );
    expect(
      await screen.findByText(/operation ID was already used/),
    ).not.toBeNull();
    await userEvent.click(
      screen.getByRole("button", { name: "Save new version" }),
    );
    expect(onSubmit.mock.calls[0]![1]).not.toBe(onSubmit.mock.calls[1]![1]);
  });

  it("preserves raw note whitespace when submitting", async () => {
    const onSubmit = vi
      .fn()
      .mockResolvedValue({ id: "ks-1", version: 1, replayed: false });
    renderEditor({
      onSubmit,
      mcpSessionsHref: (userId) =>
        `/example/mcp-sessions?subjectUrn=${encodeURIComponent(`user:${userId}`)}`,
    });
    await userEvent.selectOptions(
      screen.getByLabelText("Team member"),
      "user-1",
    );
    await userEvent.click(screen.getByLabelText("MCP tool calls"));
    await userEvent.click(screen.getByLabelText(/All MCP servers/));
    const textareas = screen.getAllByRole("textbox");
    await userEvent.type(textareas[0]!, "  Public 🙂 note  ");
    await userEvent.type(textareas[1]!, "  Internal note  ");
    await userEvent.click(
      screen.getByRole("button", { name: "Turn off MCP tool calls" }),
    );
    await userEvent.click(
      screen.getByRole("button", { name: "Turn off MCP tool calls" }),
    );
    expect(onSubmit).toHaveBeenCalledWith(
      expect.objectContaining({
        externalNote: "  Public 🙂 note  ",
        internalNote: "  Internal note  ",
      }),
      expect.any(String),
      undefined,
    );
    expect(
      screen
        .getByRole("link", { name: "View this member in MCP Sessions" })
        .getAttribute("href"),
    ).toBe("/example/mcp-sessions?subjectUrn=user%3Auser-1");
  });

  it("uses server overlap preview and reuses one operation id for an identical transport retry", async () => {
    const onSubmit = vi
      .fn()
      .mockRejectedValueOnce(new Error("network unavailable"))
      .mockResolvedValueOnce({ id: "ks-1", version: 1, replayed: true });
    const props = renderEditor({ onSubmit });

    await userEvent.selectOptions(
      screen.getByLabelText("Team member"),
      "user-1",
    );
    await userEvent.click(screen.getByLabelText("MCP tool calls"));
    await userEvent.click(screen.getByLabelText(/All MCP servers/));
    const textareas = screen.getAllByRole("textbox");
    await userEvent.type(textareas[0]!, "Access paused.");
    await userEvent.type(textareas[1]!, "Incident response.");

    await userEvent.click(
      screen.getByRole("button", { name: "Turn off MCP tool calls" }),
    );
    expect(props.onPreview).toHaveBeenCalledTimes(1);
    expect(onSubmit).not.toHaveBeenCalled();
    await userEvent.click(
      screen.getByRole("button", { name: "Turn off MCP tool calls" }),
    );
    expect(await screen.findByText("network unavailable")).not.toBeNull();
    await userEvent.click(
      screen.getByRole("button", { name: "Turn off MCP tool calls" }),
    );
    expect(onSubmit).toHaveBeenCalledTimes(2);
    expect(onSubmit.mock.calls[0]![1]).toBe(onSubmit.mock.calls[1]![1]);
  });
});
