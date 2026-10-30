import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { CreateMcpAuthenticationProvider } from "./CreateMcpAuthenticationProvider";

const mocks = vi.hoisted(() => ({
  canWrite: true,
  create: vi.fn(async () => ({ id: "provider_new" })),
}));
vi.mock("@/contexts/Auth", () => ({
  useProject: () => ({ id: "project_one", slug: "default", name: "Default" }),
}));
vi.mock("@/contexts/Sdk", () => ({
  useSdkClient: () => ({ userSessionIssuers: { create: mocks.create } }),
}));
vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({ hasScope: () => mocks.canWrite }),
}));
afterEach(() => {
  cleanup();
  mocks.canWrite = true;
  mocks.create.mockClear();
});

it("creates a provider that can give tokens to agents, then selects it", async () => {
  const onCreated = vi.fn(async () => undefined);
  render(<CreateMcpAuthenticationProvider onCreated={onCreated} />);

  fireEvent.click(
    screen.getByRole("button", { name: "Create MCP authentication provider" }),
  );

  await waitFor(() => expect(onCreated).toHaveBeenCalledWith("provider_new"));
  expect(mocks.create).toHaveBeenCalledWith({
    gramProject: "default",
    createUserSessionIssuerForm: expect.objectContaining({
      slug: "agents",
      authorizationServerMode: "shared",
    }),
  });
});

it("finds the provider again rather than creating a second one", async () => {
  const onCreated = vi
    .fn<(id: string) => Promise<void>>()
    .mockRejectedValueOnce(new Error("Not found yet"))
    .mockResolvedValueOnce(undefined);
  render(<CreateMcpAuthenticationProvider onCreated={onCreated} />);

  fireEvent.click(
    screen.getByRole("button", { name: "Create MCP authentication provider" }),
  );
  fireEvent.click(
    await screen.findByRole("button", { name: "Retry finding it" }),
  );

  await waitFor(() => expect(onCreated).toHaveBeenCalledTimes(2));
  expect(mocks.create).toHaveBeenCalledTimes(1);
});

it("says who can create one when the reader cannot", () => {
  mocks.canWrite = false;
  render(<CreateMcpAuthenticationProvider onCreated={vi.fn()} />);

  expect(screen.getByText(/needs permission to edit Default/)).toBeTruthy();
  expect(
    screen.queryByRole("button", {
      name: "Create MCP authentication provider",
    }),
  ).toBeNull();
});
