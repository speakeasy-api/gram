import { cleanup, render } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { datadogRum } from "@datadog/browser-rum";
import { DASHBOARD_CONFIG_META_NAME } from "@/lib/dashboard-config-html";
import { TelemetryProvider } from "./TelemetryProvider";

vi.mock("@datadog/browser-rum", () => ({
  datadogRum: {
    init: vi.fn(),
    getInitConfiguration: vi.fn(),
    setGlobalContextProperty: vi.fn(),
    addError: vi.fn(),
  },
}));

const posthogInit = vi.hoisted(() => vi.fn(() => null));

vi.mock("posthog-js", () => ({
  default: { init: posthogInit },
}));

const rumSettings = {
  datadogRumApplicationId: "test-app",
  datadogRumClientToken: "test-token",
  datadogRumSite: "datadoghq.eu",
  datadogRumEnv: "prod",
};

function setRuntimeConfig(config: Record<string, unknown> | null): void {
  document
    .querySelectorAll(`meta[name="${DASHBOARD_CONFIG_META_NAME}"]`)
    .forEach((meta) => meta.remove());
  if (config === null) return;
  const meta = document.createElement("meta");
  meta.name = DASHBOARD_CONFIG_META_NAME;
  meta.content = JSON.stringify(config);
  document.head.appendChild(meta);
}

beforeEach(() => {
  vi.clearAllMocks();
  // Rendering without PostHog settings warns once per module load; see the
  // dedicated test below.
  vi.spyOn(console, "warn").mockImplementation(() => {});
});

afterEach(() => {
  cleanup();
  setRuntimeConfig(null);
  vi.restoreAllMocks();
});

describe("Datadog RUM collection", () => {
  it("initializes with the deployment's settings", () => {
    setRuntimeConfig(rumSettings);
    render(<TelemetryProvider>{null}</TelemetryProvider>);
    expect(datadogRum.init).toHaveBeenCalledWith(
      expect.objectContaining({
        applicationId: "test-app",
        clientToken: "test-token",
        site: "datadoghq.eu",
        env: "prod",
        service: "gram",
        sessionSampleRate: 100,
      }),
    );
  });

  it("stays off without a configuration", () => {
    setRuntimeConfig(null);
    render(<TelemetryProvider>{null}</TelemetryProvider>);
    expect(datadogRum.init).not.toHaveBeenCalled();
  });

  it.each(Object.keys(rumSettings))("stays off without %s", (key) => {
    const partial: Record<string, unknown> = { ...rumSettings };
    delete partial[key];
    setRuntimeConfig(partial);
    render(<TelemetryProvider>{null}</TelemetryProvider>);
    expect(datadogRum.init).not.toHaveBeenCalled();
  });
});

describe("PostHog initialization", () => {
  it("uses the deployment's project key and API host", () => {
    setRuntimeConfig({
      posthogKey: "phc_test",
      posthogHost: "https://metrics.example.com",
    });
    render(<TelemetryProvider>{null}</TelemetryProvider>);
    expect(posthogInit).toHaveBeenCalledWith(
      "phc_test",
      expect.objectContaining({ api_host: "https://metrics.example.com" }),
      "speakeasy",
    );
  });

  it.each([
    ["no configuration", null],
    ["a key without a host", { posthogKey: "phc_test" }],
    ["a host without a key", { posthogHost: "https://metrics.example.com" }],
  ])("stays off with %s", (_name, config) => {
    setRuntimeConfig(config);
    render(<TelemetryProvider>{null}</TelemetryProvider>);
    expect(posthogInit).not.toHaveBeenCalled();
  });
});

describe("missing PostHog settings", () => {
  it("warns once, because every feature flag reads false without PostHog", async () => {
    vi.resetModules();
    const { TelemetryProvider: FreshProvider } =
      await import("./TelemetryProvider");
    setRuntimeConfig(null);
    render(<FreshProvider>{null}</FreshProvider>);
    render(<FreshProvider>{null}</FreshProvider>);
    expect(console.warn).toHaveBeenCalledOnce();
    expect(console.warn).toHaveBeenCalledWith(
      expect.stringContaining("feature flags are off"),
    );
  });
});
