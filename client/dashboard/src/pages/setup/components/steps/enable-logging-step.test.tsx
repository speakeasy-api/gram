import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const testState = vi.hoisted(() => ({
  data: {
    logsEnabled: false,
    toolIoLogsEnabled: false,
    sessionCaptureEnabled: false,
  } as
    | {
        logsEnabled: boolean;
        toolIoLogsEnabled: boolean;
        sessionCaptureEnabled: boolean;
      }
    | undefined,
  error: null as Error | null,
  isFetching: false,
  isLoading: false,
  organizationId: "org-active",
  mutateAsync: vi.fn(),
  productFeaturesQuery: vi.fn(),
  refetch: vi.fn(),
}));

vi.mock("@/contexts/Auth", () => ({
  useOrganization: () => ({ id: testState.organizationId }),
}));

vi.mock("@/components/require-scope", () => ({
  RequireScope: ({ children }: { children: ReactNode }) => <>{children}</>,
}));

vi.mock("@/lib/errors", () => ({ handleAPIError: vi.fn() }));

vi.mock("@/routes", () => ({
  useOrgRoutes: () => ({
    logs: { href: () => "/acme/logs" },
  }),
}));

vi.mock("react-router", () => ({
  Link: ({
    to,
    children,
    className,
  }: {
    to: string;
    children: ReactNode;
    className?: string;
  }) => (
    <a href={to} className={className}>
      {children}
    </a>
  ),
}));

vi.mock("@gram/client/react-query/featuresSet.js", () => ({
  useFeaturesSetMutation: () => ({
    isPending: false,
    mutateAsync: testState.mutateAsync,
  }),
}));

vi.mock("@gram/client/react-query/productFeatures.js", () => ({
  invalidateAllProductFeatures: vi.fn(),
  useProductFeatures: (...args: unknown[]) => {
    testState.productFeaturesQuery(...args);
    return {
      data: testState.data,
      error: testState.error,
      isFetching: testState.isFetching,
      isLoading: testState.isLoading,
      refetch: testState.refetch,
    };
  },
}));

vi.mock("@tanstack/react-query", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@tanstack/react-query")>()),
  useQueryClient: () => ({}),
}));

import { EnableLoggingStep } from "./enable-logging-step";

const onComplete = vi.fn();

function renderStep() {
  return render(
    <EnableLoggingStep
      onComplete={() => {
        onComplete();
      }}
    />,
  );
}

beforeEach(() => {
  testState.data = {
    logsEnabled: false,
    toolIoLogsEnabled: false,
    sessionCaptureEnabled: false,
  };
  testState.error = null;
  testState.isFetching = false;
  testState.isLoading = false;
  testState.organizationId = "org-active";
  testState.mutateAsync.mockReset();
  testState.mutateAsync.mockResolvedValue(undefined);
  testState.productFeaturesQuery.mockReset();
  testState.refetch.mockReset();
  onComplete.mockReset();
});

afterEach(cleanup);

describe("EnableLoggingStep", () => {
  it("shows the combined logging and session capture control", () => {
    renderStep();

    expect(
      screen.getByText(
        "Configure logging and telemetry settings for all your tool capture. When enabled, tool calls and traces are recorded for debugging and analytics. These power the insights and logs page on the platform.",
      ),
    ).toBeTruthy();
    expect(screen.getByText("Enable logging and session capture")).toBeTruthy();
    expect(
      screen.getByText(
        "Turns on Enable Logs, Record Tool I/O, and Agent Session Capture. Tool calls, request and response bodies, and agent session prompts are recorded.",
      ),
    ).toBeTruthy();
    expect(
      screen.getByRole("switch", {
        name: "Enable logging and session capture",
      }),
    ).toBeTruthy();
    expect(
      (
        screen.getByRole("link", {
          name: "Logging & Telemetry",
        }) as HTMLAnchorElement
      ).getAttribute("href"),
    ).toBe("/acme/logs");
  });

  it("enables logs, tool I/O, and session capture from the combined switch", async () => {
    renderStep();

    fireEvent.click(
      screen.getByRole("switch", {
        name: "Enable logging and session capture",
      }),
    );

    await waitFor(() => {
      expect(testState.mutateAsync).toHaveBeenCalledTimes(3);
    });
    expect(testState.mutateAsync).toHaveBeenCalledWith({
      request: {
        setProductFeatureRequestBody: {
          organizationId: "org-active",
          featureName: "logs",
          enabled: true,
        },
      },
    });
    expect(testState.mutateAsync).toHaveBeenCalledWith({
      request: {
        setProductFeatureRequestBody: {
          organizationId: "org-active",
          featureName: "tool_io_logs",
          enabled: true,
        },
      },
    });
    expect(testState.mutateAsync).toHaveBeenCalledWith({
      request: {
        setProductFeatureRequestBody: {
          organizationId: "org-active",
          featureName: "session_capture",
          enabled: true,
        },
      },
    });
  });

  it("shows a retry state when product features fail to load", () => {
    testState.data = undefined;
    testState.error = new Error("features unavailable");

    renderStep();

    expect(
      screen.getByText("Couldn't load the current logging setting."),
    ).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    expect(testState.refetch).toHaveBeenCalledOnce();
    expect(
      (screen.getByRole("button", { name: "Mark done" }) as HTMLButtonElement)
        .disabled,
    ).toBe(true);
  });

  it("blocks continue while the bundle writes are in flight", async () => {
    let releaseMutations: (() => void) | undefined;
    const gate = new Promise<void>((resolve) => {
      releaseMutations = () => {
        resolve();
      };
    });
    testState.mutateAsync.mockImplementation(async () => {
      await gate;
    });

    renderStep();
    fireEvent.click(
      screen.getByRole("switch", {
        name: "Enable logging and session capture",
      }),
    );

    await waitFor(() => {});
    expect(
      (screen.getByRole("button", { name: /Loading/ }) as HTMLButtonElement)
        .disabled,
    ).toBe(true);

    releaseMutations?.();
    await waitFor(() => {
      expect(
        (screen.getByRole("button", { name: "Mark done" }) as HTMLButtonElement)
          .disabled,
      ).toBe(false);
    });
    fireEvent.click(screen.getByRole("button", { name: "Mark done" }));
    expect(onComplete).toHaveBeenCalledOnce();
  });

  it("keeps the retry action disabled while refetching", () => {
    testState.data = undefined;
    testState.error = new Error("features unavailable");
    testState.isFetching = true;

    renderStep();

    expect(
      (screen.getByRole("button", { name: "Retrying…" }) as HTMLButtonElement)
        .disabled,
    ).toBe(true);
  });
});
