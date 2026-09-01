import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const testState = vi.hoisted(() => ({
  data: { logsEnabled: false, toolIoLogsEnabled: false } as
    | { logsEnabled: boolean; toolIoLogsEnabled: boolean }
    | undefined,
  error: null as Error | null,
  isFetching: false,
  isLoading: false,
  organizationId: "org-active",
  mutate: vi.fn(),
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
    mutate: testState.mutate,
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
const onSkip = vi.fn();
const onBack = vi.fn();

function stepProps() {
  return {
    onComplete: () => {
      onComplete();
    },
    onSkip: () => {
      onSkip();
    },
    onBack: () => {
      onBack();
    },
  };
}

function renderStep() {
  return render(<EnableLoggingStep {...stepProps()} />);
}

beforeEach(() => {
  testState.data = { logsEnabled: false, toolIoLogsEnabled: false };
  testState.error = null;
  testState.isFetching = false;
  testState.isLoading = false;
  testState.organizationId = "org-active";
  testState.mutate.mockReset();
  testState.productFeaturesQuery.mockReset();
  testState.refetch.mockReset();
  onComplete.mockReset();
  onSkip.mockReset();
  onBack.mockReset();
});

afterEach(cleanup);

describe("EnableLoggingStep", () => {
  it("shows the same Enable Logs control as Logging & Telemetry", () => {
    renderStep();

    expect(
      screen.getByText(
        "Configure logging and telemetry settings for all your tool capture. When enabled, tool calls and traces are recorded for debugging and analytics. These power the insights and logs page on the platform.",
      ),
    ).toBeTruthy();
    expect(screen.getByText("Enable Logs")).toBeTruthy();
    expect(
      screen.getByText("Record tool call traces and telemetry data"),
    ).toBeTruthy();
    expect(screen.getByRole("switch", { name: "Enable logs" })).toBeTruthy();
    expect(
      (
        screen.getByRole("link", {
          name: "Logging & Telemetry",
        }) as HTMLAnchorElement
      ).getAttribute("href"),
    ).toBe("/acme/logs");
  });

  it("enables the logs product feature from the shared switch", () => {
    renderStep();

    fireEvent.click(screen.getByRole("switch", { name: "Enable logs" }));

    expect(testState.mutate).toHaveBeenCalledWith({
      request: {
        setProductFeatureRequestBody: {
          organizationId: "org-active",
          featureName: "logs",
          enabled: true,
        },
      },
    });
  });

  it("lets the admin skip without enabling logging", () => {
    renderStep();

    fireEvent.click(screen.getByRole("button", { name: "Skip for now" }));

    expect(testState.mutate).not.toHaveBeenCalled();
    expect(onSkip).toHaveBeenCalledOnce();
  });

  it("hides skip when logging is already enabled", () => {
    testState.data = { logsEnabled: true, toolIoLogsEnabled: false };

    renderStep();

    expect(screen.queryByRole("button", { name: "Skip for now" })).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: /Continue/ }));
    expect(onComplete).toHaveBeenCalledOnce();
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
      (screen.getByRole("button", { name: /Continue/ }) as HTMLButtonElement)
        .disabled,
    ).toBe(true);
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
