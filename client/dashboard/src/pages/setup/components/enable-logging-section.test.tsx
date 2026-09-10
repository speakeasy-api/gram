import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const mocks = vi.hoisted(() => ({
  features: {
    data: undefined as undefined | Record<string, boolean>,
    isLoading: false,
    isFetching: false,
    error: null as unknown,
    refetch: vi.fn(),
  },
}));

vi.mock("@gram/client/react-query/productFeatures.js", () => ({
  useProductFeatures: () => mocks.features,
}));
vi.mock("@/contexts/Auth", () => ({
  useOrganization: () => ({ id: "org-one" }),
}));
vi.mock("@/routes", () => ({
  useOrgRoutes: () => ({ logs: { href: () => "/org/logs" } }),
}));
vi.mock("react-router", () => ({
  Link: ({ children }: { children: React.ReactNode }) => <a>{children}</a>,
}));
vi.mock("@/components/observe/LoggingPageHeader", () => ({
  LogDataRetentionBanner: () => null,
}));
vi.mock("./enable-logging-and-session-capture-setting", () => ({
  EnableLoggingAndSessionCaptureSetting: () => <div>logging switch</div>,
}));

import { EnableLoggingSection } from "./enable-logging-section";

afterEach(cleanup);
beforeEach(() => {
  mocks.features.data = {
    logsEnabled: false,
    toolIoLogsEnabled: false,
    sessionCaptureEnabled: false,
  };
  mocks.features.error = null;
  mocks.features.isLoading = false;
  mocks.features.isFetching = false;
});

describe("EnableLoggingSection", () => {
  it("stays incomplete until the whole bundle is on", () => {
    render(<EnableLoggingSection index={1} />);

    expect(screen.getByText("Enable logging")).toBeTruthy();
    expect(screen.getByText("logging switch")).toBeTruthy();
    // The number stands in for the check mark while the step is outstanding.
    expect(screen.getByText("1")).toBeTruthy();
  });

  it("completes once logs, tool I/O and session capture are all on", () => {
    mocks.features.data = {
      logsEnabled: true,
      toolIoLogsEnabled: true,
      sessionCaptureEnabled: true,
    };

    render(<EnableLoggingSection index={1} />);

    expect(screen.queryByText("1")).toBeNull();
  });

  it("clears the check when part of the bundle goes back off", () => {
    // The switch invalidates product features after every write, so a partial
    // disable — whether from a failed write or a change made elsewhere — has
    // to reopen the step rather than leave it checked.
    mocks.features.data = {
      logsEnabled: true,
      toolIoLogsEnabled: true,
      sessionCaptureEnabled: false,
    };

    render(<EnableLoggingSection index={1} />);

    expect(screen.getByText("1")).toBeTruthy();
  });

  it("offers a retry when the current setting cannot be read", () => {
    mocks.features.data = undefined;
    mocks.features.error = new Error("nope");

    render(<EnableLoggingSection index={1} />);

    expect(screen.getByRole("alert").textContent).toContain(
      "Couldn't load the current logging setting.",
    );
    expect(screen.getByRole("button", { name: "Retry" })).toBeTruthy();
  });
});
