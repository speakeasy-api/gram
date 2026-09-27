import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import {
  AttachedUserSessionsPanel,
  type AttachedUserSessionsPanelProps,
} from "./AttachedUserSessionsPanel";

afterEach(cleanup);
const defaults: AttachedUserSessionsPanelProps = {
  sessions: [
    {
      id: "session-one",
      label: "My account",
      status: "active",
      agentIds: ["agent-one"],
    },
  ],
  agents: [
    { id: "agent-one", name: "First agent", eligible: true },
    { id: "agent-two", name: "Second agent", eligible: true },
    { id: "agent-three", name: "Unauthorized agent", eligible: false },
  ],
  isLoading: false,
  isError: false,
  connectUrl: "https://gram.example/mcp/test/connect/first-party",
  onRefresh: vi.fn<() => void>(),
  onSave: vi.fn().mockResolvedValue(undefined),
};
function choose() {
  fireEvent.change(screen.getByRole("combobox"), {
    target: { value: "session-one" },
  });
}

describe("AttachedUserSessionsPanel", () => {
  it("opens the upstream connect flow without displaying credentials", () => {
    const open = vi.spyOn(window, "open").mockImplementation(() => null);
    render(<AttachedUserSessionsPanel {...defaults} />);
    fireEvent.click(screen.getByRole("button", { name: "Connect account" }));
    expect(open).toHaveBeenCalledWith(
      defaults.connectUrl,
      "_blank",
      "noopener,noreferrer",
    );
    expect(screen.queryByText(/bearer/i)).toBeNull();
    open.mockRestore();
  });
  it("multi-selects eligible agents and detaches without revoking the account", async () => {
    const save = vi.fn().mockResolvedValue(undefined);
    render(<AttachedUserSessionsPanel {...defaults} onSave={save} />);
    choose();
    expect(
      screen.queryByRole("checkbox", { name: "Unauthorized agent" }),
    ).toBeNull();
    fireEvent.click(screen.getByRole("checkbox", { name: "First agent" }));
    fireEvent.click(screen.getByRole("checkbox", { name: "Second agent" }));
    fireEvent.click(screen.getByRole("button", { name: "Save attachments" }));
    await waitFor(() =>
      expect(save).toHaveBeenCalledWith("session-one", ["agent-two"]),
    );
    expect(
      screen.getByText(/does not revoke the shared upstream session/),
    ).toBeTruthy();
  });
  it("respects per-session eligibility", () => {
    render(
      <AttachedUserSessionsPanel
        {...defaults}
        eligibleAgentIdsBySession={{ "session-one": ["agent-one"] }}
      />,
    );
    choose();
    expect(screen.queryByRole("checkbox", { name: "Second agent" })).toBeNull();
  });
  it("keeps expired attachments detachable but prevents new attachments", () => {
    render(
      <AttachedUserSessionsPanel
        {...defaults}
        sessions={[{ ...defaults.sessions[0]!, status: "unavailable" }]}
      />,
    );
    choose();
    expect(
      screen
        .getByRole("checkbox", { name: "First agent" })
        .hasAttribute("disabled"),
    ).toBe(false);
    expect(
      screen
        .getByRole("checkbox", { name: "Second agent" })
        .hasAttribute("disabled"),
    ).toBe(true);
  });
  it("shows errors without leaking backend response details", async () => {
    render(
      <AttachedUserSessionsPanel
        {...defaults}
        onSave={vi.fn().mockRejectedValue(new Error("private upstream error"))}
      />,
    );
    choose();
    fireEvent.click(screen.getByRole("checkbox", { name: "Second agent" }));
    fireEvent.click(screen.getByRole("button", { name: "Save attachments" }));
    expect(await screen.findByRole("alert")).toHaveProperty(
      "textContent",
      expect.stringContaining("Could not save all attachments"),
    );
    expect(screen.queryByText(/private upstream error/)).toBeNull();
  });
  it("renders loading, failure and empty states", () => {
    const { rerender } = render(
      <AttachedUserSessionsPanel {...defaults} isLoading />,
    );
    expect(screen.getByRole("status").textContent).toContain("Loading");
    rerender(<AttachedUserSessionsPanel {...defaults} isError />);
    expect(screen.getByRole("alert").textContent).toContain("Could not load");
    rerender(<AttachedUserSessionsPanel {...defaults} sessions={[]} />);
    expect(screen.getByText(/No upstream accounts/)).toBeTruthy();
  });
});
