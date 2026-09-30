import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { DeviceAgentConfiguration } from "@gram/client/models/components/deviceagentconfiguration.js";

const mocks = vi.hoisted(() => ({
  mutate: vi.fn(),
  isPlatformAdmin: false,
  configuration: undefined as unknown,
}));

vi.mock("@/contexts/Auth", () => ({
  useIsPlatformAdmin: () => mocks.isPlatformAdmin,
}));
vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({ hasScope: () => true }),
}));
vi.mock("@/components/require-scope", () => ({
  RequireScope: ({ children }: { children: React.ReactNode }) => children,
}));
vi.mock("@gram/client/react-query/deviceAgentConfiguration.js", () => ({
  invalidateAllDeviceAgentConfiguration: vi.fn(),
  useDeviceAgentConfiguration: () => ({
    data: mocks.configuration,
    isLoading: false,
    error: null,
  }),
}));
vi.mock("@gram/client/react-query/updateDeviceAgentConfiguration.js", () => ({
  useUpdateDeviceAgentConfigurationMutation: () => ({
    mutate: mocks.mutate,
    isPending: false,
  }),
}));
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));

import { DeviceAgentConfigurationTab } from "./device-agent-configuration";

// A config a platform administrator has already given release controls.
const storedConfiguration = {
  etag: "etag",
  isConfigured: true,
  config: {
    platforms: { cursor: "user" },
    update_channel: "beta",
    blocked_versions: ["1.2.3"],
    sync_interval_seconds: 60,
  },
} as unknown as DeviceAgentConfiguration;

function sentConfig(): Record<string, unknown> {
  expect(mocks.mutate).toHaveBeenCalledOnce();
  return mocks.mutate.mock.lastCall?.[0].request.updateConfigurationRequestBody
    .config;
}

describe("DeviceAgentConfigurationTab", () => {
  afterEach(cleanup);
  beforeEach(() => {
    vi.clearAllMocks();
    mocks.configuration = storedConfiguration;
  });

  function renderAndSave(): void {
    render(
      <QueryClientProvider client={new QueryClient()}>
        <DeviceAgentConfigurationTab />
      </QueryClientProvider>,
    );
    fireEvent.change(screen.getByLabelText("Sync interval"), {
      target: { value: "120" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Save configuration" }));
  }

  it("omits platform-admin-only keys when an org admin saves", () => {
    mocks.isPlatformAdmin = false;
    renderAndSave();

    const config = sentConfig();
    expect(config.sync_interval_seconds).toBe(120);
    expect(config).not.toHaveProperty("update_channel");
    expect(config).not.toHaveProperty("blocked_versions");
  });

  it("sends platform-admin-only keys when a platform admin saves", () => {
    mocks.isPlatformAdmin = true;
    renderAndSave();

    const config = sentConfig();
    expect(config.update_channel).toBe("beta");
    expect(config.blocked_versions).toEqual(["1.2.3"]);
  });
});
