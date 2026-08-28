import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  within,
} from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { Action } from "@/components/ui/MoreActions";
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
  enableMutate: vi.fn(),
  enableOptions: undefined as MutationOptions | undefined,
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
vi.mock("@/components/ui/MoreActions", () => ({
  MoreActions: ({ actions }: { actions: Action[] }) => (
    <div>
      {actions.map((action) => (
        <button
          key={action.label}
          disabled={action.disabled}
          onClick={action.onClick}
        >
          {action.label}
        </button>
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
    return { mutate: mocks.disableMutate, isPending: false };
  },
}));
vi.mock("@gram/client/react-query/enableAdminOpenRouterKey.js", () => ({
  useEnableAdminOpenRouterKeyMutation: (options: MutationOptions) => {
    mocks.enableOptions = options;
    return { mutate: mocks.enableMutate, isPending: false };
  },
}));
vi.mock("sonner", () => ({
  toast: { success: mocks.toastSuccess, error: mocks.toastError },
}));

import PlatformAdminOpenRouterKeys from "./OpenRouterKeys";

describe("PlatformAdminOpenRouterKeys", () => {
  afterEach(cleanup);
  beforeEach(() => vi.clearAllMocks());

  function renderPage(): QueryClient {
    const queryClient = new QueryClient();
    render(
      <QueryClientProvider client={queryClient}>
        <PlatformAdminOpenRouterKeys />
      </QueryClientProvider>,
    );
    return queryClient;
  }

  it("shows all causes and offers only the admin-lock action", () => {
    renderPage();

    const automatic = within(screen.getByTestId("automatic-cause"));
    expect(automatic.getByText("Trial demotion")).toBeDefined();
    expect(
      automatic.getByRole("button", { name: "Disable key" }),
    ).toBeDefined();
    expect(
      automatic.queryByRole("button", { name: "Remove admin lock" }),
    ).toBeNull();

    const combined = within(screen.getByTestId("combined-causes"));
    expect(combined.getByText("Admin lock")).toBeDefined();
    expect(combined.getByText("Billing inactive")).toBeDefined();
    expect(
      combined.getByRole("button", { name: "Remove admin lock" }),
    ).toBeDefined();
    expect(combined.queryByRole("button", { name: "Disable key" })).toBeNull();

    const future = within(screen.getByTestId("future-cause"));
    expect(future.getByText("future_cause")).toBeDefined();
    expect(future.getByText("Disabled")).toBeDefined();
    expect(future.getByRole("button", { name: "Disable key" })).toBeDefined();

    const legacy = within(screen.getByTestId("legacy-state"));
    expect(legacy.getByText("Disabled")).toBeDefined();
    expect(legacy.getByRole("button", { name: "Disable key" })).toBeDefined();
  });

  it("uses generated cause-specific mutations and preserves refetch and errors", () => {
    const queryClient = renderPage();
    fireEvent.click(
      within(screen.getByTestId("automatic-cause")).getByRole("button", {
        name: "Disable key",
      }),
    );
    expect(mocks.disableMutate).toHaveBeenCalledWith({
      request: {
        disableOpenRouterKeyRequestBody: {
          organizationId: "automatic-id",
          keyType: "chat",
        },
      },
    });

    fireEvent.click(
      within(screen.getByTestId("combined-causes")).getByRole("button", {
        name: "Remove admin lock",
      }),
    );
    expect(mocks.enableMutate).toHaveBeenCalledWith({
      request: {
        enableOpenRouterKeyRequestBody: {
          organizationId: "combined-id",
          keyType: "internal",
        },
      },
    });

    mocks.disableOptions?.onSuccess({
      keyType: "chat",
      organizationName: "Automatic cause",
    });
    expect(mocks.toastSuccess).toHaveBeenCalledWith(
      "Admin lock added to the chat key for Automatic cause.",
    );
    expect(mocks.invalidate).toHaveBeenCalledWith(queryClient);

    mocks.enableOptions?.onSuccess({
      keyType: "internal",
      organizationName: "Combined causes",
    });
    expect(mocks.toastSuccess).toHaveBeenCalledWith(
      "Admin lock removed from the internal key for Combined causes.",
    );
    expect(mocks.invalidate).toHaveBeenCalledTimes(2);

    mocks.enableOptions?.onError(new Error("remove failed"));
    expect(mocks.toastError).toHaveBeenCalledWith("remove failed");
  });
});
