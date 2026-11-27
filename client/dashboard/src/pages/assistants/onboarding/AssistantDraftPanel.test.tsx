import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import type { ReasoningEffort } from "./reasoning-effort";
import { AssistantDraftPanel } from "./AssistantDraftPanel";

const state = vi.hoisted(() => ({
  tab: "overview",
  assistantId: "assistant" as string | null,
  canWrite: true,
  runtimeVersion: 2,
  setReasoningEffort: vi.fn(),
}));
beforeEach(() => {
  state.tab = "overview";
  state.assistantId = "assistant";
  state.canWrite = true;
  state.runtimeVersion = 2;
  state.setReasoningEffort.mockReset();
});

vi.mock("nuqs", async () => {
  const React = await import("react");
  return {
    parseAsStringLiteral: () => ({ withDefault: () => undefined }),
    useQueryState: () => React.useState(state.tab),
  };
});
vi.mock("@/contexts/Auth", () => ({ useProject: () => ({ id: "project" }) }));
vi.mock("@/contexts/Sdk", () => ({
  useSlugs: () => ({ orgSlug: "org", projectSlug: "project" }),
}));
vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({ hasScope: () => state.canWrite }),
}));
vi.mock("@/components/require-scope", () => ({ RequireScope: () => null }));
vi.mock("@gram/client/react-query/triggers.js", () => ({
  useTriggers: () => ({}),
}));
vi.mock("@gram/client/react-query/assistantsDelete.js", () => ({
  useAssistantsDeleteMutation: () => ({ isPending: false }),
}));
vi.mock("./useAssistantDraft", async () => {
  const React = await import("react");
  return {
    useAssistantDraft: () => {
      const [reasoningEffort, setReasoningEffort] =
        React.useState<ReasoningEffort>("medium");
      return {
        assistantId: state.assistantId,
        reasoningEffort,
        getReasoningEffort: () => reasoningEffort,
        setReasoningEffort: (value: ReasoningEffort) => {
          state.setReasoningEffort(value);
          setReasoningEffort(value);
        },
        assistant: {
          id: "assistant",
          name: "Example assistant",
          instructions: "",
          agentId: "agent",
          runtimeVersion: state.runtimeVersion,
        },
      };
    },
  };
});
vi.mock("./AssistantIdentitySettings", () => ({
  AssistantIdentitySettings: () => <div>Identity settings content</div>,
}));
vi.mock("./AssistantUpstreamCredentials", () => ({
  AssistantUpstreamCredentials: () => null,
}));
vi.mock("./AssistantOverviewSettings", () => ({
  AssistantOverviewSettings: () => null,
}));
vi.mock("./AssistantMCPServersSection", () => ({
  AssistantMCPServersSection: () => <div>Attachment editor</div>,
}));
vi.mock("./AssistantKitMCPAccess", () => ({
  AssistantKitMCPAccess: ({ agentId }: { agentId: string }) => (
    <div>Access editor for {agentId}</div>
  ),
}));
vi.mock("./AssistantSkillsSection", () => ({
  AssistantSkillsSection: () => null,
}));
vi.mock("./AssistantTriggersList", () => ({
  AssistantTriggersList: () => null,
}));
vi.mock("@/components/assistants/sessions-list", () => ({
  AssistantSessionsList: () => null,
}));
vi.mock("@/components/assistants/edit-instructions-dialog", () => ({
  EditInstructionsDialog: () => null,
}));

afterEach(cleanup);
it("keeps identity settings out of Overview and places them in Identity", () => {
  render(
    <QueryClientProvider client={new QueryClient()}>
      <MemoryRouter>
        <AssistantDraftPanel />
      </MemoryRouter>
    </QueryClientProvider>,
  );
  expect(
    screen.getByRole("tab", { name: "Overview" }).getAttribute("aria-selected"),
  ).toBe("true");
  expect(screen.queryByText("Identity settings content")).toBeNull();
  fireEvent.mouseDown(screen.getByRole("tab", { name: "Identity" }), {
    button: 0,
    ctrlKey: false,
  });
  expect(screen.getByText("Identity settings content")).toBeTruthy();
});

function renderPanel() {
  return render(
    <QueryClientProvider client={new QueryClient()}>
      <MemoryRouter>
        <AssistantDraftPanel />
      </MemoryRouter>
    </QueryClientProvider>,
  );
}
it("shows the draft selection and lets onboarding select Off", () => {
  state.assistantId = null;
  renderPanel();
  const selector = screen.getByRole("combobox", { name: "Reasoning effort" });
  expect(selector.textContent).toContain("Medium");
  fireEvent.pointerDown(selector, {
    button: 0,
    ctrlKey: false,
    pointerType: "mouse",
  });
  fireEvent.click(screen.getByRole("option", { name: "Off" }));
  expect(state.setReasoningEffort).toHaveBeenCalledWith("none");
  expect(
    screen.getByRole("combobox", { name: "Reasoning effort" }).textContent,
  ).toBe("Off");
});
it("disables the onboarding selector without write permission", () => {
  state.assistantId = null;
  state.canWrite = false;
  renderPanel();
  expect(
    (
      screen.getByRole("combobox", {
        name: "Reasoning effort",
      }) as HTMLButtonElement
    ).disabled,
  ).toBe(true);
});

it("shows the attachment editor for the Default runtime", () => {
  renderPanel();
  expect(screen.queryByText("Access editor for agent")).toBeNull();
  expect(screen.getByText("Attachment editor")).toBeTruthy();
});

it("uses the backing agent's MCP access editor for the Kit runtime", () => {
  state.runtimeVersion = 3;
  renderPanel();
  expect(screen.queryByText("Attachment editor")).toBeNull();
  expect(screen.getByText("Access editor for agent")).toBeTruthy();
});
