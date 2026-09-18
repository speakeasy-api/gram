import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import CreateFromSource from "./CreateFromSource";
const api = vi.hoisted(() => ({
  create: vi.fn(),
  update: vi.fn(),
  wrapper: vi.fn(),
  complete: vi.fn(),
}));
vi.mock("@/pages/mcp/gateway/useGatewayCreation", () => ({
  useGatewayCreation: () => ({
    gatewayId: "gateway",
    createdServerId: null,
    isAttaching: false,
    attachmentError: null,
    complete: api.complete,
    cancel: vi.fn(),
  }),
}));
vi.mock("@/components/page-templates", () => ({
  FormPage: ({ children }: { children: React.ReactNode }) => (
    <div>{children}</div>
  ),
}));
vi.mock("@/routes", () => ({ useRoutes: () => ({}) }));
vi.mock("@/contexts/Sdk", () => ({
  useSdkClient: () => ({
    toolsets: { create: api.create, updateBySlug: api.update },
    mcpServers: { create: api.wrapper },
  }),
}));
vi.mock("@/components/side-panel/side-panel-context", () => ({
  useSidePanel: () => ({ openPanel: vi.fn() }),
}));
vi.mock("@/components/icon-confetti", () => ({
  useIconConfetti: () => ({ canvasRef: null, start: vi.fn(), stop: vi.fn() }),
}));
vi.mock("@/components/sources/source-list", () => ({
  useProjectSources: () => ({
    sources: [
      {
        key: "first",
        name: "First source",
        kind: "openapi",
        documentId: "first",
      },
      {
        key: "second",
        name: "Second source",
        kind: "openapi",
        documentId: "second",
      },
    ],
  }),
  sourceAssetId: () => "asset",
}));
vi.mock("@/hooks/toolTypes", () => ({
  useListTools: () => ({
    data: {
      tools: [
        { type: "http", openapiv3DocumentId: "first", toolUrn: "urn:first" },
        { type: "http", openapiv3DocumentId: "second", toolUrn: "urn:second" },
      ],
    },
  }),
}));
vi.mock("react-router", () => ({
  useSearchParams: () => [new URLSearchParams()],
}));
vi.mock("sonner", () => ({ toast: { success: vi.fn() } }));
beforeEach(() => {
  vi.resetAllMocks();
  api.create.mockResolvedValue({
    id: "toolset",
    slug: "toolset",
    name: "First source",
  });
  api.update
    .mockRejectedValueOnce(new Error("Seeding failed"))
    .mockResolvedValue(undefined);
  api.wrapper.mockResolvedValue({ id: "server" });
});
afterEach(cleanup);
it.each(["click", "Enter", " "])(
  "locks the retained source against %s activation during recovery",
  async (activation) => {
    render(<CreateFromSource />);
    const first = screen.getByRole("button", { name: /First source/ });
    const second = screen.getByRole("button", { name: /Second source/ });
    fireEvent.click(first);
    fireEvent.click(screen.getByRole("button", { name: "Create" }));
    await screen.findByText("Seeding failed");
    if (activation === "click") fireEvent.click(second);
    else fireEvent.keyDown(second, { key: activation });
    expect(first.querySelector('[aria-pressed="true"]')).not.toBeNull();
    expect(second.querySelector('[aria-pressed="false"]')).not.toBeNull();
    // Fieldset disabled does not remove div-based cards from keyboard/a11y interaction.
    expect(second.closest("[inert]")).not.toBeNull();
    expect(second.closest('[aria-disabled="true"]')).not.toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    await waitFor(() => expect(api.complete).toHaveBeenCalledWith("server"));
    expect(api.create).toHaveBeenCalledOnce();
    expect(api.update).toHaveBeenLastCalledWith({
      slug: "toolset",
      updateToolsetRequestBody: { toolUrns: ["urn:first"] },
    });
  },
);
