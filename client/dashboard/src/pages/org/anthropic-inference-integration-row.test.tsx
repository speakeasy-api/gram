import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import type { AnthropicInferenceConfig } from "@gram/client/models/components/anthropicinferenceconfig";
import { AnthropicInferenceIntegrationRow } from "./anthropic-inference-integration-row";

const state = vi.hoisted(() => ({
  config: {
    enabled: false,
    hasSigningSecret: false,
  } as AnthropicInferenceConfig,
  error: null as Error | null,
  save: vi.fn(),
  remove: vi.fn(),
  reset: vi.fn(),
  invalidate: vi.fn(),
}));
vi.mock("@tanstack/react-query", () => ({ useQueryClient: () => ({}) }));
vi.mock("@/lib/utils", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/lib/utils")>()),
  getServerURL: () => "https://api.example.com",
}));
vi.mock("@gram/client/react-query/anthropicInferenceConfig", () => ({
  useAnthropicInferenceConfig: () => ({
    data: state.config,
    error: state.error,
    isPending: false,
    refetch: vi.fn(),
  }),
  invalidateAllAnthropicInferenceConfig: state.invalidate,
}));
vi.mock("@gram/client/react-query/upsertAnthropicInferenceConfig", () => ({
  useUpsertAnthropicInferenceConfigMutation: () => ({
    mutateAsync: state.save,
    reset: state.reset,
    isPending: false,
  }),
}));
vi.mock("@gram/client/react-query/deleteAnthropicInferenceConfig", () => ({
  useDeleteAnthropicInferenceConfigMutation: () => ({
    mutateAsync: state.remove,
    isPending: false,
  }),
}));
vi.mock("@/components/code", () => ({
  CodeBlock: ({ children }: { children: string }) => <pre>{children}</pre>,
}));
beforeEach(() => {
  vi.clearAllMocks();
  state.config = { enabled: false, hasSigningSecret: false };
  state.error = null;
  state.save.mockImplementation(async ({ request }) => {
    state.config = {
      id: "example",
      webhookPath: "/hooks/anthropic-inference/example",
      hasSigningSecret: Boolean(
        request.upsertAnthropicInferenceConfigRequestBody.signingSecret,
      ),
      enabled: request.upsertAnthropicInferenceConfigRequestBody.enabled,
    };
    return state.config;
  });
});
afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

it("prepares a URL in one click without asking for organization or project IDs", async () => {
  render(<AnthropicInferenceIntegrationRow />);
  fireEvent.click(screen.getByRole("button", { name: "Connect" }));
  await waitFor(() =>
    expect(
      screen.getByText(
        "https://api.example.com/hooks/anthropic-inference/example",
      ),
    ).toBeDefined(),
  );
  expect(state.save).toHaveBeenCalledWith({
    request: { upsertAnthropicInferenceConfigRequestBody: { enabled: false } },
  });
  expect(screen.queryByLabelText("Organization ID")).toBeNull();
  expect(screen.queryByLabelText("Project ID")).toBeNull();
  fireEvent.change(screen.getByLabelText("Signing secret"), {
    target: { value: "whsec_example" },
  });
  fireEvent.click(screen.getByRole("button", { name: "Save signing secret" }));
  await waitFor(() =>
    expect(state.save).toHaveBeenLastCalledWith({
      request: {
        upsertAnthropicInferenceConfigRequestBody: {
          signingSecret: "whsec_example",
          enabled: true,
        },
      },
    }),
  );
  await waitFor(() =>
    expect(
      (screen.getByLabelText("Signing secret") as HTMLInputElement).value,
    ).toBe(""),
  );
  expect(screen.getByText("3. Enable protection in Claude")).toBeDefined();
});

it("keeps a saved secret when the field is left blank", async () => {
  state.config = {
    id: "example",
    webhookPath: "/hooks/anthropic-inference/example",
    enabled: true,
    hasSigningSecret: true,
  };
  render(<AnthropicInferenceIntegrationRow />);
  fireEvent.click(screen.getByRole("button", { name: "Configure" }));
  expect(state.save).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole("button", { name: "Save signing secret" }));
  await waitFor(() =>
    expect(state.save).toHaveBeenCalledWith({
      request: {
        upsertAnthropicInferenceConfigRequestBody: {
          signingSecret: undefined,
          enabled: true,
        },
      },
    }),
  );
});

it("does not prepare a replacement connection when loading failed", () => {
  state.error = new Error("unavailable");
  render(<AnthropicInferenceIntegrationRow />);
  expect(
    (screen.getByRole("button", { name: "Connect" }) as HTMLButtonElement)
      .disabled,
  ).toBe(true);
  expect(state.save).not.toHaveBeenCalled();
});

it("revokes the connection when the admin disconnects", async () => {
  state.config = {
    id: "example",
    webhookPath: "/hooks/anthropic-inference/example",
    enabled: true,
    hasSigningSecret: true,
  };
  vi.stubGlobal(
    "confirm",
    vi.fn(() => true),
  );
  render(<AnthropicInferenceIntegrationRow />);
  fireEvent.click(screen.getByRole("button", { name: "Configure" }));
  fireEvent.click(screen.getByRole("button", { name: "Disconnect" }));
  await waitFor(() =>
    expect(state.remove).toHaveBeenCalledWith({ request: {} }),
  );
});
