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
  target: Pick<AiScanTarget, "id" | "enabled">;
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
    enabled: true,
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
    enabled: true,
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
    enabled: false,
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
vi.mock("@gram/client/react-query/deviceAgentAiScanTargets.js", () => ({
  invalidateAllDeviceAgentAiScanTargets: mocks.invalidateList,
  useDeviceAgentAiScanTargets: () => ({
    data: { listVersion: 11, etag: "etag", targets },
    isLoading: false,
    error: null,
  }),
}));
vi.mock("@gram/client/react-query/upsertDeviceAgentAiScanTarget.js", () => ({
  useUpsertDeviceAgentAiScanTargetMutation: (
    options: MutationOptions<MutationResult>,
  ) => {
    mocks.upsertOptions.push(options);
    return { mutate: mocks.upsertMutate, isPending: false };
  },
}));
vi.mock("@gram/client/react-query/deleteDeviceAgentAiScanTarget.js", () => ({
  useDeleteDeviceAgentAiScanTargetMutation: () => ({
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

  it("lists defaults and custom targets with their source and status", () => {
    renderPage();

    const aider = within(screen.getByTestId("aider"));
    expect(aider.getByText("Aider")).toBeDefined();
    expect(aider.getByText("Speakeasy default")).toBeDefined();
    expect(aider.getByText("Served")).toBeDefined();
    expect(
      aider.getByText("1 binary · 1 config dir · 1 process name"),
    ).toBeDefined();

    const classic = within(screen.getByTestId("chatgpt-classic"));
    expect(classic.getByText("Speakeasy default")).toBeDefined();
    expect(classic.getByText("Disabled")).toBeDefined();

    const acme = within(screen.getByTestId("acme-tool"));
    expect(acme.getByText("Custom")).toBeDefined();

    expect(screen.getByText(/List version 11/)).toBeDefined();
  });

  it("switches a default off by customizing it under its own id", () => {
    renderPage();

    openRowActions("aider");
    expect(screen.queryByRole("menuitem", { name: "Edit" })).toBeNull();
    expect(screen.queryByRole("menuitem", { name: "Delete" })).toBeNull();
    fireEvent.click(screen.getByRole("menuitem", { name: "Disable" }));
    expect(mocks.upsertMutate).toHaveBeenCalledWith({
      request: {
        upsertAiScanTargetRequestBody: {
          id: "aider",
          displayName: "Aider",
          category: "harness",
          signatures: {
            bundleIds: [],
            binaries: ["aider"],
            configDirs: ["~/.aider"],
            processNames: ["aider"],
          },
          versionPlistKey: undefined,
          enabled: false,
        },
      },
    });
  });

  it("switches a customized default back on by dropping the customization", () => {
    renderPage();

    openRowActions("chatgpt-classic");
    fireEvent.click(screen.getByRole("menuitem", { name: "Enable" }));
    expect(mocks.deleteMutate).toHaveBeenCalledWith({
      request: { deleteAiScanTargetRequestBody: { id: "chatgpt-classic" } },
    });
    expect(mocks.upsertMutate).not.toHaveBeenCalled();
  });

  it("toggles a custom target in place", () => {
    renderPage();

    openRowActions("acme-tool");
    fireEvent.click(screen.getByRole("menuitem", { name: "Disable" }));
    expect(mocks.upsertMutate).toHaveBeenCalledWith({
      request: {
        upsertAiScanTargetRequestBody: expect.objectContaining({
          id: "acme-tool",
          enabled: false,
        }),
      },
    });
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
          enabled: true,
        },
      },
    });

    // The editor's save mutation is the first one the section creates.
    await act(async () => {
      await mocks.upsertOptions[0]?.onSuccess({
        listVersion: 12,
        target: { id: "chatgpt-desktop", enabled: true },
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
