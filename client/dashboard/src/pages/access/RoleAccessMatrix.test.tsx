import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import type { ScopeDefinition } from "@gram/client/models/components/scopedefinition.js";
import { afterEach, expect, it, vi } from "vitest";
import { RoleAccessMatrix } from "./RoleAccessMatrix";
import { TooltipProvider } from "@/components/ui/Tooltip";

vi.mock("./ToolAccessSheet", () => ({ ToolAccessSheet: () => null }));

afterEach(cleanup);

it("edits owner targets through the Agents Manage permission", () => {
  const onEditRule = vi.fn<() => void>();
  render(
    <TooltipProvider>
      <RoleAccessMatrix
        grants={{
          "agent:write": {
            scope: "agent:write",
            rules: [
              {
                id: "owner-rule",
                effect: "allow",
                selectors: [
                  {
                    resourceKind: "agent",
                    resourceId: "*",
                    owner: "self",
                  },
                ],
              },
            ],
          },
        }}
        scopes={[
          {
            slug: "agent:read",
            resourceType: "agent",
            visibility: "user_visible",
            description: "Read agents",
            agentEligible: false,
          } as ScopeDefinition,
          {
            slug: "agent:write",
            resourceType: "agent",
            visibility: "user_visible",
            description: "Create and manage agents",
            agentEligible: false,
          } as ScopeDefinition,
        ]}
        projects={[]}
        inventory={{
          groups: [],
          settled: true,
          isError: false,
          refetch: vi.fn<() => void>(),
        }}
        loading={false}
        markAgentIneligible={false}
        onChange={vi.fn<() => void>()}
        onEditRule={onEditRule}
        onRemoveRule={vi.fn<() => void>()}
      />
    </TooltipProvider>,
  );
  fireEvent.mouseDown(screen.getByRole("tab", { name: "Platform access" }), {
    button: 0,
    ctrlKey: false,
  });
  expect(
    screen.queryByRole("button", { name: "Change where View applies" }),
  ).toBeNull();
  fireEvent.click(
    screen.getByRole("button", { name: "Change where Manage applies" }),
  );
  expect(onEditRule).toHaveBeenCalledWith("agent:write", 0);
});
