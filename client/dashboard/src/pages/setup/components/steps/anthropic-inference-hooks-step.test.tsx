import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { useSyncExternalStore } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { AnthropicInferenceConfig } from "@gram/client/models/components/anthropicinferenceconfig";
import { AnthropicInferenceHooksStep } from "./anthropic-inference-hooks-step";

// The card reads the config and writes it back in the same breath — minting
// the URL in step 2, saving the secret in step 5 — so the mock has to push the
// new config at the component the way an invalidated query would.
const state = vi.hoisted(() => ({
  config: {
    enabled: false,
    hasSigningSecret: false,
  } as AnthropicInferenceConfig,
  listeners: new Set<() => void>(),
  save: vi.fn(),
  remove: vi.fn(),
  reset: vi.fn(),
  invalidate: vi.fn(),
}));

function publishConfig(next: AnthropicInferenceConfig): void {
  state.config = next;
  for (const listener of state.listeners) listener();
}

vi.mock("@tanstack/react-query", () => ({ useQueryClient: () => ({}) }));
vi.mock("@/lib/utils", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/lib/utils")>()),
  getServerURL: () => "https://api.example.com",
}));
vi.mock("@gram/client/react-query/anthropicInferenceConfig", () => ({
  useAnthropicInferenceConfig: () => ({
    data: useSyncExternalStore(
      (onChange: () => void) => {
        state.listeners.add(onChange);
        return () => state.listeners.delete(onChange);
      },
      () => state.config,
    ),
    error: null,
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
vi.mock("../enable-logging-section", () => ({
  EnableLoggingSection: () => <div>Enable logging section</div>,
}));
vi.mock("../confirm-inference-traffic-section", () => ({
  ConfirmInferenceTrafficSection: ({
    description,
    callout,
  }: {
    description: string;
    callout?: { title: string; body: string };
  }) => (
    <div>
      <p>Confirm inference traffic: {description}</p>
      <p>{callout?.title}</p>
    </div>
  ),
}));

const CONNECTED: AnthropicInferenceConfig = {
  id: "example",
  webhookPath: "/hooks/anthropic-inference/example",
  enabled: true,
  hasSigningSecret: true,
};

afterEach(cleanup);
beforeEach(() => {
  vi.clearAllMocks();
  state.listeners.clear();
  state.config = { enabled: false, hasSigningSecret: false };
  state.save.mockImplementation(async ({ request }) => {
    publishConfig({
      id: "example",
      webhookPath: "/hooks/anthropic-inference/example",
      hasSigningSecret: Boolean(
        request.upsertAnthropicInferenceConfigRequestBody.signingSecret,
      ),
      enabled: request.upsertAnthropicInferenceConfigRequestBody.enabled,
    });
    return state.config;
  });
});

function renderStep() {
  return render(<AnthropicInferenceHooksStep onComplete={() => {}} />);
}

describe("AnthropicInferenceHooksStep", () => {
  it("alternates between the two products, Speakeasy first", () => {
    state.config = CONNECTED;

    renderStep();

    expect(screen.getByText("Set up Anthropic observability")).toBeTruthy();
    expect(screen.getByText("Enable logging section")).toBeTruthy();
    expect(
      screen.getAllByRole("heading", { level: 3 }).map((h) => h.textContent),
    ).toEqual([
      "Enable inference hooks in Speakeasy",
      "Add the endpoint in Claude.ai",
      "Turn on inference hooks",
      "Save the signing secret",
    ]);
  });

  it("mints the webhook URL on demand and hands it to the Claude step", async () => {
    renderStep();

    expect(
      screen.getByText(/Generate the webhook URL in the previous step first/),
    ).toBeTruthy();

    fireEvent.click(
      screen.getByRole("button", { name: "Enable inference hooks" }),
    );

    await waitFor(() =>
      expect(
        screen.getByText(
          "https://api.example.com/hooks/anthropic-inference/example",
        ),
      ).toBeTruthy(),
    );
    expect(state.save).toHaveBeenCalledWith({
      request: {
        upsertAnthropicInferenceConfigRequestBody: { enabled: false },
      },
    });
    expect(
      screen.getByText("Copy this URL — the next step pastes it into Claude."),
    ).toBeTruthy();
  });

  it("holds the signing secret until there is a hook to sign", () => {
    renderStep();

    expect(
      (screen.getByLabelText("Signing secret") as HTMLInputElement).disabled,
    ).toBe(true);
    expect(
      (
        screen.getByRole("button", {
          name: "Save signing secret",
        }) as HTMLButtonElement
      ).disabled,
    ).toBe(true);
    expect(
      screen.getByText(/Generate the webhook URL in step 2 first/),
    ).toBeTruthy();
  });

  it("turns the hook on when the secret Claude revealed is saved", async () => {
    state.config = {
      id: "example",
      webhookPath: "/hooks/anthropic-inference/example",
      enabled: false,
      hasSigningSecret: false,
    };

    renderStep();

    fireEvent.change(screen.getByLabelText("Signing secret"), {
      target: { value: "whsec_example" },
    });
    fireEvent.click(
      screen.getByRole("button", { name: "Save signing secret" }),
    );

    await waitFor(() =>
      expect(state.save).toHaveBeenCalledWith({
        request: {
          upsertAnthropicInferenceConfigRequestBody: {
            signingSecret: "whsec_example",
            enabled: true,
          },
        },
      }),
    );
  });

  it("recommends Shadow mode before the endpoint has proved itself", () => {
    state.config = CONNECTED;

    renderStep();

    expect(screen.getByText("Begin your rollout in Shadow mode")).toBeTruthy();
    expect(screen.getByText(/once Endpoint status reads Healthy/)).toBeTruthy();
  });

  it("confirms traffic from conversations rather than hook events", () => {
    state.config = CONNECTED;

    renderStep();

    expect(
      screen.getByText(
        /Confirm inference traffic: Send a message in Claude.ai/,
      ),
    ).toBeTruthy();
    expect(screen.getByText("Conversations, not tool calls")).toBeTruthy();
  });
});
