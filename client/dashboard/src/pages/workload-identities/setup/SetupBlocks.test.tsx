import { cleanup, fireEvent, render, waitFor } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { TooltipProvider } from "@/components/ui/Tooltip";
import { toCatalogEntry } from "./platforms";
import {
  AgentPicker,
  CatalogChecklistItem,
  ComputedStatus,
  SubjectRulePreview,
} from "./SetupBlocks";
import type { SetupValues } from "./setupValues";
import { subjectRule } from "./template";
import { claudeTagPlatform } from "./catalogFixture";

afterEach(cleanup);

const entry = toCatalogEntry(claudeTagPlatform);

const setupValues: SetupValues = {
  values: undefined,
  unavailableReason: null,
  endpointGroups: [],
  selectedEndpointId: "",
  onEndpointChange: () => {},
};

it("shows the selected issuer for the token endpoint", () => {
  const { getByLabelText, getByText } = render(
    <ComputedStatus
      setupValues={{
        ...setupValues,
        endpointGroups: [
          { label: "Organization", options: [{ id: "o1", label: "acme" }] },
        ],
        selectedEndpointId: "o1",
      }}
    />,
  );
  expect(getByLabelText("User session issuer")).toBeTruthy();
  expect(getByText("acme")).toBeTruthy();
});

it("says why there is no issuer to choose", () => {
  const { getByText } = render(
    <ComputedStatus
      setupValues={{ ...setupValues, unavailableReason: "Nothing serves one." }}
    />,
  );
  expect(getByText("Nothing serves one.")).toBeTruthy();
});

it("shows a checklist item's value and ticks it off", () => {
  const onCheckedChange = vi.fn<(blockKey: string, checked: boolean) => void>();
  const { getByLabelText, getByText } = render(
    <TooltipProvider>
      <CatalogChecklistItem
        blockKey="console-1"
        block={{
          type: "checklist_item",
          label: "Token endpoint",
          value: "token_endpoint",
        }}
        computedValues={{
          token_endpoint: "https://gram.example.com/oauth/usi/1/token",
        }}
        checked={false}
        onCheckedChange={onCheckedChange}
      />
    </TooltipProvider>,
  );
  expect(getByText("https://gram.example.com/oauth/usi/1/token")).toBeTruthy();
  fireEvent.click(getByLabelText("Token endpoint"));
  expect(onCheckedChange).toHaveBeenCalledWith("console-1", true);
});

it("shows what to do for a checklist item without a value", () => {
  const { getByText } = render(
    <CatalogChecklistItem
      blockKey="bundle-1"
      block={{
        type: "checklist_item",
        label: "Resource (optional)",
        instruction: "Leave empty.",
      }}
      computedValues={undefined}
      checked={false}
      onCheckedChange={() => {}}
    />,
  );
  expect(getByText("Leave empty.")).toBeTruthy();
});

it("creates an agent named after the platform, which the operator can rename", async () => {
  const onCreateAgent = vi.fn(() => Promise.resolve(true));
  const { getByLabelText, getByText } = render(
    <AgentPicker
      agents={[]}
      unavailableReason={null}
      agentId=""
      onAgentChange={() => {}}
      newAgentName={entry.displayName}
      onCreateAgent={onCreateAgent}
    />,
  );
  fireEvent.click(getByText("Create a new agent"));
  const name = getByLabelText("New agent name") as HTMLInputElement;
  expect(name.value).toBe(entry.displayName);
  fireEvent.change(name, { target: { value: "Support bot" } });
  fireEvent.click(getByText("Create agent"));
  await waitFor(() =>
    expect(onCreateAgent).toHaveBeenCalledWith("Support bot"),
  );
});

it("says when the values repeat an access rule that already exists", () => {
  const { getByRole } = render(
    <SubjectRulePreview
      subject={subjectRule(entry, { org_id: "org-abc" })!.subject}
      conflict="This Anthropic organization ID is already connected."
    />,
  );
  expect(getByRole("alert").textContent).toContain(
    "This Anthropic organization ID is already connected.",
  );
});
