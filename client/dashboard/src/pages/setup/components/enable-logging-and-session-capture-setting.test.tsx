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
  organizationId: "org-active",
  mutateAsync: vi.fn(),
  productFeaturesQuery: vi.fn(),
  isAdmin: true,
}));

const handleAPIError = vi.hoisted(() => vi.fn());
const invalidateAllProductFeatures = vi.hoisted(() => vi.fn());

vi.mock("@/components/require-scope", () => ({
  RequireScope: ({ children }: { children: ReactNode }) =>
    testState.isAdmin ? <>{children}</> : null,
}));

vi.mock("@/contexts/Auth", () => ({
  useOrganization: () => ({ id: testState.organizationId }),
}));

vi.mock("@/lib/errors", () => ({ handleAPIError }));

vi.mock("@gram/client/react-query/featuresSet.js", () => ({
  useFeaturesSetMutation: () => ({
    isPending: false,
    mutateAsync: testState.mutateAsync,
  }),
}));

vi.mock("@gram/client/react-query/productFeatures.js", () => ({
  invalidateAllProductFeatures,
  useProductFeatures: (...args: unknown[]) => {
    testState.productFeaturesQuery(...args);
    return { data: testState.data };
  },
}));

vi.mock("@tanstack/react-query", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@tanstack/react-query")>()),
  useQueryClient: () => ({}),
}));

import { EnableLoggingAndSessionCaptureSetting } from "./enable-logging-and-session-capture-setting";

function requestFor(featureName: string, enabled: boolean) {
  return {
    request: {
      setProductFeatureRequestBody: {
        organizationId: "org-active",
        featureName,
        enabled,
      },
    },
  };
}

beforeEach(() => {
  testState.data = {
    logsEnabled: false,
    toolIoLogsEnabled: false,
    sessionCaptureEnabled: false,
  };
  testState.organizationId = "org-active";
  testState.isAdmin = true;
  testState.mutateAsync.mockReset();
  testState.mutateAsync.mockResolvedValue(undefined);
  testState.productFeaturesQuery.mockReset();
  handleAPIError.mockReset();
  invalidateAllProductFeatures.mockReset();
  invalidateAllProductFeatures.mockResolvedValue(undefined);
});

afterEach(cleanup);

describe("EnableLoggingAndSessionCaptureSetting", () => {
  it("enables logs, tool I/O, and session capture together", async () => {
    render(<EnableLoggingAndSessionCaptureSetting />);

    fireEvent.click(
      screen.getByRole("switch", {
        name: "Enable logging and session capture",
      }),
    );

    await waitFor(() => {
      expect(testState.mutateAsync).toHaveBeenCalledTimes(3);
    });
    expect(testState.mutateAsync).toHaveBeenNthCalledWith(
      1,
      requestFor("logs", true),
    );
    expect(testState.mutateAsync).toHaveBeenNthCalledWith(
      2,
      requestFor("tool_io_logs", true),
    );
    expect(testState.mutateAsync).toHaveBeenNthCalledWith(
      3,
      requestFor("session_capture", true),
    );
  });

  it("disables the same three features together", async () => {
    testState.data = {
      logsEnabled: true,
      toolIoLogsEnabled: true,
      sessionCaptureEnabled: true,
    };

    render(<EnableLoggingAndSessionCaptureSetting />);
    fireEvent.click(
      screen.getByRole("switch", {
        name: "Enable logging and session capture",
      }),
    );

    await waitFor(() => {
      expect(testState.mutateAsync).toHaveBeenCalledTimes(3);
    });
    expect(testState.mutateAsync).toHaveBeenNthCalledWith(
      1,
      requestFor("tool_io_logs", false),
    );
    expect(testState.mutateAsync).toHaveBeenNthCalledWith(
      2,
      requestFor("session_capture", false),
    );
    expect(testState.mutateAsync).toHaveBeenNthCalledWith(
      3,
      requestFor("logs", false),
    );
  });

  it("only enables the features that are still off", async () => {
    testState.data = {
      logsEnabled: true,
      toolIoLogsEnabled: false,
      sessionCaptureEnabled: false,
    };

    render(<EnableLoggingAndSessionCaptureSetting />);
    fireEvent.click(
      screen.getByRole("switch", {
        name: "Enable logging and session capture",
      }),
    );

    await waitFor(() => {
      expect(testState.mutateAsync).toHaveBeenCalledTimes(2);
    });
    expect(testState.mutateAsync).toHaveBeenCalledWith(
      requestFor("tool_io_logs", true),
    );
    expect(testState.mutateAsync).toHaveBeenCalledWith(
      requestFor("session_capture", true),
    );
    expect(testState.mutateAsync).not.toHaveBeenCalledWith(
      requestFor("logs", true),
    );
  });

  it("notifies the parent after a successful enable", async () => {
    const onEnabledChange = vi.fn();
    render(
      <EnableLoggingAndSessionCaptureSetting
        onEnabledChange={(enabled) => {
          onEnabledChange(enabled);
        }}
      />,
    );

    fireEvent.click(
      screen.getByRole("switch", {
        name: "Enable logging and session capture",
      }),
    );

    await waitFor(() => {
      expect(onEnabledChange).toHaveBeenCalledWith(true);
    });
    expect(invalidateAllProductFeatures).toHaveBeenCalledOnce();
  });

  it("ignores a deferred mutation completion after an organization switch", async () => {
    const onEnabledChange = vi.fn();
    let releaseMutations: (() => void) | undefined;
    const gate = new Promise<void>((resolve) => {
      releaseMutations = () => {
        resolve();
      };
    });
    testState.mutateAsync.mockImplementation(async () => {
      await gate;
    });

    const notifyEnabled = (enabled: boolean) => {
      onEnabledChange(enabled);
    };
    const { rerender } = render(
      <EnableLoggingAndSessionCaptureSetting onEnabledChange={notifyEnabled} />,
    );

    fireEvent.click(
      screen.getByRole("switch", {
        name: "Enable logging and session capture",
      }),
    );
    await waitFor(() => {
      expect(testState.mutateAsync).toHaveBeenCalled();
    });

    testState.organizationId = "org-next";
    rerender(
      <EnableLoggingAndSessionCaptureSetting onEnabledChange={notifyEnabled} />,
    );
    releaseMutations?.();

    await waitFor(() => {
      expect(testState.mutateAsync).toHaveBeenCalledTimes(3);
    });
    expect(onEnabledChange).not.toHaveBeenCalled();
    expect(invalidateAllProductFeatures).not.toHaveBeenCalled();
  });

  it("surfaces an error without marking the bundle enabled", async () => {
    const onEnabledChange = vi.fn();
    testState.mutateAsync.mockRejectedValue(new Error("write failed"));

    render(
      <EnableLoggingAndSessionCaptureSetting
        onEnabledChange={(enabled) => {
          onEnabledChange(enabled);
        }}
      />,
    );
    fireEvent.click(
      screen.getByRole("switch", {
        name: "Enable logging and session capture",
      }),
    );

    await waitFor(() => {
      expect(handleAPIError).toHaveBeenCalled();
    });
    expect(onEnabledChange).not.toHaveBeenCalled();
    expect(invalidateAllProductFeatures).toHaveBeenCalledOnce();
  });

  it("reports busy while the bundle writes are in flight", async () => {
    const onBusyChange = vi.fn();
    let releaseMutations: (() => void) | undefined;
    const gate = new Promise<void>((resolve) => {
      releaseMutations = () => {
        resolve();
      };
    });
    testState.mutateAsync.mockImplementation(async () => {
      await gate;
    });

    render(
      <EnableLoggingAndSessionCaptureSetting
        onBusyChange={(busy) => {
          onBusyChange(busy);
        }}
      />,
    );
    fireEvent.click(
      screen.getByRole("switch", {
        name: "Enable logging and session capture",
      }),
    );

    await waitFor(() => {
      expect(onBusyChange).toHaveBeenCalledWith(true);
    });
    releaseMutations?.();
    await waitFor(() => {
      expect(onBusyChange).toHaveBeenCalledWith(false);
    });
  });

  it("does not render the switch for non-admins", () => {
    testState.isAdmin = false;

    render(<EnableLoggingAndSessionCaptureSetting />);

    expect(
      screen.queryByRole("switch", {
        name: "Enable logging and session capture",
      }),
    ).toBeNull();
  });
});
