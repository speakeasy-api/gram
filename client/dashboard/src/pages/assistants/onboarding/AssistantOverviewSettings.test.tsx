import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { Assistant } from "@gram/client/models/components/assistant.js";
import { AssistantOverviewSettings } from "./AssistantOverviewSettings";

const mocks = vi.hoisted(() => ({
  canWrite: true,
  pending: false,
  mutate: vi.fn(),
}));
vi.mock("@/contexts/Auth", () => ({ useProject: () => ({ id: "project" }) }));
vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({ hasScope: () => mocks.canWrite }),
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
vi.mock("@/components/assistants/assistant-owner", () => ({
  AssistantOwner: () => null,
}));
vi.mock("@/components/assistants/status-toggle", () => ({
  AssistantStatusToggle: () => null,
}));
vi.mock("@/components/model-select", () => ({ ModelSelect: () => null }));
vi.mock("./AssistantRuntimeSettings", () => ({
  AssistantRuntimeSettings: () => null,
}));
const assistant: Assistant = {
  id: "11111111-1111-4111-8111-111111111111",
  projectId: "22222222-2222-4222-8222-222222222222",
  name: "Example assistant",
  model: "example/model",
  reasoningEffort: "medium",
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
  mocks.canWrite = true;
  mocks.pending = false;
  mocks.mutate.mockReset();
});
afterEach(cleanup);

function edit(value = assistant) {
  const result = render(<AssistantOverviewSettings assistant={value} />);
  fireEvent.click(
    screen.getByRole("button", { name: "Edit overview settings" }),
  );
  return result;
}
function select(label: string) {
  fireEvent.pointerDown(
    screen.getByRole("combobox", { name: "Reasoning effort" }),
    { button: 0, ctrlKey: false, pointerType: "mouse" },
  );
  fireEvent.click(screen.getByRole("option", { name: label }));
}

describe("assistant reasoning effort", () => {
  it("shows the saved choice and preserves it when another setting is saved", () => {
    edit({ ...assistant, reasoningEffort: "high" });
    expect(screen.getByRole("combobox").textContent).toContain("High");
    fireEvent.change(screen.getByRole("textbox", { name: "Name" }), {
      target: { value: "Renamed" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Save" }));
    expect(
      mocks.mutate.mock.calls[0]?.[0].request.updateAssistantForm
        .reasoningEffort,
    ).toBe("high");
  });
  it("defaults older data to Medium without marking the form dirty", () => {
    edit({ ...assistant, reasoningEffort: undefined } as unknown as Assistant);
    expect(screen.getByRole("combobox").textContent).toContain("Medium");
    expect(screen.queryByRole("button", { name: "Save" })).toBeNull();
  });
  it.each([
    ["Off", "none"],
    ["Low", "low"],
    ["High", "high"],
  ])("saves %s as %s", (label, value) => {
    edit();
    select(label);
    fireEvent.click(screen.getByRole("button", { name: "Save" }));
    expect(
      mocks.mutate.mock.calls[0]?.[0].request.updateAssistantForm
        .reasoningEffort,
    ).toBe(value);
  });
  it("discards the draft instead of persisting it", () => {
    edit();
    select("Off");
    fireEvent.click(screen.getByRole("button", { name: "Discard changes" }));
    expect(screen.getByText("Medium")).toBeTruthy();
    expect(mocks.mutate).not.toHaveBeenCalled();
  });
  it("keeps saved settings read-only without permission", () => {
    mocks.canWrite = false;
    render(
      <AssistantOverviewSettings
        assistant={{ ...assistant, reasoningEffort: "none" }}
      />,
    );
    expect(screen.getByText("Off")).toBeTruthy();
    expect(
      screen.queryByRole("button", { name: "Edit overview settings" }),
    ).toBeNull();
    expect(screen.queryByRole("combobox")).toBeNull();
  });
  it("disables selection while saving", () => {
    const { rerender } = edit();
    select("Off");
    mocks.pending = true;
    rerender(<AssistantOverviewSettings assistant={assistant} />);
    expect((screen.getByRole("combobox") as HTMLButtonElement).disabled).toBe(
      true,
    );
    expect(
      (
        screen.getByRole("button", {
          name: "Discard changes",
        }) as HTMLButtonElement
      ).disabled,
    ).toBe(true);
  });
});

it("disables Off for Kit and saves supported effort instead", () => {
  edit({ ...assistant, runtimeVersion: 3 });
  expect(
    screen.getByText(/Kit runtime supports Low, Medium, and High/),
  ).toBeTruthy();
  fireEvent.pointerDown(
    screen.getByRole("combobox", { name: "Reasoning effort" }),
    { button: 0, ctrlKey: false, pointerType: "mouse" },
  );
  const off = screen.getByRole("option", { name: "Off" });
  expect(off.getAttribute("aria-disabled")).toBe("true");
  fireEvent.click(off);
  expect(mocks.mutate).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole("option", { name: "Low" }));
  fireEvent.click(screen.getByRole("button", { name: "Save" }));
  expect(
    mocks.mutate.mock.calls[0]?.[0].request.updateAssistantForm.reasoningEffort,
  ).toBe("low");
});

it("blocks a stale Off draft after the effective runtime becomes Kit", () => {
  const { rerender } = edit();
  select("Off");
  rerender(
    <AssistantOverviewSettings
      assistant={{ ...assistant, runtimeVersion: 3 }}
    />,
  );
  const save = screen.getByRole("button", { name: "Save" });
  expect((save as HTMLButtonElement).disabled).toBe(true);
  fireEvent.click(save);
  expect(mocks.mutate).not.toHaveBeenCalled();
});

it("keeps Off selectable on Default runtime with model compatibility guidance", () => {
  edit();
  expect(
    screen.getByText(/Unsupported models may reject requests/),
  ).toBeTruthy();
  fireEvent.pointerDown(
    screen.getByRole("combobox", { name: "Reasoning effort" }),
    { button: 0, ctrlKey: false, pointerType: "mouse" },
  );
  expect(
    screen.getByRole("option", { name: "Off" }).getAttribute("aria-disabled"),
  ).not.toBe("true");
});
