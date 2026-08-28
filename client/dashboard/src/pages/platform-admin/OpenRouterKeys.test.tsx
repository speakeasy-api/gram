import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
  within,
} from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { Column } from "@/components/ui/Table";
import type { AdminOpenRouterKey } from "@gram/client/models/components/adminopenrouterkey.js";

type MutationOptions = {
  onSuccess: (
    key: Pick<AdminOpenRouterKey, "keyType" | "organizationName">,
  ) => void;
  onError: (error: unknown) => void;
};

const mocks = vi.hoisted(() => ({
  disableMutate: vi.fn(),
  disableOptions: undefined as MutationOptions | undefined,
  disablePending: false,
  enableMutate: vi.fn(),
  enableOptions: undefined as MutationOptions | undefined,
  enablePending: false,
  invalidate: vi.fn(),
  toastError: vi.fn(),
  toastSuccess: vi.fn(),
}));

vi.mock("@/contexts/Auth", () => ({ useIsPlatformAdmin: () => true }));
vi.mock("@/components/page-layout", () => {
  const Wrapper = ({ children }: { children: ReactNode }) => <>{children}</>;
  return {
    Page: Object.assign(Wrapper, {
      Header: Object.assign(Wrapper, { Breadcrumbs: Wrapper }),
      Body: Wrapper,
      Section: Object.assign(Wrapper, {
        Title: Wrapper,
        Description: Wrapper,
        Body: Wrapper,
      }),
    }),
  };
});
vi.mock("@/components/ui/Table", () => ({
  Table: ({
    columns,
    data,
  }: {
    columns: Column<AdminOpenRouterKey>[];
    data: AdminOpenRouterKey[];
  }) => (
    <div>
      {data.map((row) => (
        <div key={row.organizationSlug} data-testid={row.organizationSlug}>
          {columns.map((column) => (
            <div key={String(column.key)}>{column.render?.(row)}</div>
          ))}
        </div>
      ))}
    </div>
  ),
}));
vi.mock("@/components/ui/Tooltip", () => ({
  SimpleTooltip: ({ children }: { children: ReactNode }) => <>{children}</>,
}));
vi.mock("@gram/client/react-query/adminOpenRouterKeys.js", () => ({
  invalidateAllAdminOpenRouterKeys: mocks.invalidate,
  useAdminOpenRouterKeys: () => ({
    data: {
      keys: [
        {
          createdAt: new Date("2026-01-01"),
          disabled: true,
          disableCauses: ["trial_demotion"],
          gramAccountType: "free",
          keyType: "chat",
          monthlyCredits: 10,
          organizationId: "automatic-id",
          organizationName: "Automatic cause",
          organizationSlug: "automatic-cause",
          updatedAt: new Date("2026-01-01"),
        },
        {
          createdAt: new Date("2026-01-01"),
          disabled: true,
          disableCauses: ["admin_lock", "billing_inactive"],
          gramAccountType: "pro",
          keyType: "internal",
          monthlyCredits: 20,
          organizationId: "combined-id",
          organizationName: "Combined causes",
          organizationSlug: "combined-causes",
          updatedAt: new Date("2026-01-01"),
        },
        {
          createdAt: new Date("2026-01-01"),
          disabled: false,
          disableCauses: ["future_cause"],
          gramAccountType: "pro",
          keyType: "chat",
          monthlyCredits: 30,
          organizationId: "future-id",
          organizationName: "Future cause",
          organizationSlug: "future-cause",
          updatedAt: new Date("2026-01-01"),
        },
        {
          createdAt: new Date("2026-01-01"),
          disabled: true,
          gramAccountType: "pro",
          keyType: "chat",
          monthlyCredits: 40,
          organizationId: "legacy-id",
          organizationName: "Legacy state",
          organizationSlug: "legacy-state",
          updatedAt: new Date("2026-01-01"),
        },
        {
          createdAt: new Date("2026-01-01"),
          disabled: true,
          disableCauses: null,
          gramAccountType: "pro",
          keyType: "internal",
          monthlyCredits: 50,
          organizationId: "legacy-null-id",
          organizationName: "Legacy null state",
          organizationSlug: "legacy-null-state",
          updatedAt: new Date("2026-01-01"),
        },
        {
          createdAt: new Date("2026-01-01"),
          disabled: false,
          disableCauses: [],
          gramAccountType: "pro",
          keyType: "chat",
          monthlyCredits: 60,
          organizationId: "classified-empty-id",
          organizationName: "Classified empty state",
          organizationSlug: "classified-empty-state",
          updatedAt: new Date("2026-01-01"),
        },
      ],
    },
    isLoading: false,
    error: null,
  }),
}));
vi.mock("@gram/client/react-query/adminOpenRouterKeyUsage.js", () => ({
  useAdminOpenRouterKeyUsage: () => ({ data: undefined, isLoading: false }),
}));
vi.mock("@gram/client/react-query/disableAdminOpenRouterKey.js", () => ({
  useDisableAdminOpenRouterKeyMutation: (options: MutationOptions) => {
    mocks.disableOptions = options;
    return { mutate: mocks.disableMutate, isPending: mocks.disablePending };
  },
}));
vi.mock("@gram/client/react-query/enableAdminOpenRouterKey.js", () => ({
  useEnableAdminOpenRouterKeyMutation: (options: MutationOptions) => {
    mocks.enableOptions = options;
    return { mutate: mocks.enableMutate, isPending: mocks.enablePending };
  },
}));
vi.mock("sonner", () => ({
  toast: { success: mocks.toastSuccess, error: mocks.toastError },
}));

import PlatformAdminOpenRouterKeys from "./OpenRouterKeys";

describe("PlatformAdminOpenRouterKeys", () => {
  afterEach(cleanup);
  beforeEach(() => {
    vi.clearAllMocks();
    mocks.disablePending = false;
    mocks.enablePending = false;
  });

  function renderPage(): QueryClient {
    const queryClient = new QueryClient();
    render(
      <QueryClientProvider client={queryClient}>
        <PlatformAdminOpenRouterKeys />
      </QueryClientProvider>,
    );
    return queryClient;
  }

  function openRowActions(testId: string): void {
    fireEvent.pointerDown(
      within(screen.getByTestId(testId)).getByRole("button", {
        name: "Open menu",
      }),
      { button: 0, ctrlKey: false },
    );
  }

  it("shows classified causes and fails closed for unclassified legacy rows", () => {
    renderPage();

    const automatic = within(screen.getByTestId("automatic-cause"));
    expect(automatic.getByText("Trial demotion")).toBeDefined();
    openRowActions("automatic-cause");
    expect(screen.getByRole("menuitem", { name: "Disable key" })).toBeDefined();
    expect(
      screen.queryByRole("menuitem", { name: "Remove admin lock" }),
    ).toBeNull();
    fireEvent.keyDown(document.activeElement ?? document.body, {
      key: "Escape",
    });

    const combined = within(screen.getByTestId("combined-causes"));
    expect(combined.getByText("Admin lock")).toBeDefined();
    expect(combined.getByText("Billing inactive")).toBeDefined();
    openRowActions("combined-causes");
    expect(
      screen.getByRole("menuitem", { name: "Remove admin lock" }),
    ).toBeDefined();
    expect(screen.queryByRole("menuitem", { name: "Disable key" })).toBeNull();
    fireEvent.keyDown(document.activeElement ?? document.body, {
      key: "Escape",
    });

    const future = within(screen.getByTestId("future-cause"));
    expect(future.getByText("future_cause")).toBeDefined();
    expect(future.getByText("Disabled")).toBeDefined();
    openRowActions("future-cause");
    expect(screen.getByRole("menuitem", { name: "Disable key" })).toBeDefined();
    fireEvent.keyDown(document.activeElement ?? document.body, {
      key: "Escape",
    });

    for (const testId of ["legacy-state", "legacy-null-state"]) {
      const legacy = within(screen.getByTestId(testId));
      expect(legacy.getByText("Disabled")).toBeDefined();
      expect(legacy.getByText("Unclassified legacy state")).toBeDefined();
      expect(
        legacy.getByText(
          "Disable causes were not recorded for this legacy key.",
        ),
      ).toBeDefined();
      expect(legacy.queryByRole("button", { name: "Open menu" })).toBeNull();
    }

    const classifiedEmpty = within(
      screen.getByTestId("classified-empty-state"),
    );
    expect(classifiedEmpty.getByText("No disable causes")).toBeDefined();
    expect(classifiedEmpty.queryByText("Unclassified legacy state")).toBeNull();
  });

  it("locks row actions while a mutation is pending and restores trigger focus", () => {
    renderPage();
    const activeTrigger = within(
      screen.getByTestId("automatic-cause"),
    ).getByRole("button", { name: "Open menu" });
    const oppositeTrigger = within(
      screen.getByTestId("combined-causes"),
    ).getByRole("button", { name: "Open menu" });

    openRowActions("automatic-cause");
    fireEvent.click(screen.getByRole("menuitem", { name: "Disable key" }));

    expect(activeTrigger.getAttribute("aria-busy")).toBe("true");
    expect(activeTrigger.textContent).toContain("Action in progress");
    expect((activeTrigger as HTMLButtonElement).disabled).toBe(true);
    expect((oppositeTrigger as HTMLButtonElement).disabled).toBe(true);
    expect(mocks.enableMutate).not.toHaveBeenCalled();

    act(() => mocks.disableOptions?.onError(new Error("disable failed")));
    expect(activeTrigger.getAttribute("aria-busy")).toBe("false");
    expect((activeTrigger as HTMLButtonElement).disabled).toBe(false);
    expect((oppositeTrigger as HTMLButtonElement).disabled).toBe(false);
    expect(document.activeElement).toBe(activeTrigger);
  });

  it("uses generated cause-specific mutations and preserves refetch and errors", () => {
    const queryClient = renderPage();
    openRowActions("automatic-cause");
    fireEvent.click(screen.getByRole("menuitem", { name: "Disable key" }));
    expect(mocks.disableMutate).toHaveBeenCalledWith({
      request: {
        disableOpenRouterKeyRequestBody: {
          organizationId: "automatic-id",
          keyType: "chat",
        },
      },
    });

    act(() =>
      mocks.disableOptions?.onSuccess({
        keyType: "chat",
        organizationName: "Automatic cause",
      }),
    );
    expect(mocks.toastSuccess).toHaveBeenCalledWith(
      "Admin lock added to the chat key for Automatic cause.",
    );
    expect(mocks.invalidate).toHaveBeenCalledWith(queryClient);

    openRowActions("combined-causes");
    fireEvent.click(
      screen.getByRole("menuitem", { name: "Remove admin lock" }),
    );
    expect(mocks.enableMutate).toHaveBeenCalledWith({
      request: {
        enableOpenRouterKeyRequestBody: {
          organizationId: "combined-id",
          keyType: "internal",
        },
      },
    });

    act(() =>
      mocks.enableOptions?.onSuccess({
        keyType: "internal",
        organizationName: "Combined causes",
      }),
    );
    expect(mocks.toastSuccess).toHaveBeenCalledWith(
      "Admin lock removed from the internal key for Combined causes.",
    );
    expect(mocks.invalidate).toHaveBeenCalledTimes(2);

    openRowActions("combined-causes");
    fireEvent.click(
      screen.getByRole("menuitem", { name: "Remove admin lock" }),
    );
    act(() => mocks.enableOptions?.onError(new Error("remove failed")));
    expect(mocks.toastError).toHaveBeenCalledWith("remove failed");

    openRowActions("combined-causes");
    fireEvent.click(
      screen.getByRole("menuitem", { name: "Remove admin lock" }),
    );
    act(() => mocks.enableOptions?.onError("non-error rejection"));
    expect(mocks.toastError).toHaveBeenLastCalledWith(
      "Failed to remove admin lock",
    );
  });
});
