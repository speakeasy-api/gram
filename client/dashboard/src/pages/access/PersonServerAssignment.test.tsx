import { cleanup, render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { afterEach, expect, it, vi } from "vitest";
import type { ReactNode } from "react";
import type { ExplainedAccessRule } from "@gram/client/models/components/explainedaccessrule.js";
import { ServerAssignment } from "./PersonServerAssignment";

vi.mock("@/routes", () => ({
  useRoutes: () => ({
    mcp: { details: { overview: { href: () => "/server" } } },
  }),
}));
vi.mock("@/contexts/Auth", () => ({
  useOrganization: () => ({ projects: [] }),
}));
vi.mock("@/components/role-link", () => ({
  RoleLink: ({ children }: { children: ReactNode }) => <>{children}</>,
}));
vi.mock("./AssignmentServerIcon", () => ({
  AssignmentServerIcon: () => <span role="img" aria-label="Server icon" />,
}));
afterEach(cleanup);
const server = {
  id: "server",
  name: "Issue tracker",
  slug: "issues",
  projectId: "project",
  projectSlug: "default",
};
function rule(overrides: Partial<ExplainedAccessRule>): ExplainedAccessRule {
  return {
    appliesTo: "resource",
    viaDirectoryMapping: false,
    displayName: "Admin",
    effect: "allows",
    kind: "role",
    level: "use",
    principalUrn: "role:global:admin",
    ...overrides,
  };
}
it("explains a role grant suppressed by a different role's block", () => {
  render(
    <MemoryRouter>
      <ServerAssignment
        server={server}
        explanation={{
          visibility: "private",
          levels: [
            {
              level: "use",
              allowed: false,
              toolAccess: "none",
              rules: [
                rule({ effect: "blocked" }),
                rule({
                  displayName: "Engineer",
                  effect: "blocks",
                  level: "blocked",
                  principalUrn: "role:organization:engineer",
                }),
              ],
            },
          ],
        }}
      />
    </MemoryRouter>,
  );
  expect(screen.getByRole("img", { name: "Server icon" })).toBeTruthy();
  expect(screen.getByText("Blocked", { exact: true })).toBeTruthy();
  expect(screen.getByText(/Assigned through/).textContent).toBe(
    "Assigned through Admin, but blocked by Engineer",
  );
  expect(screen.queryByText("Default project")).toBeNull();
});
it("explains a direct grant overriding a role block", () => {
  render(
    <MemoryRouter>
      <ServerAssignment
        server={server}
        explanation={{
          visibility: "private",
          levels: [
            {
              level: "use",
              allowed: true,
              toolAccess: "all",
              rules: [
                rule({
                  kind: "user",
                  effect: "overrides",
                  principalUrn: "user:person",
                }),
                rule({
                  displayName: "Engineer",
                  effect: "overridden",
                  level: "blocked",
                  principalUrn: "role:organization:engineer",
                }),
              ],
            },
          ],
        }}
      />
    </MemoryRouter>,
  );
  expect(screen.getByText("All tools")).toBeTruthy();
  expect(screen.getByText(/Direct assignment overrides/).textContent).toContain(
    "Direct assignment overrides the block from Engineer",
  );
  expect(screen.queryByText("Blocked", { exact: true })).toBeNull();
});

it("shows role blocks on public servers without implying public access is prevented", () => {
  render(
    <MemoryRouter>
      <ServerAssignment
        server={server}
        explanation={{
          visibility: "public",
          levels: [
            {
              level: "use",
              allowed: false,
              toolAccess: "none",
              rules: [
                rule({
                  displayName: "Analyst",
                  effect: "blocked",
                  principalUrn: "role:organization:analyst",
                }),
                rule({ effect: "blocks", level: "blocked" }),
              ],
            },
          ],
        }}
      />
    </MemoryRouter>,
  );
  expect(screen.getByText("Blocked by role")).toBeTruthy();
  expect(screen.getByText(/Assigned through/).textContent).toBe(
    "Assigned through Analyst, but blocked by Admin",
  );
  expect(
    screen.getByText(
      "The public endpoint remains accessible without authentication.",
    ),
  ).toBeTruthy();
});

it.each(["role", "user", "everyone"] as const)(
  "names a public server's %s block without duplicate public text",
  (kind) => {
    render(
      <MemoryRouter>
        <ServerAssignment
          server={server}
          explanation={{
            visibility: "public",
            levels: [
              {
                level: "use",
                allowed: false,
                toolAccess: "none",
                rules: [
                  rule({
                    kind,
                    displayName: "Engineer",
                    effect: "blocks",
                    level: "blocked",
                  }),
                ],
              },
            ],
          }}
        />
      </MemoryRouter>,
    );
    expect(
      screen.getByText("Blocked by", { selector: "span" }).textContent,
    ).toBe(
      kind === "user" ? "Blocked by Direct assignment" : "Blocked by Engineer",
    );
    expect(screen.queryByText("Public server")).toBeNull();
    expect(screen.queryByText("Blocked by role") !== null).toBe(
      kind === "role",
    );
  },
);

it("does not present an overridden block's tool scope as an active restriction", () => {
  render(
    <MemoryRouter>
      <ServerAssignment
        server={server}
        explanation={{
          visibility: "private",
          levels: [
            {
              level: "use",
              allowed: true,
              toolAccess: "all",
              rules: [
                rule({
                  kind: "user",
                  principalUrn: "user:person",
                  effect: "overrides",
                }),
                rule({
                  effect: "overridden",
                  level: "blocked",
                  tools: ["delete_issue"],
                }),
              ],
            },
          ],
        }}
      />
    </MemoryRouter>,
  );
  expect(screen.getByText("All tools")).toBeTruthy();
  expect(screen.queryByText(/delete_issue/)).toBeNull();
});
