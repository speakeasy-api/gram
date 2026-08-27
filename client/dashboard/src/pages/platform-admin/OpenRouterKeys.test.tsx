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
};

const mocks = vi.hoisted(() => ({
  disableMutate: vi.fn(),
  disableOptions: undefined as MutationOptions | undefined,
  enableMutate: vi.fn(),
  enableOptions: undefined as MutationOptions | undefined,
  invalidate: vi.fn(),
  toastSuccess: vi.fn(),
}));

vi.mock("@/contexts/Auth", () => ({
  useIsPlatformAdmin: () => true,
}));

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
        <button key={action.label} onClick={action.onClick}>
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
  toast: { success: mocks.toastSuccess, error: vi.fn() },
}));

import PlatformAdminOpenRouterKeys from "./OpenRouterKeys";

describe("PlatformAdminOpenRouterKeys", () => {
  afterEach(cleanup);

  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("shows combined causes and never offers removing an automatic cause", () => {
    render(
      <QueryClientProvider client={new QueryClient()}>
        <PlatformAdminOpenRouterKeys />
      </QueryClientProvider>,
    );

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
  });

  it("uses the generated admin mutations for the eligible action", () => {
    const queryClient = new QueryClient();
    render(
      <QueryClientProvider client={queryClient}>
        <PlatformAdminOpenRouterKeys />
      </QueryClientProvider>,
    );

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

    const automaticKey = {
      keyType: "chat",
      organizationName: "Automatic cause",
    };
    mocks.disableOptions?.onSuccess(automaticKey);
    expect(mocks.toastSuccess).toHaveBeenCalledWith(
      "Admin lock added to the chat key for Automatic cause.",
    );
    expect(mocks.invalidate).toHaveBeenCalledWith(queryClient);

    const combinedKey = {
      keyType: "internal",
      organizationName: "Combined causes",
    };
    mocks.enableOptions?.onSuccess(combinedKey);
    expect(mocks.toastSuccess).toHaveBeenCalledWith(
      "Admin lock removed from the internal key for Combined causes.",
    );
    expect(mocks.invalidate).toHaveBeenCalledTimes(2);
    expect(mocks.invalidate).toHaveBeenLastCalledWith(queryClient);
  });
});
