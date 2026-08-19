import { cleanup, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { Features } from "@/pages/organization/Features";
import { anOrganization } from "@/test/fixtures";
import { renderWithApp } from "@/test/harness";

const mocks = vi.hoisted(() => ({
  getOrganizationFeatures: vi.fn(),
}));

vi.mock("@/lib/gramAdminApi", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/lib/gramAdminApi")>();
  return {
    ...actual,
    getOrganizationFeatures: mocks.getOrganizationFeatures,
  };
});

const ORG = anOrganization();

beforeEach(() => {
  for (const mock of Object.values(mocks)) mock.mockReset();
  mocks.getOrganizationFeatures.mockResolvedValue({
    consent_tool_filtering_enabled: true,
    hooks_browser_login_enabled: false,
    hooks_fail_open_enabled: true,
    platform_mcp_enabled: true,
    remote_session_auto_refresh_policy: "enforced",
    session_capture_enabled: true,
    skill_capture_metadata_only: false,
    skills_enabled: true,
  });
});

afterEach(cleanup);

async function renderFeatures(): Promise<void> {
  await renderWithApp(<Features org={ORG} />);
}

describe("Features", () => {
  it("renders the curated organization feature states", async () => {
    await renderFeatures();

    expect(await screen.findByText("Consent tool filtering")).toBeTruthy();
    expect(screen.getByText("Platform MCP")).toBeTruthy();
    expect(screen.getAllByText("Enabled")).toHaveLength(5);
    expect(screen.getAllByText("Disabled")).toHaveLength(2);
    expect(screen.getByText("Enforced")).toBeTruthy();
    expect(mocks.getOrganizationFeatures).toHaveBeenCalledWith(ORG.id);
  });

  it("shows a loading state while the query is pending", async () => {
    mocks.getOrganizationFeatures.mockImplementation(() => new Promise(() => {}));

    await renderFeatures();

    expect(screen.getByText("Loading...")).toBeTruthy();
  });

  it("shows a load failure message when the query fails", async () => {
    mocks.getOrganizationFeatures.mockRejectedValue(new Error("boom"));

    await renderFeatures();

    expect(await screen.findByText("Unable to load features")).toBeTruthy();
  });
});
