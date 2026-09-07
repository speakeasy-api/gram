import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const testState = vi.hoisted(() => ({
  metadataOnly: false,
  organizationId: "org-active",
  mutationOptions: [] as Array<{ onSuccess?: () => Promise<void> }>,
  mutate: vi.fn(),
  productFeaturesQuery: vi.fn(),
}));

vi.mock("@/contexts/Auth", () => ({
  useOrganization: () => ({ id: testState.organizationId }),
}));

vi.mock("@/components/require-scope", () => ({
  RequireScope: ({ children }: { children: ReactNode }) => <>{children}</>,
}));

vi.mock("@/lib/errors", () => ({ handleAPIError: vi.fn() }));

vi.mock("@gram/client/react-query/featuresSet.js", () => ({
  useFeaturesSetMutation: (options: { onSuccess?: () => Promise<void> }) => {
    testState.mutationOptions.push(options);
    return { isPending: false, mutate: testState.mutate };
  },
}));

const invalidateAllProductFeatures = vi.hoisted(() => vi.fn());

vi.mock("@gram/client/react-query/productFeatures.js", () => ({
  invalidateAllProductFeatures,
  useProductFeatures: (...args: unknown[]) => {
    testState.productFeaturesQuery(...args);
    return {
      data: {
        skillCaptureMetadataOnly: testState.metadataOnly,
      },
    };
  },
}));

vi.mock("@tanstack/react-query", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@tanstack/react-query")>()),
  useQueryClient: () => ({}),
}));

import { SkillContentUploadSetting } from "./SkillContentUploadSetting";

beforeEach(() => {
  testState.metadataOnly = false;
  testState.organizationId = "org-active";
  testState.mutationOptions = [];
  invalidateAllProductFeatures.mockReset();
  testState.mutate.mockReset();
  testState.productFeaturesQuery.mockReset();
});

afterEach(cleanup);

describe("SkillContentUploadSetting", () => {
  it("ignores a deferred mutation completion after an organization switch", async () => {
    const { rerender } = render(<SkillContentUploadSetting />);
    const activeMutation = testState.mutationOptions.at(-1);
    expect(activeMutation?.onSuccess).toBeTypeOf("function");

    testState.organizationId = "org-next";
    rerender(<SkillContentUploadSetting />);
    await activeMutation!.onSuccess!();

    expect(invalidateAllProductFeatures).not.toHaveBeenCalled();
    expect(testState.productFeaturesQuery).toHaveBeenLastCalledWith(
      { organizationId: "org-next" },
      undefined,
      expect.anything(),
    );
  });

  it("invalidates product features after a current-organization update", async () => {
    render(<SkillContentUploadSetting />);
    const activeMutation = testState.mutationOptions.at(-1);
    expect(activeMutation?.onSuccess).toBeTypeOf("function");

    await activeMutation!.onSuccess!();

    expect(invalidateAllProductFeatures).toHaveBeenCalledOnce();
  });

  it("updates the metadata-only feature through the upload toggle", () => {
    render(<SkillContentUploadSetting />);

    expect(testState.productFeaturesQuery).toHaveBeenCalledWith(
      { organizationId: "org-active" },
      undefined,
      expect.anything(),
    );
    const toggle = screen.getByRole("switch", {
      name: "Upload skill content",
    });
    expect(toggle.getAttribute("aria-checked")).toBe("true");

    fireEvent.click(toggle);

    expect(testState.mutate).toHaveBeenCalledWith({
      request: {
        setProductFeatureRequestBody: {
          enabled: true,
          featureName: "skill_capture_metadata_only",
          organizationId: "org-active",
        },
      },
    });
  });
});
