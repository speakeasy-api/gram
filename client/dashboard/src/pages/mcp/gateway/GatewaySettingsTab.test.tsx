import type { MetaMcpServer } from "@gram/client/models/components/metamcpserver.js";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { GatewayInstructionsSection } from "./GatewaySettingsTab";

const state = vi.hoisted(() => ({
  hasScope: vi.fn(),
  requireScope: vi.fn(),
  mutate: vi.fn(),
  isPending: false,
  onSuccess: undefined as undefined | (() => Promise<void>),
  invalidateQueries: vi.fn().mockResolvedValue(undefined),
  invalidateGet: vi.fn().mockResolvedValue(undefined),
  invalidateList: vi.fn().mockResolvedValue(undefined),
}));
vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({ hasScope: state.hasScope }),
}));
vi.mock("@/components/require-scope", () => ({
  RequireScope: (props: {
    children: React.ReactNode;
    resourceId?: string;
    scope: string;
  }) => {
    state.requireScope(props);
    return props.children;
  },
}));
vi.mock("@/routes", () => ({ useRoutes: vi.fn() }));
vi.mock(
  "@/pages/mcp/x/tabs/settings/sections/authentication/AuthenticationSection",
  () => ({ AuthenticationSectionBody: vi.fn() }),
);
vi.mock(
  "@/pages/mcp/x/tabs/settings/sections/authentication/authTarget",
  () => ({ useMetaMcpAuthTarget: vi.fn() }),
);
vi.mock("@/pages/mcp/x/tabs/settings/sections/ServerUrlSection", () => ({
  MCP_SERVER_URL_SECTION_ID: "server-url",
  ServerUrlSection: vi.fn(),
}));
vi.mock("@tanstack/react-query", () => ({
  useQueryClient: () => ({ invalidateQueries: state.invalidateQueries }),
}));
vi.mock("@gram/client/react-query/updateMetaMcpServer.js", () => ({
  useUpdateMetaMcpServerMutation: ({
    onSuccess,
  }: {
    onSuccess: () => Promise<void>;
  }) => {
    state.onSuccess = onSuccess;
    return { mutate: state.mutate, isPending: state.isPending, isError: false };
  },
}));
vi.mock("@gram/client/react-query/getMetaMcpServer.js", () => ({
  invalidateAllGetMetaMcpServer: state.invalidateGet,
}));
vi.mock("@gram/client/react-query/metaMcpServers.js", () => ({
  invalidateAllMetaMcpServers: state.invalidateList,
}));

const server = {
  id: "gateway-test",
  projectId: "project-test",
  name: "Gateway",
  instructions: "Original",
} as MetaMcpServer;
const renderSection = (instructions = server.instructions) =>
  render(
    <GatewayInstructionsSection metaMcpServer={{ ...server, instructions }} />,
  );
const textarea = () =>
  screen.getByLabelText("Custom instructions") as HTMLTextAreaElement;
const save = () =>
  screen.getByRole("button", { name: /sav/i }) as HTMLButtonElement;

afterEach(cleanup);
beforeEach(() => {
  vi.clearAllMocks();
  state.hasScope.mockReturnValue(true);
  state.isPending = false;
});

describe("Gateway instructions", () => {
  it("checks both edit and save permissions against the gateway project", () => {
    state.hasScope.mockReturnValue(false);
    renderSection();
    expect(state.hasScope).toHaveBeenCalledWith("mcp:write", server.projectId);
    expect(state.requireScope).toHaveBeenCalledWith(
      expect.objectContaining({
        scope: "mcp:write",
        resourceId: server.projectId,
      }),
    );
    expect(textarea().disabled).toBe(true);
    fireEvent.change(textarea(), { target: { value: "Changed" } });
    expect(save().disabled).toBe(true);
  });

  it("counts Unicode code points like the server at the length boundary", () => {
    renderSection();
    fireEvent.change(textarea(), {
      target: { value: `  ${"😀".repeat(10000)}  ` },
    });
    expect(screen.getByText("10,000 / 10,000 characters.")).toBeTruthy();
    expect(save().disabled).toBe(false);
    fireEvent.click(save());
    expect(state.mutate).toHaveBeenCalledWith(
      expect.objectContaining({
        request: expect.objectContaining({
          updateMetaMcpServerForm: expect.objectContaining({
            instructions: "😀".repeat(10000),
          }),
        }),
      }),
    );
    fireEvent.change(textarea(), { target: { value: "😀".repeat(10001) } });
    expect(save().disabled).toBe(true);
    expect(
      screen.getByText("Instructions must be 10,000 characters or fewer."),
    ).toBeTruthy();
  });

  it("sends an empty string to restore defaults and preserves other update fields", () => {
    renderSection();
    fireEvent.change(textarea(), { target: { value: " \n " } });
    fireEvent.click(save());
    expect(state.mutate).toHaveBeenCalledWith({
      request: {
        updateMetaMcpServerForm: {
          id: server.id,
          name: server.name,
          userSessionIssuerId: undefined,
          instructions: "",
          instructionsMode: "append",
        },
      },
    });
  });

  it("switching the mode alone makes the section dirty and is sent on save", () => {
    renderSection();
    expect(save().disabled).toBe(true);
    fireEvent.click(
      screen.getAllByRole("radio", {
        name: /Replace the built-in instructions/i,
      })[0]!,
    );
    expect(save().disabled).toBe(false);
    fireEvent.click(save());
    expect(state.mutate).toHaveBeenCalledWith(
      expect.objectContaining({
        request: expect.objectContaining({
          updateMetaMcpServerForm: expect.objectContaining({
            instructions: "Original",
            instructionsMode: "replace",
          }),
        }),
      }),
    );
  });

  describe("copy default instructions", () => {
    const original = Object.getOwnPropertyDescriptor(navigator, "clipboard");
    const stubClipboard = (writeText: (text: string) => Promise<void>) => {
      Object.defineProperty(navigator, "clipboard", {
        value: { writeText },
        configurable: true,
      });
    };
    afterEach(() => {
      if (original) Object.defineProperty(navigator, "clipboard", original);
      else Reflect.deleteProperty(navigator, "clipboard");
    });

    it("copies the built-in text and confirms", async () => {
      const writeText = vi.fn().mockResolvedValue(undefined);
      stubClipboard(writeText);
      renderSection();
      fireEvent.click(
        screen.getByRole("button", { name: /Copy default instructions/i }),
      );
      expect(writeText).toHaveBeenCalledTimes(1);
      expect(writeText.mock.calls[0]![0]).toContain("Work from the outside in");
      expect(
        await screen.findByRole("button", { name: /Copied/i }),
      ).toBeTruthy();
    });

    it("does not claim success when the clipboard write fails", async () => {
      stubClipboard(vi.fn().mockRejectedValue(new Error("denied")));
      renderSection();
      fireEvent.click(
        screen.getByRole("button", { name: /Copy default instructions/i }),
      );
      await new Promise((resolve) => {
        setTimeout(resolve, 0);
      });
      expect(screen.queryByRole("button", { name: /Copied/i })).toBeNull();
      expect(
        screen.getByRole("button", { name: /Copy default instructions/i }),
      ).toBeTruthy();
    });
  });

  it("does not permit edits that would be overwritten by an in-flight save", () => {
    state.isPending = true;
    renderSection();
    expect(textarea().disabled).toBe(true);
    expect(save().disabled).toBe(true);
  });

  it("invalidates live inspection after saving so the next connection shows new instructions", async () => {
    renderSection();
    await state.onSuccess!();
    expect(state.invalidateGet).toHaveBeenCalled();
    expect(state.invalidateList).toHaveBeenCalled();
    expect(state.invalidateQueries).toHaveBeenCalledWith({
      queryKey: ["gatewayInspection"],
    });
  });

  it("starts pristine with defaults and resets the draft when navigating gateways", () => {
    const view = render(
      <GatewayInstructionsSection
        metaMcpServer={{ ...server, instructions: undefined }}
      />,
    );
    expect(textarea().value).toBe("");
    expect(save().disabled).toBe(true);
    fireEvent.change(textarea(), { target: { value: "Unsaved" } });
    view.rerender(
      <GatewayInstructionsSection
        metaMcpServer={{
          ...server,
          id: "other-gateway",
          instructions: "Other",
        }}
      />,
    );
    expect(textarea().value).toBe("Other");
    expect(save().disabled).toBe(true);
  });
});
