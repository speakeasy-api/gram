import { QueryClient } from "@tanstack/react-query";
import { cleanup, fireEvent, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { organizationFeaturesQuery } from "@/lib/adminQueries";
import type { AdminOrganizationFeatures } from "@/lib/gramAdminApi";
import { Features } from "@/pages/organization/Features";
import { anOrganization } from "@/test/fixtures";
import { renderWithApp } from "@/test/harness";

const mocks = vi.hoisted(() => ({
  getOrganizationFeatures: vi.fn(),
  setOrganizationFeature: vi.fn(),
}));

vi.mock("@/lib/gramAdminApi", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/lib/gramAdminApi")>();
  return {
    ...actual,
    getOrganizationFeatures: mocks.getOrganizationFeatures,
    setOrganizationFeature: mocks.setOrganizationFeature,
  };
});

const ORG = anOrganization();
const FEATURES: AdminOrganizationFeatures = {
  authz_challenge_logging_enabled: true,
  customer_managed_encryption_keys_enabled: false,
  custom_model_keys_enabled: true,
  platform_mcp_enabled: true,
  remote_session_auto_refresh_enabled: false,
  sso_enabled: true,
  scim_enabled: false,
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
      "Automatic Remote Session Refresh",
      "SSO",
      "SCIM",
    ]) {
      expect(await screen.findByText(label)).toBeTruthy();
    }
    expect(screen.queryByText("Consent tool filtering")).toBeNull();
    expect(mocks.getOrganizationFeatures).toHaveBeenCalledWith(ORG.id);
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
