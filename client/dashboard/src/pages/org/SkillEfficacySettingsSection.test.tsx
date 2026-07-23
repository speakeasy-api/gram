import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
} from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const testState = vi.hoisted(() => ({
  hasAdmin: true,
  skillsEnabled: true,
  mutate: vi.fn(),
  mutationOptions: undefined as
    | {
        onError?: (error: Error) => void;
        onSuccess?: () => Promise<void>;
      }
    | undefined,
}));

vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({
    hasScope: () => testState.hasAdmin,
    isLoading: false,
  }),
}));

vi.mock("@gram/client/react-query/productFeatures.js", () => ({
  useProductFeatures: () => ({
    data: { skillsEnabled: testState.skillsEnabled },
    isLoading: false,
  }),
}));

vi.mock("@gram/client/react-query/skillEfficacySettings.js", () => ({
  invalidateAllSkillEfficacySettings: vi.fn(),
  useSkillEfficacySettings: () => ({
    data: {
      enabled: true,
      isDefault: true,
      newVersionBurst: 25,
      orgDailyCap: 100,
      organizationId: "org-1",
      perSkillDailyCap: 10,
    },
    error: null,
    isLoading: false,
    refetch: vi.fn(),
  }),
}));

vi.mock("@gram/client/react-query/upsertSkillEfficacySettings.js", () => ({
  useUpsertSkillEfficacySettingsMutation: (options: {
    onError?: (error: Error) => void;
    onSuccess?: () => Promise<void>;
  }) => {
    testState.mutationOptions = options;
    return { mutate: testState.mutate, isPending: false };
  },
}));

vi.mock("@tanstack/react-query", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@tanstack/react-query")>()),
  useQueryClient: () => ({}),
}));

vi.mock("sonner", () => ({
  toast: { error: vi.fn(), success: vi.fn() },
}));

import { SkillEfficacySettingsSection } from "./SkillEfficacySettingsSection";

beforeEach(() => {
  testState.hasAdmin = true;
  testState.skillsEnabled = true;
  testState.mutate.mockReset();
  testState.mutationOptions = undefined;
});

afterEach(cleanup);

describe("SkillEfficacySettingsSection", () => {
  it("shows defaults and sampling semantics to organization admins", () => {
    render(<SkillEfficacySettingsSection />);

    expect(
      (screen.getByLabelText("Per-skill daily cap") as HTMLInputElement).value,
    ).toBe("10");
    expect(
      (screen.getByLabelText("Organization daily ceiling") as HTMLInputElement)
        .value,
    ).toBe("100");
    expect(
      (screen.getByLabelText("New-version lifetime burst") as HTMLInputElement)
        .value,
    ).toBe("25");
    expect(screen.getByText(/reset at 00:00 UTC/i)).not.toBeNull();
    expect(
      screen.getByText(/never bypasses the organization daily ceiling/i),
    ).not.toBeNull();
  });

  it("validates limits and submits all settings together", () => {
    render(<SkillEfficacySettingsSection />);

    fireEvent.change(screen.getByLabelText("Per-skill daily cap"), {
      target: { value: "12" },
    });
    fireEvent.change(screen.getByLabelText("Organization daily ceiling"), {
      target: { value: "150" },
    });
    fireEvent.change(screen.getByLabelText("New-version lifetime burst"), {
      target: { value: "30" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Save settings" }));

    expect(testState.mutate).toHaveBeenCalledWith({
      request: {
        upsertSettingsRequestBody: {
          enabled: true,
          newVersionBurst: 30,
          orgDailyCap: 150,
          perSkillDailyCap: 12,
        },
      },
    });

    fireEvent.change(screen.getByLabelText("Per-skill daily cap"), {
      target: { value: "10001" },
    });
    expect(screen.getByText(/from 0 to 10,000/i)).not.toBeNull();
    expect(
      (
        screen.getByRole("button", {
          name: "Save settings",
        }) as HTMLButtonElement
      ).disabled,
    ).toBe(true);
  });

  it("surfaces API failures without changing the saved settings", () => {
    render(<SkillEfficacySettingsSection />);

    act(() => {
      testState.mutationOptions?.onError?.(new Error("Request rejected"));
    });

    expect(screen.getByText("Request rejected")).not.toBeNull();
    expect(
      (screen.getByLabelText("Per-skill daily cap") as HTMLInputElement).value,
    ).toBe("10");
  });

  it("stays hidden without both the Skills feature and org admin scope", () => {
    testState.hasAdmin = false;
    const { rerender } = render(<SkillEfficacySettingsSection />);
    expect(screen.queryByText("Skill efficacy sampling")).toBeNull();

    testState.hasAdmin = true;
    testState.skillsEnabled = false;
    rerender(<SkillEfficacySettingsSection />);
    expect(screen.queryByText("Skill efficacy sampling")).toBeNull();
  });
});
