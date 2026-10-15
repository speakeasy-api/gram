import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { getAgentPlatforms } from "../setup-data";
import { PlatformSetupStepBody } from "./platform-setup-steps";

const mocks = vi.hoisted(() => ({
  pluginName: "example-observability" as string | undefined,
}));
vi.mock("@gram/client/react-query/publishStatus", () => ({
  usePublishStatus: () => ({
    data: { claudeObservabilityPlugin: mocks.pluginName },
  }),
}));
vi.mock("@gram/client/react-query/marketplaceSettings", () => ({
  useMarketplaceSettings: () => ({}),
}));
vi.mock("@/contexts/Sdk", () => ({
  useProjectSlugForRequests: () => "default",
}));
vi.mock("@/routes", () => ({
  useOrgRoutes: () => ({
    deviceAgent: { href: () => "/example/device-agent" },
  }),
}));

const step = getAgentPlatforms("https://app.getgram.ai")
  .find(({ id }) => id === "claude-cowork")!
  .setupSteps.find(({ title }) => title === "Enable OTEL export")!;
const writeText = vi.fn<(value: string) => Promise<void>>();
const retry = vi.fn<() => void>();
function body(
  props: {
    apiKey?: string;
    apiKeyPending?: boolean;
    apiKeyError?: string;
  } = {},
) {
  return (
    <PlatformSetupStepBody
      step={step}
      eyebrow="Step 3"
      onRetryApiKey={retry}
      onEligibilityAnswer={() => {}}
      {...props}
    />
  );
}
function copy(label: string) {
  return screen.getByRole("button", {
    name: `Copy OTLP ${label}`,
  }) as HTMLButtonElement;
}
beforeEach(() => {
  writeText.mockReset().mockResolvedValue(undefined);
  retry.mockReset();
  mocks.pluginName = "example-observability";
  Object.defineProperty(navigator, "clipboard", {
    configurable: true,
    value: { writeText },
  });
});
afterEach(() => {
  cleanup();
  vi.useRealTimers();
});

describe("copyable setup values", () => {
  it("copies only each value, masks the key everywhere in the DOM, and confirms each independently", async () => {
    const { container } = render(body({ apiKey: "EXAMPLE_GENERATED_KEY" }));
    expect(container.innerHTML).not.toContain("EXAMPLE_GENERATED_KEY");
    expect(
      screen.getByText(
        "Speakeasy-AI-Project=default,Speakeasy-AI-Key=••••••••",
      ),
    ).toBeTruthy();
    expect(
      screen.getByText(
        "Save the settings and start a new Cowork session to verify receipt. The native exporter automatically allowlists the collector hostname.",
      ),
    ).toBeTruthy();
    for (const [label, value] of [
      ["endpoint", "https://app.getgram.ai/rpc/hooks.otel"],
      ["protocol", "http/json"],
      [
        "headers",
        "Speakeasy-AI-Project=default,Speakeasy-AI-Key=EXAMPLE_GENERATED_KEY",
      ],
    ] as const) {
      fireEvent.click(copy(label));
      await waitFor(() => expect(copy(label).textContent).toContain("Copied"));
      expect(writeText).toHaveBeenLastCalledWith(value);
    }
    expect(writeText).toHaveBeenCalledTimes(3);
    expect(container.innerHTML).not.toContain("EXAMPLE_GENERATED_KEY");
  });

  it("shows Copied only after clipboard success and resets it briefly afterwards", async () => {
    vi.useFakeTimers();
    let resolve!: () => void;
    writeText.mockReturnValue(
      new Promise<void>((done) => {
        resolve = done;
      }),
    );
    render(body({ apiKey: "EXAMPLE_KEY" }));
    fireEvent.click(copy("protocol"));
    expect(screen.queryByText("Copied")).toBeNull();
    await act(async () => {
      resolve();
    });
    expect(copy("protocol").textContent).toContain("Copied");
    expect(copy("endpoint").textContent).not.toContain("Copied");
    act(() => {
      vi.advanceTimersByTime(2000);
    });
    expect(screen.queryByText("Copied")).toBeNull();
  });

  it.each(["pending", "error"])(
    "keeps static values copyable during key %s and enables headers after retry",
    async (state) => {
      const { rerender } = render(
        body(
          state === "pending"
            ? { apiKeyPending: true }
            : { apiKeyError: "Example failure" },
        ),
      );
      expect(copy("headers").disabled).toBe(true);
      fireEvent.click(copy("headers"));
      expect(writeText).not.toHaveBeenCalled();
      for (const label of ["endpoint", "protocol"]) {
        expect(copy(label).disabled).toBe(false);
        fireEvent.click(copy(label));
        await waitFor(() =>
          expect(copy(label).textContent).toContain("Copied"),
        );
      }
      if (state === "error") {
        expect(screen.getByText("Couldn't generate an API key")).toBeTruthy();
        fireEvent.click(screen.getByRole("button", { name: "Retry" }));
        expect(retry).toHaveBeenCalledOnce();
      } else expect(screen.getByText("Generating API key…")).toBeTruthy();
      rerender(body({ apiKey: "EXAMPLE_RETRY_KEY" }));
      expect(copy("headers").disabled).toBe(false);
      fireEvent.click(copy("headers"));
      await waitFor(() =>
        expect(writeText).toHaveBeenLastCalledWith(
          "Speakeasy-AI-Project=default,Speakeasy-AI-Key=EXAMPLE_RETRY_KEY",
        ),
      );
    },
  );

  it.each(["rejected", "unavailable"])(
    "handles %s clipboard without false success and allows retry",
    async (failure) => {
      render(body({ apiKey: "EXAMPLE_KEY" }));
      if (failure === "rejected")
        writeText.mockRejectedValueOnce(new Error("Denied"));
      else
        Object.defineProperty(navigator, "clipboard", {
          configurable: true,
          value: undefined,
        });
      fireEvent.click(copy("headers"));
      expect(await screen.findByRole("alert")).toHaveProperty(
        "textContent",
        "Couldn't copy. Check clipboard permissions and try again.",
      );
      expect(screen.queryByText("Copied")).toBeNull();
      Object.defineProperty(navigator, "clipboard", {
        configurable: true,
        value: { writeText },
      });
      fireEvent.click(copy("headers"));
      await waitFor(() =>
        expect(copy("headers").textContent).toContain("Copied"),
      );
      expect(screen.queryByRole("alert")).toBeNull();
    },
  );
});

describe("inline setup identifiers", () => {
  it.each(["example-observability", "renamed-observability", undefined])(
    "renders the resolved plugin %s inline without a copyable code block",
    (pluginName) => {
      mocks.pluginName = pluginName;
      const requiredStep = getAgentPlatforms("https://app.getgram.ai")
        .find(({ id }) => id === "claude-cowork")!
        .setupSteps.find(
          ({ title }) => title === "Mark the observability plugin as Required",
        )!;
      const { container } = render(
        <PlatformSetupStepBody
          step={requiredStep}
          eyebrow="Step 2"
          onRetryApiKey={retry}
          onEligibilityAnswer={() => {}}
        />,
      );
      expect(container.textContent).toContain(
        `Find ${pluginName ?? "the observability plugin"} in the plugin list and set Default access → Required.`,
      );
      expect(container.textContent).toContain("prevents disabling or removal");
      expect(container.textContent).not.toContain("{{GRAM_");
      expect(screen.queryByRole("button", { name: /Copy/ })).toBeNull();
      expect(container.querySelector("pre")).toBeNull();
      expect(container.querySelector("code")?.textContent).toBe(pluginName);
    },
  );
});
