import { afterEach, describe, expect, it, vi } from "vitest";
import { MemoryRouter, Link } from "react-router";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  within,
} from "@testing-library/react";
import { TooltipProvider } from "@/components/ui/Tooltip";
import type { IdentityProviderConnectionChecklistItem } from "@gram/client/models/components/identityproviderconnectionchecklistitem.js";
import type { OktaIdentityProviderConnection } from "@gram/client/models/components/oktaidentityproviderconnection.js";

import { ConnectionChecklist } from "./ConnectionChecklist";

const scrollIntoView = vi.fn();
Object.defineProperty(Element.prototype, "scrollIntoView", {
  configurable: true,
  value: scrollIntoView,
});
vi.mock(
  "@gram/client/react-query/recordIdentityProviderConnectionAgent.js",
  () => ({
    useRecordIdentityProviderConnectionAgentMutation: () => ({
      mutate: vi.fn(),
      isPending: false,
      error: null,
    }),
  }),
);
afterEach(() => {
  cleanup();
  scrollIntoView.mockClear();
});

function item(
  key: string,
  group: IdentityProviderConnectionChecklistItem["group"],
  completed?: boolean,
): IdentityProviderConnectionChecklistItem {
  return {
    key,
    group,
    title: `Step ${key}`,
    description: `Do ${key}.`,
    details:
      key === "create_ai_agent" ? ["Create it", "Delegate", "Activate"] : [],
    ...(completed === undefined ? {} : { completed }),
  };
}

function connectionWith(
  status: OktaIdentityProviderConnection["status"],
  checklist: IdentityProviderConnectionChecklistItem[],
): OktaIdentityProviderConnection {
  return {
    status,
    checklist,
    orgUrl: "https://acme.okta.com",
    jwksUrl: "https://speakeasy.test/jwks.json",
  } as OktaIdentityProviderConnection;
}

const pendingChecklist = [
  item("create_api_services_app", "connect"),
  item("public_key_auth", "connect"),
  item("submit_client_id", "connect", false),
  item("create_ai_agent", "cross_app_access"),
];

function renderChecklist(
  connection: OktaIdentityProviderConnection,
  route = "/identity",
) {
  return render(
    <MemoryRouter initialEntries={[route]}>
      <QueryClientProvider client={new QueryClient()}>
        <TooltipProvider>
          <Link to="?tab=enterprise-managed-auth&provider=okta&view=setup#agent">
            Set up agent
          </Link>
          <ConnectionChecklist connection={connection} />
        </TooltipProvider>
      </QueryClientProvider>
    </MemoryRouter>,
  );
}

describe("ConnectionChecklist", () => {
  it("expands Connect while pending and collapses the agent phase with a count", () => {
    renderChecklist(connectionWith("pending", pendingChecklist));
    expect(screen.getByText("Step create_api_services_app")).toBeTruthy();
    expect(
      screen.queryByRole("region", { name: "Save Okta AI agent" }),
    ).toBeNull();
    expect(
      screen.getByRole("button", {
        name: "Cross App Access setup, 0 of 1 complete",
      }),
    ).toBeTruthy();
    expect(
      screen
        .getByRole("link", { name: "Open the Okta Admin Console" })
        .getAttribute("href"),
    ).toBe("https://acme-admin.okta.com");
  });

  it("expands the agent phase once verified and ticks observed steps", () => {
    renderChecklist(
      connectionWith("verified", [
        item("public_key_auth", "connect", true),
        item("dpop", "connect", true),
        item("assign_admin_roles", "connect"),
        item("submit_client_id", "connect", true),
        item("create_ai_agent", "cross_app_access"),
      ]),
    );
    expect(screen.getByText("Step create_ai_agent")).toBeTruthy();
    expect(screen.getByText("Activate")).toBeTruthy();
    const connect = screen.getByRole("button", {
      name: "Connect, 3 of 4 complete",
    });
    fireEvent.click(connect);
    expect(screen.getByText("Step dpop").textContent).toContain("(done)");
    expect(
      screen.getByText("Step assign_admin_roles").textContent,
    ).not.toContain("(done)");
  });

  it("exposes agent settings after a degraded verification", () => {
    renderChecklist(
      connectionWith("degraded", [
        item("grant_scopes", "connect", false),
        item("create_ai_agent", "cross_app_access"),
      ]),
    );
    expect(
      screen
        .getByRole("button", {
          name: "Cross App Access setup, 0 of 1 complete",
        })
        .getAttribute("aria-expanded"),
    ).toBe("true");
    expect(
      screen.getByRole("region", { name: "Save Okta AI agent" }),
    ).toBeTruthy();
  });
});

it("distinguishes unobserved steps from steps that need attention", () => {
  renderChecklist(connectionWith("pending", pendingChecklist));
  const unknown = screen
    .getByText("Step create_api_services_app")
    .closest("li")!;
  const incomplete = screen.getByText("Step submit_client_id").closest("li")!;
  expect(unknown.textContent).toContain("Not checked");
  expect(unknown.textContent).not.toContain("Needs attention");
  expect(incomplete.textContent).toContain("Needs attention");
  expect(incomplete.textContent).not.toContain("Not checked");
});

it("shows six of six complete and starts Connect collapsed after verification", () => {
  const checklist = [
    "create_api_services_app",
    "public_key_auth",
    "dpop",
    "grant_scopes",
    "assign_admin_roles",
    "submit_client_id",
  ].map((key) => item(key, "connect", true));
  renderChecklist(
    connectionWith("verified", [
      ...checklist,
      item("create_ai_agent", "cross_app_access"),
    ]),
  );
  const connect = screen.getByRole("button", {
    name: "Connect, 6 of 6 complete",
  });
  expect(connect.getAttribute("aria-expanded")).toBe("false");
  expect(
    screen.getByText("Okta connection and required access verified."),
  ).toBeTruthy();
  expect(screen.queryByText("Step create_api_services_app")).toBeNull();
  expect(screen.getByText("Step create_ai_agent")).toBeTruthy();
  fireEvent.click(connect);
  expect(
    screen.getByText("Step create_api_services_app").textContent,
  ).toContain("(done)");
  expect(screen.queryByText("Needs attention")).toBeNull();
  expect(
    within(
      screen.getByText("Step create_api_services_app").closest("ol")!,
    ).queryByText("Not checked"),
  ).toBeNull();
});

it("places the save form inside the agent checklist step and preserves drafts when collapsed", () => {
  renderChecklist(connectionWith("verified", pendingChecklist));
  const step = screen.getByText("Step create_ai_agent").closest("li")!;
  const form = within(step).getByRole("region", { name: "Save Okta AI agent" });
  expect(within(form).getByRole("button", { name: "Save agent" })).toBeTruthy();
  expect(screen.getAllByLabelText("Agent ID")).toHaveLength(1);
  fireEvent.change(within(form).getByLabelText("Agent ID"), {
    target: { value: "draft-agent" },
  });
  const toggle = screen.getByRole("button", {
    name: "Cross App Access setup, 0 of 1 complete",
  });
  fireEvent.click(toggle);
  expect(
    screen.queryByRole("region", { name: "Save Okta AI agent" }),
  ).toBeNull();
  fireEvent.click(toggle);
  expect((screen.getByLabelText("Agent ID") as HTMLInputElement).value).toBe(
    "draft-agent",
  );
});

it("opens the agent step for an initial hash link even while pending", () => {
  renderChecklist(
    connectionWith("pending", pendingChecklist),
    "/identity#agent",
  );
  expect(
    screen.getByRole("region", { name: "Save Okta AI agent" }),
  ).toBeTruthy();
  expect(scrollIntoView).toHaveBeenCalled();
});

it("opens a collapsed agent step when following an in-page setup link", () => {
  renderChecklist(connectionWith("pending", pendingChecklist));
  expect(
    screen.queryByRole("region", { name: "Save Okta AI agent" }),
  ).toBeNull();
  fireEvent.click(screen.getByRole("link", { name: "Set up agent" }));
  expect(
    screen.getByRole("region", { name: "Save Okta AI agent" }),
  ).toBeTruthy();
  expect(scrollIntoView).toHaveBeenCalled();
});

it("does not offer agent editing for a revoked connection", () => {
  renderChecklist(
    connectionWith("revoked", pendingChecklist),
    "/identity#agent",
  );
  expect(
    screen.queryByRole("region", { name: "Save Okta AI agent" }),
  ).toBeNull();
});
