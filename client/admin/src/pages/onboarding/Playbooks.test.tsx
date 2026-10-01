import { cleanup, fireEvent, screen, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { OnboardingPlaybooks } from "./Playbooks";
import { TooltipProvider } from "@/components/ui/tooltip";
import { anOrganization } from "@/test/fixtures";
import { renderWithApp } from "@/test/harness";

const mocks = vi.hoisted(() => ({
  getOrganization: vi.fn(),
  listOrganizations: vi.fn(),
}));
vi.mock("@/lib/gramAdminApi", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/lib/gramAdminApi")>();
  return {
    ...actual,
    getOrganization: mocks.getOrganization,
    listOrganizations: mocks.listOrganizations,
  };
});

const steps = [
  {
    slug: "identity-provider",
    title: "Set up identity provider",
    description: "",
    completion: "children",
    hidden_by_default: false,
    method_slugs: [],
    requires: [],
  },
  {
    slug: "connect-idp",
    title: "Connect identity provider",
    description: "",
    parent_slug: "identity-provider",
    completion: "fact",
    hidden_by_default: false,
    method_slugs: [],
    requires: [],
  },
  {
    slug: "mcp-distribution",
    title: "Distribute MCP servers",
    description: "",
    completion: "children",
    hidden_by_default: true,
    method_slugs: [],
    requires: [],
  },
  {
    slug: "platform-mcp",
    title: "Set up Platform MCP",
    description: "",
    completion: "manual",
    hidden_by_default: true,
    method_slugs: [],
    requires: [],
  },
  {
    slug: "anthropic-observability",
    title: "Set up Anthropic observability",
    description: "",
    completion: "manual",
    hidden_by_default: false,
    method_slugs: ["inference"],
    requires: [],
  },
];
type UseCase = {
  id: string;
  slug: string;
  name: string;
  description: string;
  default_playbook_id?: string;
};
// A playbook belongs to a use case or to an organization, never both.
type Playbook = {
  id: string;
  use_case_id?: string;
  use_case_slug?: string;
  use_case_name?: string;
  organization_id?: string;
  organization_name?: string;
  name: string;
  description: string;
  is_default: boolean;
  steps: { slug: string; title: string }[];
};
const ORG = anOrganization({ id: "org_acme", name: "Acme" });
const distribution: UseCase = {
  id: "uc-distribution",
  slug: "distribution",
  name: "Distribution",
  description: "Get MCP servers to every agent.",
  default_playbook_id: "pb-1",
};
const gatewayFirst: Playbook = {
  id: "pb-1",
  use_case_id: "uc-distribution",
  use_case_slug: "distribution",
  use_case_name: "Distribution",
  name: "Gateway first",
  description: "Gateway, then the marketplace.",
  is_default: true,
  steps: [
    { slug: "platform-mcp", title: "Set up Platform MCP" },
    { slug: "mcp-distribution", title: "Distribute MCP servers" },
  ],
};
const second: Playbook = {
  ...gatewayFirst,
  id: "pb-2",
  name: "Second",
  description: "",
  is_default: false,
  steps: [{ slug: "mcp-distribution", title: "Distribute MCP servers" }],
};
const acmeOwn: Playbook = {
  id: "pb-acme",
  organization_id: "org_acme",
  organization_name: "Acme",
  name: "Acme's way",
  description: "",
  is_default: false,
  steps: [{ slug: "platform-mcp", title: "Set up Platform MCP" }],
};
const fetchMock = vi.fn();
let useCases: UseCase[] = [];
let playbooks: Playbook[] = [];
let assignedId: string | undefined;
let rejectCreate = false;
function json(value: unknown, status = 200): Response {
  return new Response(JSON.stringify(value), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}
function withSteps(slugs: string[]) {
  return slugs.map((slug) => ({
    slug,
    title: steps.find((s) => s.slug === slug)!.title,
  }));
}
function refused(message: string): Response {
  return json(
    {
      name: "bad_request",
      id: "x",
      message,
      temporary: false,
      timeout: false,
      fault: false,
    },
    400,
  );
}
function assignment() {
  const playbook = playbooks.find((p) => p.id === assignedId);
  return {
    organization_id: "org_acme",
    playbook,
    applicability: (playbook?.steps ?? []).map((step) => ({
      slug: step.slug,
      title: step.title,
      applies: true,
      reason: "",
    })),
  };
}
beforeEach(() => {
  useCases = [];
  playbooks = [];
  assignedId = undefined;
  rejectCreate = false;
  mocks.getOrganization.mockReset().mockResolvedValue(ORG);
  mocks.listOrganizations
    .mockReset()
    .mockResolvedValue({ total: 1, organizations: [ORG] });
  fetchMock.mockReset().mockImplementation(async (request: Request) => {
    const url = new URL(request.url);
    const path = url.pathname;
    const body =
      request.method === "POST" ? await request.clone().json() : null;
    switch (path) {
      case "/admin/onboarding.steps":
        return json({ steps });
      case "/admin/onboarding.useCases":
        return json({ use_cases: useCases });
      case "/admin/onboarding.playbooks": {
        // Everything, or the use cases' plus the named organization's own.
        const organization = url.searchParams.get("organization_id");
        return json({
          playbooks: organization
            ? playbooks.filter(
                (p) => !p.organization_id || p.organization_id === organization,
              )
            : playbooks,
        });
      }
      case "/admin/organization.onboardingPlaybook": {
        if (request.method === "GET") return json(assignment());
        const playbook = playbooks.find((p) => p.id === body.playbook_id)!;
        // The stack under test has no Anthropic in it.
        if (playbook.steps.some((s) => s.slug === "anthropic-observability")) {
          return refused(
            "the stack does not support: Set up Anthropic observability (needs Anthropic in the stack)",
          );
        }
        // Mirrors the server: a customer already on its copy of the template
        // keeps it, and only a template it does not walk yet is copied.
        const current = playbooks.find((p) => p.id === assignedId);
        const walksTemplate =
          !!current?.organization_id &&
          current.name === playbook.name &&
          current.steps.map((s) => s.slug).join() ===
            playbook.steps.map((s) => s.slug).join();
        if (playbook.organization_id) {
          assignedId = playbook.id;
        } else if (!walksTemplate) {
          // A use case's playbook is a template: the customer gets a copy.
          const copy: Playbook = {
            ...structuredClone(playbook),
            id: `${playbook.id}-copy`,
            use_case_id: undefined,
            use_case_slug: undefined,
            use_case_name: undefined,
            organization_id: body.organization_id,
            organization_name: ORG.name,
            is_default: false,
          };
          playbooks = [...playbooks, copy];
          assignedId = copy.id;
        }
        return json(assignment());
      }
      case "/admin/onboarding.useCases.create": {
        const created = {
          id: `uc-${body.slug}`,
          slug: body.slug,
          name: body.name,
          description: body.description ?? "",
        };
        useCases = [...useCases, created];
        return json(created);
      }
      case "/admin/onboarding.useCases.delete":
        useCases = useCases.filter((u) => u.id !== body.use_case_id);
        playbooks = playbooks.filter((p) => p.use_case_id !== body.use_case_id);
        return json({ use_cases: useCases });
      case "/admin/onboarding.playbooks.create": {
        if (rejectCreate) {
          return refused(
            'step "connect-idp" sits under "identity-provider"; list the group instead',
          );
        }
        // Mirrors the server: exactly one owner.
        if (!body.use_case_id === !body.organization_id) {
          return refused(
            "a playbook belongs to a use case or to an organization, not both",
          );
        }
        const useCase = useCases.find((u) => u.id === body.use_case_id);
        const created: Playbook = {
          id: `pb-${playbooks.length + 1}`,
          use_case_id: useCase?.id,
          use_case_slug: useCase?.slug,
          use_case_name: useCase?.name,
          organization_id: body.organization_id,
          organization_name: body.organization_id ? ORG.name : undefined,
          name: body.name,
          description: body.description ?? "",
          is_default: body.is_default === true,
          steps: withSteps(body.step_slugs),
        };
        playbooks = [...playbooks, created];
        if (created.is_default && useCase) {
          useCase.default_playbook_id = created.id;
        }
        return json(created);
      }
      case "/admin/onboarding.playbooks.update": {
        const index = playbooks.findIndex((p) => p.id === body.playbook_id);
        const updated: Playbook = {
          ...playbooks[index]!,
          name: body.name,
          description: body.description ?? "",
          is_default: body.is_default === true,
          steps: withSteps(body.step_slugs),
        };
        playbooks = playbooks.map((p) =>
          p.id === updated.id
            ? updated
            : updated.is_default && p.use_case_id === updated.use_case_id
              ? { ...p, is_default: false }
              : p,
        );
        if (updated.is_default) {
          useCases.find(
            (u) => u.id === updated.use_case_id,
          )!.default_playbook_id = updated.id;
        }
        return json(updated);
      }
      case "/admin/onboarding.playbooks.delete":
        playbooks = playbooks.filter((p) => p.id !== body.playbook_id);
        if (assignedId === body.playbook_id) assignedId = undefined;
        return json({ playbooks });
      default:
        throw new Error(`unexpected ${request.method} ${path}`);
    }
  });
  vi.stubGlobal("fetch", fetchMock);
});
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});
const posts = () =>
  fetchMock.mock.calls
    .map(([r]) => r as Request)
    .filter((r) => r.method === "POST");
const postTo = (path: string) =>
  posts().filter((r) => new URL(r.url).pathname === path);

// The app's sidebar mounts the tooltip provider; a bare render needs its own.
function renderPage(organizationId?: string) {
  return renderWithApp(
    <TooltipProvider>
      <OnboardingPlaybooks organizationId={organizationId} />
    </TooltipProvider>,
  );
}
// Radix opens a menu on pointer down, not click.
function openMenu(scope: HTMLElement, label: string): void {
  fireEvent.pointerDown(within(scope).getByRole("button", { name: label }), {
    button: 0,
    ctrlKey: false,
    pointerType: "mouse",
  });
}
async function pick(name: string, option: RegExp): Promise<void> {
  fireEvent.keyDown(screen.getByRole("combobox", { name }), {
    key: "ArrowDown",
  });
  fireEvent.click(await screen.findByRole("option", { name: option }));
}
const useCasesTable = () => screen.getByRole("table", { name: "Use cases" });
const playbooksTable = () => screen.getByRole("table", { name: "Playbooks" });
function rowsOf(table: HTMLElement): (string | null)[] {
  return Array.from(table.querySelectorAll("tr[data-playbook]")).map((row) =>
    row.getAttribute("data-playbook"),
  );
}
/** The playbooks whose rows are highlighted right now. */
function highlightedRows(): (string | null)[] {
  return Array.from(
    playbooksTable().querySelectorAll("tr[data-playbook][data-highlighted]"),
  ).map((row) => row.getAttribute("data-playbook"));
}
const customerField = () => screen.getByRole("combobox", { name: "Customer" });
// The picker searches by name; the mock answers every term with Acme.
async function pickCustomer(name: RegExp): Promise<void> {
  fireEvent.click(customerField());
  fireEvent.change(await screen.findByPlaceholderText("Search customers"), {
    target: { value: "ac" },
  });
  fireEvent.click(await screen.findByText(name));
}
const ownerTab = (name: string) => screen.getByRole("tab", { name });
// Radix switches a tab on mouse down, not on click.
function chooseOwner(name: string): void {
  fireEvent.mouseDown(ownerTab(name), { button: 0 });
  fireEvent.click(ownerTab(name));
}

describe("OnboardingPlaybooks", () => {
  it("creates a use case in a dialog, then its default playbook", async () => {
    await renderPage();
    expect(await screen.findByText("No use cases yet.")).toBeTruthy();
    expect(screen.getByText("No playbooks yet.")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Create use case" }));
    const dialog = await screen.findByRole("form", { name: "New use case" });
    fireEvent.change(screen.getByRole("textbox", { name: "Use case name" }), {
      target: { value: "Distribution" },
    });
    // The slug follows the name and cannot be typed.
    const slug = screen.getByRole("textbox", {
      name: "Use case slug",
    }) as HTMLInputElement;
    expect(slug.value).toBe("distribution");
    expect(slug.disabled).toBe(true);
    fireEvent.change(
      screen.getByRole("textbox", { name: "Use case description" }),
      { target: { value: "Get MCP servers to every agent." } },
    );
    fireEvent.click(
      within(dialog).getByRole("button", { name: "Create use case" }),
    );
    await screen.findByText("No default playbook");
    expect(screen.queryByRole("form", { name: "New use case" })).toBeNull();
    expect(await posts()[0]!.clone().json()).toEqual({
      slug: "distribution",
      name: "Distribution",
      description: "Get MCP servers to every agent.",
    });

    // From the use case's row the owner is settled, so the form does not ask.
    openMenu(useCasesTable(), "Actions for Distribution");
    fireEvent.click(
      await screen.findByRole("menuitem", { name: "New playbook" }),
    );
    const form = await screen.findByRole("form", { name: "New playbook" });
    expect(form.textContent).toContain("For Distribution.");
    expect(screen.queryByRole("tablist", { name: "For" })).toBeNull();
    expect(screen.queryByRole("combobox", { name: "Use case" })).toBeNull();
    expect(screen.queryByRole("combobox", { name: "Customer" })).toBeNull();
    fireEvent.change(screen.getByRole("textbox", { name: "Playbook name" }), {
      target: { value: "Gateway first" },
    });
    // The first playbook of a use case is offered as its default.
    expect(
      screen
        .getByRole("checkbox", { name: "Default for this use case" })
        .getAttribute("aria-checked"),
    ).toBe("true");
    fireEvent.keyDown(screen.getByRole("combobox", { name: "Add a step" }), {
      key: "ArrowDown",
    });
    expect(
      screen.queryByRole("option", { name: "Connect identity provider" }),
    ).toBeNull();
    fireEvent.click(
      await screen.findByRole("option", { name: "Distribute MCP servers" }),
    );
    await pick("Add a step", /Set up Platform MCP/);
    fireEvent.click(
      screen.getByRole("button", { name: "Move Set up Platform MCP up" }),
    );
    fireEvent.click(screen.getByRole("button", { name: "Create playbook" }));
    await screen.findByRole("table", { name: "Playbooks" });
    expect(await posts().at(-1)!.clone().json()).toEqual({
      use_case_id: "uc-distribution",
      name: "Gateway first",
      description: "",
      is_default: true,
      step_slugs: ["platform-mcp", "mcp-distribution"],
    });
    // The use case names its new default; the Playbooks table files it under
    // the use case.
    expect(within(useCasesTable()).getByText("Gateway first")).toBeTruthy();
    const row = playbooksTable().querySelector('tr[data-playbook="pb-1"]');
    expect(row?.querySelectorAll("td")[1]?.textContent).toBe("Distribution");
    expect(
      within(row as HTMLElement).getByRole("img", { name: "Default playbook" }),
    ).toBeTruthy();
    expect(
      screen.getByText("Set up Platform MCP → Distribute MCP servers"),
    ).toBeTruthy();
    expect(screen.queryByRole("form")).toBeNull();
  });

  it("keeps the dialog open with the server's reason when a playbook is refused", async () => {
    useCases = [{ ...distribution, default_playbook_id: undefined }];
    rejectCreate = true;
    await renderPage();
    await screen.findByText("Distribution");
    fireEvent.click(screen.getByRole("button", { name: "New playbook" }));
    const form = await screen.findByRole("form", { name: "New playbook" });
    // From the table the form asks who it is for, starting with a use case.
    expect(form.textContent).toContain(
      "For a use case, shared, or for one customer.",
    );
    expect(ownerTab("Use case").getAttribute("aria-selected")).toBe("true");
    await pick("Use case", /Distribution/);
    fireEvent.change(screen.getByRole("textbox", { name: "Playbook name" }), {
      target: { value: "Broken" },
    });
    await pick("Add a step", /Set up Platform MCP/);
    fireEvent.click(screen.getByRole("button", { name: "Create playbook" }));
    const alert = await within(form).findByRole("alert");
    expect(alert.textContent).toContain("list the group instead");
  });

  it("lists every playbook, a use case's and a customer's, and makes another one the default", async () => {
    useCases = [structuredClone(distribution)];
    playbooks = [
      structuredClone(gatewayFirst),
      structuredClone(second),
      structuredClone(acmeOwn),
    ];
    await renderPage();
    await screen.findByRole("table", { name: "Playbooks" });
    expect(
      within(playbooksTable())
        .getAllByRole("columnheader")
        .map((cell) => cell.textContent),
    ).toEqual(["Name", "Applies to", "Steps", "Actions"]);
    expect(within(useCasesTable()).getByText("Gateway first")).toBeTruthy();
    expect(rowsOf(playbooksTable())).toEqual(["pb-1", "pb-2", "pb-acme"]);
    // A use case's playbook applies to the use case, a customer's to them.
    expect(
      rowsOf(playbooksTable()).map(
        (id) =>
          playbooksTable().querySelector(
            `tr[data-playbook="${id}"] td:nth-child(2)`,
          )?.textContent,
      ),
    ).toEqual(["Distribution", "Distribution", "Acme"]);
    // Unscoped, nobody is assigned, so nothing is highlighted.
    expect(highlightedRows()).toEqual([]);
    const first = playbooksTable().querySelector('tr[data-playbook="pb-1"]');
    expect(
      within(first as HTMLElement).getByRole("img", {
        name: "Default playbook",
      }),
    ).toBeTruthy();
    // Only a use case's playbook can become the default.
    openMenu(playbooksTable(), "Actions for Acme's way");
    await screen.findByRole("menuitem", { name: "Edit" });
    expect(screen.queryByRole("menuitem", { name: "Make default" })).toBeNull();
    fireEvent.keyDown(screen.getByRole("menu"), { key: "Escape" });

    openMenu(playbooksTable(), "Actions for Second");
    expect(screen.queryByRole("menuitem", { name: /Assign to/ })).toBeNull();
    fireEvent.click(
      await screen.findByRole("menuitem", { name: "Make default" }),
    );
    await screen.findByText("Second", { selector: "td:nth-child(3)" });
    expect(
      await postTo("/admin/onboarding.playbooks.update")[0]!.clone().json(),
    ).toEqual({
      playbook_id: "pb-2",
      name: "Second",
      description: "",
      is_default: true,
      step_slugs: ["mcp-distribution"],
    });
    const rows = playbooksTable().querySelectorAll("tr[data-playbook]");
    expect(
      within(rows[0] as HTMLElement).queryByRole("img", {
        name: "Default playbook",
      }),
    ).toBeNull();
    expect(
      within(rows[1] as HTMLElement).getByRole("img", {
        name: "Default playbook",
      }),
    ).toBeTruthy();
  });

  it("asks before deleting a playbook", async () => {
    useCases = [structuredClone(distribution)];
    playbooks = [structuredClone(gatewayFirst), structuredClone(second)];
    await renderPage();
    await screen.findByRole("table", { name: "Playbooks" });
    openMenu(playbooksTable(), "Actions for Second");
    fireEvent.click(await screen.findByRole("menuitem", { name: "Delete" }));
    const dialog = await screen.findByRole("dialog");
    expect(within(dialog).getByText("Delete Second?")).toBeTruthy();
    expect(postTo("/admin/onboarding.playbooks.delete")).toHaveLength(0);
    fireEvent.click(within(dialog).getByRole("button", { name: "Delete" }));
    await vi.waitFor(() => {
      expect(rowsOf(playbooksTable())).toEqual(["pb-1"]);
    });
    expect(
      await postTo("/admin/onboarding.playbooks.delete")[0]!.clone().json(),
    ).toEqual({ playbook_id: "pb-2" });
  });

  it("scoped to a customer, shows their playbooks, assigns a use case's, and writes them a new one", async () => {
    useCases = [structuredClone(distribution)];
    playbooks = [structuredClone(gatewayFirst), structuredClone(acmeOwn)];
    assignedId = "pb-acme";
    await renderPage("org_acme");
    await screen.findByRole("table", { name: "Playbooks" });
    expect(screen.getByText(/Scoped to Acme/)).toBeTruthy();
    expect(screen.getByRole("link", { name: "Show all" })).toBeTruthy();
    expect(rowsOf(playbooksTable())).toEqual(["pb-1", "pb-acme"]);
    // The assigned playbook's row is highlighted for a moment, no marker.
    expect(highlightedRows()).toEqual(["pb-acme"]);
    expect(screen.queryByRole("img", { name: "Assigned playbook" })).toBeNull();
    await vi.waitFor(() => expect(highlightedRows()).toEqual([]), {
      timeout: 4000,
    });

    // A use case's playbook can be assigned from its menu: Acme gets a copy
    // of its own, and the highlight lands on that copy.
    openMenu(playbooksTable(), "Actions for Gateway first");
    fireEvent.click(
      await screen.findByRole("menuitem", { name: "Assign to Acme" }),
    );
    await vi.waitFor(() => expect(highlightedRows()).toEqual(["pb-1-copy"]));
    expect(rowsOf(playbooksTable())).toEqual(["pb-1", "pb-acme", "pb-1-copy"]);
    const copied = playbooksTable().querySelector(
      'tr[data-playbook="pb-1-copy"]',
    );
    expect(copied?.querySelectorAll("td")[1]?.textContent).toBe("Acme");
    expect(
      await postTo("/admin/organization.onboardingPlaybook")[0]!.clone().json(),
    ).toEqual({ organization_id: "org_acme", playbook_id: "pb-1" });

    // A new playbook here starts as Acme's: no use case, no default, assigned
    // on save.
    fireEvent.click(screen.getByRole("button", { name: "New playbook" }));
    await screen.findByRole("form", { name: "New playbook" });
    expect(ownerTab("Customer").getAttribute("aria-selected")).toBe("true");
    expect(customerField().textContent).toContain("Acme");
    expect(screen.queryByRole("combobox", { name: "Use case" })).toBeNull();
    expect(
      screen.queryByRole("checkbox", { name: "Default for this use case" }),
    ).toBeNull();
    fireEvent.change(screen.getByRole("textbox", { name: "Playbook name" }), {
      target: { value: "Acme again" },
    });
    await pick("Add a step", /Distribute MCP servers/);
    fireEvent.click(screen.getByRole("button", { name: "Create playbook" }));
    await vi.waitFor(() => {
      expect(rowsOf(playbooksTable())).toEqual([
        "pb-1",
        "pb-acme",
        "pb-1-copy",
        "pb-4",
      ]);
    });
    expect(
      await postTo("/admin/onboarding.playbooks.create")[0]!.clone().json(),
    ).toEqual({
      organization_id: "org_acme",
      name: "Acme again",
      description: "",
      is_default: false,
      step_slugs: ["mcp-distribution"],
    });
    expect(
      await postTo("/admin/organization.onboardingPlaybook")
        .at(-1)!
        .clone()
        .json(),
    ).toEqual({ organization_id: "org_acme", playbook_id: "pb-4" });
    const created = playbooksTable().querySelector('tr[data-playbook="pb-4"]');
    expect(created?.querySelectorAll("td")[1]?.textContent).toBe("Acme");
    expect(highlightedRows()).toEqual(["pb-4"]);
  });

  it("writes a customer a playbook from the form, unscoped", async () => {
    useCases = [structuredClone(distribution)];
    playbooks = [structuredClone(gatewayFirst)];
    await renderPage();
    await screen.findByRole("table", { name: "Playbooks" });
    fireEvent.click(screen.getByRole("button", { name: "New playbook" }));
    await screen.findByRole("form", { name: "New playbook" });
    expect(
      screen.getByRole("checkbox", { name: "Default for this use case" }),
    ).toBeTruthy();
    chooseOwner("Customer");
    expect(screen.queryByRole("combobox", { name: "Use case" })).toBeNull();
    expect(customerField().textContent).toContain("Choose a customer");
    // A customer's playbook cannot be a default.
    expect(
      screen.queryByRole("checkbox", { name: "Default for this use case" }),
    ).toBeNull();
    await pickCustomer(/^Acme$/);
    expect(customerField().textContent).toContain("Acme");
    expect(mocks.listOrganizations).toHaveBeenCalledWith(
      expect.objectContaining({ q: "ac" }),
    );
    fireEvent.change(screen.getByRole("textbox", { name: "Playbook name" }), {
      target: { value: "For Acme" },
    });
    await pick("Add a step", /Set up Platform MCP/);
    fireEvent.click(screen.getByRole("button", { name: "Create playbook" }));
    await vi.waitFor(() => {
      expect(rowsOf(playbooksTable())).toEqual(["pb-1", "pb-2"]);
    });
    expect(
      await postTo("/admin/onboarding.playbooks.create")[0]!.clone().json(),
    ).toEqual({
      organization_id: "org_acme",
      name: "For Acme",
      description: "",
      is_default: false,
      step_slugs: ["platform-mcp"],
    });
    expect(
      await postTo("/admin/organization.onboardingPlaybook")[0]!.clone().json(),
    ).toEqual({ organization_id: "org_acme", playbook_id: "pb-2" });
    const created = playbooksTable().querySelector('tr[data-playbook="pb-2"]');
    expect(created?.querySelectorAll("td")[1]?.textContent).toBe("Acme");
  });

  it("keeps nothing when the stack refuses a customer's new playbook", async () => {
    useCases = [structuredClone(distribution)];
    playbooks = [structuredClone(gatewayFirst)];
    await renderPage("org_acme");
    await screen.findByRole("table", { name: "Playbooks" });
    fireEvent.click(screen.getByRole("button", { name: "New playbook" }));
    const form = await screen.findByRole("form", { name: "New playbook" });
    fireEvent.change(screen.getByRole("textbox", { name: "Playbook name" }), {
      target: { value: "Anthropic only" },
    });
    await pick("Add a step", /Set up Anthropic observability/);
    fireEvent.click(screen.getByRole("button", { name: "Create playbook" }));
    const alert = await within(form).findByRole("alert");
    expect(alert.textContent).toContain("needs Anthropic in the stack");
    expect(
      await postTo("/admin/onboarding.playbooks.delete")[0]!.clone().json(),
    ).toEqual({ playbook_id: "pb-2" });
    expect(playbooks.map((p) => p.id)).toEqual(["pb-1"]);
  });
});
