import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { AssistantDraftPanel } from "./AssistantDraftPanel";

const state = vi.hoisted(() => ({ flag: "enabled", tab: "overview" }));
beforeEach(() => {
  state.flag = "enabled";
  state.tab = "overview";
});
vi.mock("@/hooks/useFeatureFlag", () => ({
  useFeatureFlag: () => ({ status: state.flag }),
}));

vi.mock("nuqs", async () => {
  const React = await import("react");
  return {
    parseAsStringLiteral: () => ({ withDefault: () => undefined }),
    useQueryState: () => React.useState(state.tab),
  };
});
vi.mock("@/contexts/Auth", () => ({ useProject: () => ({ id: "project" }) }));
vi.mock("@/routes", () => ({ useRoutes: () => ({}) }));
vi.mock("@/components/require-scope", () => ({ RequireScope: () => null }));
vi.mock("@gram/client/react-query/triggers.js", () => ({
  useTriggers: () => ({}),
}));
vi.mock("@gram/client/react-query/assistantsDelete.js", () => ({
  useAssistantsDeleteMutation: () => ({ isPending: false }),
}));
vi.mock("./useAssistantDraft", () => ({
  useAssistantDraft: () => ({
    assistantId: "assistant",
    assistant: { id: "assistant", name: "Example assistant", instructions: "" },
  }),
}));
vi.mock("./AssistantIdentitySettings", () => ({
  AssistantIdentitySettings: () => <div>Identity settings content</div>,
}));
vi.mock("./AssistantOverviewSettings", () => ({
  AssistantOverviewSettings: () => null,
}));
vi.mock("./AssistantMCPServersSection", () => ({
  AssistantMCPServersSection: () => null,
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
      <AssistantDraftPanel />
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

it("falls back to Overview when an Identity deep link is unavailable", () => {
  state.flag = "disabled";
  state.tab = "identity";
  render(
    <QueryClientProvider client={new QueryClient()}>
      <AssistantDraftPanel />
    </QueryClientProvider>,
  );
  expect(
    screen.getByRole("tab", { name: "Overview" }).getAttribute("aria-selected"),
  ).toBe("true");
  expect(screen.queryByRole("tab", { name: "Identity" })).toBeNull();
});
