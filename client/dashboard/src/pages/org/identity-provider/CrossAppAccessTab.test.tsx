import { afterEach, beforeEach, expect, it, vi } from "vitest";
import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router";
import { TooltipProvider } from "@/components/ui/Tooltip";
import type { OktaIdentityProviderConnection } from "@gram/client/models/components/oktaidentityproviderconnection.js";
import type { XaaServerReadiness } from "@gram/client/models/components/xaaserverreadiness.js";
import { CrossAppAccessTab } from "./CrossAppAccessTab";

const mocks = vi.hoisted(() => ({
  confirm: vi.fn(),
  reset: vi.fn(),
  resetState: vi.fn(),
  resetOptions: {} as {
    onSuccess?: () => void;
    onError?: (error: Error) => void;
  },
  readiness: vi.fn(),
  applications: vi.fn(),
  invalidate: vi.fn(),
}));
vi.mock("@gram/client/react-query/xaaReadiness.js", () => ({
  useXaaReadiness: mocks.readiness,
}));
vi.mock("@gram/client/react-query/confirmXaaConnections.js", () => ({
  useConfirmXaaConnectionsMutation: () => ({ mutateAsync: mocks.confirm }),
}));
vi.mock("@gram/client/react-query/resetXaaConnection.js", () => ({
  useResetXaaConnectionMutation: (options: typeof mocks.resetOptions) => {
    mocks.resetOptions = options;
    return { isPending: false, mutate: mocks.reset, reset: mocks.resetState };
  },
}));
vi.mock(
  "@gram/client/react-query/identityProviderConnectionApplications.js",
  () => ({
    useIdentityProviderConnectionApplications: mocks.applications,
  }),
);
vi.mock("@gram/client/react-query/_context.js", () => ({
  useGramContext: () => ({}),
}));
vi.mock("./identityProviderQueries", async (importOriginal) => ({
  ...(await importOriginal<typeof import("./identityProviderQueries")>()),
  invalidateIdentityProviderQueries: mocks.invalidate,
}));

afterEach(cleanup);
beforeEach(() => {
  vi.resetAllMocks();
  mocks.applications.mockReturnValue({ data: { applications: [] } });
  HTMLElement.prototype.scrollIntoView = vi.fn<() => void>();
});

function row(index: number): XaaServerReadiness {
  return {
    mcpServerId: `server-${index}`,
    serverName: `Server ${index}`,
    serverSlug: `server-${index}`,
    projectId: "project",
    projectSlug: "project",
    resourceIndicator: "https://resource.example.com",
    issuerId: "00000000-0000-4000-8000-000000000001",
    scopes: [],
    clientBinding: "single",
    state: "needs_connection",
    pending: true,
  };
}
function setReadiness(rows: XaaServerReadiness[], placeholder = false) {
  mocks.readiness.mockReturnValue({
    data: {
      servers: rows,
      totalCount: rows.length,
      pendingCount: rows.length,
      undiscoveredCount: 0,
      agentRecorded: true,
    },
    isPlaceholderData: placeholder,
  });
}
function show(rows: XaaServerReadiness[], placeholder = false) {
  setReadiness(rows, placeholder);
  const client = new QueryClient();
  const ui = () => (
    <QueryClientProvider client={client}>
      <MemoryRouter>
        <TooltipProvider>
          <CrossAppAccessTab
            connection={
              {
                id: "connection",
                status: "verified",
              } as OktaIdentityProviderConnection
            }
            rolloutEnabled
          />
        </TooltipProvider>
      </MemoryRouter>
    </QueryClientProvider>
  );
  const view = render(ui());
  return {
    ...view,
    refresh: (nextRows: XaaServerReadiness[], stale = false) => {
      setReadiness(nextRows, stale);
      view.rerender(ui());
    },
  };
}
function selectAndConfirm(count: number) {
  fireEvent.click(
    screen.getByRole("checkbox", {
      name: "Select every server that can be confirmed",
    }),
  );
  fireEvent.change(screen.getByLabelText("Issuer URL"), {
    target: { value: "https://issuer.example.com" },
  });
  fireEvent.click(
    screen.getByRole("button", {
      name: `Confirm ${count} ${count === 1 ? "server" : "servers"}`,
    }),
  );
}

it("freezes the selection and filter during confirmation", async () => {
  let resolve!: (value: { servers: XaaServerReadiness[] }) => void;
  mocks.confirm.mockImplementation(
    () =>
      new Promise((done) => {
        resolve = done;
      }),
  );
  show([row(0), row(1)]);
  selectAndConfirm(2);
  expect(
    screen.getByRole("button", { name: "All" }).hasAttribute("disabled"),
  ).toBe(true);
  for (const checkbox of screen.getAllByRole("checkbox"))
    expect(checkbox.hasAttribute("disabled")).toBe(true);
  fireEvent.keyDown(screen.getByLabelText("Issuer URL"), {
    key: "Enter",
  });
  expect(mocks.confirm).toHaveBeenCalledTimes(1);
  resolve({ servers: [row(0), row(1)] });
  await waitFor(() =>
    expect(
      screen.getByRole("button", { name: "All" }).hasAttribute("disabled"),
    ).toBe(false),
  );
});

it("keeps only failed batches selected and reports partial success", async () => {
  const rows = Array.from({ length: 201 }, (_, index) => row(index));
  mocks.confirm
    .mockResolvedValueOnce({ servers: rows.slice(0, 200) })
    .mockRejectedValueOnce(new Error("Retry this batch"));
  show(rows);
  selectAndConfirm(201);
  await waitFor(() =>
    expect(
      screen.getByText(/200 servers confirmed before the request failed/),
    ).toBeTruthy(),
  );
  expect(screen.getByRole("button", { name: "Confirm 1 server" })).toBeTruthy();
  expect(
    screen
      .getByRole("checkbox", { name: "Select Server 0" })
      .getAttribute("aria-checked"),
  ).toBe("false");
  expect(
    screen
      .getByRole("checkbox", { name: "Select Server 200" })
      .getAttribute("aria-checked"),
  ).toBe("true");
  mocks.confirm.mockResolvedValueOnce({ servers: [rows[200]] });
  fireEvent.click(screen.getByRole("button", { name: "Confirm 1 server" }));
  await waitFor(() => expect(mocks.confirm).toHaveBeenCalledTimes(3));
  expect(
    mocks.confirm.mock.calls[2]?.[0].request.confirmXaaConnectionsRequestBody
      .connections,
  ).toEqual([
    { mcpServerId: "server-200", audience: "https://issuer.example.com" },
  ]);
});

it("does not allow selecting stale rows while a filter loads", () => {
  show([row(0)], true);
  expect(screen.getByRole("status").textContent).toBe("Loading servers...");
  for (const checkbox of screen.getAllByRole("checkbox"))
    expect(checkbox.hasAttribute("disabled")).toBe(true);
});

it("counts a shared server only once across successful batches", async () => {
  const rows = Array.from({ length: 201 }, (_, index) => row(index));
  mocks.confirm
    .mockResolvedValueOnce({ servers: rows.slice(0, 200) })
    .mockResolvedValueOnce({ servers: rows.slice(199) });
  show(rows);
  selectAndConfirm(201);
  await waitFor(() =>
    expect(
      screen.getByText(/201 servers confirmed and moved out of Needs action/),
    ).toBeTruthy(),
  );
  expect(screen.queryByText(/202 servers confirmed/)).toBeNull();
});

it("preserves long copyable values and reserves a separate track for each copy control", () => {
  const server = {
    ...row(1),
    state: "connected" as const,
    resourceIndicator: `https://resource.example.com/${"resource".repeat(12)}`,
    clientId: `client-${"id".repeat(40)}`,
    scopes: ["read", `urn:example:${"scope".repeat(20)}`],
    audience: `https://auth.example.com/${"audience".repeat(12)}`,
  };
  show([server]);
  expect(
    screen.getByRole("columnheader", { name: "Okta configuration" }),
  ).toBeTruthy();
  expect(
    screen.queryByRole("columnheader", { name: "Client ID at resource" }),
  ).toBeNull();
  expect(screen.getByRole("table").className).toContain("[&_td]:items-center");
  for (const label of [
    "resource indicator",
    "client ID",
    "scopes",
    "Issuer URL",
  ]) {
    expect(screen.getByRole("button", { name: `Copy ${label}` })).toBeTruthy();
  }
  for (const value of [
    server.resourceIndicator,
    server.clientId,
    server.scopes.join(" "),
    server.audience,
  ]) {
    const text = screen.getByTitle(value);
    expect(text.textContent).toBe(value);
    expect(text.className).toContain("truncate");
    const copyCell = text.parentElement;
    expect(copyCell?.className).toContain("grid-cols-[minmax(0,1fr)_auto]");
    expect(copyCell?.querySelector("button")).not.toBeNull();
  }
});

const connectionsUrl =
  "https://example-admin.okta.com/admin/workload-principals/ai-agents/example-agent/resource-connections";

function confirmedRow(): XaaServerReadiness {
  return {
    ...row(0),
    state: "connected",
    pending: false,
    audience: "https://issuer.example.com/saved",
    oktaApplicationId: "recorded-app",
    deepLink: `${connectionsUrl}/create`,
  };
}

function openClearDialog(serverName = "Server 0") {
  fireEvent.keyDown(
    screen.getByRole("button", { name: `More actions for ${serverName}` }),
    { key: "ArrowDown" },
  );
  fireEvent.click(screen.getByRole("menuitem", { name: "Clear confirmation" }));
  return screen.getByRole("dialog", { name: "Clear this confirmation?" });
}

function submitClear(serverName = "Server 0") {
  const dialog = openClearDialog(serverName);
  expect(dialog.textContent).toContain(
    "does not delete or change the connection in Okta",
  );
  expect(dialog.textContent).toContain(
    "Other servers that share this confirmation",
  );
  fireEvent.click(
    within(dialog).getByRole("button", { name: "Clear confirmation" }),
  );
}

it("reviews and edits without resetting or dropping an unavailable recorded app", async () => {
  const server = confirmedRow();
  mocks.confirm.mockResolvedValue({ servers: [server] });
  show([server]);
  fireEvent.click(screen.getByRole("button", { name: "Review / edit" }));
  expect((screen.getByLabelText("Issuer URL") as HTMLInputElement).value).toBe(
    server.audience,
  );
  expect(screen.getByRole("combobox").textContent).toContain(
    "Keep recorded app",
  );
  expect(screen.getByText(/retains the current app/)).toBeTruthy();
  fireEvent.change(screen.getByLabelText("Issuer URL"), {
    target: { value: "https://issuer.example.com/edited" },
  });
  fireEvent.click(screen.getByRole("button", { name: "Save confirmation" }));
  await waitFor(() => expect(mocks.confirm).toHaveBeenCalledTimes(1));
  // Omission means retain the backend app binding, not clear it.
  expect(
    mocks.confirm.mock.calls[0]?.[0].request.confirmXaaConnectionsRequestBody
      .connections,
  ).toEqual([
    {
      mcpServerId: server.mcpServerId,
      audience: "https://issuer.example.com/edited",
    },
  ]);
  expect(mocks.reset).not.toHaveBeenCalled();
  await waitFor(() =>
    expect(
      screen.queryByRole("button", { name: "Save confirmation" }),
    ).toBeNull(),
  );
});

it("opens existing Okta connections first with creation an explicit review action", () => {
  show([confirmedRow()]);
  fireEvent.keyDown(
    screen.getByRole("button", { name: "More actions for Server 0" }),
    { key: "ArrowDown" },
  );
  const menuLink = screen.getByRole("menuitem", {
    name: "Open Okta connections",
  });
  expect(menuLink.getAttribute("href")).toBe(connectionsUrl);
  fireEvent.keyDown(menuLink, { key: "Escape" });
  fireEvent.click(screen.getByRole("button", { name: "Review / edit" }));
  const listLink = screen.getByRole("link", { name: "Open Okta connections" });
  const createLink = screen.getByRole("link", { name: "Create a connection" });
  expect(listLink.getAttribute("href")).toBe(connectionsUrl);
  expect(createLink.getAttribute("href")).toBe(`${connectionsUrl}/create`);
  expect(
    listLink.compareDocumentPosition(createLink) &
      Node.DOCUMENT_POSITION_FOLLOWING,
  ).toBeTruthy();
});

it("clears through the menu and dialog, then Undo restores the saved audience and app", async () => {
  availableApp();
  const server = confirmedRow();
  mocks.confirm.mockResolvedValue({ servers: [server] });
  show([server]);
  submitClear();
  expect(mocks.reset).toHaveBeenCalledWith(
    expect.objectContaining({
      request: {
        resetXaaConnectionRequestBody: { mcpServerId: server.mcpServerId },
      },
    }),
  );
  expect(screen.queryByRole("button", { name: "Undo" })).toBeNull();
  act(() => mocks.resetOptions.onSuccess?.());
  expect(screen.queryByRole("dialog")).toBeNull();
  expect(
    screen.getByText(/Previous settings are retained on this page/),
  ).toBeTruthy();
  fireEvent.click(screen.getByRole("button", { name: "Undo" }));
  await waitFor(() => expect(mocks.confirm).toHaveBeenCalledTimes(1));
  expect(
    mocks.confirm.mock.calls[0]?.[0].request.confirmXaaConnectionsRequestBody
      .connections,
  ).toEqual([
    {
      mcpServerId: server.mcpServerId,
      audience: server.audience,
      oktaApplicationId: server.oktaApplicationId,
    },
  ]);
  await waitFor(() =>
    expect(screen.queryByRole("button", { name: "Undo" })).toBeNull(),
  );
  expect(mocks.invalidate).toHaveBeenCalled();
});

it("keeps a failed clear open without claiming success or discarding the saved confirmation", () => {
  const server = confirmedRow();
  show([server]);
  submitClear();
  act(() => mocks.resetOptions.onError?.(new Error("Clear failed")));
  const dialog = screen.getByRole("dialog", {
    name: "Clear this confirmation?",
  });
  expect(
    within(dialog)
      .getByRole("button", { name: "Clear confirmation" })
      .hasAttribute("disabled"),
  ).toBe(false);
  expect(screen.queryByRole("button", { name: "Undo" })).toBeNull();
  expect(mocks.invalidate).not.toHaveBeenCalled();
  fireEvent.click(within(dialog).getByRole("button", { name: "Cancel" }));
  expect(mocks.resetState).toHaveBeenCalled();
  fireEvent.click(screen.getByRole("button", { name: "Review / edit" }));
  expect((screen.getByLabelText("Issuer URL") as HTMLInputElement).value).toBe(
    server.audience,
  );
  expect(mocks.confirm).not.toHaveBeenCalled();
});

it("retains Undo and guards recovery with an unavailable app after Undo fails", async () => {
  availableApp();
  const server = confirmedRow();
  mocks.confirm.mockRejectedValueOnce(new Error("Restore failed"));
  show([server]);
  submitClear();
  act(() => mocks.resetOptions.onSuccess?.());
  fireEvent.click(screen.getByRole("button", { name: "Undo" }));
  await waitFor(() => expect(screen.getByText(/Restore failed/)).toBeTruthy());
  expect(
    screen.getByRole("button", { name: "Undo" }).hasAttribute("disabled"),
  ).toBe(false);
  expect(
    screen.getByText(/Previous settings are retained on this page/),
  ).toBeTruthy();
  mocks.applications.mockReturnValue({ data: { applications: [] } });
  fireEvent.click(screen.getByRole("button", { name: "Review setup" }));
  expect((screen.getByLabelText("Issuer URL") as HTMLInputElement).value).toBe(
    server.audience,
  );
  expect(screen.getByRole("combobox").textContent).toContain(
    "Unavailable application",
  );
  expect(screen.getByText(/no longer available/)).toBeTruthy();
  expect(
    screen
      .getByRole("button", { name: "Confirm setup" })
      .hasAttribute("disabled"),
  ).toBe(true);
  fireEvent.click(screen.getByRole("button", { name: "Confirm setup" }));
  expect(mocks.confirm).toHaveBeenCalledTimes(1);
  expect(mocks.reset).toHaveBeenCalledTimes(1);
});

function availableApp() {
  mocks.applications.mockReturnValue({
    data: {
      applications: [{ oktaAppId: "recorded-app", label: "Recorded app" }],
    },
  });
}

it("warns before clearing an unavailable app, disables Undo, and guards the recovery form", () => {
  const server = confirmedRow();
  show([server]);
  const dialog = openClearDialog();
  expect(dialog.textContent).toContain(
    "Undo cannot fully restore these settings",
  );
  fireEvent.click(
    within(dialog).getByRole("button", { name: "Clear confirmation" }),
  );
  act(() => mocks.resetOptions.onSuccess?.());
  const undo = screen.getByRole("button", { name: "Undo" });
  expect(undo.hasAttribute("disabled")).toBe(true);
  fireEvent.click(undo);
  expect(mocks.confirm).not.toHaveBeenCalled();
  expect(screen.getByText(/Undo is unavailable because/)).toBeTruthy();
  fireEvent.click(screen.getByRole("button", { name: "Review setup" }));
  expect((screen.getByLabelText("Issuer URL") as HTMLInputElement).value).toBe(
    server.audience,
  );
  expect(screen.getByRole("combobox").textContent).toContain(
    "Unavailable application",
  );
  expect(
    screen
      .getByRole("button", { name: "Confirm setup" })
      .hasAttribute("disabled"),
  ).toBe(true);
  fireEvent.keyDown(screen.getByRole("combobox"), { key: "ArrowDown" });
  fireEvent.click(screen.getByRole("option", { name: "Not recorded" }));
  expect(
    screen
      .getByRole("button", { name: "Confirm setup" })
      .hasAttribute("disabled"),
  ).toBe(false);
});

it("retains multiple independent clears and Undo restores each snapshot separately", async () => {
  availableApp();
  const first = confirmedRow();
  const second = {
    ...confirmedRow(),
    ...row(1),
    state: "connected" as const,
    pending: false,
    resourceIndicator: "https://other-resource.example.com",
    audience: "https://issuer.example.com/other",
  };
  mocks.confirm
    .mockResolvedValueOnce({ servers: [first] })
    .mockResolvedValueOnce({ servers: [second] });
  show([first, second]);
  submitClear();
  act(() => mocks.resetOptions.onSuccess?.());
  submitClear(second.serverName);
  act(() => mocks.resetOptions.onSuccess?.());
  expect(screen.getAllByRole("button", { name: "Undo" })).toHaveLength(2);
  expect(screen.getByText(/Confirmation cleared for Server 0/)).toBeTruthy();
  expect(screen.getByText(/Confirmation cleared for Server 1/)).toBeTruthy();
  fireEvent.click(screen.getAllByRole("button", { name: "Undo" })[0]!);
  await waitFor(() =>
    expect(screen.getAllByRole("button", { name: "Undo" })).toHaveLength(1),
  );
  expect(screen.getByText(/Confirmation cleared for Server 1/)).toBeTruthy();
  fireEvent.click(screen.getByRole("button", { name: "Undo" }));
  await waitFor(() =>
    expect(screen.queryByRole("button", { name: "Undo" })).toBeNull(),
  );
  expect(
    mocks.confirm.mock.calls.map(
      ([input]) => input.request.confirmXaaConnectionsRequestBody.connections,
    ),
  ).toEqual([
    [
      {
        mcpServerId: first.mcpServerId,
        audience: first.audience,
        oktaApplicationId: first.oktaApplicationId,
      },
    ],
    [
      {
        mcpServerId: second.mcpServerId,
        audience: second.audience,
        oktaApplicationId: second.oktaApplicationId,
      },
    ],
  ]);
});

it("retires shared-resource Undo after a sibling server is confirmed individually", async () => {
  availableApp();
  const sibling = row(1);
  mocks.confirm.mockResolvedValue({ servers: [sibling] });
  show([confirmedRow(), sibling]);
  submitClear();
  act(() => mocks.resetOptions.onSuccess?.());
  expect(screen.getByRole("button", { name: "Undo" })).toBeTruthy();
  const siblingRow = screen.getByText(sibling.serverName).closest("tr")!;
  fireEvent.click(
    within(siblingRow).getByRole("button", { name: "Review setup" }),
  );
  fireEvent.change(screen.getByLabelText("Issuer URL"), {
    target: { value: "https://issuer.example.com/new" },
  });
  fireEvent.click(screen.getByRole("button", { name: "Confirm setup" }));
  await waitFor(() =>
    expect(screen.queryByRole("button", { name: "Undo" })).toBeNull(),
  );
  expect(mocks.confirm).toHaveBeenCalledTimes(1);
});

it("retires shared-resource Undo on successful bulk batches even when a later batch fails", async () => {
  availableApp();
  const siblings = Array.from({ length: 201 }, (_, index) => row(index + 1));
  mocks.confirm
    .mockResolvedValueOnce({ servers: siblings.slice(0, 200) })
    .mockRejectedValueOnce(new Error("Later batch failed"));
  show([confirmedRow(), ...siblings]);
  submitClear();
  act(() => mocks.resetOptions.onSuccess?.());
  expect(screen.getByRole("button", { name: "Undo" })).toBeTruthy();
  selectAndConfirm(201);
  await waitFor(() =>
    expect(
      screen.getByText(/200 servers confirmed before the request failed/),
    ).toBeTruthy(),
  );
  expect(screen.queryByRole("button", { name: "Undo" })).toBeNull();
  expect(screen.getByRole("button", { name: "Confirm 1 server" })).toBeTruthy();
});

it("retires stale Undo on fresh confirmed sibling readiness, but not placeholder data", () => {
  availableApp();
  const first = confirmedRow();
  const view = show([first]);
  submitClear();
  act(() => mocks.resetOptions.onSuccess?.());
  const sibling = { ...row(1), state: "connected" as const, pending: false };
  view.refresh([sibling], true);
  expect(screen.getByRole("button", { name: "Undo" })).toBeTruthy();
  view.refresh([sibling]);
  expect(screen.queryByRole("button", { name: "Undo" })).toBeNull();
  expect(mocks.confirm).not.toHaveBeenCalled();
});

it("does not prefill another server from a cleared snapshot with the same resource", () => {
  availableApp();
  const first = confirmedRow();
  const second = {
    ...confirmedRow(),
    mcpServerId: "server-1",
    serverName: "Server 1",
    serverSlug: "server-1",
    audience: "https://other-issuer.example.com/saved",
    oktaApplicationId: "other-app",
  };
  mocks.applications.mockReturnValue({
    data: {
      applications: [
        { oktaAppId: "recorded-app", label: "Recorded app" },
        { oktaAppId: "other-app", label: "Other app" },
      ],
    },
  });
  show([first, second]);
  submitClear();
  act(() => mocks.resetOptions.onSuccess?.());
  const secondRow = screen.getByText(second.serverName).closest("tr")!;
  fireEvent.click(
    within(secondRow).getByRole("button", { name: "Review / edit" }),
  );
  expect((screen.getByLabelText("Issuer URL") as HTMLInputElement).value).toBe(
    second.audience,
  );
  expect(screen.getByRole("combobox").textContent).toContain("Other app");
  expect(mocks.confirm).not.toHaveBeenCalled();
});

it("keeps separate cleared snapshots for different servers sharing a resource", () => {
  availableApp();
  const first = confirmedRow();
  const second = {
    ...confirmedRow(),
    mcpServerId: "server-1",
    serverName: "Server 1",
    audience: "https://other-issuer.example.com/saved",
  };
  show([first, second]);
  submitClear();
  act(() => mocks.resetOptions.onSuccess?.());
  submitClear(second.serverName);
  act(() => mocks.resetOptions.onSuccess?.());
  expect(screen.getAllByRole("button", { name: "Undo" })).toHaveLength(2);
  expect(screen.getByText(/Confirmation cleared for Server 0/)).toBeTruthy();
  expect(screen.getByText(/Confirmation cleared for Server 1/)).toBeTruthy();
});

it.each([
  "javascript:alert(1)",
  "blob:https://tenant.okta.com/example",
  "http://tenant.okta.com/create",
  "https://attacker.example/create",
  "https://tenant.okta.com.attacker.example/create",
])("does not render unsafe review links: %s", (deepLink) => {
  show([{ ...confirmedRow(), deepLink }]);
  fireEvent.click(screen.getByRole("button", { name: "Review / edit" }));
  expect(
    screen.queryByRole("link", { name: "Create a connection" }),
  ).toBeNull();
  expect(
    screen.queryByRole("link", { name: "Open Okta connections" }),
  ).toBeNull();
});

it("retains Undo when a distinct issuer ID sharing the resource is confirmed", async () => {
  availableApp();
  const sibling = {
    ...row(1),
    issuerId: "00000000-0000-4000-8000-000000000002",
  };
  mocks.confirm.mockResolvedValue({ servers: [sibling] });
  const view = show([confirmedRow(), sibling]);
  submitClear();
  act(() => mocks.resetOptions.onSuccess?.());
  const siblingRow = screen.getByText(sibling.serverName).closest("tr")!;
  fireEvent.click(
    within(siblingRow).getByRole("button", { name: "Review setup" }),
  );
  fireEvent.change(screen.getByLabelText("Issuer URL"), {
    target: { value: "https://issuer.example.com/new" },
  });
  fireEvent.click(screen.getByRole("button", { name: "Confirm setup" }));
  await waitFor(() => expect(mocks.confirm).toHaveBeenCalledTimes(1));
  await waitFor(() =>
    expect(screen.queryByRole("button", { name: "Confirm setup" })).toBeNull(),
  );
  expect(screen.getByRole("button", { name: "Undo" })).toBeTruthy();
  view.refresh([{ ...sibling, state: "connected", pending: false }]);
  expect(screen.getByRole("button", { name: "Undo" })).toBeTruthy();
});

it("retains Undo across bulk confirmation of a distinct issuer ID sharing the resource", async () => {
  availableApp();
  const sibling = {
    ...row(1),
    issuerId: "00000000-0000-4000-8000-000000000002",
  };
  mocks.confirm.mockResolvedValue({ servers: [sibling] });
  const first = confirmedRow();
  const view = show([first, sibling]);
  submitClear();
  act(() => mocks.resetOptions.onSuccess?.());
  view.refresh([sibling]);
  selectAndConfirm(1);
  await waitFor(() => expect(mocks.confirm).toHaveBeenCalledTimes(1));
  await waitFor(() =>
    expect(
      screen.getByText(/1 server confirmed and moved out of Needs action/),
    ).toBeTruthy(),
  );
  expect(screen.getByRole("button", { name: "Undo" })).toBeTruthy();
});
