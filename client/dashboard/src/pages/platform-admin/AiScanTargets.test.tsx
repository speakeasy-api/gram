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
import type { AiScanTarget } from "@gram/client/models/components/aiscantarget.js";

type MutationOptions<T> = {
  onSuccess: (result: T) => void | Promise<void>;
  onError: (error: unknown) => void;
  onSettled?: () => void;
};

type MutationResult = {
  listVersion: number;
  target: Pick<AiScanTarget, "id" | "enabled">;
};

const mocks = vi.hoisted(() => ({
  upsertMutate: vi.fn(),
  upsertOptions: undefined as MutationOptions<MutationResult> | undefined,
  setEnabledMutate: vi.fn(),
  setEnabledOptions: undefined as MutationOptions<MutationResult> | undefined,
  deleteMutate: vi.fn(),
  deleteOptions: undefined as MutationOptions<unknown> | undefined,
  invalidateList: vi.fn(),
  invalidateRevisions: vi.fn(),
  toastError: vi.fn(),
  toastSuccess: vi.fn(),
  isPlatformAdmin: true,
}));

const targets: AiScanTarget[] = [
  {
    id: "aider",
    displayName: "Aider",
    category: "harness",
    signatures: {
      bundleIds: [],
      binaries: ["aider"],
      configDirs: ["~/.aider"],
      processNames: ["aider"],
    },
    enabled: true,
    createdAt: new Date("2026-09-01T00:00:00Z"),
    updatedAt: new Date("2026-09-01T00:00:00Z"),
  },
  {
    id: "chatgpt-classic",
    displayName: "ChatGPT Classic",
    category: "harness",
    signatures: {
      bundleIds: ["com.openai.chat"],
      binaries: [],
      configDirs: [],
      processNames: [],
    },
    enabled: false,
    createdAt: new Date("2026-09-08T00:00:00Z"),
    updatedAt: new Date("2026-09-08T00:00:00Z"),
  },
];

vi.mock("@/contexts/Auth", () => ({
  useIsPlatformAdmin: () => mocks.isPlatformAdmin,
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
    rowKey,
  }: {
    columns: Column<AiScanTarget>[];
    data: AiScanTarget[];
    rowKey: (row: AiScanTarget) => string;
  }) => (
    <div>
      {data.map((row) => (
        <div key={rowKey(row)} data-testid={rowKey(row)}>
          {columns.map((column) => (
            <div key={String(column.key)}>{column.render?.(row)}</div>
          ))}
        </div>
      ))}
    </div>
  ),
}));
vi.mock("@gram/client/react-query/platformAiScanTargetsList.js", () => ({
  invalidateAllPlatformAiScanTargetsList: mocks.invalidateList,
  usePlatformAiScanTargetsList: () => ({
    data: { listVersion: 11, etag: "etag", targets },
    isLoading: false,
    error: null,
  }),
}));
vi.mock(
  "@gram/client/react-query/platformAiScanTargetsListRevisions.js",
  () => ({
    invalidateAllPlatformAiScanTargetsListRevisions: mocks.invalidateRevisions,
    usePlatformAiScanTargetsListRevisions: () => ({
      data: {
        revisions: [
          {
            revision: 11,
            targetId: "chatgpt-classic",
            action: "upsert",
            actorEmail: "admin@example.com",
            reason: "customer ask",
            createdAt: new Date("2026-09-08T00:00:00Z"),
          },
        ],
      },
      isLoading: false,
      error: null,
    }),
  }),
);
vi.mock("@gram/client/react-query/platformAiScanTargetsUpsert.js", () => ({
  usePlatformAiScanTargetsUpsertMutation: (
    options: MutationOptions<MutationResult>,
  ) => {
    mocks.upsertOptions = options;
    return { mutate: mocks.upsertMutate, isPending: false };
  },
}));
vi.mock("@gram/client/react-query/platformAiScanTargetsSetEnabled.js", () => ({
  usePlatformAiScanTargetsSetEnabledMutation: (
    options: MutationOptions<MutationResult>,
  ) => {
    mocks.setEnabledOptions = options;
    return { mutate: mocks.setEnabledMutate, isPending: false };
  },
}));
vi.mock("@gram/client/react-query/platformAiScanTargetsDelete.js", () => ({
  usePlatformAiScanTargetsDeleteMutation: (
    options: MutationOptions<unknown>,
  ) => {
    mocks.deleteOptions = options;
    return { mutate: mocks.deleteMutate, isPending: false };
  },
}));
vi.mock("sonner", () => ({
  toast: { success: mocks.toastSuccess, error: mocks.toastError },
}));

import PlatformAdminAiScanTargets from "./AiScanTargets";

describe("PlatformAdminAiScanTargets", () => {
  afterEach(cleanup);
  beforeEach(() => {
    vi.clearAllMocks();
    mocks.isPlatformAdmin = true;
  });

  function renderPage(): void {
    render(
      <QueryClientProvider client={new QueryClient()}>
        <PlatformAdminAiScanTargets />
      </QueryClientProvider>,
    );
  }

  function openRowActions(testId: string): void {
    fireEvent.pointerDown(
      within(screen.getByTestId(testId)).getByRole("button", {
        name: "Open menu",
      }),
      { button: 0, ctrlKey: false },
    );
  }

  it("refuses non-platform-admins before fetching anything", () => {
    mocks.isPlatformAdmin = false;
    renderPage();
    expect(
      screen.getByText("This page is available to platform admins only."),
    ).toBeDefined();
    expect(screen.queryByText("Aider")).toBeNull();
  });

  it("lists the catalog with its serving status and revision", () => {
    renderPage();

    const aider = within(screen.getByTestId("aider"));
    expect(aider.getByText("Aider")).toBeDefined();
    expect(aider.getByText("Served")).toBeDefined();
    expect(
      aider.getByText("1 binary · 1 config dir · 1 process name"),
    ).toBeDefined();

    const classic = within(screen.getByTestId("chatgpt-classic"));
    expect(classic.getByText("ChatGPT Classic")).toBeDefined();
    expect(classic.getByText("Disabled")).toBeDefined();
    expect(classic.getByText("1 bundle id")).toBeDefined();

    expect(screen.getByText(/Catalog revision 11/)).toBeDefined();
    expect(screen.getByText("admin@example.com")).toBeDefined();
  });

  it("toggles serving from the row menu", () => {
    renderPage();

    openRowActions("chatgpt-classic");
    fireEvent.click(screen.getByRole("menuitem", { name: "Enable" }));
    expect(mocks.setEnabledMutate).toHaveBeenCalledWith({
      request: {
        setEnabledRequestBody: { id: "chatgpt-classic", enabled: true },
      },
    });
  });

  it("asks before deleting and then deletes", () => {
    renderPage();

    openRowActions("aider");
    fireEvent.click(screen.getByRole("menuitem", { name: "Delete" }));
    expect(mocks.deleteMutate).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button", { name: "Delete target" }));
    expect(mocks.deleteMutate).toHaveBeenCalledWith({
      request: { deleteRequestBody2: { id: "aider" } },
    });
  });

  it("validates the editor before submitting and reports the saved revision", async () => {
    renderPage();

    fireEvent.click(screen.getByRole("button", { name: "Add target" }));
    fireEvent.change(screen.getByLabelText("Id"), {
      target: { value: "ChatGPT" },
    });
    fireEvent.change(screen.getByLabelText("Display name"), {
      target: { value: "ChatGPT" },
    });
    fireEvent.submit(
      screen.getByRole("button", { name: "Add target" }).closest("form")!,
    );
    expect(mocks.upsertMutate).not.toHaveBeenCalled();
    expect(screen.getByText(/Use lowercase letters/)).toBeDefined();

    fireEvent.change(screen.getByLabelText("Id"), {
      target: { value: "chatgpt" },
    });
    fireEvent.change(screen.getByLabelText("Bundle ids"), {
      target: { value: "com.openai.codex" },
    });
    fireEvent.submit(
      screen.getByRole("button", { name: "Add target" }).closest("form")!,
    );
    expect(mocks.upsertMutate).toHaveBeenCalledWith({
      request: {
        upsertRequestBody2: {
          id: "chatgpt",
          displayName: "ChatGPT",
          category: "harness",
          signatures: {
            bundleIds: ["com.openai.codex"],
            binaries: [],
            configDirs: [],
            processNames: [],
          },
          versionPlistKey: undefined,
          enabled: true,
          reason: undefined,
        },
      },
    });

    await act(async () => {
      await mocks.upsertOptions?.onSuccess({
        listVersion: 12,
        target: { id: "chatgpt", enabled: true },
      });
    });
    expect(mocks.toastSuccess).toHaveBeenCalledWith(
      expect.stringContaining("revision 12"),
    );
    expect(mocks.invalidateList).toHaveBeenCalled();
    expect(mocks.invalidateRevisions).toHaveBeenCalled();
  });

  it("keeps the editor open and shows the server's reason on failure", () => {
    renderPage();

    fireEvent.click(screen.getByRole("button", { name: "Add target" }));
    act(() => {
      mocks.upsertOptions?.onError(
        new Error('invalid ai scan target: target "x": no install signature'),
      );
    });
    expect(screen.getByRole("alert").textContent).toContain(
      "no install signature",
    );
    expect(mocks.toastError).not.toHaveBeenCalled();
  });
});
