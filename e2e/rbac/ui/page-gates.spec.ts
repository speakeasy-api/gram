import { test } from "@playwright/test";
import { expectAllowed, expectDenied, pageAs } from "../lib/browser.ts";
import type { UserPersona } from "../lib/personas.ts";

// Which dashboard pages each persona may open. One row per page: `allowed`
// personas must see the page's marker text and no permission gate, `denied`
// personas must see the gate. Like the API contract table, coverage grows by
// adding rows.

type PageGate = {
  slice: "organization" | "projects" | "mcp" | "agents" | "access";
  path: string;
  // Text only the real page renders.
  marker: string;
  allowed: UserPersona[];
  denied: UserPersona[];
  note?: string;
};

const SANDBOX = "/speakeasy/projects/rbac-e2e-sandbox";
const DEFAULT = "/speakeasy/projects/default";

const GATES: PageGate[] = [
  {
    slice: "organization",
    path: "/speakeasy/team",
    marker: "Team Members",
    allowed: ["admin"],
    denied: ["member", "reader", "blocked", "norole"],
  },
  {
    slice: "organization",
    path: "/speakeasy/api-keys",
    marker: "API Keys",
    allowed: ["admin"],
    denied: ["member", "reader", "blocked", "norole"],
  },
  {
    slice: "projects",
    path: DEFAULT,
    marker: "Project Overview",
    allowed: ["admin", "member", "reader", "blocked"],
    denied: ["norole"],
  },
  {
    slice: "projects",
    path: `${DEFAULT}/settings`,
    marker: "Project Settings",
    allowed: ["admin", "blocked"],
    denied: ["member", "reader", "norole"],
    note:
      "The blocked persona holds project:blocked_write on this project and still opens the " +
      "page; the Save button is what the block disables (see the save journey).",
  },
  {
    slice: "projects",
    path: `${SANDBOX}/settings`,
    marker: "Project Settings",
    allowed: ["admin", "blocked"],
    denied: ["member", "reader", "norole"],
  },
  {
    slice: "mcp",
    path: `${DEFAULT}/mcp`,
    marker: "MCP Servers",
    allowed: ["admin", "member", "blocked"],
    denied: ["reader", "norole"],
  },
  {
    slice: "agents",
    path: `${DEFAULT}/assistants`,
    marker: "Assistants",
    allowed: ["admin", "member"],
    denied: ["reader", "norole"],
  },
  {
    slice: "access",
    path: "/speakeasy/access/roles",
    marker: "Roles & Permissions",
    allowed: ["admin", "member", "reader", "blocked"],
    denied: ["norole"],
  },
];

for (const gate of GATES) {
  test.describe(`${gate.slice} › ${gate.path}`, () => {
    for (const persona of gate.allowed) {
      test(`${persona} may open it`, async ({ browser }) => {
        const page = await pageAs(browser, persona);
        await page.goto(gate.path);
        await expectAllowed(page, gate.marker);
        await page.context().close();
      });
    }
    for (const persona of gate.denied) {
      test(`${persona} is turned away`, async ({ browser }) => {
        const page = await pageAs(browser, persona);
        await page.goto(gate.path);
        await expectDenied(page);
        await page.context().close();
      });
    }
  });
}
