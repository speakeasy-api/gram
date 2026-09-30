import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { WorkloadAdmission } from "@gram/client/models/components/workloadadmission.js";
import type { ReactNode } from "react";
import { Link, MemoryRouter, Route, Routes } from "react-router";
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
  useRoutes: () => ({
    workloadIssuers: { href: () => "/access-hub" },
  }),
}));

const mocks = vi.hoisted(() => ({
  invalidate: vi.fn(),
  toastSuccess: vi.fn(),
  toastError: vi.fn(),
  updateIssuer: vi.fn(),
  updateOptions: {} as {
    onSuccess?: () => Promise<void> | void;
    onError?: (error: unknown) => void;
  },
}));

vi.mock("sonner", () => ({
  toast: { success: mocks.toastSuccess, error: mocks.toastError },
}));

const ISSUER_ID = "11111111-1111-1111-1111-111111111111";
const OTHER_ISSUER_ID = "33333333-3333-3333-3333-333333333333";

const baseIssuer = {
  id: ISSUER_ID,
  organizationId: "example-org",
  projectId: "",
  name: "Example CI",
  issuer: "https://ci-identity.example.com",
  jwksUri: "https://ci-identity.example.com/jwks",
  description: "Deploy jobs for the main repository",
  allowWildcardAdmission: false,
  tags: [] as string[],
  createdAt: new Date("2026-09-25T00:00:00Z"),
  updatedAt: new Date("2026-09-25T00:00:00Z"),
};

let issuer = baseIssuer;

const otherIssuer = {
  ...baseIssuer,
  id: OTHER_ISSUER_ID,
  name: "Other CI",
  issuer: "https://other-ci-identity.example.com",
  jwksUri: "https://other-ci-identity.example.com/jwks",
};

function admission(
  index: number,
  tags: string[] = [],
  workloadIssuerId: string = ISSUER_ID,
): WorkloadAdmission {
  const label = `machine-${String(index).padStart(2, "0")}`;
  const platform = workloadIssuerId === OTHER_ISSUER_ID ? otherIssuer : issuer;
  return {
    id: `admission-${workloadIssuerId}-${index}`,
    organizationId: "example-org",
    projectId: "",
    workloadIssuerId,
    issuer: platform.issuer,
    issuerName: platform.name,
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
    data: { issuers: [issuer, otherIssuer], admissions },
    isPending: false,
    isError: false,
    refetch: vi.fn(),
  }),
  invalidateAllWorkloadIdentities: mocks.invalidate,
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
vi.mock("@gram/client/react-query/updateWorkloadIssuer.js", () => ({
  useUpdateWorkloadIssuerMutation: (
    options: typeof mocks.updateOptions = {},
  ) => {
    mocks.updateOptions = options;
    return { mutate: mocks.updateIssuer, isPending: false };
  },
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
  issuer = baseIssuer;
  mocks.updateIssuer.mockReset();
  mocks.invalidate.mockReset();
  mocks.toastSuccess.mockReset();
  mocks.toastError.mockReset();
  mocks.updateOptions = {};
});
afterEach(cleanup);

function renderPage() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  const page = () => (
    <QueryClientProvider client={client}>
      <MemoryRouter initialEntries={[`/access-hub/${ISSUER_ID}`]}>
        <Link to={`/access-hub/${OTHER_ISSUER_ID}`}>Other platform</Link>
        <Routes>
          <Route
            path="/access-hub/:issuerId"
            element={<WorkloadIssuerDetailPage />}
          />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>
  );
  const { rerender } = render(page());
  return { rerender: () => rerender(page()) };
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

it("offers to allow access and to remove a machine", () => {
  admissions = [admission(1)];
  renderPage();

  expect(screen.getByRole("button", { name: "Allow access" })).toBeTruthy();
  expect(screen.queryByRole("button", { name: "Withdraw" })).toBeNull();

  fireEvent.click(screen.getByRole("button", { name: "Remove" }));
  expect(screen.getByText("Remove this machine's access?")).toBeTruthy();
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

it("starts each platform on the first page", () => {
  admissions = [
    ...Array.from({ length: 12 }, (_, i) => admission(i + 1)),
    ...Array.from({ length: 12 }, (_, i) =>
      admission(i + 1, [], OTHER_ISSUER_ID),
    ),
  ];
  renderPage();

  fireEvent.click(screen.getByRole("button", { name: "Next page" }));
  expect(screen.getByText("11–12 of 12")).toBeTruthy();

  fireEvent.click(screen.getByRole("link", { name: "Other platform" }));

  expect(screen.getByText("Other CI")).toBeTruthy();
  expect(screen.getByText("1–10 of 12")).toBeTruthy();
  expect(visibleMachines()[0]).toBe("machine-01");
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

  expect(mocks.updateIssuer).toHaveBeenCalledTimes(1);
  expect(mocks.updateIssuer).toHaveBeenCalledWith({
    request: {
      updateWorkloadIssuerForm: {
        id: ISSUER_ID,
        name: "Example deploys",
        description: "",
      },
    },
  });
});

it("repoints the JWKS URI and clears the tags", () => {
  issuer = { ...baseIssuer, tags: ["deploys"] };
  renderPage();

  fireEvent.click(screen.getByRole("button", { name: "Edit" }));
  fireEvent.change(screen.getByLabelText("JWKS URI"), {
    target: { value: " https://keys.example.com/jwks " },
  });
  fireEvent.click(screen.getByRole("button", { name: "Remove deploys" }));
  fireEvent.click(screen.getByRole("button", { name: "Save changes" }));

  expect(mocks.updateIssuer).toHaveBeenCalledWith({
    request: {
      updateWorkloadIssuerForm: {
        id: ISSUER_ID,
        jwksUri: "https://keys.example.com/jwks",
        tags: [],
      },
    },
  });
});

it("diffs against the platform as it stood when the sheet opened", () => {
  const { rerender } = renderPage();

  fireEvent.click(screen.getByRole("button", { name: "Edit" }));
  fireEvent.change(screen.getByLabelText("Name"), {
    target: { value: "Example deploys" },
  });

  // Another operator's edit arrives through a refresh while the sheet is open.
  issuer = {
    ...baseIssuer,
    description: "Updated elsewhere",
    jwksUri: "https://ci-identity.example.com/keys",
  };
  rerender();

  fireEvent.click(screen.getByRole("button", { name: "Save changes" }));

  expect(mocks.updateIssuer).toHaveBeenCalledWith({
    request: {
      updateWorkloadIssuerForm: { id: ISSUER_ID, name: "Example deploys" },
    },
  });
});

it("refreshes, closes the sheet and confirms once the edit saves", async () => {
  renderPage();

  fireEvent.click(screen.getByRole("button", { name: "Edit" }));
  fireEvent.change(screen.getByLabelText("Name"), {
    target: { value: "Example deploys" },
  });
  fireEvent.click(screen.getByRole("button", { name: "Save changes" }));

  await act(async () => {
    await mocks.updateOptions.onSuccess?.();
  });

  expect(mocks.invalidate).toHaveBeenCalledTimes(1);
  expect(mocks.toastSuccess).toHaveBeenCalledWith("Platform updated");
  await waitFor(() => expect(screen.queryByText("Edit platform")).toBeNull());
});
