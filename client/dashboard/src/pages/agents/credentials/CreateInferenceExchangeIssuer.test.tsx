import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { CreateInferenceExchangeIssuer } from "./CreateInferenceExchangeIssuer";
const mocks = vi.hoisted(() => ({ create: vi.fn(), permitted: true }));
vi.mock("@/contexts/Auth", () => ({
  useProject: () => ({
    id: "project_one",
    slug: "project-one",
    name: "Project one",
  }),
}));
vi.mock("@/contexts/Sdk", () => ({
  useSdkClient: () => ({ userSessionIssuers: { create: mocks.create } }),
}));
vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({
    hasScope: (scope: string, id: string) =>
      mocks.permitted && scope === "project:write" && id === "project_one",
  }),
}));
vi.mock("@/pages/user-session-issuers/invalidateIssuers", () => ({
  invalidateIssuers: vi.fn().mockResolvedValue(undefined),
}));
afterEach(cleanup);
beforeEach(() => {
  vi.clearAllMocks();
  mocks.permitted = true;
  mocks.create.mockResolvedValue({ id: "new_issuer" });
});
function setup(
  onCreated = vi
    .fn<(id: string) => Promise<void>>()
    .mockResolvedValue(undefined),
) {
  const onBusy = vi.fn<(busy: boolean) => void>();
  render(
    <QueryClientProvider client={new QueryClient()}>
      <CreateInferenceExchangeIssuer onCreated={onCreated} onBusy={onBusy} />
    </QueryClientProvider>,
  );
  return { onCreated, onBusy };
}
it("creates a shared project issuer without any MCP or external issuer API", async () => {
  const { onCreated, onBusy } = setup();
  fireEvent.change(
    screen.getByLabelText("Speakeasy authorization server slug"),
    { target: { value: "inference-worker" } },
  );
  fireEvent.click(
    screen.getByRole("button", { name: "Create shared authorization server" }),
  );
  await screen.findByText(/created and selected/);
  expect(mocks.create).toHaveBeenCalledWith({
    gramProject: "project-one",
    createUserSessionIssuerForm: {
      slug: "inference-worker",
      authnChallengeMode: "chain",
      sessionDurationHours: 1,
      authorizationServerMode: "shared",
    },
  });
  expect(onCreated).toHaveBeenCalledWith("new_issuer");
  expect(onBusy).toHaveBeenLastCalledWith(false);
});
it("allows project administrators to create an issuer without inference or workload privileges", async () => {
  setup();
  fireEvent.click(
    screen.getByRole("button", { name: "Create shared authorization server" }),
  );
  await waitFor(() => expect(mocks.create).toHaveBeenCalledTimes(1));
});
it("explains project:write and disables creation when it is absent", () => {
  mocks.permitted = false;
  setup();
  expect(
    screen.getByText(
      /Creating an exchange authorization server requires project:write/,
    ),
  ).toBeTruthy();
  expect(
    (
      screen.getByRole("button", {
        name: "Create shared authorization server",
      }) as HTMLButtonElement
    ).disabled,
  ).toBe(true);
  expect(mocks.create).not.toHaveBeenCalled();
});
it("shows creation errors and permits correcting the slug before retry", async () => {
  mocks.create.mockRejectedValueOnce(new Error("Slug already exists"));
  setup();
  fireEvent.click(
    screen.getByRole("button", { name: "Create shared authorization server" }),
  );
  expect((await screen.findByRole("alert")).textContent).toContain(
    "Slug already exists",
  );
  fireEvent.change(
    screen.getByLabelText("Speakeasy authorization server slug"),
    { target: { value: "inference-two" } },
  );
  fireEvent.click(
    screen.getByRole("button", { name: "Create shared authorization server" }),
  );
  await screen.findByText(/created and selected/);
  expect(mocks.create).toHaveBeenCalledTimes(2);
});
it("retries endpoint discovery without duplicating a successfully created issuer", async () => {
  const onCreated = vi
    .fn<(id: string) => Promise<void>>()
    .mockRejectedValueOnce(new Error("Discovery unavailable"))
    .mockResolvedValue(undefined);
  setup(onCreated);
  fireEvent.click(
    screen.getByRole("button", { name: "Create shared authorization server" }),
  );
  await screen.findByRole("alert");
  fireEvent.click(
    screen.getByRole("button", { name: "Retry loading exchange server" }),
  );
  await screen.findByText(/created and selected/);
  expect(mocks.create).toHaveBeenCalledTimes(1);
  expect(onCreated).toHaveBeenCalledTimes(2);
});
