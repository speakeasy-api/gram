import { cleanup, fireEvent, render, waitFor } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { TooltipProvider } from "@/components/ui/Tooltip";
import { toCatalogEntry } from "./platforms";
import { SetupBlockView, type SetupBlockContext } from "./SetupBlocks";
import { testPlatform } from "./testPlatform";

afterEach(cleanup);

const context: SetupBlockContext = {
  entry: toCatalogEntry(testPlatform),
  values: {},
  onValueChange: () => {},
  agents: [],
  agentsUnavailable: null,
  agentId: "",
  onAgentChange: () => {},
  onCreateAgent: () => Promise.resolve(true),
  tags: [],
  onTagsChange: () => {},
  ruleConflict: null,
  setupValues: {
    values: undefined,
    unavailableReason: null,
    endpointGroups: [],
    selectedEndpointId: "",
    onEndpointChange: () => {},
  },
  checkedItems: new Set(),
  onCheckedChange: () => {},
};

it("renders no image from definition text, whatever its source", () => {
  const { container, getByText } = render(
    <SetupBlockView
      blockKey="b"
      block={{
        type: "text",
        markdown:
          "Before ![x](https://evil.example.com/px) after ![y][ref]\n\n[ref]: /ok.png",
      }}
      context={context}
    />,
  );
  expect(container.querySelector("img")).toBeNull();
  expect(getByText(/Before/)).toBeTruthy();
});

it("shows the selected issuer for the token endpoint", () => {
  const { getByLabelText, getByText } = render(
    <SetupBlockView
      blockKey="b"
      block={{ type: "computed_status" }}
      context={{
        ...context,
        setupValues: {
          ...context.setupValues,
          endpointGroups: [
            { label: "Organization", options: [{ id: "o1", label: "acme" }] },
          ],
          selectedEndpointId: "o1",
        },
      }}
    />,
  );
  expect(getByLabelText("User session issuer")).toBeTruthy();
  expect(getByText("acme")).toBeTruthy();
});

it("says why there is no issuer to choose", () => {
  const { getByText } = render(
    <SetupBlockView
      blockKey="b"
      block={{ type: "computed_status" }}
      context={{
        ...context,
        setupValues: {
          ...context.setupValues,
          unavailableReason: "Nothing serves one.",
        },
      }}
    />,
  );
  expect(getByText("Nothing serves one.")).toBeTruthy();
});

it("shows a checklist item's value and ticks it off", () => {
  const onCheckedChange = vi.fn<(blockKey: string, checked: boolean) => void>();
  const { getByLabelText, getByText } = render(
    <TooltipProvider>
      <SetupBlockView
        blockKey="console-1"
        block={{
          type: "checklist_item",
          label: "Token endpoint",
          value: "token_endpoint",
        }}
        context={{
          ...context,
          onCheckedChange,
          setupValues: {
            ...context.setupValues,
            values: {
              token_endpoint: "https://gram.example.com/oauth/usi/1/token",
            },
          },
        }}
      />
    </TooltipProvider>,
  );
  expect(getByText("https://gram.example.com/oauth/usi/1/token")).toBeTruthy();
  fireEvent.click(getByLabelText("Token endpoint"));
  expect(onCheckedChange).toHaveBeenCalledWith("console-1", true);
});

it("shows what to do for a checklist item without a value", () => {
  const { getByText } = render(
    <SetupBlockView
      blockKey="bundle-1"
      block={{
        type: "checklist_item",
        label: "Resource (optional)",
        instruction: "Leave empty.",
      }}
      context={context}
    />,
  );
  expect(getByText("Leave empty.")).toBeTruthy();
});

it("creates an agent named after the platform, which the operator can rename", async () => {
  const onCreateAgent = vi.fn(() => Promise.resolve(true));
  const { getByLabelText, getByText } = render(
    <SetupBlockView
      blockKey="agent-0"
      block={{ type: "agent_picker" }}
      context={{ ...context, onCreateAgent }}
    />,
  );
  fireEvent.click(getByText("Create a new agent"));
  const name = getByLabelText("New agent name") as HTMLInputElement;
  expect(name.value).toBe(context.entry.displayName);
  fireEvent.change(name, { target: { value: "Support bot" } });
  fireEvent.click(getByText("Create agent"));
  await waitFor(() =>
    expect(onCreateAgent).toHaveBeenCalledWith("Support bot"),
  );
});

it("says when the values repeat an access rule that already exists", () => {
  const { getByRole } = render(
    <SetupBlockView
      blockKey="organization-1"
      block={{ type: "subject_rule" }}
      context={{
        ...context,
        values: { org_id: "org-abc" },
        ruleConflict: "This Anthropic organization ID is already connected.",
      }}
    />,
  );
  expect(getByRole("alert").textContent).toContain(
    "This Anthropic organization ID is already connected.",
  );
});
