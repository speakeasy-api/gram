import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { RemoteSession } from "@gram/client/models/components/remotesession.js";
import { StepAuthentication } from "./StepAuthentication";
import type { InventoryServer } from "./useServerInventory";

vi.mock("@/contexts/Sdk", () => ({
  useSlugs: () => ({ orgSlug: "org", projectSlug: "project" }),
}));

const popup = vi.hoisted(() => ({
  onComplete: undefined as
    | undefined
    | ((reason: "popup-closed" | "focus") => void | Promise<void>),
}));
// The real popup hook, with its completion callback kept so a test can say
// the popup closed.
vi.mock("@/components/sessions/useOAuthPopup", async (importOriginal) => {
  const real =
    await importOriginal<
      typeof import("@/components/sessions/useOAuthPopup")
    >();
  return {
    useOAuthPopup: (
      onComplete: (reason: "popup-closed" | "focus") => void | Promise<void>,
    ) => {
      popup.onComplete = onComplete;
      return real.useOAuthPopup(onComplete);
    },
  };
});

vi.mock("@/contexts/Auth", () => ({
  useSession: () => ({ user: { id: "editor", displayName: "Example Editor" } }),
}));
afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
});

const server: InventoryServer = {
  id: "server_one",
  resourceId: "server_one",
  name: "Example server",
  slug: "example",
  projectId: "project_one",
  projectSlug: "project-one",
  projectName: "Project one",
  kind: "Remote",
  issuerId: "issuer_one",
};
const mine = {
  id: "mine",
  subjectUrn: "user:editor",
  hasRefreshToken: true,
} as RemoteSession;
const expired = {
  id: "expired",
  subjectUrn: "user:other",
  hasRefreshToken: false,
  accessExpiresAt: new Date(0),
} as RemoteSession;

describe("authentication assignment warnings", () => {
  it("shows the editor's live account when the agent's assignment has ended, since saving assigns it", () => {
    render(
      <StepAuthentication
        servers={[server]}
        allServers
        onRemove={vi.fn<(id: string) => void>()}
        connections={{
          byServer: new Map([
            [server.id, { status: "ready", sessions: [mine] }],
          ]),
          assignedByServer: new Map([
            [server.id, { status: "ended", sessions: [expired] }],
          ]),
          refetch: vi.fn<() => Promise<void>>().mockResolvedValue(undefined),
        }}
      />,
      { wrapper: MemoryRouter },
    );
    expect(screen.queryByRole("alert")).toBeNull();
    expect(screen.getAllByText("Your connection").length).toBeGreaterThan(0);
  });
  it("waits for the editor's own connection before judging an ended assignment", () => {
    render(
      <StepAuthentication
        servers={[server]}
        allServers
        onRemove={vi.fn<(id: string) => void>()}
        connections={{
          byServer: new Map([[server.id, { status: "checking" }]]),
          assignedByServer: new Map([
            [server.id, { status: "ended", sessions: [expired] }],
          ]),
          refetch: vi.fn<() => Promise<void>>().mockResolvedValue(undefined),
        }}
      />,
      { wrapper: MemoryRouter },
    );
    expect(screen.queryByRole("alert")).toBeNull();
    expect(screen.getAllByText("Checking connection…").length).toBeGreaterThan(
      0,
    );
  });
  it("keeps a valid delegated assignment clear of candidate warnings", () => {
    render(
      <StepAuthentication
        servers={[server]}
        allServers
        onRemove={vi.fn<(id: string) => void>()}
        connections={{
          byServer: new Map([[server.id, { status: "missing" }]]),
          assignedByServer: new Map([
            [server.id, { status: "ready", sessions: [mine] }],
          ]),
          refetch: vi.fn<() => Promise<void>>().mockResolvedValue(undefined),
        }}
      />,
      { wrapper: MemoryRouter },
    );
    expect(screen.queryByRole("alert")).toBeNull();
  });
});

describe("create connection reauthentication", () => {
  it.each(["ready", "ended", "missing"] as const)(
    "targets the known upstream client for %s connections",
    (status) => {
      const popup = {
        location: { href: "" },
        opener: window,
        focus: vi.fn(),
        closed: false,
      };
      vi.spyOn(window, "open").mockReturnValue(popup as unknown as Window);
      const session = {
        ...mine,
        remoteSessionClientId: "client_actual",
        hasRefreshToken: false,
        accessExpiresAt: new Date(
          status === "ended" ? 0 : Date.now() + 3600000,
        ),
      };
      render(
        <StepAuthentication
          servers={[
            {
              ...server,
              connectUrl:
                "https://example.test/mcp/connect/first-party?existing=keep",
            },
          ]}
          allServers={false}
          onRemove={vi.fn<(id: string) => void>()}
          connections={{
            byServer: new Map([
              [
                server.id,
                status === "missing"
                  ? { status }
                  : { status, sessions: [session] },
              ],
            ]),
            refetch: vi.fn<() => Promise<void>>().mockResolvedValue(undefined),
          }}
        />,
        { wrapper: MemoryRouter },
      );
      // Rows start closed; the badge alone says how it signs in.
      expect(
        screen.queryByRole("button", {
          name: status === "missing" ? "Connect account" : "Reauthenticate",
        }),
      ).toBeNull();
      fireEvent.click(screen.getByRole("button", { name: /^Example server/ }));
      fireEvent.click(
        screen.getByRole("button", {
          name: status === "missing" ? "Connect account" : "Reauthenticate",
        }),
      );
      const url = new URL(popup.location.href);
      expect(url.searchParams.get("reauthenticate_client_id")).toBe(
        status === "missing" ? null : "client_actual",
      );
      expect(url.searchParams.get("existing")).toBe("keep");
    },
  );
});

describe("saving after a connection", () => {
  const fakePopup = () =>
    ({
      closed: false,
      opener: null,
      location: { href: "" },
      focus: () => {},
      close: () => {},
    }) as unknown as Window;
  const connectable = { ...server, connectUrl: "https://example.test/connect" };
  function row(status: "missing" | "ready", onConnected: () => void) {
    return (
      <StepAuthentication
        servers={[connectable]}
        allServers={false}
        onRemove={vi.fn<(id: string) => void>()}
        onConnected={onConnected}
        connections={{
          byServer: new Map([
            [
              connectable.id,
              status === "ready"
                ? { status: "ready", sessions: [mine] }
                : { status: "missing" },
            ],
          ]),
          refetch: vi.fn<() => Promise<void>>().mockResolvedValue(undefined),
        }}
      />
    );
  }

  it("saves once the reader's connection is ready after the sign-in popup closes", async () => {
    vi.spyOn(window, "open").mockReturnValue(fakePopup());
    const onConnected = vi.fn<() => void>();
    const view = render(row("missing", onConnected), { wrapper: MemoryRouter });
    fireEvent.click(screen.getByText("Example server"));
    fireEvent.click(screen.getByRole("button", { name: "Connect account" }));
    // The refetch the popup's close starts reads the new connection.
    view.rerender(row("ready", onConnected));
    await act(async () => {
      await popup.onComplete?.("popup-closed");
    });
    await waitFor(() => expect(onConnected).toHaveBeenCalledTimes(1));
  });

  it("does not save when the popup closes without a connection", async () => {
    vi.spyOn(window, "open").mockReturnValue(fakePopup());
    const onConnected = vi.fn<() => void>();
    render(row("missing", onConnected), { wrapper: MemoryRouter });
    fireEvent.click(screen.getByText("Example server"));
    fireEvent.click(screen.getByRole("button", { name: "Connect account" }));
    await act(async () => {
      await popup.onComplete?.("popup-closed");
    });
    expect(onConnected).not.toHaveBeenCalled();
  });
});
