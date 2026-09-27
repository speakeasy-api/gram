import {
  cleanup,
  fireEvent,
  render,
  screen,
  within,
} from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { AgentPolicyGrantForm } from "@gram/client/models/components/agentpolicygrantform.js";
import { AgentKeyReview } from "./AgentKeyReview";
import type { KeyServer } from "./AgentKeyServers";
import { reviewLabel, summarizeKeyReview } from "./agent-key-review";

const server: KeyServer = {
  id: "server_internal",
  resourceId: "toolset_internal",
  projectId: "project_internal",
  projectSlug: "engineering",
  name: "Issue tracker",
  kind: "Hosted",
  issuerId: "issuer_internal",
  endpoints: ["https://private.example/mcp/secret"],
  unavailable: true,
};
const other: KeyServer = {
  ...server,
  id: "other_internal",
  projectId: "other_project",
  name: "Support desk",
  projectSlug: "support",
};
function grant(
  tool?: string,
  disposition?: "read_only" | "destructive",
  scope = "mcp:connect",
  projectId = server.projectId,
): AgentPolicyGrantForm {
  return {
    effect: "allow",
    scope,
    selector: {
      resourceKind: "mcp",
      resourceId: server.resourceId,
      projectId,
      tool,
      disposition,
    },
  };
}
function setup(
  grants: AgentPolicyGrantForm[],
  overrides: Partial<React.ComponentProps<typeof AgentKeyReview>> = {},
) {
  const callbacks = {
    onEditServers: vi.fn(),
    onEditAccounts: vi.fn(),
    onEditAccess: vi.fn(),
  };
  const view = render(
    <AgentKeyReview
      name="Automation"
      servers={[server]}
      grants={grants}
      {...callbacks}
      {...overrides}
    />,
  );
  return { ...view, ...callbacks };
}
afterEach(cleanup);

describe("AgentKeyReview", () => {
  it("preserves descriptive labels containing UUIDs but hides ID-only labels", () => {
    const id = "11111111-2222-4333-8444-555555555555";
    expect(reviewLabel(`  ${id}  `)).toBeUndefined();
    expect(reviewLabel(`Release ${id}`)).toBe(`Release ${id}`);
  });
  it("reports no account requirement for a confirmed empty account inventory", () => {
    setup([grant("find_issue")], {
      accounts: [
        {
          resourceId: server.resourceId,
          projectId: server.projectId,
          status: "not-required",
        },
      ],
    });
    expect(screen.getByText("No connected account required.")).toBeTruthy();
    expect(screen.queryByText(/Account identity unavailable/)).toBeNull();
  });
  it("keeps category-wide future coverage distinct from unrestricted all-tools access", () => {
    const group = summarizeKeyReview(
      [server],
      [grant(undefined, "read_only"), grant("find_issue", "read_only")],
    ).servers[0]?.access[0];
    expect(group).toMatchObject({
      allTools: false,
      categoryTools: true,
      tools: [],
    });
  });
  it("keeps repeated tool names in their own disposition groups", () => {
    const summary = summarizeKeyReview(
      [server],
      [
        grant("update", "read_only"),
        grant("update", "destructive"),
        grant("update", "read_only"),
      ],
    );
    expect(
      summary.servers[0]?.access.map((group) => [group.condition, group.tools]),
    ).toEqual([
      ["Read-only tools", ["update"]],
      ["Tools that may delete or overwrite data", ["update"]],
    ]);
  });

  it("does not assign credential access to an unproxied server", () => {
    const unproxied = { ...server, kind: "Unproxied" };
    expect(
      summarizeKeyReview([unproxied], [grant("find_issue")]).servers[0]?.access,
    ).toEqual([]);
    setup([], { servers: [unproxied] });
    expect(
      screen.getByText("This server does not use a Gram API key."),
    ).toBeTruthy();
  });

  it("unions selected tools without crossing dispositions or actions", () => {
    const summary = summarizeKeyReview(
      [server],
      [
        grant("find_issue", "read_only"),
        grant("list_issues", "read_only"),
        grant("remove_issue", "destructive"),
        grant("find_issue", "read_only", "mcp:read"),
      ],
    );
    expect(summary.servers[0]?.access).toEqual([
      {
        action: "Use tools",
        condition: "Read-only tools",
        allTools: false,
        tools: ["find_issue", "list_issues"],
      },
      {
        action: "Use tools",
        condition: "Tools that may delete or overwrite data",
        allTools: false,
        tools: ["remove_issue"],
      },
      {
        action: "View server",
        condition: "Read-only tools",
        allTools: false,
        tools: ["find_issue"],
      },
    ]);
    setup([
      grant("find_issue", "read_only"),
      grant("list_issues", "read_only"),
      grant("remove_issue", "destructive"),
    ]);
    expect(screen.queryByText(/All eligible tools/)).toBeNull();
    const lists = screen.getAllByRole("list", { name: "Selected tools" });
    expect(within(lists[0]!).getByText("list_issues")).toBeTruthy();
    expect(within(lists[0]!).queryByText("remove_issue")).toBeNull();
  });

  it("matches authorization resource identity, not server ID, and isolates projects", () => {
    const summary = summarizeKeyReview(
      [server, other],
      [
        grant("find_issue"),
        grant("reply", undefined, "mcp:connect", other.projectId),
      ],
    );
    expect(summary.servers.map((row) => row.access[0]?.tools)).toEqual([
      ["find_issue"],
      ["reply"],
    ]);
    const wrongId = grant("find_issue");
    wrongId.selector.resourceId = server.id;
    expect(summarizeKeyReview([server], [wrongId]).hasUnmappedAccess).toBe(
      true,
    );
  });

  it("shows explicit future breadth only for an unrestricted tool group", () => {
    setup([
      grant(undefined, "read_only"),
      grant("remove_issue", "destructive"),
    ]);
    expect(
      screen.getByText(
        "All eligible tools in this category, including future tools.",
      ),
    ).toBeTruthy();
    expect(screen.getByText("remove_issue")).toBeTruthy();
    expect(
      screen.queryByText("All eligible tools, including future tools."),
    ).toBeNull();
    expect(
      summarizeKeyReview([server], [grant("*"), grant("find_issue")]).servers[0]
        ?.access,
    ).toEqual([
      { action: "Use tools", condition: undefined, allTools: true, tools: [] },
    ]);
  });

  it("does not expose internal IDs, selector keys, endpoints, or availability warnings", () => {
    const { container } = setup([grant("find_issue")]);
    expect(screen.getByRole("article", { name: "Issue tracker" })).toBeTruthy();
    for (const value of [
      server.id,
      server.resourceId,
      server.projectId,
      server.issuerId!,
      "resourceId",
      "resourceKind",
      "projectId",
      "disposition",
      "mcp:connect",
      "https://",
      "unavailable platform",
    ]) {
      expect(container.textContent).not.toContain(value);
    }
  });

  it("uses honest name and account fallbacks rather than inventing identity", () => {
    setup([grant("find_issue")], { servers: [{ ...server, name: server.id }] });
    expect(
      screen.getByRole("article", { name: "Server name unavailable" }),
    ).toBeTruthy();
    expect(screen.getByText(/Account identity unavailable/)).toBeTruthy();
    expect(screen.queryByText(/Account connected/)).toBeNull();
  });

  it("shows only actual account identities for the matching server and project", () => {
    setup([grant("find_issue")], {
      accounts: [
        {
          resourceId: server.resourceId,
          projectId: server.projectId,
          status: "connected",
          displayName: "Example user",
          email: "user@example.test",
        },
        {
          resourceId: server.resourceId,
          projectId: other.projectId,
          status: "connected",
          displayName: "Other account",
        },
      ],
    });
    expect(screen.getByText("Example user · user@example.test")).toBeTruthy();
    expect(screen.queryByText("Other account")).toBeNull();
  });

  it("warns rather than silently hiding unsupported or overly broad access", () => {
    const broad = grant("find_issue");
    broad.selector.resourceId = "*";
    broad.selector.projectId = "*";
    setup([broad, grant(undefined, undefined, "environment:read")]);
    expect(screen.getByText(/beyond these servers or projects/)).toBeTruthy();
    expect(screen.getByText(/could not be matched/)).toBeTruthy();
  });

  it("offers non-submitting edit actions and honors the pending state", () => {
    const callbacks = setup([grant("find_issue")]);
    fireEvent.click(screen.getByRole("button", { name: "Edit servers" }));
    fireEvent.click(
      screen.getByRole("button", { name: "Edit accounts for Issue tracker" }),
    );
    fireEvent.click(
      screen.getByRole("button", { name: "Edit access for Issue tracker" }),
    );
    expect(callbacks.onEditServers).toHaveBeenCalledOnce();
    expect(callbacks.onEditAccounts).toHaveBeenCalledOnce();
    expect(callbacks.onEditAccess).toHaveBeenCalledOnce();
    for (const button of screen.getAllByRole("button"))
      expect(button.getAttribute("type")).toBe("button");
    cleanup();
    setup([grant("find_issue")], { disabled: true });
    for (const button of screen.getAllByRole("button"))
      expect((button as HTMLButtonElement).disabled).toBe(true);
  });
});
