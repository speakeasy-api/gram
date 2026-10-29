import { cleanup, render } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { datadogRum } from "@datadog/browser-rum";
import { getServerURL } from "@/lib/utils";
import { TelemetryProvider } from "./TelemetryProvider";

vi.mock("@datadog/browser-rum", () => ({
  datadogRum: {
    init: vi.fn(),
    getInitConfiguration: vi.fn(),
    setGlobalContextProperty: vi.fn(),
    addError: vi.fn(),
  },
}));

vi.mock("posthog-js", () => ({
  default: { init: vi.fn(() => null) },
}));

vi.mock("@/lib/utils", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/lib/utils")>()),
  getServerURL: vi.fn(),
}));

beforeEach(() => {
  vi.resetModules();
  vi.clearAllMocks();
  vi.stubEnv("VITE_DATADOG_APPLICATION_ID", "test-app");
  vi.stubEnv("VITE_DATADOG_CLIENT_TOKEN", "test-token");
  vi.stubEnv("VITE_DATADOG_ENV", "prod");
});

afterEach(() => {
  cleanup();
  vi.unstubAllEnvs();
});

describe.each(["dashboard", "elements"] as const)(
  "%s RUM collection",
  (entry) => {
    async function initialize() {
      if (entry === "dashboard") {
        render(<TelemetryProvider>{null}</TelemetryProvider>);
      } else {
        const { initErrorTracking } =
          await import("@/elements/lib/errorTracking");
        initErrorTracking();
      }
    }

    it.each([
      "https://dev.getgram.ai",
      "https://dev.ai.speakeasy.com",
      "https://staging.getgram.ai",
      "https://pr-123.dev.getgram.ai",
      "https://localhost:5173",
    ])(
      "does not initialize collection on %s even with prod credentials",
      async (url) => {
        vi.mocked(getServerURL).mockReturnValue(url);
        await initialize();
        expect(datadogRum.init).not.toHaveBeenCalled();
      },
    );

    it.each(["https://app.getgram.ai", "https://ai.speakeasy.com"])(
      "initializes collection on %s",
      async (url) => {
        vi.mocked(getServerURL).mockReturnValue(url);
        await initialize();
        expect(datadogRum.init).toHaveBeenCalledWith(
          expect.objectContaining({ env: "prod", sessionSampleRate: 100 }),
        );
      },
    );
  },
);

it.each(["dev", "staging"])(
  "does not send Elements errors with an explicit %s environment",
  async (env) => {
    vi.mocked(getServerURL).mockReturnValue("https://app.getgram.ai");
    vi.stubEnv("VITE_DATADOG_ENV", env);
    const { initErrorTracking, trackError, isErrorTrackingEnabled } =
      await import("@/elements/lib/errorTracking");
    initErrorTracking();
    trackError(new Error("test error"), { source: "custom" });
    expect(datadogRum.init).not.toHaveBeenCalled();
    expect(datadogRum.addError).not.toHaveBeenCalled();
    expect(isErrorTrackingEnabled()).toBe(false);
  },
);
