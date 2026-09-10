import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { useState } from "react";
import { AgentGrantSelector, type GrantNarrowings } from "./AgentGrantSelector";
import {
  buildRequestedGrants,
  delegableGrantKey,
} from "./agent-api-key-grants";
import type { AgentPolicyGrantForm } from "@gram/client/models/components/agentpolicygrantform.js";

vi.mock("@/contexts/Auth", () => ({
  useOrganization: () => ({
    projects: [{ id: "project_one", name: "Project one", slug: "project-one" }],
  }),
}));
vi.mock("@/pages/access/useOrgMcpServers", () => ({
  useOrgMcpServers: () => ({
    isSettled: true,
    isError: false,
    refetch: vi.fn(),
    groups: [
      {
        projectId: "project_one",
        projectName: "Project one",
        servers: [
          {
            id: "server_one",
            name: "Example server",
            slug: "example",
            dynamicTools: false,
            remoteBacked: false,
            tools: [
              { id: "search", name: "search", type: "http" },
              { id: "fetch", name: "fetch", type: "http" },
            ],
          },
        ],
      },
    ],
  }),
}));
vi.mock("@/hooks/useToolMetadata", () => ({
  useToolMetadata: () => ({
    metadataByTool: {},
    isLoading: false,
    isError: false,
    refetch: vi.fn(),
  }),
}));
afterEach(cleanup);
const grant: AgentPolicyGrantForm = {
  effect: "allow",
  scope: "mcp:connect",
  selector: {
    resourceKind: "mcp",
    resourceId: "server_one",
    projectId: "project_one",
  },
};
function Editor({ candidate = grant }: { candidate?: AgentPolicyGrantForm }) {
  const key = delegableGrantKey(candidate);
  const [narrowings, setNarrowings] = useState<GrantNarrowings>({ [key]: {} });
  return (
    <>
      <AgentGrantSelector
        grants={[candidate]}
        narrowings={narrowings}
        onChange={setNarrowings}
      />
      <output data-testid="selection">{JSON.stringify(narrowings[key])}</output>
    </>
  );
}
function selection() {
  return JSON.parse(screen.getByTestId("selection").textContent!);
}

describe("AgentGrantSelector fine-tuning", () => {
  it("reuses multi-tool selection and preserves the project ceiling without a project picker", () => {
    render(<Editor />);
    expect(screen.queryByRole("combobox", { name: /Project/ })).toBeNull();
    fireEvent.click(screen.getByText("search", { exact: true }));
    fireEvent.click(screen.getByText("fetch", { exact: true }));
    expect(selection().tools).toEqual(["search", "fetch"]);
    expect(
      buildRequestedGrants([{ grant, narrowing: selection() }]).map(
        ({ selector }) => selector.projectId,
      ),
    ).toEqual(["project_one", "project_one"]);
  });
  it("selects multiple dispositions and switches away from manual tools", () => {
    render(<Editor />);
    fireEvent.click(screen.getByText("search", { exact: true }));
    fireEvent.click(screen.getByRole("button", { name: /By annotation/ }));
    fireEvent.click(screen.getByRole("checkbox", { name: /Read-only/ }));
    fireEvent.click(screen.getByRole("checkbox", { name: /Idempotent/ }));
    expect(selection()).toMatchObject({
      dispositions: ["read_only", "idempotent"],
    });
    expect(selection().tools).toBeUndefined();
    fireEvent.click(
      screen.getByRole("button", { name: /Reset tool restrictions/ }),
    );
    expect(selection()).toEqual({});
  });
  it("keeps an explicit empty selection until the user resets it", () => {
    render(<Editor />);
    fireEvent.click(screen.getByText("search", { exact: true }));
    fireEvent.click(screen.getByText("search", { exact: true }));
    expect(selection().tools).toEqual([]);
    expect(() =>
      buildRequestedGrants([{ grant, narrowing: selection() }]),
    ).toThrow(/at least one/);
  });
  it("does not offer replacing a disposition pinned by the candidate", () => {
    render(
      <Editor
        candidate={{
          ...grant,
          selector: { ...grant.selector, disposition: "read_only" },
        }}
      />,
    );
    expect(screen.queryByRole("button", { name: /By annotation/ })).toBeNull();
    fireEvent.click(screen.getByText("search", { exact: true }));
    expect(selection().tools).toEqual(["search"]);
  });
});
