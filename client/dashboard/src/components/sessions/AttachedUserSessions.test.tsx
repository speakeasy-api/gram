import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { AttachedUserSessions } from "./AttachedUserSessions";

const mocks = vi.hoisted(() => ({
  organization: { id: "org", slug: "org" },
  session: {
    user: { id: "user" },
    organizationOverride: false,
    impersonatorEmail: "",
  },
  scope: null as string | null,
  list: vi.fn(),
  candidates: vi.fn(),
  listRemoteSessions: vi.fn(),
  bindings: vi.fn(),
  attach: vi.fn(),
  detach: vi.fn(),
}));
vi.mock("@/contexts/Auth", () => ({
  useOrganization: () => mocks.organization,
  useProject: () => ({ id: "project", slug: "project" }),
  useSession: () => mocks.session,
  useIsPlatformAdmin: () => false,
}));
vi.mock("@/contexts/Sdk", () => ({
  useSdkClient: () => ({
    agents: { list: mocks.list },
    remoteSessions: {
      list: mocks.listRemoteSessions,
      listBindings: async (request: { principalId: string }) => ({
        items: await mocks.bindings(request.principalId),
      }),
    },
  }),
}));
vi.mock("@/components/dev-toolbar-utils", () => ({
  getRBACScopeOverrideHeader: () => mocks.scope,
}));
vi.mock("@/hooks/useToolsetUrl", () => ({ useInternalMcpUrl: () => "" }));

const agent = (id: string, authorize = true, lifecycle = "active") => ({
  id,
  name: id,
  ownerUserId: "user",
  permissions: { authorize },
  lifecycle,
});
function mount(issuerId = "issuer") {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  const view = render(
    <QueryClientProvider client={client}>
      <AttachedUserSessions issuerId={issuerId} />
    </QueryClientProvider>,
  );
  return {
    ...view,
    switchIssuer: (nextIssuer = "other") =>
      view.rerender(
        <QueryClientProvider client={client}>
          <AttachedUserSessions issuerId={nextIssuer} />
        </QueryClientProvider>,
      ),
  };
}
async function choose() {
  await screen.findByRole("combobox");
  fireEvent.change(screen.getByRole("combobox"), {
    target: { value: "session" },
  });
}
beforeEach(() => {
  vi.resetAllMocks();
  mocks.organization = { id: "org", slug: "org" };
  mocks.session = {
    user: { id: "user" },
    organizationOverride: false,
    impersonatorEmail: "",
  };
  mocks.scope = null;
  mocks.list.mockResolvedValue([
    agent("first"),
    agent("second"),
    agent("denied", false),
    { ...agent("not-owned"), ownerUserId: "another-user" },
  ]);
  mocks.candidates.mockResolvedValue([
    { id: "session", remoteSessionClientId: "client", scopes: [] },
  ]);
  mocks.listRemoteSessions.mockImplementation(
    async (request: { principalId: string }) => [
      { result: { items: await mocks.candidates(request.principalId) } },
    ],
  );
  mocks.bindings.mockResolvedValue([]);
  mocks.attach.mockResolvedValue(undefined);
});
afterEach(cleanup);
describe("AttachedUserSessions container", () => {
  it("loads authorized agents only and uses contract session identifiers", async () => {
    mount();
    expect(screen.getByRole("status").textContent).toContain("Loading");
    await choose();
    expect(mocks.candidates.mock.calls.map((call) => call[0])).toEqual([
      "first",
      "second",
    ]);
    expect(
      screen.getByRole("option", { name: /Identity unavailable/ }),
    ).toBeTruthy();
    expect(screen.queryByRole("checkbox", { name: "denied" })).toBeNull();
    expect(screen.queryByRole("checkbox", { name: "not-owned" })).toBeNull();
    expect(mocks.bindings.mock.calls.map((call) => call[0])).toEqual([
      "first",
      "second",
    ]);
  });
  it("drains every canonical list page with principal, issuer, project and cancellation scope", async () => {
    mocks.listRemoteSessions.mockResolvedValue([
      {
        result: {
          items: [
            {
              id: "session",
              remoteSessionClientId: "client",
              scopes: [],
              upstreamDisplayName: "First account",
            },
          ],
        },
      },
      {
        result: {
          items: [
            {
              id: "second-session",
              remoteSessionClientId: "client",
              scopes: [],
              upstreamDisplayName: "Second account",
            },
          ],
        },
      },
    ]);
    const view = mount();
    await choose();
    expect(screen.getByRole("option", { name: /Second account/ })).toBeTruthy();
    expect(mocks.listRemoteSessions).toHaveBeenCalledWith(
      {
        gramProject: "project",
        principalId: "first",
        userSessionIssuerId: "issuer",
      },
      undefined,
      { signal: expect.any(AbortSignal) },
    );
    mocks.listRemoteSessions.mockImplementation(
      (_request, _security, { signal }: { signal: AbortSignal }) =>
        new Promise((_resolve, reject) => {
          signal.addEventListener("abort", () => reject(signal.reason), {
            once: true,
          });
        }),
    );
    fireEvent.click(screen.getByRole("button", { name: "Refresh accounts" }));
    await waitFor(() =>
      expect(mocks.listRemoteSessions).toHaveBeenCalledTimes(4),
    );
    const signal = mocks.listRemoteSessions.mock.calls[2]![2]
      .signal as AbortSignal;
    view.unmount();
    expect(signal.aborted).toBe(true);
  });
  it.each(["override", "impersonation", "scope", "demo"])(
    "blocks %s sessions before any reads",
    (kind) => {
      if (kind === "override") mocks.session.organizationOverride = true;
      if (kind === "impersonation")
        mocks.session.impersonatorEmail = "admin@example.test";
      if (kind === "scope") mocks.scope = "override";
      if (kind === "demo") mocks.organization.slug = "acme-demo";
      mount();
      expect(mocks.list).not.toHaveBeenCalled();
    },
  );
  it("fails closed if any agent's binding read fails", async () => {
    mocks.bindings.mockRejectedValue(new Error("forbidden"));
    mount();
    expect((await screen.findByRole("alert")).textContent).toContain(
      "Could not load",
    );
    expect(screen.queryByRole("combobox")).toBeNull();
  });
  it("displays the canonical identity of a binding from a compatible source issuer", async () => {
    mocks.list.mockResolvedValue([agent("first")]);
    mocks.candidates.mockResolvedValue([]);
    mocks.bindings.mockResolvedValue([
      {
        id: "binding",
        remoteSessionId: "session",
        remoteSessionClientId: "client",
        user_session_issuer_id: "target-issuer",
        remoteSession: {
          id: "session",
          remoteSessionClientId: "client",
          userSessionIssuerId: "source-issuer",
          upstreamDisplayName: "Example upstream user",
          upstreamEmail: "upstream@example.test",
          subjectDisplayName: "Gram subject",
        },
      },
    ]);
    mount();
    await choose();
    expect(
      screen.getByRole("option", {
        name: /Example upstream user · upstream@example.test/,
      }),
    ).toBeTruthy();
    expect(
      screen.queryByText(/Gram subject|source-issuer|target-issuer/),
    ).toBeNull();
  });
  it("does not label a tombstone as active when a new grant reuses its session ID", async () => {
    mocks.list.mockResolvedValue([agent("first")]);
    mocks.bindings.mockResolvedValue([
      {
        id: "binding",
        remoteSessionId: "session",
        remoteSessionClientId: "client",
      },
    ]);
    mount();
    await choose();
    expect(
      screen.getByRole("option", {
        name: /Account authorization unavailable · unavailable/,
      }),
    ).toBeTruthy();
    expect(
      screen.queryByRole("option", { name: /available to attach/ }),
    ).toBeNull();
    fireEvent.click(screen.getByRole("checkbox", { name: "first" }));
    fireEvent.click(screen.getByRole("button", { name: "Save attachments" }));
    await waitFor(() =>
      expect(mocks.detach).toHaveBeenCalledWith(
        "first",
        "binding",
        expect.any(AbortSignal),
      ),
    );
    expect(mocks.attach).not.toHaveBeenCalled();
  });
  it("keeps unavailable existing bindings detachable", async () => {
    mocks.list.mockResolvedValue([agent("first", true, "disabled")]);
    mocks.candidates.mockResolvedValue([]);
    mocks.bindings.mockResolvedValue([
      {
        id: "binding",
        remoteSessionId: "session",
        remoteSessionClientId: "client",
      },
    ]);
    mount();
    await choose();
    fireEvent.click(screen.getByRole("checkbox"));
    fireEvent.click(screen.getByRole("button", { name: "Save attachments" }));
    await waitFor(() =>
      expect(mocks.detach).toHaveBeenCalledWith(
        "first",
        "binding",
        expect.any(AbortSignal),
      ),
    );
    expect(mocks.attach).not.toHaveBeenCalled();
  });
  it("validates every selected agent before starting even earlier detach writes", async () => {
    const second = agent("second");
    mocks.list.mockResolvedValue([agent("first"), second]);
    mocks.bindings.mockImplementation((id) =>
      Promise.resolve(
        id === "first"
          ? [
              {
                id: "binding",
                remoteSessionId: "session",
                remoteSessionClientId: "client",
                remoteSession: {
                  id: "session",
                  remoteSessionClientId: "client",
                },
              },
            ]
          : [],
      ),
    );
    mount();
    await choose();
    fireEvent.click(screen.getByRole("checkbox", { name: "first" }));
    fireEvent.click(screen.getByRole("checkbox", { name: "second" }));
    second.lifecycle = "disabled";
    fireEvent.click(screen.getByRole("button", { name: "Save attachments" }));
    await screen.findByRole("alert");
    expect(mocks.attach).not.toHaveBeenCalled();
    expect(mocks.detach).not.toHaveBeenCalled();
  });
  it("waits for all partial writes and requires refresh before retry", async () => {
    let finish!: () => void;
    mocks.attach
      .mockRejectedValueOnce(new Error("conflict"))
      .mockImplementationOnce(
        () =>
          new Promise<void>((resolve) => {
            finish = resolve;
          }),
      );
    mount();
    await choose();
    for (const checkbox of screen.getAllByRole("checkbox"))
      fireEvent.click(checkbox);
    fireEvent.click(screen.getByRole("button", { name: "Save attachments" }));
    await waitFor(() => expect(mocks.attach).toHaveBeenCalledTimes(2));
    expect(
      (
        screen.getByRole("button", {
          name: "Refresh accounts",
        }) as HTMLButtonElement
      ).disabled,
    ).toBe(true);
    finish();
    expect((await screen.findByRole("alert")).textContent).toContain(
      "Some changes may have succeeded",
    );
    expect(mocks.list).toHaveBeenCalledTimes(1);
    fireEvent.click(screen.getByRole("button", { name: "Refresh accounts" }));
    await choose();
    await waitFor(() => expect(mocks.list).toHaveBeenCalledTimes(2));
    fireEvent.click(screen.getAllByRole("checkbox")[0]!);
    expect(
      screen.getByRole("button", { name: "Save attachments" }),
    ).toHaveProperty("disabled", false);
    fireEvent.click(screen.getByRole("button", { name: "Save attachments" }));
    await waitFor(() => expect(mocks.attach).toHaveBeenCalledTimes(3));
  });
  it.each(["issuer", "user", "organization"])(
    "aborts old writes after %s switch without refreshing old scope",
    async (scope) => {
      let finish!: () => void;
      mocks.attach.mockImplementation(
        () =>
          new Promise<void>((resolve) => {
            finish = resolve;
          }),
      );
      const view = mount();
      await choose();
      fireEvent.click(screen.getByRole("checkbox", { name: "first" }));
      fireEvent.click(screen.getByRole("button", { name: "Save attachments" }));
      await waitFor(() => expect(mocks.attach).toHaveBeenCalledTimes(1));
      const signal = mocks.attach.mock.calls[0]![2] as AbortSignal;
      if (scope === "user") mocks.session.user.id = "other-user";
      if (scope === "organization") mocks.organization.id = "other-org";
      view.switchIssuer(scope === "issuer" ? "other" : "issuer");
      expect(signal.aborted).toBe(true);
      finish();
      if (scope === "user") {
        await screen.findByText(
          "Create an agent you own to attach upstream sessions.",
        );
        expect(screen.queryByRole("combobox")).toBeNull();
      } else {
        await screen.findByRole("combobox");
      }
      expect(mocks.list).toHaveBeenCalledTimes(2);
    },
  );
});

vi.mock("@gram/client/react-query/remoteSessionsAttachBinding.js", () => ({
  useRemoteSessionsAttachBindingMutation: () => ({
    mutateAsync: ({
      request,
      options,
    }: {
      request: {
        attachBindingRequestBody: {
          principalId: string;
          remoteSessionId: string;
        };
      };
      options?: { signal?: AbortSignal };
    }) => {
      const body = request.attachBindingRequestBody;
      return options
        ? mocks.attach(body.principalId, body.remoteSessionId, options.signal)
        : mocks.attach(body.principalId, body.remoteSessionId);
    },
  }),
}));
vi.mock("@gram/client/react-query/remoteSessionsDetachBinding.js", () => ({
  useRemoteSessionsDetachBindingMutation: () => ({
    mutateAsync: ({
      request,
      options,
    }: {
      request: {
        detachBindingRequestBody: { principalId: string; id: string };
      };
      options?: { signal?: AbortSignal };
    }) => {
      const body = request.detachBindingRequestBody;
      return options
        ? mocks.detach(body.principalId, body.id, options.signal)
        : mocks.detach(body.principalId, body.id);
    },
  }),
}));
