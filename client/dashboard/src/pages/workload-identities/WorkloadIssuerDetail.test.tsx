import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { WorkloadAdmission } from "@gram/client/models/components/workloadadmission.js";
import type { ReactNode } from "react";
import { MemoryRouter, Route, Routes } from "react-router";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { WorkloadIssuerDetailPage } from "./WorkloadIssuerDetail";

vi.mock("@/components/require-scope", () => ({
  RequireScope: ({ children }: { children: ReactNode }) => <>{children}</>,
}));
vi.mock("@/components/page-templates", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/components/page-templates")>()),
  ResourceListPage: ({
    children,
    title,
    description,
    belowHeader,
    primaryAction,
  }: {
    children: ReactNode;
    title: string;
    description?: string;
    belowHeader?: ReactNode;
    primaryAction?: ReactNode;
  }) => (
    <>
      <h1>{title}</h1>
      {description && <p data-testid="description">{description}</p>}
      <div data-testid="actions">{primaryAction}</div>
      {belowHeader}
      {children}
    </>
  ),
}));
vi.mock("@/routes", () => ({
  useOrgRoutes: () => ({
    workloadIssuers: { href: () => "/access-hub" },
  }),
}));

const ISSUER_ID = "11111111-1111-1111-1111-111111111111";

const issuer = {
  id: ISSUER_ID,
  organizationId: "example-org",
  projectId: "",
  name: "Example CI",
  issuer: "https://ci-identity.example.com",
  jwksUri: "https://ci-identity.example.com/jwks",
  description: "Deploy jobs for the main repository",
  allowWildcardAdmission: false,
  tags: [],
  createdAt: new Date("2026-09-25T00:00:00Z"),
  updatedAt: new Date("2026-09-25T00:00:00Z"),
};

function admission(index: number, tags: string[] = []): WorkloadAdmission {
  const label = `machine-${String(index).padStart(2, "0")}`;
  return {
    id: `admission-${index}`,
    organizationId: "example-org",
    projectId: "",
    workloadIssuerId: ISSUER_ID,
    issuer: issuer.issuer,
    issuerName: issuer.name,
    subject: `repo:example/${label}`,
    matchKind: "exact",
    name: label,
    tags,
    agentId: "22222222-2222-2222-2222-222222222222",
    agentName: "Release assistant",
    wildcardActive: true,
    createdAt: new Date("2026-09-28T00:00:00Z"),
    updatedAt: new Date("2026-09-28T00:00:00Z"),
  };
}

let admissions: WorkloadAdmission[] = [];

vi.mock("@gram/client/react-query/workloadIdentities.js", () => ({
  useWorkloadIdentities: () => ({
    data: { issuers: [issuer], admissions },
    isPending: false,
    isError: false,
    refetch: vi.fn(),
  }),
  invalidateAllWorkloadIdentities: vi.fn(),
}));
vi.mock("@gram/client/react-query/agents.js", () => ({
  useAgents: () => ({
    data: [
      {
        id: "22222222-2222-2222-2222-222222222222",
        name: "Release assistant",
        lifecycle: "active",
      },
    ],
    isPending: false,
    isError: false,
  }),
}));
vi.mock("@gram/client/react-query/admitWorkloadSubject.js", () => ({
  useAdmitWorkloadSubjectMutation: () => ({
    mutate: vi.fn(),
    isPending: false,
  }),
}));
const updateIssuer = vi.fn();
vi.mock("@gram/client/react-query/updateWorkloadIssuer.js", () => ({
  useUpdateWorkloadIssuerMutation: () => ({
    mutate: updateIssuer,
    isPending: false,
  }),
}));
const updateSubject = vi.fn();
vi.mock("@gram/client/react-query/updateWorkloadSubject.js", () => ({
  useUpdateWorkloadSubjectMutation: () => ({
    mutate: updateSubject,
    isPending: false,
  }),
}));
vi.mock("@gram/client/react-query/withdrawWorkloadIssuer.js", () => ({
  useWithdrawWorkloadIssuerMutation: () => ({
    mutate: vi.fn(),
    isPending: false,
  }),
}));
vi.mock("@gram/client/react-query/withdrawWorkloadSubject.js", () => ({
  useWithdrawWorkloadSubjectMutation: () => ({
    mutate: vi.fn(),
    isPending: false,
  }),
}));

beforeEach(() => {
  admissions = [];
  updateIssuer.mockReset();
  updateSubject.mockReset();
});
afterEach(cleanup);

function renderPage() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  render(
    <QueryClientProvider client={client}>
      <MemoryRouter initialEntries={[`/access-hub/${ISSUER_ID}`]}>
        <Routes>
          <Route
            path="/access-hub/:issuerId"
            element={<WorkloadIssuerDetailPage />}
          />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

function visibleMachines(): string[] {
  return screen
    .queryAllByText(/^machine-\d+$/)
    .map((node) => node.textContent ?? "");
}

it("keeps the issuer URL on its own labeled line, apart from the description", () => {
  renderPage();

  const description = screen.getByTestId("description");
  expect(description.textContent).toBe("Deploy jobs for the main repository");
  expect(screen.getByText("Issuer URL")).toBeTruthy();
  expect(screen.getByText("https://ci-identity.example.com")).toBeTruthy();
});

it("gives tags a column of their own", () => {
  admissions = [admission(1, ["production"])];
  renderPage();

  expect(screen.getByText("Tags")).toBeTruthy();
  const tag = screen.getByText("production");
  const subject = screen.getByText("repo:example/machine-01");
  // The subject's cell does not also hold the tags.
  expect(subject.parentElement?.contains(tag)).toBe(false);
});

// Opens a machine row's menu and picks one of its actions.
function chooseMachineAction(action: "Edit" | "Remove", row = 0): void {
  const trigger = screen.getAllByRole("button", { name: /^Actions for / })[row];
  if (trigger === undefined) {
    throw new Error(`no machine row ${row}`);
  }
  fireEvent.pointerDown(trigger, { button: 0, ctrlKey: false });
  fireEvent.click(screen.getByRole("menuitem", { name: action }));
}

it("offers to allow access and to remove a machine", () => {
  admissions = [admission(1)];
  renderPage();

  expect(screen.getByRole("button", { name: "Allow access" })).toBeTruthy();
  expect(screen.queryByRole("button", { name: "Withdraw" })).toBeNull();

  chooseMachineAction("Remove");
  expect(screen.getByText("Remove this machine's access?")).toBeTruthy();
});

it("puts a machine's Edit and Remove in its row menu", () => {
  admissions = [admission(1)];
  renderPage();

  const trigger = screen.getByRole("button", {
    name: "Actions for machine-01",
  });
  fireEvent.pointerDown(trigger, { button: 0, ctrlKey: false });

  expect(
    screen.getAllByRole("menuitem").map((item) => item.textContent),
  ).toEqual(["Edit", "Remove"]);
});

it("shows ten machines a page", () => {
  admissions = Array.from({ length: 12 }, (_, i) => admission(i + 1));
  renderPage();

  expect(visibleMachines()).toHaveLength(10);
  expect(screen.getByText("1–10 of 12")).toBeTruthy();

  fireEvent.click(screen.getByRole("button", { name: "Next page" }));

  expect(visibleMachines()).toEqual(["machine-11", "machine-12"]);
  expect(screen.getByText("11–12 of 12")).toBeTruthy();
});

it("returns to the first page when the search changes", async () => {
  admissions = Array.from({ length: 12 }, (_, i) => admission(i + 1));
  renderPage();

  fireEvent.click(screen.getByRole("button", { name: "Next page" }));
  expect(visibleMachines()).toEqual(["machine-11", "machine-12"]);

  fireEvent.change(
    screen.getByPlaceholderText("Search subject, label, tag or agent…"),
    { target: { value: "machine-0" } },
  );

  await waitFor(() => expect(visibleMachines()).toHaveLength(9));
  expect(visibleMachines()[0]).toBe("machine-01");
  expect(screen.queryByRole("button", { name: "Next page" })).toBeNull();
});

it("puts stop trusting in its own section below the machines", () => {
  admissions = [admission(1)];
  renderPage();

  const actions = screen.getByTestId("actions");
  expect(actions.textContent).not.toContain("Stop trusting");

  const heading = screen.getByText("Stop trusting this platform");
  const machine = screen.getByText("machine-01");
  expect(
    machine.compareDocumentPosition(heading) & Node.DOCUMENT_POSITION_FOLLOWING,
  ).toBeTruthy();

  fireEvent.click(screen.getByRole("button", { name: "Stop trusting" }));
  expect(screen.getByText("Stop trusting this platform?")).toBeTruthy();
});

it("opens the edit sheet prefilled, with the issuer URL read-only", () => {
  renderPage();

  fireEvent.click(screen.getByRole("button", { name: "Edit" }));

  expect(screen.getByText("Edit platform")).toBeTruthy();
  expect((screen.getByLabelText("Name") as HTMLInputElement).value).toBe(
    "Example CI",
  );
  expect(
    (screen.getByLabelText("Description") as HTMLTextAreaElement).value,
  ).toBe("Deploy jobs for the main repository");
  expect((screen.getByLabelText("JWKS URI") as HTMLInputElement).value).toBe(
    "https://ci-identity.example.com/jwks",
  );

  const issuerUrl = screen.getByLabelText("Issuer") as HTMLInputElement;
  expect(issuerUrl.value).toBe("https://ci-identity.example.com");
  expect(issuerUrl.readOnly).toBe(true);
});

it("saves nothing until a field changes", () => {
  renderPage();

  fireEvent.click(screen.getByRole("button", { name: "Edit" }));

  const save = screen.getByRole("button", {
    name: "Save changes",
  }) as HTMLButtonElement;
  expect(save.disabled).toBe(true);
});

it("sends only the fields the edit changed", () => {
  renderPage();

  fireEvent.click(screen.getByRole("button", { name: "Edit" }));
  fireEvent.change(screen.getByLabelText("Name"), {
    target: { value: "  Example deploys  " },
  });
  fireEvent.change(screen.getByLabelText("Description"), {
    target: { value: "" },
  });
  fireEvent.click(screen.getByRole("button", { name: "Save changes" }));

  expect(updateIssuer).toHaveBeenCalledTimes(1);
  expect(updateIssuer).toHaveBeenCalledWith({
    request: {
      updateWorkloadIssuerForm: {
        id: ISSUER_ID,
        name: "Example deploys",
        description: "",
      },
    },
  });
});

it("opens the machine edit sheet prefilled, with the subject read-only", () => {
  admissions = [admission(1, ["production"])];
  renderPage();

  chooseMachineAction("Edit");

  expect(screen.getByText("Edit access")).toBeTruthy();
  expect(
    (screen.getByLabelText("Label (optional)") as HTMLInputElement).value,
  ).toBe("machine-01");
  // Once in the machine's row and once in the sheet's tag field.
  expect(screen.getAllByText("production")).toHaveLength(2);

  const subject = screen.getByLabelText("Subject") as HTMLInputElement;
  expect(subject.value).toBe("repo:example/machine-01");
  expect(subject.readOnly).toBe(true);
});

it("saves no machine edit until a field changes", () => {
  admissions = [admission(1)];
  renderPage();

  chooseMachineAction("Edit");

  const save = screen.getByRole("button", {
    name: "Save changes",
  }) as HTMLButtonElement;
  expect(save.disabled).toBe(true);

  fireEvent.change(screen.getByLabelText("Label (optional)"), {
    target: { value: "Release bot" },
  });
  expect(save.disabled).toBe(false);
});

it("sends only the machine fields the edit changed", () => {
  admissions = [admission(1)];
  renderPage();

  chooseMachineAction("Edit");
  fireEvent.change(screen.getByLabelText("Label (optional)"), {
    target: { value: "  Release bot  " },
  });
  fireEvent.click(screen.getByRole("button", { name: "Save changes" }));

  expect(updateSubject).toHaveBeenCalledTimes(1);
  expect(updateSubject).toHaveBeenCalledWith({
    request: {
      updateWorkloadSubjectForm: {
        id: "admission-1",
        name: "Release bot",
      },
    },
  });
});

it("keeps the wildcard caution on a wildcard machine being edited", () => {
  admissions = [
    {
      ...admission(1),
      subject: "repo:example/*",
      matchKind: "wildcard",
      wildcardActive: false,
    },
  ];
  renderPage();

  chooseMachineAction("Edit");

  expect(
    screen.getByText("This rule admits more than one identity"),
  ).toBeTruthy();
  // The subject is fixed, so the inactive-wildcard refusal meant for a new rule
  // does not block saving the label.
  fireEvent.change(screen.getByLabelText("Label (optional)"), {
    target: { value: "Every repository" },
  });
  expect(
    (screen.getByRole("button", { name: "Save changes" }) as HTMLButtonElement)
      .disabled,
  ).toBe(false);
});

it("opens each machine's own values when editing one after another", () => {
  admissions = [admission(1), admission(2)];
  renderPage();

  chooseMachineAction("Edit", 0);
  expect(
    (screen.getByLabelText("Label (optional)") as HTMLInputElement).value,
  ).toBe("machine-01");
  fireEvent.click(screen.getByRole("button", { name: "Cancel" }));

  chooseMachineAction("Edit", 1);
  expect(
    (screen.getByLabelText("Label (optional)") as HTMLInputElement).value,
  ).toBe("machine-02");
});
