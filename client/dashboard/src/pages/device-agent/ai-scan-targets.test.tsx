import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
  within,
} from "@testing-library/react";
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
  target: Pick<AiScanTarget, "id">;
};

const mocks = vi.hoisted(() => ({
  upsertMutate: vi.fn(),
  upsertOptions: [] as MutationOptions<MutationResult>[],
  deleteMutate: vi.fn(),
  invalidateList: vi.fn(),
  toastError: vi.fn(),
  toastSuccess: vi.fn(),
}));

const targets: AiScanTarget[] = [
  {
    id: "acme-tool",
    displayName: "Acme Tool",
    category: "harness",
    signatures: {
      bundleIds: [],
      binaries: ["acme"],
      configDirs: [],
      processNames: [],
    },
    gatewayClient: {
      cimdVendorKeys: [],
      clientInfoNames: [],
      oauthClientIds: [],
    },
    origin: "organization",
    customized: false,
    createdAt: new Date("2026-09-09T00:00:00Z"),
    updatedAt: new Date("2026-09-09T00:00:00Z"),
  },
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
    gatewayClient: {
      cimdVendorKeys: [],
      clientInfoNames: [],
      oauthClientIds: [],
    },
    origin: "default",
    customized: false,
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
    gatewayClient: {
      cimdVendorKeys: [],
      clientInfoNames: [],
      oauthClientIds: [],
    },
    origin: "default",
    customized: true,
    createdAt: new Date("2026-09-08T00:00:00Z"),
    updatedAt: new Date("2026-09-08T00:00:00Z"),
  },
];

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
vi.mock("@gram/client/react-query/aiScanTargets.js", () => ({
  invalidateAllAiScanTargets: mocks.invalidateList,
  useAiScanTargets: () => ({
    data: { listVersion: 11, etag: "etag", targets },
    isLoading: false,
    error: null,
  }),
}));
vi.mock("@gram/client/react-query/upsertAiScanTarget.js", () => ({
  useUpsertAiScanTargetMutation: (options: MutationOptions<MutationResult>) => {
    mocks.upsertOptions.push(options);
    return { mutate: mocks.upsertMutate, isPending: false };
  },
}));
vi.mock("@gram/client/react-query/deleteAiScanTarget.js", () => ({
  useDeleteAiScanTargetMutation: () => ({
    mutate: mocks.deleteMutate,
    isPending: false,
  }),
}));
vi.mock("sonner", () => ({
  toast: { success: mocks.toastSuccess, error: mocks.toastError },
}));

import { AiScanTargetsSection } from "./ai-scan-targets";

describe("AiScanTargetsSection", () => {
  afterEach(cleanup);
  beforeEach(() => {
    vi.clearAllMocks();
    mocks.upsertOptions.length = 0;
  });

  function renderPage(): void {
    render(
      <QueryClientProvider client={new QueryClient()}>
        <AiScanTargetsSection />
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

  it("lists built-in and custom targets with their source", () => {
    renderPage();

    const aider = within(screen.getByTestId("aider"));
    expect(aider.getByText("Aider")).toBeDefined();
    expect(aider.getByText("Speakeasy default")).toBeDefined();
    expect(
      aider.getByText("1 binary · 1 config dir · 1 process name"),
    ).toBeDefined();

    const classic = within(screen.getByTestId("chatgpt-classic"));
    expect(classic.getByText("Speakeasy default")).toBeDefined();

    const acme = within(screen.getByTestId("acme-tool"));
    expect(acme.getByText("Custom")).toBeDefined();

    expect(screen.getByText(/List version 11/)).toBeDefined();
  });

  // A built-in is probed for as long as it is in Speakeasy's catalog, so an
  // organization has no per-row action on one: no menu at all, not an empty
  // one.
  it("offers no row actions on a built-in", () => {
    renderPage();

    for (const testId of ["aider", "chatgpt-classic"]) {
      expect(
        within(screen.getByTestId(testId)).queryByRole("button", {
          name: "Open menu",
        }),
      ).toBeNull();
    }
    expect(mocks.upsertMutate).not.toHaveBeenCalled();
    expect(mocks.deleteMutate).not.toHaveBeenCalled();
  });

  it("offers edit and delete on a target the organization added", () => {
    renderPage();

    openRowActions("acme-tool");
    expect(screen.getByRole("menuitem", { name: "Edit" })).toBeDefined();
    expect(screen.getByRole("menuitem", { name: "Delete" })).toBeDefined();
    expect(screen.queryByRole("menuitem", { name: "Disable" })).toBeNull();
    expect(screen.queryByRole("menuitem", { name: "Enable" })).toBeNull();
  });

  it("asks before deleting a custom target and then deletes", () => {
    renderPage();

    openRowActions("acme-tool");
    fireEvent.click(screen.getByRole("menuitem", { name: "Delete" }));
    expect(mocks.deleteMutate).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button", { name: "Delete target" }));
    expect(mocks.deleteMutate).toHaveBeenCalledWith({
      request: { deleteAiScanTargetRequestBody: { id: "acme-tool" } },
    });
  });

  it("validates the editor before submitting and reports the saved version", async () => {
    renderPage();

    fireEvent.click(screen.getByRole("button", { name: "Add target" }));
    fireEvent.change(screen.getByLabelText("Name"), {
      target: { value: "ChatGPT Desktop" },
    });
    expect(screen.getByText("chatgpt-desktop")).toBeDefined();
    fireEvent.submit(
      screen.getByRole("button", { name: "Add target" }).closest("form")!,
    );
    expect(mocks.upsertMutate).not.toHaveBeenCalled();
    expect(screen.getByText(/at least one install signature/)).toBeDefined();

    fireEvent.change(screen.getByLabelText("Binaries"), {
      target: { value: "chatgpt" },
    });
    fireEvent.keyDown(screen.getByLabelText("Binaries"), { key: "," });
    expect(
      screen.getByRole("button", { name: "Remove chatgpt" }),
    ).toBeDefined();
    fireEvent.submit(
      screen.getByRole("button", { name: "Add target" }).closest("form")!,
    );
    expect(mocks.upsertMutate).toHaveBeenCalledWith({
      request: {
        upsertAiScanTargetRequestBody: {
          id: "chatgpt-desktop",
          displayName: "ChatGPT Desktop",
          category: "harness",
          signatures: {
            bundleIds: [],
            binaries: ["chatgpt"],
            configDirs: [],
            processNames: [],
          },
          versionPlistKey: undefined,
        },
      },
    });

    // The editor's save mutation is the first one the section creates.
    await act(async () => {
      await mocks.upsertOptions[0]?.onSuccess({
        listVersion: 12,
        target: { id: "chatgpt-desktop" },
      });
    });
    expect(mocks.toastSuccess).toHaveBeenCalledWith(
      expect.stringContaining("List version 12"),
    );
    expect(mocks.invalidateList).toHaveBeenCalled();
  });

  it("keeps the editor open and shows the server's reason on failure", () => {
    renderPage();

    fireEvent.click(screen.getByRole("button", { name: "Add target" }));
    act(() => {
      mocks.upsertOptions[0]?.onError(
        new Error('invalid ai scan target: target "x": no install signature'),
      );
    });
    expect(screen.getByRole("alert").textContent).toContain(
      "no install signature",
    );
    expect(mocks.toastError).not.toHaveBeenCalled();
  });
});
