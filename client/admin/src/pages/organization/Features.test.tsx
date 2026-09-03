import { QueryClient } from "@tanstack/react-query";
import { cleanup, fireEvent, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { organizationFeaturesQuery } from "@/lib/gramAdminClient";
import type { AdminOrganizationChatAnalysisSettings } from "@/lib/gramAdminApi";
import type { ProductFeatures } from "@gram/admin-client/models/components/productfeatures";
import type { FeatureName } from "@gram/admin-client/models/components/setorganizationfeaturerequestbody";
import { queryKeyAdminOrganizationFeatures } from "@gram/admin-client/react-query/adminOrganizationFeatures.core";
import { Features } from "@/pages/organization/Features";
import { anOrganization } from "@/test/fixtures";
import { renderWithApp } from "@/test/harness";

const mocks = vi.hoisted(() => ({
  featureFetch: vi.fn(),
  getOrganizationChatAnalysisSettings: vi.fn(),
  setOrganizationChatAnalysisSetting: vi.fn(),
  triggerOrganizationChatAnalysis: vi.fn(),
}));

vi.mock("@/lib/gramAdminApi", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/lib/gramAdminApi")>();
  return {
    ...actual,
    getOrganizationChatAnalysisSettings:
      mocks.getOrganizationChatAnalysisSettings,
    setOrganizationChatAnalysisSetting:
      mocks.setOrganizationChatAnalysisSetting,
    triggerOrganizationChatAnalysis: mocks.triggerOrganizationChatAnalysis,
  };
});

const ORG = anOrganization();
const FEATURES: ProductFeatures = {
  aiPlatformPushIntegrationsEnabled: false,
  authzChallengeLoggingEnabled: true,
  consentToolFilteringEnabled: false,
  customModelKeysEnabled: true,
  customerManagedEncryptionKeysEnabled: false,
  deviceAgent: false,
  hooksBrowserLoginEnabled: false,
  hooksFailOpenEnabled: false,
  logsEnabled: true,
  platformMcpEnabled: true,
  remoteSessionAutoRefreshEnabled: false,
  remoteSessionAutoRefreshEnforcedEnabled: false,
  scimEnabled: false,
  sessionCaptureEnabled: false,
  sessionPortabilityEnabled: false,
  skillCaptureMetadataOnly: false,
  skillsEnabled: true,
  ssoEnabled: true,
  toolIoLogsEnabled: true,
};

const FEATURES_RESPONSE = {
  ai_platform_push_integrations_enabled: false,
  authz_challenge_logging_enabled: true,
  consent_tool_filtering_enabled: false,
  custom_model_keys_enabled: true,
  customer_managed_encryption_keys_enabled: false,
  device_agent: false,
  hooks_browser_login_enabled: false,
  hooks_fail_open_enabled: false,
  logs_enabled: true,
  platform_mcp_enabled: true,
  remote_session_auto_refresh_enabled: false,
  remote_session_auto_refresh_enforced_enabled: false,
  scim_enabled: false,
  session_capture_enabled: false,
  session_portability_enabled: false,
  skill_capture_metadata_only: false,
  skills_enabled: true,
  sso_enabled: true,
  tool_io_logs_enabled: true,
};

const TOGGLE_FEATURES = [
  {
    featureName: "ai_platform_push_integrations",
    enabledKey: "aiPlatformPushIntegrationsEnabled",
    label: "AI Platform Push Integrations",
  },
  {
    featureName: "authz_challenge_logging",
    enabledKey: "authzChallengeLoggingEnabled",
    label: "Authz Challenge Logging",
  },
  {
    featureName: "customer_managed_encryption_keys",
    enabledKey: "customerManagedEncryptionKeysEnabled",
    label: "Customer-Managed Encryption Keys",
  },
  {
    featureName: "custom_model_keys",
    enabledKey: "customModelKeysEnabled",
    label: "Custom Model Provider Keys",
  },
  {
    featureName: "platform_mcp",
    enabledKey: "platformMcpEnabled",
    label: "Platform MCP access",
  },
  {
    featureName: "remote_session_auto_refresh",
    enabledKey: "remoteSessionAutoRefreshEnabled",
    label: "Automatic Remote Session Refresh",
  },
  {
    featureName: "session_portability",
    enabledKey: "sessionPortabilityEnabled",
    label: "Session Portability",
  },
  { featureName: "sso", enabledKey: "ssoEnabled", label: "SSO" },
  { featureName: "scim", enabledKey: "scimEnabled", label: "SCIM" },
] as const satisfies readonly {
  featureName: FeatureName;
  enabledKey: keyof ProductFeatures;
  label: string;
}[];

type ToggleFeatureName = (typeof TOGGLE_FEATURES)[number]["featureName"];

const MANAGED_FEATURES = {
  logs: "managed-elsewhere",
  tool_io_logs: "managed-elsewhere",
  session_capture: "managed-elsewhere",
  hooks_browser_login: "managed-elsewhere",
  hooks_fail_open: "managed-elsewhere",
  skills: "managed-elsewhere",
  skill_capture_metadata_only: "managed-elsewhere",
  remote_session_auto_refresh_enforced: "managed-elsewhere",
  consent_tool_filtering: "managed-elsewhere",
} as const satisfies Record<
  Exclude<FeatureName, ToggleFeatureName>,
  "managed-elsewhere"
>;

function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

function serviceError(message: string, status: number): Response {
  return jsonResponse(
    {
      fault: status >= 500,
      id: "placeholder",
      message,
      name: status >= 500 ? "internal_error" : "invalid_request",
      temporary: false,
      timeout: false,
    },
    status,
  );
}
const CHAT_ANALYSIS: AdminOrganizationChatAnalysisSettings = {
  organization_id: ORG.id,
  work_units_enabled: true,
  work_units_daily_cap: 25,
  business_memory_enabled: false,
  business_memory_daily_cap: 0,
  is_default: false,
};

beforeEach(() => {
  for (const mock of Object.values(mocks)) mock.mockReset();
  mocks.featureFetch.mockImplementation(async (input: RequestInfo | URL) => {
    const request = input as Request;
    if (request.method === "GET") return jsonResponse(FEATURES_RESPONSE);

    const body = (await request.clone().json()) as {
      enabled: boolean;
      feature_name: string;
    };
    return jsonResponse({
      ...FEATURES_RESPONSE,
      [`${body.feature_name}_enabled`]: body.enabled,
    });
  });
  vi.stubGlobal("fetch", mocks.featureFetch);
  mocks.getOrganizationChatAnalysisSettings.mockResolvedValue(CHAT_ANALYSIS);
  mocks.setOrganizationChatAnalysisSetting.mockResolvedValue(CHAT_ANALYSIS);
  mocks.triggerOrganizationChatAnalysis.mockResolvedValue({
    projects_signaled: 2,
  });
});

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

async function renderFeatures(queryClient?: QueryClient): Promise<void> {
  await renderWithApp(<Features org={ORG} />, { queryClient });
}

describe("Features", () => {
  it("renders exactly the nine typed direct toggles", async () => {
    await renderFeatures();

    const switches = await screen.findAllByRole("switch");
    expect(switches).toHaveLength(TOGGLE_FEATURES.length);
    expect(
      TOGGLE_FEATURES.map(({ label }) =>
        screen.getByRole("switch", { name: `Toggle ${label}` }),
      ),
    ).toEqual(switches);
    expect(
      Object.values(MANAGED_FEATURES).filter(
        (classification) => classification === "managed-elsewhere",
      ),
    ).toHaveLength(9);
  });

  it("uses an organization-scoped generated query", async () => {
    await renderFeatures();

    await screen.findAllByRole("switch");
    const request = mocks.featureFetch.mock.calls[0]?.[0] as Request;
    const url = new URL(request.url);
    expect(url.pathname).toBe("/admin/organization.features");
    expect(url.searchParams.get("organization_id")).toBe(ORG.id);
    expect(organizationFeaturesQuery(ORG.id).queryKey).toEqual(
      queryKeyAdminOrganizationFeatures({ organizationId: ORG.id }),
    );
  });

  it.each(TOGGLE_FEATURES)(
    "maps $featureName to $enabledKey and emits its generated name",
    async ({ featureName, enabledKey, label }) => {
      const queryClient = new QueryClient({
        defaultOptions: { queries: { retry: false, staleTime: Infinity } },
      });
      queryClient.setQueryData(organizationFeaturesQuery(ORG.id).queryKey, {
        ...FEATURES,
        ...Object.fromEntries(
          TOGGLE_FEATURES.map((feature) => [feature.enabledKey, true]),
        ),
        [enabledKey]: false,
      });
      await renderFeatures(queryClient);

      const control = screen.getByRole("switch", { name: `Toggle ${label}` });
      expect(control.getAttribute("data-state")).toBe("unchecked");
      fireEvent.click(control);

      await waitFor(() => {
        expect(
          mocks.featureFetch.mock.calls.some(
            ([input]) => (input as Request).method === "POST",
          ),
        ).toBe(true);
      });
      const request = mocks.featureFetch.mock.calls
        .map(([input]) => input as Request)
        .find((input) => input.method === "POST");
      expect(await request?.clone().json()).toEqual({
        organization_id: ORG.id,
        feature_name: featureName,
        enabled: true,
      });
    },
  );

  it("shows chat analysis controls beneath product features", async () => {
    await renderFeatures();

    expect(await screen.findByText("Chat analysis")).toBeTruthy();
    expect(screen.getByText("Work Delivered Chat Analysis")).toBeTruthy();
    expect(screen.getByText("Business Memory Extraction")).toBeTruthy();
    expect(
      screen.getByText(
        "Caps are evaluations per UTC day; a cap of 0 disables the pipeline.",
      ),
    ).toBeTruthy();
    const runNow = screen.getByRole("button", { name: "Run now" });
    expect(runNow.previousElementSibling?.textContent).toBe("Disable");
  });

  it("triggers chat analysis from the enabled row", async () => {
    await renderFeatures();

    fireEvent.click(await screen.findByRole("button", { name: "Run now" }));

    await waitFor(() => {
      expect(mocks.triggerOrganizationChatAnalysis).toHaveBeenCalledWith(
        ORG.id,
      );
    });
    expect(await screen.findByText("Triggered 2 projects.")).toBeTruthy();
  });

  it("enables a disabled zero-cap judge with the default cap", async () => {
    mocks.setOrganizationChatAnalysisSetting.mockResolvedValue({
      ...CHAT_ANALYSIS,
      business_memory_enabled: true,
      business_memory_daily_cap: 100,
    });
    await renderFeatures();

    fireEvent.click(await screen.findByRole("button", { name: "Enable" }));

    await waitFor(() => {
      expect(mocks.setOrganizationChatAnalysisSetting).toHaveBeenCalledWith({
        organizationID: ORG.id,
        judge: "business_memory",
        enabled: true,
        dailyCap: 100,
      });
    });
    expect(
      (
        screen.getByLabelText(
          "Business memory daily extraction cap",
        ) as HTMLInputElement
      ).value,
    ).toBe("100");
  });

  it("saves an integer cap and replaces settings from the response", async () => {
    mocks.setOrganizationChatAnalysisSetting.mockResolvedValue({
      ...CHAT_ANALYSIS,
      work_units_daily_cap: 48,
      business_memory_daily_cap: 7,
    });
    await renderFeatures();

    const input = await screen.findByLabelText(
      "Work delivered daily evaluation cap",
    );
    const otherInput = screen.getByLabelText(
      "Business memory daily extraction cap",
    );
    fireEvent.change(otherInput, { target: { value: "12" } });
    fireEvent.change(input, { target: { value: "48" } });
    fireEvent.click(screen.getByRole("button", { name: "Save cap" }));

    await waitFor(() => {
      expect(mocks.setOrganizationChatAnalysisSetting).toHaveBeenCalledWith({
        organizationID: ORG.id,
        judge: "work_units",
        enabled: true,
        dailyCap: 48,
      });
    });
    expect((otherInput as HTMLInputElement).value).toBe("12");
  });

  it("disables an enabled judge with its stored cap", async () => {
    mocks.setOrganizationChatAnalysisSetting.mockResolvedValue({
      ...CHAT_ANALYSIS,
      work_units_enabled: false,
    });
    await renderFeatures();

    fireEvent.click(await screen.findByRole("button", { name: "Disable" }));

    await waitFor(() => {
      expect(mocks.setOrganizationChatAnalysisSetting).toHaveBeenCalledWith({
        organizationID: ORG.id,
        judge: "work_units",
        enabled: false,
        dailyCap: 25,
      });
    });
  });

  it("rejects non-integer and out-of-range caps", async () => {
    await renderFeatures();
    const input = await screen.findByLabelText(
      "Work delivered daily evaluation cap",
    );

    for (const value of ["", "1.5", "10001", "-1"]) {
      fireEvent.change(input, { target: { value } });
      expect(
        screen.getByText("Cap must be a whole number from 0 to 10,000."),
      ).toBeTruthy();
      expect(
        (screen.getByRole("button", { name: "Disable" }) as HTMLButtonElement)
          .disabled,
      ).toBe(true);
    }
    expect(mocks.setOrganizationChatAnalysisSetting).not.toHaveBeenCalled();
  });

  it("reports a chat analysis mutation failure on its row", async () => {
    mocks.setOrganizationChatAnalysisSetting.mockRejectedValue(
      new Error("chat write failed"),
    );
    await renderFeatures();

    fireEvent.click(await screen.findByRole("button", { name: "Disable" }));

    expect(await screen.findByText("chat write failed")).toBeTruthy();
    expect(screen.getByText("Enabled")).toBeTruthy();
  });

  it("cancels an in-flight refresh before the optimistic generated-key update", async () => {
    let getCount = 0;
    let finishRefresh!: (response: Response) => void;
    let finishMutation!: (response: Response) => void;
    mocks.featureFetch.mockImplementation((input: RequestInfo | URL) => {
      const request = input as Request;
      if (request.method === "POST") {
        return new Promise<Response>((resolve) => {
          finishMutation = resolve;
        });
      }
      getCount += 1;
      if (getCount === 1)
        return Promise.resolve(jsonResponse(FEATURES_RESPONSE));
      return new Promise<Response>((resolve) => {
        finishRefresh = resolve;
      });
    });
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    await renderFeatures(queryClient);

    const control = await screen.findByRole("switch", {
      name: "Toggle Customer-Managed Encryption Keys",
    });
    void queryClient.invalidateQueries(organizationFeaturesQuery(ORG.id));
    await waitFor(() => expect(getCount).toBe(2));
    fireEvent.click(control);

    await waitFor(() => {
      expect(control.getAttribute("data-state")).toBe("checked");
      for (const item of screen.getAllByRole("switch")) {
        expect((item as HTMLButtonElement).disabled).toBe(true);
      }
    });
    const request = mocks.featureFetch.mock.calls
      .map(([input]) => input as Request)
      .find((input) => input.method === "POST");
    expect(new URL(request!.url).pathname).toBe("/admin/organization.features");
    expect(await request?.clone().json()).toEqual({
      organization_id: ORG.id,
      feature_name: "customer_managed_encryption_keys",
      enabled: true,
    });

    finishRefresh(jsonResponse(FEATURES_RESPONSE));
    await waitFor(() => {
      expect(control.getAttribute("data-state")).toBe("checked");
    });
    finishMutation(
      jsonResponse({
        ...FEATURES_RESPONSE,
        customer_managed_encryption_keys_enabled: true,
      }),
    );
    await waitFor(() => {
      expect((control as HTMLButtonElement).disabled).toBe(false);
    });
  });

  it("replaces the full snapshot from success without refetching", async () => {
    mocks.featureFetch.mockImplementation((input: RequestInfo | URL) => {
      const request = input as Request;
      return Promise.resolve(
        jsonResponse(
          request.method === "GET"
            ? FEATURES_RESPONSE
            : {
                ...FEATURES_RESPONSE,
                customer_managed_encryption_keys_enabled: true,
                sso_enabled: false,
              },
        ),
      );
    });
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    await renderFeatures(queryClient);

    fireEvent.click(
      await screen.findByRole("switch", {
        name: "Toggle Customer-Managed Encryption Keys",
      }),
    );

    const sso = screen.getByRole("switch", { name: "Toggle SSO" });
    await waitFor(() => {
      expect(sso.getAttribute("data-state")).toBe("unchecked");
    });
    expect(
      queryClient.getQueryData(organizationFeaturesQuery(ORG.id).queryKey),
    ).toEqual({
      ...FEATURES,
      customerManagedEncryptionKeysEnabled: true,
      ssoEnabled: false,
    });
    expect(
      mocks.featureFetch.mock.calls.filter(
        ([input]) => (input as Request).method === "GET",
      ),
    ).toHaveLength(1);
  });

  it("restores the previous state before refreshing and reports a 4xx on its row", async () => {
    let getCount = 0;
    let finishRefresh!: (response: Response) => void;
    mocks.featureFetch.mockImplementation((input: RequestInfo | URL) => {
      const request = input as Request;
      if (request.method === "POST") {
        return Promise.resolve(serviceError("write failed", 422));
      }
      getCount += 1;
      if (getCount === 1)
        return Promise.resolve(jsonResponse(FEATURES_RESPONSE));
      return new Promise<Response>((resolve) => {
        finishRefresh = resolve;
      });
    });
    await renderFeatures();

    const control = await screen.findByRole("switch", {
      name: "Toggle SCIM",
    });
    fireEvent.click(control);

    await waitFor(() => {
      expect(control.getAttribute("data-state")).toBe("unchecked");
    });
    finishRefresh(jsonResponse(FEATURES_RESPONSE));
    expect(await screen.findByText("write failed")).toBeTruthy();
    expect(control.parentElement?.textContent).toContain("write failed");
    expect(
      screen.getByRole("switch", { name: "Toggle SSO" }).parentElement
        ?.textContent,
    ).not.toContain("write failed");
    expect(control.getAttribute("data-state")).toBe("unchecked");
  });

  it("does not expose a generated 5xx body", async () => {
    mocks.featureFetch.mockImplementation((input: RequestInfo | URL) => {
      const request = input as Request;
      return Promise.resolve(
        request.method === "GET"
          ? jsonResponse(FEATURES_RESPONSE)
          : serviceError("sensitive internal detail", 500),
      );
    });
    await renderFeatures();

    fireEvent.click(await screen.findByRole("switch", { name: "Toggle SCIM" }));

    expect(
      await screen.findByText("gram admin 500 Internal Server Error"),
    ).toBeTruthy();
    expect(screen.queryByText("sensitive internal detail")).toBeNull();
  });

  it("surfaces a mutation 401 without redirecting or refreshing", async () => {
    let getCount = 0;
    mocks.featureFetch.mockImplementation((input: RequestInfo | URL) => {
      const request = input as Request;
      if (request.method === "POST") {
        return Promise.resolve(serviceError("unauthorized", 401));
      }
      getCount += 1;
      return Promise.resolve(
        getCount === 1
          ? jsonResponse(FEATURES_RESPONSE)
          : serviceError("unauthorized", 401),
      );
    });
    const href = vi.spyOn(window.location, "href", "set");
    await renderFeatures();

    fireEvent.click(await screen.findByRole("switch", { name: "Toggle SCIM" }));

    expect(await screen.findByText("unauthorized")).toBeTruthy();
    expect(getCount).toBe(1);
    expect(href).not.toHaveBeenCalled();
  });

  it("shows a loading state while the query is pending", async () => {
    mocks.featureFetch.mockImplementation(() => new Promise(() => {}));
    await renderFeatures();
    expect(screen.getByText("Loading...")).toBeTruthy();
  });

  it("shows a load failure message when the query fails", async () => {
    mocks.featureFetch.mockResolvedValue(serviceError("boom", 500));
    await renderFeatures();
    expect(await screen.findByText("Unable to load features")).toBeTruthy();
  });

  it("keeps the last loaded state when a refresh fails", async () => {
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    await renderFeatures(queryClient);
    expect(await screen.findByText("Authz Challenge Logging")).toBeTruthy();

    mocks.featureFetch.mockRejectedValue(new Error("boom"));
    await queryClient.invalidateQueries(organizationFeaturesQuery(ORG.id));

    expect(
      await screen.findByText(
        "Unable to refresh features; showing the last loaded state.",
      ),
    ).toBeTruthy();
    expect(screen.getByText("Authz Challenge Logging")).toBeTruthy();
    expect(
      screen
        .getByRole("switch", { name: "Toggle Authz Challenge Logging" })
        .getAttribute("data-state"),
    ).toBe("checked");
  });
});
