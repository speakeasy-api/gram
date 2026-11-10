import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import type { Assistant } from "@gram/client/models/components/assistant.js";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { AssistantRuntimeSettings } from "./AssistantRuntimeSettings";

const mocks = vi.hoisted(() => ({
  flag: "enabled",
  pending: false,
  mutate: vi.fn(),
}));
vi.mock("@/hooks/useFeatureFlag", () => ({
  useFeatureFlag: () => ({ status: mocks.flag }),
}));
vi.mock("@tanstack/react-query", () => ({ useQueryClient: () => ({}) }));
vi.mock("@gram/client/react-query/assistantsUpdate.js", () => ({
  useAssistantsUpdateMutation: () => ({
    mutate: mocks.mutate,
    isPending: mocks.pending,
  }),
}));
vi.mock("@gram/client/react-query/assistantsList.js", () => ({
  invalidateAllAssistantsList: vi.fn(),
}));
const assistant: Assistant = {
  id: "11111111-1111-4111-8111-111111111111",
  projectId: "22222222-2222-4222-8222-222222222222",
  name: "Example assistant",
  model: "example/model",
  instructions: "",
  toolsets: [],
  mcpServers: [],
  skills: [],
  warmTtlSeconds: 300,
  maxConcurrency: 1,
  runtimeVersion: 2,
  status: "active",
  createdAt: new Date(),
  updatedAt: new Date(),
};

beforeEach(() => {
  mocks.flag = "enabled";
  mocks.pending = false;
  mocks.mutate.mockReset();
});
afterEach(cleanup);

describe("assistant runtime selection", () => {
  it("submits the saved version as the switch precondition without optimistic state", () => {
    render(<AssistantRuntimeSettings assistant={assistant} canWrite />);
    const toggle = screen.getByRole("switch", { name: "Use Kit runtime" });
    fireEvent.click(toggle);
    expect(mocks.mutate).toHaveBeenCalledWith({
      request: {
        updateAssistantForm: {
          id: assistant.id,
          runtimeVersion: 3,
          expectedRuntimeVersion: 2,
        },
      },
    });
    expect(toggle.getAttribute("aria-checked")).toBe("false");
  });

  it.each(["disabled", "missing", "error", "loading"])(
    "hides the toggle for %s without changing saved Kit selection",
    (flag) => {
      mocks.flag = flag;
      render(
        <AssistantRuntimeSettings
          assistant={{ ...assistant, runtimeVersion: 3 }}
          canWrite
        />,
      );
      expect(screen.queryByRole("switch")).toBeNull();
      expect(screen.getByText("Kit")).toBeTruthy();
      expect(mocks.mutate).not.toHaveBeenCalled();
    },
  );

  it("requires management permission even when the flag is enabled", () => {
    render(<AssistantRuntimeSettings assistant={assistant} canWrite={false} />);
    fireEvent.click(screen.getByRole("switch"));
    expect(mocks.mutate).not.toHaveBeenCalled();
  });

  it("switches back using the persisted Kit version", () => {
    render(
      <AssistantRuntimeSettings
        assistant={{ ...assistant, runtimeVersion: 3 }}
        canWrite
      />,
    );
    fireEvent.click(screen.getByRole("switch"));
    expect(mocks.mutate).toHaveBeenCalledWith({
      request: {
        updateAssistantForm: {
          id: assistant.id,
          runtimeVersion: 2,
          expectedRuntimeVersion: 3,
        },
      },
    });
  });
});
