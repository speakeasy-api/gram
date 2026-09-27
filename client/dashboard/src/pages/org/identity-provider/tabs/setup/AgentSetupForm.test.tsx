import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";

import { AgentSetupForm } from "./OktaConnectionForms";
import { AGENT_SECTION_ID } from "../../tabs";
import { SESSION_SECURITY } from "../../identityProviderQueries";
import { makeConnection } from "./testFixtures";

const mocks = vi.hoisted(() => ({
  mutate: vi.fn(),
  invalidate: vi.fn(),
  success: vi.fn(),
  queryClient: {},
  pending: false,
  error: undefined as unknown,
  options: {} as { onSuccess: () => void; onError: (error: unknown) => void },
}));
vi.mock("@tanstack/react-query", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@tanstack/react-query")>()),
  useQueryClient: () => mocks.queryClient,
}));
vi.mock("sonner", () => ({ toast: { success: mocks.success } }));
vi.mock("../../identityProviderQueries", () => ({
  SESSION_SECURITY: { session: "test-session" },
  inlineError: vi.fn(),
  invalidateIdentityProviderQueries: mocks.invalidate,
}));
vi.mock(
  "@gram/client/react-query/recordIdentityProviderConnectionAgent.js",
  () => ({
    useRecordIdentityProviderConnectionAgentMutation: (
      options: typeof mocks.options,
    ) => {
      mocks.options = options;
      return {
        mutate: mocks.mutate,
        isPending: mocks.pending,
        error: mocks.error,
      };
    },
  }),
);

const connection = makeConnection({ agentId: "", agentAppId: "" });
const agentInput = () => screen.getByLabelText("Agent ID");
const appInput = () => screen.getByLabelText("Bound application ID (optional)");
const change = (input: HTMLElement, value: string) =>
  fireEvent.change(input, { target: { value } });
const payload = (agentId: string, agentAppId: string) => ({
  security: SESSION_SECURITY,
  request: {
    recordIdentityProviderConnectionAgentRequestBody: {
      id: connection.id,
      agentId,
      agentAppId,
    },
  },
});

afterEach(cleanup);
beforeEach(() => {
  vi.clearAllMocks();
  mocks.pending = false;
  mocks.error = undefined;
});

describe("AgentSetupForm", () => {
  it("renders inline without a settings heading and guards unchanged saves", () => {
    render(<AgentSetupForm connection={connection} />);
    expect(screen.getByRole("region", { name: "Save Okta AI agent" }).id).toBe(
      AGENT_SECTION_ID,
    );
    expect(screen.queryByRole("heading")).toBeNull();
    expect(
      screen
        .getByRole("link", { name: "Okta AI agent registration guide" })
        .getAttribute("href"),
    ).toBe(
      "https://help.okta.com/oie/en-us/content/topics/ai-agents/ai-agent-add-manually.htm",
    );
    expect(
      screen.getByText(/drives the deep links on the Cross App Access tab/),
    ).toBeTruthy();
    expect(
      screen
        .getByRole("button", { name: "Save agent" })
        .hasAttribute("disabled"),
    ).toBe(true);
    change(agentInput(), "   ");
    fireEvent.keyDown(agentInput(), { key: "Enter" });
    expect(mocks.mutate).not.toHaveBeenCalled();
  });

  it("trims IDs, saves on click, and invalidates after success", () => {
    render(<AgentSetupForm connection={connection} />);
    change(agentInput(), " test-agent ");
    change(appInput(), " test-app ");
    fireEvent.click(screen.getByRole("button", { name: "Save agent" }));
    expect(mocks.mutate).toHaveBeenCalledWith(
      payload("test-agent", "test-app"),
    );
    mocks.options.onSuccess();
    expect(mocks.success).toHaveBeenCalledWith("Agent details saved");
    expect(mocks.invalidate).toHaveBeenCalledWith(mocks.queryClient);
  });

  it("allows the optional app to remain empty and saves with Enter", () => {
    render(<AgentSetupForm connection={connection} />);
    change(agentInput(), "test-agent");
    fireEvent.keyDown(agentInput(), { key: "Enter" });
    expect(mocks.mutate).toHaveBeenCalledWith(payload("test-agent", ""));
  });

  it("clears saved IDs by sending empty strings", () => {
    render(
      <AgentSetupForm
        connection={{
          ...connection,
          agentId: "old-agent",
          agentAppId: "old-app",
        }}
      />,
    );
    change(agentInput(), "");
    change(appInput(), "  ");
    fireEvent.click(screen.getByRole("button", { name: "Save agent" }));
    expect(mocks.mutate).toHaveBeenCalledWith(payload("", ""));
  });

  it("freezes both inputs and prevents duplicate submissions while pending", () => {
    const { rerender } = render(<AgentSetupForm connection={connection} />);
    change(agentInput(), "test-agent");
    mocks.pending = true;
    rerender(<AgentSetupForm connection={connection} />);
    expect(agentInput().hasAttribute("disabled")).toBe(true);
    expect(appInput().hasAttribute("disabled")).toBe(true);
    expect(
      screen
        .getByRole("button", { name: "Saving..." })
        .hasAttribute("disabled"),
    ).toBe(true);
    fireEvent.keyDown(agentInput(), { key: "Enter" });
    fireEvent.keyDown(appInput(), { key: "Enter" });
    expect(mocks.mutate).not.toHaveBeenCalled();
  });

  it("shows save errors inline without discarding the draft", () => {
    const { rerender } = render(<AgentSetupForm connection={connection} />);
    change(agentInput(), "test-agent");
    mocks.error = { statusCode: 412, message: "The connection is not ready" };
    rerender(<AgentSetupForm connection={connection} />);
    expect(screen.getByText("The connection is not ready")).toBeTruthy();
    expect((agentInput() as HTMLInputElement).value).toBe("test-agent");
    expect(
      screen
        .getByRole("button", { name: "Save agent" })
        .hasAttribute("disabled"),
    ).toBe(false);
  });
});
