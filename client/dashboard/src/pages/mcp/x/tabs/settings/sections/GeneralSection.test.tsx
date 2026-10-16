import { TooltipProvider } from "@/components/ui/Tooltip";
import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import type { RemoteMcpServer } from "@gram/client/models/components/remotemcpserver.js";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { GeneralSection } from "./GeneralSection";
import {
  SOURCE_DESTINATION_LOCK_REASON,
  SOURCE_DESTINATION_UNKNOWN_REASON,
} from "./sourceDestinationLock";
import type { UpstreamUrlDraft } from "./useUpstreamUrlDraft";

const mocks = vi.hoisted(() => ({
  saveBranding: vi.fn(async () => {}),
  updateServer: vi.fn(async (_: unknown) => ({ id: "server-1", slug: "s" })),
  saveUrl: vi.fn(async () => {}),
  brandingDirty: false,
  upstream: {} as Partial<UpstreamUrlDraft>,
}));

vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({
    hasScope: () => true,
    hasAnyScope: () => true,
    hasAllScopes: () => true,
    isLoading: false,
  }),
}));

vi.mock("@/components/mcp_install_page/useMcpMetadataForm", () => ({
  useMcpMetadataMetadataForm: () => ({
    brandingDirty: mocks.brandingDirty,
    isLoading: false,
    saveAsync: mocks.saveBranding,
    logoUploadHandlers: {
      renderFilePreview: () => null,
      onUpload: vi.fn(async () => {}),
    },
  }),
}));

vi.mock("@gram/client/react-query/getMcpMetadata.js", () => ({
  useGetMcpMetadata: () => ({
    data: { metadata: {} },
    isLoading: false,
    isError: false,
    isRefetchError: false,
    error: null,
  }),
}));

vi.mock("@gram/client/react-query/updateMcpServer.js", () => ({
  useUpdateMcpServerMutation: () => ({
    mutateAsync: mocks.updateServer,
    isPending: false,
    isError: false,
    error: null,
  }),
}));

vi.mock("@gram/client/react-query/getMcpServer.js", () => ({
  invalidateAllGetMcpServer: vi.fn(async () => {}),
}));
vi.mock("@gram/client/react-query/mcpServers.js", () => ({
  invalidateAllMcpServers: vi.fn(async () => {}),
}));

vi.mock("@/routes", () => ({
  useRoutes: () => ({ mcp: { x: { settings: { href: () => "/" } } } }),
}));

vi.mock("@/pages/sources/remote-mcp/VerifyRemoteMcpUrlButton", () => ({
  VerifyRemoteMcpUrlButton: () => null,
  VerifyRemoteMcpUrlAlert: () => null,
}));

vi.mock("./useUpstreamUrlDraft", () => ({
  useUpstreamUrlDraft: (): UpstreamUrlDraft => ({
    draft: "https://example.com/mcp",
    setDraft: vi.fn(() => {}),
    touch: vi.fn(() => {}),
    fieldError: null,
    dirty: false,
    invalid: false,
    pending: false,
    lockedReason: null,
    verify: { status: "idle" } as unknown as UpstreamUrlDraft["verify"],
    save: mocks.saveUrl,
    ...mocks.upstream,
  }),
}));

const server = {
  id: "server-1",
  name: "jamf",
  remoteMcpServerId: "remote-1",
  visibility: "private",
} as unknown as McpServer;

const remote = {
  id: "remote-1",
  projectId: "project-1",
  url: "https://example.com/mcp",
} as unknown as RemoteMcpServer;

function renderSection() {
  render(
    <MemoryRouter>
      <QueryClientProvider client={new QueryClient()}>
        <TooltipProvider>
          <GeneralSection mcpServer={server} remoteMcpServer={remote} />
        </TooltipProvider>
      </QueryClientProvider>
    </MemoryRouter>,
  );
}

function saveButton(): HTMLButtonElement {
  return screen.getByRole("button", { name: /save/i }) as HTMLButtonElement;
}

beforeEach(() => {
  mocks.saveBranding.mockClear();
  mocks.updateServer.mockClear();
  mocks.saveUrl.mockClear();
  mocks.brandingDirty = false;
  mocks.upstream = {};
});

afterEach(cleanup);

describe("GeneralSection with a locked remote URL", () => {
  it.each([
    ["linked", SOURCE_DESTINATION_LOCK_REASON],
    ["unconfirmed", SOURCE_DESTINATION_UNKNOWN_REASON],
  ])("does not start a combined save on a %s source", (_, reason) => {
    mocks.brandingDirty = true;
    mocks.upstream = {
      dirty: true,
      draft: "https://example.com/moved",
      lockedReason: reason,
    };
    renderSection();
    fireEvent.change(screen.getByLabelText("Display Name"), {
      target: { value: "renamed" },
    });

    expect(saveButton().disabled).toBe(true);
    fireEvent.click(saveButton());
    expect(mocks.saveBranding).not.toHaveBeenCalled();
    expect(mocks.saveUrl).not.toHaveBeenCalled();
    expect(mocks.updateServer).not.toHaveBeenCalled();
  });

  it("still saves a name-only edit", async () => {
    mocks.upstream = { lockedReason: SOURCE_DESTINATION_LOCK_REASON };
    renderSection();
    fireEvent.change(screen.getByLabelText("Display Name"), {
      target: { value: "renamed" },
    });

    expect(saveButton().disabled).toBe(false);
    fireEvent.click(saveButton());
    await waitFor(() => expect(mocks.updateServer).toHaveBeenCalledTimes(1));
    expect(mocks.saveUrl).not.toHaveBeenCalled();
  });

  it("saves a URL change when the source is unlocked", async () => {
    mocks.upstream = { dirty: true, draft: "https://example.com/moved" };
    renderSection();

    expect(saveButton().disabled).toBe(false);
    fireEvent.click(saveButton());
    await waitFor(() => expect(mocks.saveUrl).toHaveBeenCalledTimes(1));
  });
});
