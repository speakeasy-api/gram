import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

const LONG_ORGANIZATION_ID =
  "org_0000000000000000000000000000000000000000000000000000000000000000";

vi.mock("@/contexts/Auth", () => ({
  useOrganization: () => ({
    id: LONG_ORGANIZATION_ID,
    slug: "test-org",
  }),
}));

vi.mock("@/contexts/Sdk", () => ({
  useSdkClient: () => ({ auth: { logout: vi.fn() } }),
}));

import { PlatformAdminInfoPanel } from "./platform-admin-panel";

afterEach(cleanup);

describe("PlatformAdminInfoPanel", () => {
  it("wraps a long organization ID instead of cropping it", () => {
    render(<PlatformAdminInfoPanel />);

    const organizationId = screen.getByText(LONG_ORGANIZATION_ID);
    expect(organizationId.classList.contains("break-all")).toBe(true);
    expect(organizationId.classList.contains("truncate")).toBe(false);
  });
});
