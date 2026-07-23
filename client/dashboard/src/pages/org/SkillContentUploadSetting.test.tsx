import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const testState = vi.hoisted(() => ({
  metadataOnly: false,
  mutate: vi.fn(),
  skillsEnabled: true,
}));

vi.mock("@/components/require-scope", () => ({
  RequireScope: ({ children }: { children: ReactNode }) => <>{children}</>,
}));

vi.mock("@/lib/errors", () => ({ handleAPIError: vi.fn() }));

vi.mock("@gram/client/react-query/featuresSet.js", () => ({
  useFeaturesSetMutation: () => ({
    isPending: false,
    mutate: testState.mutate,
  }),
}));

vi.mock("@gram/client/react-query/productFeatures.js", () => ({
  invalidateAllProductFeatures: vi.fn(),
  useProductFeatures: () => ({
    data: {
      skillCaptureMetadataOnly: testState.metadataOnly,
      skillsEnabled: testState.skillsEnabled,
    },
  }),
}));

vi.mock("@tanstack/react-query", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@tanstack/react-query")>()),
  useQueryClient: () => ({}),
}));

import { SkillContentUploadSetting } from "./SkillContentUploadSetting";

beforeEach(() => {
  testState.metadataOnly = false;
  testState.mutate.mockReset();
  testState.skillsEnabled = true;
});

afterEach(cleanup);

describe("SkillContentUploadSetting", () => {
  it("updates the metadata-only feature through the upload toggle", () => {
    render(<SkillContentUploadSetting />);

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
        },
      },
    });
  });

  it("stays hidden when Skills is disabled", () => {
    testState.skillsEnabled = false;
    render(<SkillContentUploadSetting />);

    expect(screen.queryByText("Upload Skill Content")).toBeNull();
  });
});
