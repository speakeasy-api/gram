import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { RegisterPanel } from "./register-panel";

const locationReplace = vi.fn();

const mocks = vi.hoisted(() => ({
  authRegister: vi.fn(),
  authInfo: vi.fn(),
  useGramContext: vi.fn(),
  useMutation: vi.fn(),
  useTelemetry: vi.fn(),
}));

vi.mock("@/contexts/Telemetry", () => ({ useTelemetry: mocks.useTelemetry }));
vi.mock("@gram/client/funcs/authInfo", () => ({ authInfo: mocks.authInfo }));
vi.mock("@gram/client/react-query/_context.js", () => ({
  useGramContext: mocks.useGramContext,
}));
vi.mock("@gram/client/funcs/authRegister.js", () => ({
  authRegister: mocks.authRegister,
}));
vi.mock("@tanstack/react-query", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@tanstack/react-query")>();
  return {
    ...actual,
    useMutation: (...args: Parameters<typeof actual.useMutation>) =>
      mocks.useMutation.getMockImplementation()
        ? mocks.useMutation(...args)
        : actual.useMutation(...args),
  };
});

let queryClient: QueryClient;

beforeEach(() => {
  mocks.useTelemetry.mockReturnValue({ capture: vi.fn() });
  mocks.useGramContext.mockReturnValue({});
  // Only the callback-race tests below replace the real hook implementation.
  mocks.useMutation.mockReset();
  queryClient = new QueryClient({
    defaultOptions: { mutations: { retry: false } },
  });
  mocks.authRegister
    .mockReset()
    .mockResolvedValue({ ok: true, value: undefined });
  mocks.authInfo.mockResolvedValue({
    ok: true,
    value: {
      result: {
        activeOrganizationId: "new-org-id",
        organizations: [{ id: "new-org-id", slug: "new-org" }],
      },
    },
  });
  vi.stubGlobal("location", {
    origin: "https://app.example",
    replace: locationReplace,
  });
});

afterEach(() => {
  cleanup();
  queryClient.clear();
  vi.clearAllMocks();
  vi.unstubAllGlobals();
});

describe("RegisterPanel", () => {
  it("returns to the preserved destination after organization creation", async () => {
    const user = userEvent.setup();
    render(
      <QueryClientProvider client={queryClient}>
        <MemoryRouter>
          <RegisterPanel redirectTo="/cli/callback" />
        </MemoryRouter>
      </QueryClientProvider>,
    );

    await user.type(screen.getByLabelText("Company name"), "Acme Inc");
    await user.click(
      screen.getByRole("button", { name: /create organization/i }),
    );

    await waitFor(() =>
      expect(locationReplace).toHaveBeenCalledWith(
        "https://app.example/cli/callback",
      ),
    );
  });

  it("returns to the dashboard root when no destination was preserved", async () => {
    const user = userEvent.setup();
    render(
      <QueryClientProvider client={queryClient}>
        <MemoryRouter>
          <RegisterPanel />
        </MemoryRouter>
      </QueryClientProvider>,
    );

    await user.type(screen.getByLabelText("Company name"), "Acme Inc");
    await user.click(
      screen.getByRole("button", { name: /create organization/i }),
    );

    await waitFor(() => expect(locationReplace).toHaveBeenCalledWith("/"));
  });
});

it("retains the partner code after registration fails and retries successfully", async () => {
  mocks.authRegister.mockRejectedValueOnce(new Error("Try again"));
  const user = userEvent.setup();
  render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter>
        <RegisterPanel partnerCode="partner" redirectTo="/cli/callback" />
      </MemoryRouter>
    </QueryClientProvider>,
  );
  expect(screen.getByText(/valid partner code/)).toBeTruthy();
  await user.type(screen.getByLabelText("Company name"), "Acme");
  const submit = screen.getByRole("button", { name: /create organization/i });
  await user.click(submit);

  expect(await screen.findByText("Try again")).toBeTruthy();
  expect(locationReplace).not.toHaveBeenCalled();
  expect(screen.getByLabelText("Partner code (optional)")).toHaveProperty(
    "value",
    "partner",
  );
  expect(submit).toHaveProperty("disabled", false);
  expect(mocks.authRegister.mock.calls[0]?.[1]).toEqual({
    registerRequestBody: { orgName: "Acme", partnerCode: "partner" },
  });

  await user.click(submit);

  await waitFor(() =>
    expect(locationReplace).toHaveBeenCalledWith("/new-org/billing"),
  );
  expect(mocks.authRegister).toHaveBeenCalledTimes(2);
  expect(mocks.authRegister.mock.calls[1]?.[1]).toEqual({
    registerRequestBody: { orgName: "Acme", partnerCode: "partner" },
  });
  expect(screen.queryByText("Try again")).toBeNull();
});

it.each([
  ["partner", "", "/new-org/billing"],
  ["", "partner", "https://app.example/cli/callback"],
])(
  "uses submitted code %j, not the edited field, for the success destination",
  async (submittedCode, editedCode, destination) => {
    const mutate = vi.fn();
    mocks.useMutation.mockImplementation(() => ({
      error: null,
      isPending: false,
      mutate,
    }));
    const user = userEvent.setup();
    render(
      <QueryClientProvider client={queryClient}>
        <MemoryRouter>
          <RegisterPanel
            partnerCode={submittedCode}
            redirectTo="/cli/callback"
          />
        </MemoryRouter>
      </QueryClientProvider>,
    );
    await user.type(screen.getByLabelText("Company name"), "Acme");
    await user.click(
      screen.getByRole("button", { name: /create organization/i }),
    );
    const field = screen.getByLabelText("Partner code (optional)");
    await user.clear(field);
    if (editedCode) await user.type(field, editedCode);
    // Invoke the current callback with the request that actually succeeded.
    mocks.useMutation.mock.lastCall![0].onSuccess(
      { slug: "new-org" },
      mutate.mock.calls[0]![0],
    );
    expect(locationReplace).toHaveBeenCalledWith(destination);
  },
);
