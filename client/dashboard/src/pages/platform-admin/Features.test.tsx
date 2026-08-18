import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import PlatformAdminFeatures from "./Features";
import type { ReactNode } from "react";
import { queryKeyChatAnalysisSettings } from "@gram/client/react-query/chatAnalysisSettings.js";

const mocks = vi.hoisted(() => ({
  businessMemoryError: null as Error | null,
  businessMemoryMutate: vi.fn(),
  organizationId: "org-target",
  query: vi.fn(),
  triggerMutate: vi.fn(),
  workUnitsMutate: vi.fn(),
  workUnitsPending: false,
}));

vi.mock("@/contexts/Auth", () => ({
  useOrganization: () => ({ id: mocks.organizationId }),
}));
vi.mock("./AdminSection", () => ({
  AdminSection: ({ children }: { children: ReactNode }) => <>{children}</>,
  AdminRow: ({
    action,
    children,
  }: {
    action?: ReactNode;
    children?: ReactNode;
  }) => (
    <div>
      {action}
      {children}
    </div>
  ),
}));
vi.mock("./PlatformAdminGate", () => ({
  PlatformAdminGate: ({ children }: { children: ReactNode }) => <>{children}</>,
}));
vi.mock("@/components/page-layout", () => {
  function Page({ children }: { children: ReactNode }) {
    return <>{children}</>;
  }
  function Header({ children }: { children?: ReactNode }) {
    return <>{children}</>;
  }
  Header.Breadcrumbs = () => null;
  function Section({ children }: { children: ReactNode }) {
    return <>{children}</>;
  }
  Section.Title = ({ children }: { children: ReactNode }) => <>{children}</>;
  Section.Description = ({ children }: { children: ReactNode }) => (
    <>{children}</>
  );
  Section.Body = ({ children }: { children: ReactNode }) => <>{children}</>;
  Page.Header = Header;
  Page.Body = ({ children }: { children: ReactNode }) => <>{children}</>;
  Page.Section = Section;
  return { Page };
});
vi.mock(
  "@gram/client/react-query/chatAnalysisSettings.js",
  async (original) => ({
    ...(await original<
      typeof import("@gram/client/react-query/chatAnalysisSettings.js")
    >()),
    invalidateAllChatAnalysisSettings: vi.fn(),
    useChatAnalysisSettings: mocks.query,
  }),
);
vi.mock("@gram/client/react-query/upsertChatAnalysisSettings.js", async () => {
  const { useState } = await import("react");
  return {
    useUpsertChatAnalysisSettingsMutation: () =>
      useState(() => ({
        error: null,
        isPending: mocks.workUnitsPending,
        mutate: mocks.workUnitsMutate,
      }))[0],
  };
});
vi.mock(
  "@gram/client/react-query/upsertBusinessMemoryAnalysisSettings.js",
  async () => {
    const { useState } = await import("react");
    return {
      useUpsertBusinessMemoryAnalysisSettingsMutation: () =>
        useState(() => ({
          error: mocks.businessMemoryError,
          isPending: false,
          mutate: mocks.businessMemoryMutate,
        }))[0],
    };
  },
);
vi.mock("@gram/client/react-query/triggerChatAnalysis.js", () => ({
  useTriggerChatAnalysisMutation: () => ({
    isPending: false,
    mutate: mocks.triggerMutate,
  }),
}));
vi.mock("@gram/client/react-query/productFeatures.js", () => ({
  invalidateAllProductFeatures: vi.fn(),
  useProductFeatures: () => ({ data: null, error: null, isLoading: true }),
}));
vi.mock("@gram/client/react-query/featuresSet.js", () => ({
  useFeaturesSetMutation: () => ({
    error: null,
    isPending: false,
    mutate: vi.fn(),
    variables: undefined,
  }),
}));
vi.mock("@/hooks/useFeatureFlag", () => ({
  useFeatureFlag: () => ({ status: "disabled" }),
}));
vi.mock("@/hooks/useOrgMemoryDeveloperToggle", () => ({
  useOrgMemoryDeveloperToggle: () => [false, vi.fn()],
}));
vi.mock("@tanstack/react-query", async (original) => ({
  ...(await original<typeof import("@tanstack/react-query")>()),
  useQueryClient: () => ({}),
}));

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
  mocks.businessMemoryError = null;
  mocks.organizationId = "org-target";
  mocks.workUnitsPending = false;
});

describe("PlatformAdminFeatures", () => {
  it("scopes every chat analysis request to the selected organization", () => {
    mocks.query.mockReturnValue({
      data: {
        businessMemoryDailyCap: 20,
        businessMemoryEnabled: true,
        workUnitsDailyCap: 10,
        workUnitsEnabled: true,
      },
      error: null,
      isLoading: false,
    });

    render(<PlatformAdminFeatures />);

    expect(mocks.query).toHaveBeenCalledTimes(2);
    for (const call of mocks.query.mock.calls) {
      expect(call[0]).toEqual({ organizationId: "org-target" });
    }

    const disableButtons = screen.getAllByRole("button", { name: "Disable" });
    fireEvent.click(disableButtons[0]!);
    fireEvent.click(disableButtons[1]!);
    fireEvent.click(screen.getAllByRole("button", { name: "Run now" })[0]!);

    expect(mocks.workUnitsMutate).toHaveBeenCalledWith(
      {
        request: {
          upsertWorkUnitsSettingsRequestBody: {
            organizationId: "org-target",
            workUnitsDailyCap: 10,
            workUnitsEnabled: false,
          },
        },
      },
      expect.anything(),
    );
    expect(mocks.businessMemoryMutate).toHaveBeenCalledWith(
      {
        request: {
          upsertBusinessMemorySettingsRequestBody: {
            businessMemoryDailyCap: 20,
            businessMemoryEnabled: false,
            organizationId: "org-target",
          },
        },
      },
      expect.anything(),
    );
    expect(mocks.triggerMutate).toHaveBeenCalledWith({
      request: {
        triggerAnalysisRequestBody: { organizationId: "org-target" },
      },
    });
  });

  it("discards an edited cap when the selected organization changes", () => {
    mocks.organizationId = "org-a";
    mocks.query.mockImplementation(({ organizationId }) => ({
      data: {
        businessMemoryDailyCap: organizationId === "org-a" ? 20 : 21,
        businessMemoryEnabled: true,
        workUnitsDailyCap: organizationId === "org-a" ? 10 : 11,
        workUnitsEnabled: true,
      },
      error: null,
      isLoading: false,
    }));

    const { rerender } = render(<PlatformAdminFeatures />);
    fireEvent.change(
      screen.getByRole("textbox", {
        name: "Work delivered daily evaluation cap",
      }),
      { target: { value: "999" } },
    );

    mocks.organizationId = "org-b";
    rerender(<PlatformAdminFeatures />);

    const capInput = screen.getByRole("textbox", {
      name: "Work delivered daily evaluation cap",
    });
    expect((capInput as HTMLInputElement).value).toBe("11");
    fireEvent.click(screen.getAllByRole("button", { name: "Disable" })[0]!);

    expect(mocks.workUnitsMutate).toHaveBeenCalledWith(
      {
        request: {
          upsertWorkUnitsSettingsRequestBody: {
            organizationId: "org-b",
            workUnitsDailyCap: 11,
            workUnitsEnabled: false,
          },
        },
      },
      expect.anything(),
    );
  });

  it("discards mutation state when the selected organization changes", () => {
    mocks.organizationId = "org-a";
    mocks.workUnitsPending = true;
    mocks.businessMemoryError = new Error("Update failed");
    mocks.query.mockReturnValue({
      data: {
        businessMemoryDailyCap: 20,
        businessMemoryEnabled: true,
        workUnitsDailyCap: 10,
        workUnitsEnabled: true,
      },
      error: null,
      isLoading: false,
    });

    const { rerender } = render(<PlatformAdminFeatures />);
    expect(
      (
        screen.getAllByRole("button", {
          name: "Disable",
        })[0] as HTMLButtonElement
      ).disabled,
    ).toBe(true);
    expect(screen.getByText("Update failed")).toBeTruthy();

    mocks.organizationId = "org-b";
    mocks.workUnitsPending = false;
    mocks.businessMemoryError = null;
    rerender(<PlatformAdminFeatures />);

    expect(
      (
        screen.getAllByRole("button", {
          name: "Disable",
        })[0] as HTMLButtonElement
      ).disabled,
    ).toBe(false);
    expect(screen.queryByText("Update failed")).toBeNull();
  });

  it("includes organization scope in the generated settings query key", () => {
    expect(
      queryKeyChatAnalysisSettings({ organizationId: "org-target" }),
    ).toEqual([
      "@gram/client",
      "adminChatAnalysis",
      "getSettings",
      { organizationId: "org-target" },
    ]);
  });
});
