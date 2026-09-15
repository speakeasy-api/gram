import { renderHook } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

const testState = vi.hoisted(() => ({ organizationId: "org-active" }));

vi.mock("@/contexts/Auth", () => ({
  useOrganization: () => ({ id: testState.organizationId }),
}));

import { useIsCurrentOrganization } from "./useIsCurrentOrganization";

describe("useIsCurrentOrganization", () => {
  it("checks a captured organization against the latest active organization", () => {
    const { result, rerender } = renderHook(() =>
      useIsCurrentOrganization("org-active"),
    );
    const isOriginalOrganizationCurrent = result.current;

    expect(isOriginalOrganizationCurrent()).toBe(true);

    testState.organizationId = "org-next";
    rerender();

    expect(isOriginalOrganizationCurrent()).toBe(false);
  });
});
