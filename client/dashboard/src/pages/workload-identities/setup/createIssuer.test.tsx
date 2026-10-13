import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, expect, it, vi } from "vitest";
import { TooltipProvider } from "@/components/ui/Tooltip";
import { ServiceError } from "@gram/client/models/errors/serviceerror.js";
import { ComputedStatus } from "./SetupBlocks";
import { useSetupValues } from "./setupValues";

/**
 * Creating an authorization server from the console step: who may, what a
 * failure says, and that the created issuer is the one selected afterwards.
 */

const state = vi.hoisted(() => ({
  endpoints: [] as { userSessionIssuerId: string }[],
  isPending: false,
  isError: false,
  isAdmin: true,
  mutateAsync: vi.fn(),
  invalidate: vi.fn(),
}));

vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({
    isLoading: false,
    hasScope: () => state.isAdmin,
    hasAllScopes: () => state.isAdmin,
    hasAnyScope: () => state.isAdmin,
  }),
}));
vi.mock("@gram/client/react-query/workloadTokenEndpoints.js", () => ({
  useWorkloadTokenEndpoints: () => ({
    data:
      state.isPending || state.isError ? undefined : { items: state.endpoints },
    isPending: state.isPending,
    isError: state.isError,
  }),
  invalidateAllWorkloadTokenEndpoints: state.invalidate,
}));
vi.mock(
  "@gram/client/react-query/createSharedOrganizationUserSessionIssuer.js",
  () => ({
    useCreateSharedOrganizationUserSessionIssuerMutation: () => ({
      mutateAsync: state.mutateAsync,
      isPending: false,
    }),
  }),
);

function endpoint(id: string) {
  return {
    userSessionIssuerId: id,
    userSessionIssuerSlug: id,
    projectId: "",
    projectName: "",
    issuer: `https://gram.example.com/oauth/usi/${id}`,
    tokenEndpoint: `https://gram.example.com/oauth/usi/${id}/token`,
    mcpHost: "gram.example.com",
  };
}

function Harness(): JSX.Element {
  const setupValues = useSetupValues();
  return (
    <>
      <ComputedStatus setupValues={setupValues} />
      <output data-testid="selected">{setupValues.selectedEndpointId}</output>
      <output data-testid="issuer-url">
        {setupValues.values?.issuer_url ?? ""}
      </output>
    </>
  );
}

function renderHarness() {
  render(
    <QueryClientProvider client={new QueryClient()}>
      <TooltipProvider>
        <Harness />
      </TooltipProvider>
    </QueryClientProvider>,
  );
}

afterEach(() => {
  cleanup();
  state.endpoints = [];
  state.isPending = false;
  state.isError = false;
  state.isAdmin = true;
  state.mutateAsync.mockReset();
  state.invalidate.mockReset();
});

it("offers to create an authorization server only when none serves a token endpoint", () => {
  renderHarness();
  expect(
    screen.getByText(/An organization administrator can create one here/),
  ).toBeTruthy();
  expect(
    screen.getByRole("button", { name: "Create authorization server" }),
  ).toBeTruthy();

  cleanup();
  state.endpoints = [endpoint("o1")];
  renderHarness();
  expect(
    screen.queryByRole("button", { name: "Create authorization server" }),
  ).toBeNull();
  expect(screen.getByTestId("selected").textContent).toBe("o1");
});

it("does not offer creation while the list is loading or failed", () => {
  state.isPending = true;
  renderHarness();
  expect(
    screen.queryByRole("button", { name: "Create authorization server" }),
  ).toBeNull();
  cleanup();
  state.isPending = false;
  state.isError = true;
  renderHarness();
  expect(
    screen.queryByRole("button", { name: "Create authorization server" }),
  ).toBeNull();
  expect(screen.getByText(/couldn't load its token endpoints/)).toBeTruthy();
});

it("disables creation for anyone but an organization administrator", () => {
  state.isAdmin = false;
  renderHarness();
  const button = screen.getByRole("button", {
    name: "Create authorization server",
  });
  expect(button.hasAttribute("disabled")).toBe(true);
  expect(state.mutateAsync).not.toHaveBeenCalled();
});

it("selects the created issuer once the refetched list holds it", async () => {
  state.mutateAsync.mockResolvedValue({ id: "new" });
  // The refetch is what brings the created issuer into the list; until it
  // lands the first issuer would be the one selected.
  state.invalidate.mockImplementation(async () => {
    state.endpoints = [endpoint("o1"), endpoint("new")];
  });
  renderHarness();
  fireEvent.click(
    screen.getByRole("button", { name: "Create authorization server" }),
  );
  await waitFor(() =>
    expect(screen.getByTestId("selected").textContent).toBe("new"),
  );
  expect(screen.getByTestId("issuer-url").textContent).toBe(
    "https://gram.example.com/oauth/usi/new",
  );
  expect(keyOf(state.mutateAsync.mock.calls[0])).toMatch(/[0-9a-f-]{36}/);
  expect(
    screen.queryByRole("button", { name: "Create authorization server" }),
  ).toBeNull();
});

it("keeps the attempt key until the created issuer is listed", async () => {
  let release: () => void = () => {};
  state.mutateAsync.mockResolvedValue({ id: "new" });
  state.invalidate.mockImplementation(
    () =>
      new Promise<void>((resolve) => {
        release = () => {
          state.endpoints = [endpoint("new")];
          resolve();
        };
      }),
  );
  renderHarness();
  const button = screen.getByRole("button", {
    name: "Create authorization server",
  });
  fireEvent.click(button);
  await waitFor(() => expect(state.invalidate).toHaveBeenCalledTimes(1));
  // The list has not caught up, so the button still shows but is busy: a
  // second click must not start a second attempt.
  expect(screen.getByRole("button", { name: "Creating…" })).toBeTruthy();
  fireEvent.click(screen.getByRole("button", { name: "Creating…" }));
  expect(state.mutateAsync).toHaveBeenCalledTimes(1);
  release();
  await waitFor(() =>
    expect(screen.getByTestId("selected").textContent).toBe("new"),
  );
});

it("reports a failure, offers a retry with the same attempt, and refetches", async () => {
  state.mutateAsync.mockRejectedValueOnce(new Error("network down"));
  renderHarness();
  fireEvent.click(
    screen.getByRole("button", { name: "Create authorization server" }),
  );
  expect(
    await screen.findByText("Failed to create the authorization server"),
  ).toBeTruthy();
  expect(state.invalidate).toHaveBeenCalledTimes(1);
  const firstKey = keyOf(state.mutateAsync.mock.calls[0]);

  state.mutateAsync.mockResolvedValueOnce({ id: "new" });
  fireEvent.click(screen.getByRole("button", { name: "Try again" }));
  await waitFor(() => expect(state.invalidate).toHaveBeenCalledTimes(2));
  expect(keyOf(state.mutateAsync.mock.calls[1])).toBe(firstKey);
  expect(
    screen.queryByText("Failed to create the authorization server"),
  ).toBeNull();
});

it("does not show a transport error's own text", async () => {
  state.mutateAsync.mockRejectedValueOnce(new Error("ECONNRESET at 10.0.0.1"));
  renderHarness();
  fireEvent.click(
    screen.getByRole("button", { name: "Create authorization server" }),
  );
  expect(
    await screen.findByText("Failed to create the authorization server"),
  ).toBeTruthy();
  expect(screen.queryByText(/ECONNRESET/)).toBeNull();
});

it("explains a permission refusal in the administrator's terms", async () => {
  state.mutateAsync.mockRejectedValueOnce(
    new ServiceError(
      {
        fault: false,
        id: "error",
        message: "forbidden",
        name: "forbidden",
        temporary: false,
        timeout: false,
      },
      {
        request: new Request("https://platform.example"),
        response: new Response(null, { status: 403 }),
        body: "",
      },
    ),
  );
  renderHarness();
  fireEvent.click(
    screen.getByRole("button", { name: "Create authorization server" }),
  );
  expect(
    await screen.findByText(/Only an organization administrator/),
  ).toBeTruthy();
});

function keyOf(call: unknown[] | undefined): string {
  const [variables] = call as [
    {
      request: {
        createSharedOrganizationUserSessionIssuerForm: {
          idempotencyKey: string;
        };
      };
    },
  ];
  return variables.request.createSharedOrganizationUserSessionIssuerForm
    .idempotencyKey;
}
