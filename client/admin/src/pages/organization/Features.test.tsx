import type {
  AdminOrganizationChatAnalysisSettings,
  AdminOrganizationFeatures,
} from "@/lib/gramAdminApi";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, screen, waitFor } from "@testing-library/react";

import { Features } from "@/pages/organization/Features";
import { QueryClient } from "@tanstack/react-query";
import { anOrganization } from "@/test/fixtures";
import { organizationFeaturesQuery } from "@/lib/adminQueries";
import { renderWithApp } from "@/test/harness";

const mocks = vi.hoisted(() => ({
  getOrganizationFeatures: vi.fn(),
  setOrganizationFeature: vi.fn(),
  getOrganizationChatAnalysisSettings: vi.fn(),
  setOrganizationChatAnalysisSetting: vi.fn(),
  triggerOrganizationChatAnalysis: vi.fn(),
}));

vi.mock("@/lib/gramAdminApi", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/lib/gramAdminApi")>();
  return {
    ...actual,
    getOrganizationFeatures: mocks.getOrganizationFeatures,
    setOrganizationFeature: mocks.setOrganizationFeature,
    getOrganizationChatAnalysisSettings:
      mocks.getOrganizationChatAnalysisSettings,
    setOrganizationChatAnalysisSetting:
      mocks.setOrganizationChatAnalysisSetting,
    triggerOrganizationChatAnalysis: mocks.triggerOrganizationChatAnalysis,
  };
});

const ORG = anOrganization();
const FEATURES: AdminOrganizationFeatures = {
  authz_challenge_logging_enabled: true,
  customer_managed_encryption_keys_enabled: false,
  custom_model_keys_enabled: true,
  platform_mcp_enabled: true,
  network_ingress_enabled: false,
  remote_session_auto_refresh_enabled: false,
  session_portability_enabled: false,
  sso_enabled: true,
  scim_enabled: false,
};
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
  mocks.getOrganizationFeatures.mockResolvedValue(FEATURES);
  mocks.setOrganizationFeature.mockImplementation(
    ({ featureName, enabled }: { featureName: string; enabled: boolean }) =>
      Promise.resolve({
        ...FEATURES,
        [`${featureName}_enabled`]: enabled,
      }),
  );
  mocks.getOrganizationChatAnalysisSettings.mockResolvedValue(CHAT_ANALYSIS);
  mocks.setOrganizationChatAnalysisSetting.mockResolvedValue(CHAT_ANALYSIS);
  mocks.triggerOrganizationChatAnalysis.mockResolvedValue({
    projects_signaled: 2,
  });
});

afterEach(cleanup);

async function renderFeatures(queryClient?: QueryClient): Promise<void> {
  await renderWithApp(<Features org={ORG} />, { queryClient });
}

describe("Features", () => {
  it("matches the dashboard platform-admin product feature list", async () => {
    await renderFeatures();

    for (const label of [
      "Authz Challenge Logging",
      "Customer-Managed Encryption Keys",
      "Custom Model Provider Keys",
      "Platform MCP access",
      "Private Network Ingress",
      "Automatic Remote Session Refresh",
      "SSO",
      "SCIM",
    ]) {
      expect(await screen.findByText(label)).toBeTruthy();
    }
    expect(screen.queryByText("Consent tool filtering")).toBeNull();
    expect(mocks.getOrganizationFeatures).toHaveBeenCalledWith(ORG.id);
  });

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

  it("updates an organization feature from its switch", async () => {
    await renderFeatures();

    const control = await screen.findByRole("switch", {
      name: "Toggle Customer-Managed Encryption Keys",
    });
    expect(control.getAttribute("data-state")).toBe("unchecked");
    fireEvent.click(control);

    await waitFor(() => {
      expect(mocks.setOrganizationFeature).toHaveBeenCalledWith({
        organizationID: ORG.id,
        featureName: "customer_managed_encryption_keys",
        enabled: true,
      });
    });
    expect(control.getAttribute("data-state")).toBe("checked");
  });

  it("restores the previous state and reports a mutation failure", async () => {
    mocks.setOrganizationFeature.mockRejectedValue(new Error("write failed"));
    await renderFeatures();

    const control = await screen.findByRole("switch", {
      name: "Toggle SCIM",
    });
    fireEvent.click(control);

    expect(await screen.findByText("write failed")).toBeTruthy();
    expect(control.getAttribute("data-state")).toBe("unchecked");
  });

  it("shows a loading state while the query is pending", async () => {
    mocks.getOrganizationFeatures.mockImplementation(
      () => new Promise(() => {}),
    );
    await renderFeatures();
    expect(screen.getByText("Loading...")).toBeTruthy();
  });

  it("shows a load failure message when the query fails", async () => {
    mocks.getOrganizationFeatures.mockRejectedValue(new Error("boom"));
    await renderFeatures();
    expect(await screen.findByText("Unable to load features")).toBeTruthy();
  });

  it("keeps the last loaded state when a refresh fails", async () => {
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    await renderFeatures(queryClient);
    expect(await screen.findByText("Authz Challenge Logging")).toBeTruthy();

    mocks.getOrganizationFeatures.mockRejectedValue(new Error("boom"));
    await queryClient.invalidateQueries(organizationFeaturesQuery(ORG.id));

    expect(
      await screen.findByText(
        "Unable to refresh features; showing the last loaded state.",
      ),
    ).toBeTruthy();
    expect(screen.getByText("Authz Challenge Logging")).toBeTruthy();
  });
});
