import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import {
  AgentSessionsSection,
  type AgentSessionsSectionProps,
  type AgentSessionRow,
} from "./AgentSessions";

// Keep expiry scheduling assertions independent of the relative-date clock.
vi.mock("@/lib/dates", () => ({
  HumanizeDateTime: ({ date }: { date: Date }) => (
    <span>{date.toISOString()}</span>
  ),
}));

const session: AgentSessionRow = {
  id: "session_example",
  clientName: "Example client",
  createdAt: new Date("2026-01-01T00:00:00Z"),
  expiresAt: new Date("2099-01-01T00:00:00Z"),
};

function setup(overrides: Partial<AgentSessionsSectionProps> = {}) {
  const props = {
    sessions: [session],
    isLoading: false,
    isError: false,
    canRevoke: true,
    onRetry: vi.fn(),
    onRevoke: vi.fn().mockResolvedValue(undefined),
    ...overrides,
  };
  const view = render(<AgentSessionsSection {...props} />);
  return { ...view, props };
}

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
  vi.useRealTimers();
});

describe("Agent sessions", () => {
  it("uses refresh expiry, not the expired access token, and formats future deadlines absolutely", () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date("2026-01-01T12:00:00Z"));
    const refreshExpiresAt = new Date("2026-01-01T13:00:00Z");
    setup({
      sessions: [
        {
          ...session,
          expiresAt: new Date("2026-01-01T11:00:00Z"),
          refreshExpiresAt,
        },
      ],
    });
    expect(screen.getByText("Active")).toBeTruthy();
    const expiry = document.querySelector("time");
    expect(expiry?.getAttribute("datetime")).toBe(
      refreshExpiresAt.toISOString(),
    );
    expect(expiry?.textContent).not.toContain("ago");
  });
  it("updates expiry while mounted and cleans up the clock", async () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date("2026-01-01T12:00:00Z"));
    const view = setup({
      sessions: [{ ...session, refreshExpiresAt: new Date(Date.now() + 1000) }],
    });
    expect(screen.getByText("Active")).toBeTruthy();
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1000);
    });
    expect(screen.getByText("Expired")).toBeTruthy();
    const clear = vi.spyOn(window, "clearTimeout");
    view.unmount();
    expect(clear).toHaveBeenCalled();
  });
  it("schedules sequential refresh deadlines without a recurring clock", async () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date("2026-01-01T12:00:00Z"));
    const start = Date.now();
    setup({
      sessions: [
        { ...session, refreshExpiresAt: new Date(start + 1000) },
        {
          ...session,
          id: "session_second",
          refreshExpiresAt: new Date(start + 3000),
        },
      ],
    });
    expect(vi.getTimerCount()).toBe(1);
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1000);
    });
    expect(screen.getAllByText("Expired")).toHaveLength(1);
    expect(screen.getAllByText("Active")).toHaveLength(1);
    expect(vi.getTimerCount()).toBe(1);
    await act(async () => {
      await vi.advanceTimersByTimeAsync(2000);
    });
    expect(screen.getAllByText("Expired")).toHaveLength(2);
    expect(vi.getTimerCount()).toBe(0);
  });
  it("reschedules changed session data and stops on permission loss", async () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date("2026-01-01T12:00:00Z"));
    const view = setup({
      sessions: [{ ...session, refreshExpiresAt: new Date(Date.now() + 1000) }],
    });
    const sessions = [
      { ...session, refreshExpiresAt: new Date(Date.now() + 5000) },
    ];
    view.rerender(<AgentSessionsSection {...view.props} sessions={sessions} />);
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1000);
    });
    expect(screen.getByText("Active")).toBeTruthy();
    view.rerender(
      <AgentSessionsSection
        {...view.props}
        sessions={sessions}
        canRead={false}
      />,
    );
    expect(vi.getTimerCount()).toBe(0);
    await act(async () => {
      await vi.advanceTimersByTimeAsync(5000);
    });
    view.rerender(<AgentSessionsSection {...view.props} sessions={sessions} />);
    expect(screen.getByText("Expired")).toBeTruthy();
    expect(vi.getTimerCount()).toBe(0);
  });
  it("bounds distant expiry timeouts and cleans up on unmount", () => {
    vi.useFakeTimers();
    const timeout = vi.spyOn(window, "setTimeout");
    const view = setup({
      sessions: [
        { ...session, refreshExpiresAt: new Date(Date.now() + 3_000_000_000) },
      ],
    });
    expect(timeout).toHaveBeenCalledWith(expect.any(Function), 2_147_483_647);
    view.unmount();
    expect(vi.getTimerCount()).toBe(0);
  });
  it.each([
    { sessions: [] },
    { canRead: false },
    { isLoading: true },
    { isError: true },
  ])(
    "does not schedule an expiry for hidden or empty content: %j",
    (overrides) => {
      vi.useFakeTimers();
      setup({
        sessions: [
          { ...session, refreshExpiresAt: new Date(Date.now() + 1000) },
        ],
        ...overrides,
      });
      expect(vi.getTimerCount()).toBe(0);
    },
  );
  it("pauses while the document is hidden and updates on visibility", async () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date("2026-01-01T12:00:00Z"));
    const hidden = vi.spyOn(document, "hidden", "get").mockReturnValue(true);
    setup({
      sessions: [{ ...session, refreshExpiresAt: new Date(Date.now() + 1000) }],
    });
    expect(vi.getTimerCount()).toBe(0);
    await act(async () => {
      await vi.advanceTimersByTimeAsync(2000);
    });
    hidden.mockReturnValue(false);
    fireEvent(document, new Event("visibilitychange"));
    expect(screen.getByText("Expired")).toBeTruthy();
    expect(vi.getTimerCount()).toBe(0);
  });
  it("closes and clears metadata when read permission is revoked", () => {
    const view = setup();
    fireEvent.click(screen.getByRole("button", { name: "Revoke session" }));
    expect(screen.getByRole("dialog")).toBeTruthy();
    view.rerender(
      <AgentSessionsSection
        {...view.props}
        canRead={false}
        canRevoke={false}
      />,
    );
    expect(screen.queryByRole("dialog")).toBeNull();
    expect(screen.queryByText(/Example client/)).toBeNull();
    view.rerender(<AgentSessionsSection {...view.props} />);
    expect(screen.queryByRole("dialog")).toBeNull();
  });
  it("does not show stale sessions or an empty state without credential read permission", () => {
    setup({ canRead: false, canRevoke: false });
    expect(screen.getByText(/do not have permission to view/)).toBeTruthy();
    expect(screen.queryByText("Example client")).toBeNull();
    expect(screen.queryByText("No sessions yet")).toBeNull();
  });
  it("treats an elapsed refresh deadline as expired", () => {
    setup({
      sessions: [
        { ...session, refreshExpiresAt: new Date("2020-01-01T00:00:00Z") },
      ],
    });
    expect(screen.getByText("Expired")).toBeTruthy();
  });
  it("offers pagination without replacing the current sessions", () => {
    const onLoadMore = vi.fn<() => void>();
    setup({ hasMore: true, onLoadMore, loadMoreError: true });
    expect(screen.getByText("Example client")).toBeTruthy();
    expect(screen.getByRole("alert").textContent).toContain(
      "Unable to load more",
    );
    fireEvent.click(screen.getByRole("button", { name: "Load more sessions" }));
    expect(onLoadMore).toHaveBeenCalledOnce();
  });
  it("prevents repeated revocation while the request is pending", async () => {
    let finish: () => void = () => {};
    const onRevoke = vi.fn(
      () =>
        new Promise<void>((resolve) => {
          finish = resolve;
        }),
    );
    setup({ onRevoke });
    fireEvent.click(screen.getByRole("button", { name: "Revoke session" }));
    fireEvent.click(screen.getByRole("button", { name: "Confirm revoke" }));
    fireEvent.click(screen.getByRole("button", { name: "Revoking…" }));
    expect(onRevoke).toHaveBeenCalledOnce();
    finish();
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
  });
  it("shows loading without implying that no sessions exist", () => {
    setup({ isLoading: true, sessions: [] });
    expect(
      screen.getByRole("status", { name: "Loading agent sessions" }),
    ).toBeTruthy();
    expect(screen.queryByText("No sessions yet")).toBeNull();
  });
  it("offers retry on a list error instead of an empty state", () => {
    const { props } = setup({ isError: true, sessions: [] });
    expect(screen.getByRole("alert").textContent).toContain("Unable to load");
    fireEvent.click(screen.getByRole("button", { name: "Try again" }));
    expect(props.onRetry).toHaveBeenCalledOnce();
    expect(screen.queryByText("No sessions yet")).toBeNull();
  });
  it("explains how to establish an agent session in the empty state", () => {
    setup({ sessions: [] });
    expect(screen.getByText("No sessions yet")).toBeTruthy();
    expect(screen.getByText(/Choose this agent when authorizing/)).toBeTruthy();
  });
  it("lists the client as unverified without exposing raw session IDs", () => {
    setup();
    expect(screen.getByText("Client name (unverified)")).toBeTruthy();
    expect(screen.getByText("Example client")).toBeTruthy();
    expect(screen.getByText("Active")).toBeTruthy();
    expect(screen.queryByText("session_example")).toBeNull();
  });
  it("requires confirmation and passes only the selected session to revoke", async () => {
    const { props } = setup();
    fireEvent.click(screen.getByRole("button", { name: "Revoke session" }));
    expect(props.onRevoke).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button", { name: "Confirm revoke" }));
    await waitFor(() =>
      expect(props.onRevoke).toHaveBeenCalledExactlyOnceWith(session),
    );
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
  });
  it("canceling does not revoke", () => {
    const { props } = setup();
    fireEvent.click(screen.getByRole("button", { name: "Revoke session" }));
    fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
    expect(props.onRevoke).not.toHaveBeenCalled();
    expect(screen.queryByRole("dialog")).toBeNull();
  });
  it("retains confirmation and reports revoke errors", async () => {
    setup({ onRevoke: vi.fn().mockRejectedValue(new Error("forbidden")) });
    fireEvent.click(screen.getByRole("button", { name: "Revoke session" }));
    fireEvent.click(screen.getByRole("button", { name: "Confirm revoke" }));
    expect(await screen.findByRole("alert")).toHaveProperty(
      "textContent",
      "Unable to revoke the session. Try again.",
    );
    expect(screen.getByRole("button", { name: "Confirm revoke" })).toBeTruthy();
  });
  it("does not offer revocation without the server-provided credential permission", () => {
    setup({ canRevoke: false });
    expect(screen.queryByRole("button", { name: "Revoke session" })).toBeNull();
    expect(screen.getByText(/do not have permission to revoke/)).toBeTruthy();
  });
  it("does not let an open confirmation bypass a lost permission", async () => {
    const { props, rerender } = setup();
    fireEvent.click(screen.getByRole("button", { name: "Revoke session" }));
    rerender(<AgentSessionsSection {...props} canRevoke={false} />);
    fireEvent.click(screen.getByRole("button", { name: "Confirm revoke" }));
    expect(props.onRevoke).not.toHaveBeenCalled();
  });
  it("does not offer to revoke an already revoked session", () => {
    setup({ sessions: [{ ...session, revokedAt: new Date() }] });
    expect(screen.getByText("Revoked")).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Revoke session" })).toBeNull();
  });
});
