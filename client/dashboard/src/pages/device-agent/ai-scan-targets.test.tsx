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
    gatewayClient: {
      cimdVendorKeys: [],
      oauthClientIds: [],
      clientInfoNames: [],
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
    gatewayClient: {
      cimdVendorKeys: [],
      oauthClientIds: [],
      clientInfoNames: [],
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
    gatewayClient: {
      cimdVendorKeys: [],
      oauthClientIds: [],
      clientInfoNames: [],
    },
    enabled: false,
    origin: "default",
    customized: true,
    createdAt: new Date("2026-09-08T00:00:00Z"),
    updatedAt: new Date("2026-09-08T00:00:00Z"),
  },
  {
    id: "openclaw",
    displayName: "OpenClaw",
    category: "assistant",
    signatures: {
      bundleIds: [],
      binaries: ["openclaw"],
      configDirs: ["~/.openclaw"],
      processNames: ["openclaw"],
    },
    gatewayClient: {
      cimdVendorKeys: [],
      oauthClientIds: [],
      clientInfoNames: ["openclaw"],
    },
    enabled: true,
    origin: "default",
    customized: false,
  },
  {
    id: "ollama",
    displayName: "Ollama",
    category: "local_model",
    signatures: {
      bundleIds: [],
      binaries: ["ollama"],
      configDirs: ["~/.ollama"],
      processNames: ["ollama"],
    },
    gatewayClient: {
      cimdVendorKeys: [],
      oauthClientIds: [],
      clientInfoNames: [],
    },
    enabled: true,
    origin: "default",
    customized: false,
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
// The CIMD documents field probes a URL through the verify endpoint; the
// editor tests here are about the draft, not the probe.
vi.mock(
  "@gram/client/react-query/verifyUserSessionIssuerCimdClientURL.js",
  () => ({
    useVerifyUserSessionIssuerCimdClientURLMutation: () => ({
      mutate: vi.fn(),
      isPending: false,
    }),
  }),
);

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

  // The <div> wrapping one origin half, found from its eyebrow heading. Walks
  // up rather than taking a fixed number of parents so the helper survives the
  // heading gaining a sibling — which is exactly what the version badge did.
  function originSection(title: string): HTMLElement {
    let node: HTMLElement | null = screen.getByText(title);
    while (node && node.parentElement) {
      node = node.parentElement;
      if (within(node).queryAllByRole("button").length > 0) return node;
    }
    throw new Error(`no section found for ${title}`);
  }

  // Built-in kinds start collapsed, so a test that reaches a built-in row has
  // to open its section first. Custom is already open.
  function expandBuiltin(kind: string): void {
    fireEvent.click(within(originSection("Built-in")).getByText(kind));
  }

  function openRowActions(testId: string): void {
    fireEvent.pointerDown(
      within(screen.getByTestId(testId)).getByRole("button", {
        name: "Open menu",
      }),
      { button: 0, ctrlKey: false },
    );
  }

  it("splits custom from built-in and groups each by kind", () => {
    renderPage();

    // Custom is open on arrival and built-in is not: the organization's own
    // targets are the short list somebody came here to work on.
    expect(screen.queryByTestId("aider")).toBeNull();
    expect(screen.getByTestId("acme-tool")).toBeDefined();
    expandBuiltin("Harness");

    // The name alone identifies a row — no id subtitle, no signature summary,
    // and no origin badge: which half of the page a row is on says that now.
    const aider = within(screen.getByTestId("aider"));
    expect(aider.getByText("Aider")).toBeDefined();
    expect(aider.getByText("Served")).toBeDefined();
    expect(aider.queryByText("aider")).toBeNull();
    expect(aider.queryByText("Built-in")).toBeNull();

    const classic = within(screen.getByTestId("chatgpt-classic"));
    expect(classic.getByText("Disabled")).toBeDefined();

    const acme = within(screen.getByTestId("acme-tool"));
    expect(acme.queryByText("Custom")).toBeNull();

    // Two halves, each with a collapsible row per kind it holds. Harness
    // appears on both sides; the kinds only one side has appear once.
    expect(screen.getByText("Custom")).toBeDefined();
    expect(screen.getByText("Built-in")).toBeDefined();
    expect(screen.getAllByText("Harness")).toHaveLength(2);
    expect(screen.getAllByText("Assistants")).toHaveLength(1);
    expect(screen.getAllByText("Open models")).toHaveLength(1);
    expect(screen.getByText(/Library version 11/)).toBeDefined();
  });

  // A kind nobody has a target for on one side is not worth a header there.
  it("omits a kind that has nothing in it on that side", () => {
    renderPage();

    const custom = originSection("Custom");
    expect(within(custom).queryByText("Open models")).toBeNull();
    expect(within(custom).queryByText("Assistants")).toBeNull();
  });

  it("switches a default off by customizing it under its own id", () => {
    renderPage();

    expandBuiltin("Harness");
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
          gatewayClient: {
            cimdVendorKeys: [],
            oauthClientIds: [],
            clientInfoNames: [],
          },
          versionPlistKey: undefined,
          enabled: false,
        },
      },
    });
  });

  it("switches a customized default back on by dropping the customization", () => {
    renderPage();

    expandBuiltin("Harness");
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
          gatewayClient: {
            cimdVendorKeys: [],
            oauthClientIds: [],
            clientInfoNames: [],
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
