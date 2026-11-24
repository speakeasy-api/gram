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

// The picker reads the server's credentials itself; here it only has to show
// where it sits.
vi.mock("./KeyCredentialChoice", () => ({
  KeyCredentialChoice: ({
    serverName,
    accounts,
  }: {
    serverName: string;
    accounts: boolean;
  }) => (
    <div data-accounts={String(accounts)}>
      Credential picker for {serverName}
    </div>
  ),
}));

const toast = vi.hoisted(() => ({
  success: vi.fn<(message: string) => void>(),
  warning: vi.fn<(message: string) => void>(),
  info: vi.fn<(message: string) => void>(),
  error: vi.fn<(message: string) => void>(),
}));
vi.mock("sonner", () => ({ toast }));

vi.mock("@/contexts/Auth", () => ({
  useSession: () => ({ user: { id: "editor", displayName: "Example Editor" } }),
}));
afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
  for (const tell of Object.values(toast)) tell.mockClear();
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
  const assignedNone = (
    own: "ready" | "checking",
    assigned: "missing" | "ended",
    onAssign?: (serverId: string) => void,
  ) =>
    render(
      <StepAuthentication
        servers={[server]}
        allServers={false}
        onRemove={vi.fn<(id: string) => void>()}
        onAssign={onAssign}
        connections={{
          byServer: new Map([
            [
              server.id,
              own === "ready"
                ? { status: "ready", sessions: [mine] }
                : { status: "checking" },
            ],
          ]),
          assignedByServer: new Map([
            [
              server.id,
              assigned === "ended"
                ? { status: "ended", sessions: [expired] }
                : { status: "missing" },
            ],
          ]),
          refetch: vi.fn<() => Promise<void>>().mockResolvedValue(undefined),
        }}
      />,
      { wrapper: MemoryRouter },
    );

  it("never shows the editor's own connection as one the agent has", () => {
    assignedNone("ready", "missing", vi.fn<(serverId: string) => void>());
    expect(screen.queryByText("Your connection")).toBeNull();
    expect(
      screen.getByRole("button", {
        name: /Example server.*Requires connection/,
      }),
    ).toBeTruthy();
  });

  it("shows an ended assignment as its owner's, not the editor's", () => {
    assignedNone("ready", "ended", vi.fn<(serverId: string) => void>());
    fireEvent.click(
      screen.getByRole("button", { name: /Example server.*Expired/ }),
    );
    expect(
      screen.getByText("Another member's connection has expired"),
    ).toBeTruthy();
    expect(screen.queryByText("Your connection")).toBeNull();
  });

  it.each(["missing", "ended"] as const)(
    "assigns the editor's connection only when they ask, for a %s assignment",
    (assigned) => {
      const onAssign = vi.fn<(serverId: string) => void>();
      assignedNone("ready", assigned, onAssign);
      fireEvent.click(screen.getByRole("button", { name: /^Example server/ }));
      expect(onAssign).not.toHaveBeenCalled();
      expect(
        screen.getByText(/The agent can use it only once you assign it/),
      ).toBeTruthy();
      fireEvent.click(
        screen.getByRole("button", { name: "Use your connection" }),
      );
      expect(onAssign).toHaveBeenCalledWith(server.id);
    },
  );

  it("offers nothing to assign until the editor's own connection is known", () => {
    assignedNone("checking", "missing", vi.fn<(serverId: string) => void>());
    fireEvent.click(screen.getByRole("button", { name: /^Example server/ }));
    expect(
      screen.queryByRole("button", { name: "Use your connection" }),
    ).toBeNull();
  });

  it("shows an ended assignment while the editor's own connection is still being checked", () => {
    assignedNone("checking", "ended", vi.fn<(serverId: string) => void>());
    expect(screen.queryByText("Checking connection…")).toBeNull();
    fireEvent.click(
      screen.getByRole("button", { name: /Example server.*Expired/ }),
    );
    expect(
      screen.getByText("Another member's connection has expired"),
    ).toBeTruthy();
  });

  it("offers nothing to assign where the page cannot assign it", () => {
    assignedNone("ready", "missing");
    fireEvent.click(screen.getByRole("button", { name: /^Example server/ }));
    expect(
      screen.queryByRole("button", { name: "Use your connection" }),
    ).toBeNull();
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

describe("a server still missing other sign-ins", () => {
  it("names them on the row, not only that it requires a connection", () => {
    render(
      <StepAuthentication
        servers={[server]}
        allServers={false}
        onRemove={vi.fn<(id: string) => void>()}
        connections={{
          byServer: new Map([
            [
              server.id,
              {
                status: "missing",
                signIns: [
                  { id: "stripe", credentialOwner: "subject", name: "Stripe" },
                  { id: "vercel", credentialOwner: "subject", name: "Vercel" },
                ],
              },
            ],
          ]),
          refetch: vi.fn<() => Promise<void>>().mockResolvedValue(undefined),
        }}
      />,
      { wrapper: MemoryRouter },
    );
    fireEvent.click(
      screen.getByRole("button", {
        name: /Example server.*Requires connection/,
      }),
    );
    expect(
      screen.getByText("Still needs a connection to Stripe and Vercel."),
    ).toBeTruthy();
  });
});

describe("the credential the agent's keys use", () => {
  const remote = { ...server, kind: "Remote" } as InventoryServer;
  const row = (value: string | undefined) =>
    render(
      <StepAuthentication
        servers={[remote]}
        allServers={false}
        onRemove={vi.fn<(id: string) => void>()}
        credentialChoice={{
          show: () => true,
          value: () => value,
          onChange: vi.fn<(serverId: string, authClientId: string) => void>(),
        }}
        connections={{
          byServer: new Map([[remote.id, { status: "missing" }]]),
          refetch: vi.fn<() => Promise<void>>().mockResolvedValue(undefined),
        }}
      />,
      { wrapper: MemoryRouter },
    );

  it("asks only once the server's row is open", () => {
    row("account");
    expect(
      screen.queryByText("Credential picker for Example server"),
    ).toBeNull();
    fireEvent.click(
      screen.getByRole("button", {
        name: /Example server.*Requires connection/,
      }),
    );
    expect(
      screen.getByText("Credential picker for Example server"),
    ).toBeTruthy();
  });

  it("needs no personal connection once a shared credential is chosen", () => {
    row("token_shared");
    fireEvent.click(
      screen.getByRole("button", { name: /Example server.*Shared credential/ }),
    );
    expect(
      screen.queryByRole("button", { name: "Connect account" }),
    ).toBeNull();
    expect(screen.getByText(/No personal connection is needed/)).toBeTruthy();
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
  function row(
    status: "missing" | "ready",
    onConnected: (serverId: string) => void,
  ) {
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
    const onConnected = vi.fn<(serverId: string) => void>();
    const view = render(row("missing", onConnected), { wrapper: MemoryRouter });
    fireEvent.click(screen.getByText("Example server"));
    fireEvent.click(screen.getByRole("button", { name: "Connect account" }));
    // The refetch the popup's close starts reads the new connection.
    view.rerender(row("ready", onConnected));
    await act(async () => {
      await popup.onComplete?.("popup-closed");
    });
    await waitFor(() => expect(onConnected).toHaveBeenCalledTimes(1));
    expect(onConnected).toHaveBeenCalledWith(connectable.id);
  });

  it("does not save when the popup closes without a connection", async () => {
    vi.spyOn(window, "open").mockReturnValue(fakePopup());
    const onConnected = vi.fn<(serverId: string) => void>();
    render(row("missing", onConnected), { wrapper: MemoryRouter });
    fireEvent.click(screen.getByText("Example server"));
    fireEvent.click(screen.getByRole("button", { name: "Connect account" }));
    await act(async () => {
      await popup.onComplete?.("popup-closed");
    });
    expect(onConnected).not.toHaveBeenCalled();
  });
});

describe("an agent connection the viewer may not see", () => {
  const view = (choice?: string) =>
    render(
      <StepAuthentication
        servers={[server]}
        allServers
        onRemove={vi.fn<(id: string) => void>()}
        credentialChoice={
          choice === undefined
            ? undefined
            : {
                show: () => true,
                value: () => choice,
                onChange:
                  vi.fn<(serverId: string, authClientId: string) => void>(),
              }
        }
        connections={{
          byServer: new Map([[server.id, { status: "missing" }]]),
          assignedByServer: new Map([[server.id, { status: "forbidden" }]]),
          refetch: vi.fn<() => Promise<void>>().mockResolvedValue(undefined),
        }}
      />,
      { wrapper: MemoryRouter },
    );

  it("says so on the row and explains why, without asking for a sign-in", () => {
    view();
    expect(screen.queryByRole("alert")).toBeNull();
    fireEvent.click(
      screen.getByRole("button", {
        name: /Example server.*Connection not visible to you/,
      }),
    );
    expect(
      screen.getByText(
        /You don't have permission to see which connection this agent uses for Example server/,
      ),
    ).toBeTruthy();
    expect(
      screen.queryByRole("button", { name: "Connect account" }),
    ).toBeNull();
  });

  it("keeps a personal connection already picked for the agent's keys", () => {
    view("account");
    fireEvent.click(
      screen.getByRole("button", {
        name: /Example server.*Connection not visible to you/,
      }),
    );
    expect(
      screen
        .getByText("Credential picker for Example server")
        .getAttribute("data-accounts"),
    ).toBe("true");
  });
});

describe("refreshing a connection's status", () => {
  const connectable = { ...server, connectUrl: "https://example.test/connect" };
  const ownExpired = { ...expired, subjectUrn: "user:editor" } as RemoteSession;
  function row(connection: "ended" | "ready", refetch: () => Promise<void>) {
    return (
      <StepAuthentication
        servers={[connectable]}
        allServers={false}
        onRemove={vi.fn<(id: string) => void>()}
        connections={{
          byServer: new Map([
            [
              connectable.id,
              connection === "ready"
                ? { status: "ready", sessions: [mine] }
                : { status: "ended", sessions: [ownExpired] },
            ],
          ]),
          refetch,
        }}
      />
    );
  }
  const refreshButton = () =>
    screen.getByRole("button", {
      name: "Refresh Example server connection status",
    });

  it("shows it is checking until the re-read lands", async () => {
    let finish: () => void = () => {};
    const refetch = vi.fn<() => Promise<void>>().mockReturnValue(
      new Promise<void>((resolve) => {
        finish = resolve;
      }),
    );
    render(row("ended", refetch), { wrapper: MemoryRouter });
    fireEvent.click(screen.getByText("Example server"));
    fireEvent.click(refreshButton());
    expect(refetch).toHaveBeenCalledTimes(1);
    expect(refreshButton().hasAttribute("disabled")).toBe(true);
    expect(refreshButton().getAttribute("aria-busy")).toBe("true");
    expect(
      refreshButton().querySelector("svg")?.classList.contains("animate-spin"),
    ).toBe(true);
    await act(async () => finish());
    expect(
      refreshButton().querySelector("svg")?.classList.contains("animate-spin"),
    ).toBe(false);
    expect(refreshButton().hasAttribute("disabled")).toBe(false);
  });

  it("says a connection that is still expired needs reauthenticating", async () => {
    const refetch = vi.fn<() => Promise<void>>().mockResolvedValue(undefined);
    render(row("ended", refetch), { wrapper: MemoryRouter });
    fireEvent.click(screen.getByText("Example server"));
    await act(async () => fireEvent.click(refreshButton()));
    expect(toast.warning).toHaveBeenCalledWith(
      "The connection to Example server is still expired. Reauthenticate to sign in again.",
    );
  });

  it("says the connection works once the re-read finds a new one", async () => {
    let finish: () => void = () => {};
    const refetch = vi.fn<() => Promise<void>>().mockReturnValue(
      new Promise<void>((resolve) => {
        finish = resolve;
      }),
    );
    const view = render(row("ended", refetch), { wrapper: MemoryRouter });
    fireEvent.click(screen.getByText("Example server"));
    fireEvent.click(refreshButton());
    // Signed in again in another tab: the re-read finds the new connection.
    view.rerender(row("ready", refetch));
    await act(async () => finish());
    await waitFor(() =>
      expect(toast.success).toHaveBeenCalledWith(
        "The connection to Example server is working.",
      ),
    );
    expect(toast.warning).not.toHaveBeenCalled();
  });

  it("says the check failed when the re-read is rejected", async () => {
    const refetch = vi
      .fn<() => Promise<void>>()
      .mockRejectedValue(new Error("network"));
    render(row("ended", refetch), { wrapper: MemoryRouter });
    fireEvent.click(screen.getByText("Example server"));
    await act(async () => fireEvent.click(refreshButton()));
    expect(toast.error).toHaveBeenCalledWith(
      "Could not check the connection to Example server. Try again.",
    );
    expect(toast.warning).not.toHaveBeenCalled();
    expect(refreshButton().hasAttribute("disabled")).toBe(false);
  });

  it("holds every row's refresh until the running one lands", async () => {
    const other = { ...connectable, id: "server_two", name: "Other server" };
    let finish: () => void = () => {};
    const refetch = vi.fn<() => Promise<void>>().mockReturnValue(
      new Promise<void>((resolve) => {
        finish = resolve;
      }),
    );
    render(
      <StepAuthentication
        servers={[connectable, other]}
        allServers={false}
        onRemove={vi.fn<(id: string) => void>()}
        connections={{
          byServer: new Map([
            [connectable.id, { status: "ended", sessions: [ownExpired] }],
            [other.id, { status: "ended", sessions: [ownExpired] }],
          ]),
          refetch,
        }}
      />,
      { wrapper: MemoryRouter },
    );
    fireEvent.click(screen.getByText("Example server"));
    fireEvent.click(screen.getByText("Other server"));
    fireEvent.click(refreshButton());
    const otherRefresh = screen.getByRole("button", {
      name: "Refresh Other server connection status",
    });
    expect(otherRefresh.hasAttribute("disabled")).toBe(true);
    expect(otherRefresh.getAttribute("aria-busy")).toBe("false");
    await act(async () => finish());
    expect(otherRefresh.hasAttribute("disabled")).toBe(false);
    expect(refetch).toHaveBeenCalledTimes(1);
  });
});
