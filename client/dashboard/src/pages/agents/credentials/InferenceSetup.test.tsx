import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { InferenceSetup } from "./InferenceSetup";
import { inferencePolicyGrants } from "./inferenceSetupInstructions";

const mocks = vi.hoisted(() => ({
  create: vi.fn(),
  register: vi.fn(),
  admit: vi.fn(),
  workloadWrite: true,
  inferenceProject: "project_one",
  projectWrite: true,
  projectId: "project_one",
  serverURL: "https://api.example.test",
  emptyEndpoints: false,
  admin: false,
  emptyIssuers: false,
  includeScopedIssuers: false,
  duplicateProject: null as string | null,
}));
vi.mock("@/lib/utils", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/lib/utils")>()),
  getServerURL: () => mocks.serverURL,
}));
vi.mock("@/contexts/Auth", () => ({
  useOrganization: () => ({ id: "org_example" }),
  useProject: () => ({
    id: mocks.projectId,
    slug: mocks.projectId,
    name: mocks.projectId === "project_one" ? "Project one" : "Project two",
  }),
  useSession: () => ({ user: { id: "owner" } }),
}));
vi.mock("@/contexts/Sdk", () => ({
  useSdkClient: () => ({ agents: { create: mocks.create } }),
  useSlugs: () => ({ orgSlug: "example", projectSlug: "project_one" }),
}));
vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({
    hasScope: (scope: string, resourceId?: string, projectId?: string) => {
      if (scope === "org:admin")
        return mocks.admin && resourceId === "org_example";
      if (scope === "workload:write") return mocks.workloadWrite;
      if (scope === "inference:execute")
        return resourceId === "*" && projectId === mocks.inferenceProject;
      if (scope === "project:write")
        return mocks.projectWrite && resourceId === mocks.projectId;
      return false;
    },
  }),
}));
vi.mock("../agent-policy-grants", () => ({ invalidateAgentPolicy: vi.fn() }));
const issuer = {
  id: "issuer_one",
  name: "Workload issuer",
  organizationId: "org_example",
  projectId: "",
  issuer: "https://issuer.example.test",
  jwksUri: "https://issuer.example.test/jwks",
  tags: [],
  allowAssertionReuse: false,
};
vi.mock("@gram/client/react-query/workloadIdentities.js", async () => {
  const { useQuery } = await import("@tanstack/react-query");
  const scopedIssuers = () =>
    mocks.includeScopedIssuers
      ? [
          {
            ...issuer,
            id: "issuer_project",
            name: "Project issuer",
            projectId: "project_one",
          },
          {
            ...issuer,
            id: "issuer_other_project",
            name: "Other project issuer",
            projectId: "project_other",
          },
          {
            ...issuer,
            id: "issuer_other_org",
            name: "Other organization issuer",
            organizationId: "org_other",
          },
        ]
      : [];
  const admissions = () =>
    mocks.duplicateProject === null
      ? []
      : [
          {
            id: "admission_existing",
            issuer: issuer.issuer,
            subject: "workload:worker",
            matchKind: "exact",
            projectId: mocks.duplicateProject,
            organizationId: "org_example",
            agentId: "agent_existing",
            agentName: "Existing worker",
          },
        ];
  const queryKeyWorkloadIdentities = (request: unknown) => [
    "workload-identities",
    request,
  ];
  return {
    queryKeyWorkloadIdentities,
    useWorkloadIdentities: (request: unknown) =>
      useQuery({
        queryKey: queryKeyWorkloadIdentities(request),
        queryFn: async () => ({
          issuers: mocks.emptyIssuers ? [] : [issuer, ...scopedIssuers()],
          admissions: admissions(),
        }),
        initialData: {
          issuers: mocks.emptyIssuers ? [] : [issuer, ...scopedIssuers()],
          admissions: admissions(),
        },
        staleTime: Infinity,
      }),
    invalidateAllWorkloadIdentities: vi.fn(),
  };
});
vi.mock("@gram/client/react-query/registerWorkloadIssuer.js", () => ({
  useRegisterWorkloadIssuerMutation: () => ({ mutateAsync: mocks.register }),
}));
vi.mock("@gram/client/react-query/admitWorkloadSubject.js", () => ({
  useAdmitWorkloadSubjectMutation: () => ({ mutateAsync: mocks.admit }),
}));
vi.mock("@gram/client/react-query/withdrawWorkloadSubject.js", () => ({
  useWithdrawWorkloadSubjectMutation: () => ({ mutateAsync: vi.fn() }),
}));
vi.mock("@gram/client/react-query/workloadTokenEndpoints.js", () => ({
  useWorkloadTokenEndpoints: () => ({
    data: {
      items: mocks.emptyEndpoints
        ? []
        : [
            {
              userSessionIssuerId: "usi_one",
              userSessionIssuerSlug: "shared",
              projectId: "",
              projectName: "",
              issuer: "https://auth.example.test/oauth/usi/usi_one",
              tokenEndpoint:
                "https://auth.example.test/oauth/usi/usi_one/token",
              mcpHost: "https://mcp.example.test",
            },
          ],
    },
    isPending: false,
    isError: false,
  }),
}));

function setup() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  const onDone = vi.fn<() => void>();
  const ui = () => (
    <MemoryRouter>
      <QueryClientProvider client={client}>
        <InferenceSetup onDone={onDone} />
      </QueryClientProvider>
    </MemoryRouter>
  );
  const view = render(ui());
  return { ...view, refreshProject: () => view.rerender(ui()) };
}

function fill() {
  fireEvent.change(screen.getByLabelText("Agent identity name"), {
    target: { value: "Inference worker" },
  });
  fireEvent.change(screen.getByLabelText("Workload issuer"), {
    target: { value: "issuer_one" },
  });
  fireEvent.change(screen.getByLabelText("Subject"), {
    target: { value: "workload:worker" },
  });
}
afterEach(cleanup);
beforeEach(() => {
  vi.resetAllMocks();
  mocks.workloadWrite = true;
  mocks.inferenceProject = "project_one";
  mocks.projectWrite = true;
  mocks.projectId = "project_one";
  mocks.serverURL = "https://api.example.test";
  mocks.emptyEndpoints = false;
  mocks.admin = false;
  mocks.emptyIssuers = false;
  mocks.includeScopedIssuers = false;
  mocks.duplicateProject = null;
  mocks.create.mockResolvedValue({ id: "agent_one" });
  mocks.register.mockResolvedValue({
    issuers: [{ ...issuer, projectId: "project_one" }],
    admissions: [],
  });
  mocks.admit.mockResolvedValue({});
});
describe("inference workload setup", () => {
  it("requires an explicit project and grants only inference", () => {
    expect(() => inferencePolicyGrants("")).toThrow("Select a project");
    expect(inferencePolicyGrants("project_one")).toEqual([
      {
        effect: "allow",
        scope: "inference:execute",
        selector: {
          resourceKind: "inference",
          resourceId: "*",
          projectId: "project_one",
        },
      },
    ]);
  });
  it("selects existing issuer trust with opt-in, then shows inference-only exchange guidance", async () => {
    setup();
    fill();
    expect(
      (
        screen.getByRole("button", {
          name: "Create inference identity",
        }) as HTMLButtonElement
      ).disabled,
    ).toBe(true);
    fireEvent.click(
      screen.getByRole("checkbox", {
        name: "Allow inference:execute in Project one. No MCP or administration permissions.",
      }),
    );
    fireEvent.click(
      screen.getByRole("button", { name: "Create inference identity" }),
    );
    await screen.findByRole("status");
    expect(mocks.create).toHaveBeenCalledWith({
      createAgentForm: {
        name: "Inference worker",
        projectId: "project_one",
        policyGrants: inferencePolicyGrants("project_one"),
      },
    });
    expect(mocks.register).not.toHaveBeenCalled();
    expect(mocks.admit).toHaveBeenCalledWith({
      request: {
        gramProject: mocks.projectId,
        admitWorkloadSubjectForm: {
          projectScoped: false,
          issuer: "https://issuer.example.test",
          subject: "workload:worker",
          matchKind: "exact",
          agentId: "agent_one",
          tags: [],
        },
      },
    });
    expect(
      screen.getByText(/grant_type=urn:ietf:params:oauth:grant-type:jwt-bearer/)
        .textContent,
    ).toContain("resource=urn:speakeasy:inference\nproject_id=project_one");
    expect(
      screen.getByText("Speakeasy issuer / external JWT audience"),
    ).toBeTruthy();
    expect(screen.getByText("POST chat completions")).toBeTruthy();
    expect(screen.queryByText("MCP host")).toBeNull();
  });
  it("does not create identities when permission is absent", () => {
    mocks.workloadWrite = false;
    setup();
    expect(screen.getByRole("alert").textContent).toContain(
      "explicit inference:execute",
    );
    expect(
      (
        screen.getByRole("button", {
          name: "Create inference identity",
        }) as HTMLButtonElement
      ).disabled,
    ).toBe(true);
    expect(mocks.create).not.toHaveBeenCalled();
  });
  it("retries a failed admission without creating another identity", async () => {
    mocks.admit.mockRejectedValueOnce(new Error("Admission unavailable"));
    setup();
    fill();
    fireEvent.click(
      screen.getByRole("checkbox", {
        name: "Allow inference:execute in Project one. No MCP or administration permissions.",
      }),
    );
    fireEvent.click(
      screen.getByRole("button", { name: "Create inference identity" }),
    );
    await screen.findByRole("alert");
    await waitFor(() =>
      expect(
        (
          screen.getByRole("button", {
            name: "Create inference identity",
          }) as HTMLButtonElement
        ).disabled,
      ).toBe(false),
    );
    fireEvent.click(
      screen.getByRole("button", { name: "Create inference identity" }),
    );
    await waitFor(() => expect(mocks.admit).toHaveBeenCalledTimes(2));
    expect(mocks.create).toHaveBeenCalledTimes(1);
    expect(mocks.register).not.toHaveBeenCalled();
  });
});

it("links administrators without inference access to owner role configuration", () => {
  mocks.workloadWrite = false;
  mocks.inferenceProject = "";
  mocks.admin = true;
  setup();
  expect(
    screen
      .getByRole("link", { name: "open Roles & Permissions" })
      .getAttribute("href"),
  ).toBe("/example/access/roles");
  expect(screen.getByRole("alert").textContent).toContain(
    "assign the role to the agent owner",
  );
  expect(mocks.create).not.toHaveBeenCalled();
});

it("requires an eligible exchange issuer before creating an inference identity", () => {
  mocks.emptyEndpoints = true;
  setup();
  fill();
  fireEvent.click(
    screen.getByRole("checkbox", {
      name: "Allow inference:execute in Project one. No MCP or administration permissions.",
    }),
  );
  expect(
    screen.getByText(
      "No shared Speakeasy authorization server is available for this project or organization.",
    ),
  ).toBeTruthy();
  expect(
    (
      screen.getByRole("button", {
        name: "Create inference identity",
      }) as HTMLButtonElement
    ).disabled,
  ).toBe(true);
  expect(mocks.create).not.toHaveBeenCalled();
});

it.each([
  {
    workloadWrite: false,
    inferenceProject: "project_one",
    reason: "missing workload trust permission",
  },
  {
    workloadWrite: true,
    inferenceProject: "",
    reason: "missing inference permission",
  },
  {
    workloadWrite: true,
    inferenceProject: "other_project",
    reason: "inference permission for another project",
  },
])("blocks mutation with $reason", ({ workloadWrite, inferenceProject }) => {
  mocks.workloadWrite = workloadWrite;
  mocks.inferenceProject = inferenceProject;
  setup();
  expect(
    (
      screen.getByRole("button", {
        name: "Create inference identity",
      }) as HTMLButtonElement
    ).disabled,
  ).toBe(true);
  expect(mocks.create).not.toHaveBeenCalled();
});
it("does not require project:write to use an existing exchange authorization server", async () => {
  mocks.projectWrite = false;
  setup();
  fill();
  fireEvent.click(
    screen.getByRole("checkbox", {
      name: "Allow inference:execute in Project one. No MCP or administration permissions.",
    }),
  );
  fireEvent.click(
    screen.getByRole("button", { name: "Create inference identity" }),
  );
  await screen.findByRole("status");
  expect(mocks.create).toHaveBeenCalledTimes(1);
});
it("blocks provisioning when the configured inference destination is plaintext on a remote host", () => {
  mocks.serverURL = "http://api.example.test";
  setup();
  fill();
  fireEvent.click(
    screen.getByRole("checkbox", {
      name: "Allow inference:execute in Project one. No MCP or administration permissions.",
    }),
  );
  expect(screen.getByRole("alert").textContent).toContain("must use HTTPS");
  expect(
    (
      screen.getByRole("button", {
        name: "Create inference identity",
      }) as HTMLButtonElement
    ).disabled,
  ).toBe(true);
  expect(mocks.create).not.toHaveBeenCalled();
});

it.each(["connected", "partial"])(
  "resets %s inference setup when the active project changes",
  async (state) => {
    mocks.create
      .mockResolvedValueOnce({ id: "first_agent" })
      .mockResolvedValueOnce({ id: "second_agent" });
    if (state === "partial")
      mocks.admit.mockRejectedValueOnce(new Error("Admission unavailable"));
    const view = setup();
    fill();
    fireEvent.click(
      screen.getByRole("checkbox", {
        name: "Allow inference:execute in Project one. No MCP or administration permissions.",
      }),
    );
    fireEvent.click(
      screen.getByRole("button", { name: "Create inference identity" }),
    );
    if (state === "connected") await screen.findByRole("status");
    else {
      await screen.findByRole("alert");
      await waitFor(() =>
        expect(
          (
            screen.getByRole("button", {
              name: "Create inference identity",
            }) as HTMLButtonElement
          ).disabled,
        ).toBe(false),
      );
    }
    mocks.projectId = "project_two";
    mocks.inferenceProject = "project_two";
    view.refreshProject();
    expect(
      (screen.getByLabelText("Agent identity name") as HTMLInputElement).value,
    ).toBe("");
    expect(
      (
        screen.getByRole("checkbox", {
          name: "Allow inference:execute in Project two. No MCP or administration permissions.",
        }) as HTMLInputElement
      ).checked,
    ).toBe(false);
    expect(screen.queryByRole("status")).toBeNull();
    fill();
    fireEvent.change(screen.getByLabelText("Subject"), {
      target: { value: "workload:second" },
    });
    fireEvent.click(
      screen.getByRole("checkbox", {
        name: "Allow inference:execute in Project two. No MCP or administration permissions.",
      }),
    );
    fireEvent.click(
      screen.getByRole("button", { name: "Create inference identity" }),
    );
    await screen.findByRole("status");
    expect(mocks.create).toHaveBeenCalledTimes(2);
    expect(mocks.create).toHaveBeenLastCalledWith({
      createAgentForm: {
        name: "Inference worker",
        projectId: "project_two",
        policyGrants: inferencePolicyGrants("project_two"),
      },
    });
    expect(mocks.admit).toHaveBeenLastCalledWith({
      request: {
        gramProject: mocks.projectId,
        admitWorkloadSubjectForm: {
          projectScoped: false,
          issuer: "https://issuer.example.test",
          subject: "workload:second",
          matchKind: "exact",
          agentId: "second_agent",
          tags: [],
        },
      },
    });
  },
);

it("creates issuer trust separately before provisioning an inference identity", async () => {
  mocks.emptyIssuers = true;
  setup();
  expect(
    screen.queryByRole("combobox", { name: "Workload issuer" }),
  ).toBeNull();
  fireEvent.click(
    screen.getByRole("button", { name: "Create new workload issuer" }),
  );
  for (const [label, value] of [
    ["Name", "Workload issuer"],
    ["Issuer URL", issuer.issuer],
    ["JWKS URL", issuer.jwksUri],
  ]) {
    fireEvent.change(screen.getByLabelText(label!), { target: { value } });
  }
  fireEvent.click(
    screen.getByRole("button", { name: "Create workload issuer" }),
  );
  await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
  expect(
    (screen.getByLabelText("Workload issuer") as HTMLSelectElement).value,
  ).toBe("issuer_one");
  expect(mocks.register).toHaveBeenCalledWith({
    request: {
      gramProject: "project_one",
      registerWorkloadIssuerForm: {
        name: "Workload issuer",
        issuer: issuer.issuer,
        jwksUri: issuer.jwksUri,
        description: `Federated from ${issuer.issuer}`,
        projectScoped: true,
        tags: [],
      },
    },
  });
  expect(mocks.create).not.toHaveBeenCalled();
  expect(mocks.admit).not.toHaveBeenCalled();
  fill();
  fireEvent.click(
    screen.getByRole("checkbox", {
      name: "Allow inference:execute in Project one. No MCP or administration permissions.",
    }),
  );
  fireEvent.click(
    screen.getByRole("button", { name: "Create inference identity" }),
  );
  await screen.findByRole("status");
  expect(mocks.register).toHaveBeenCalledTimes(1);
  expect(mocks.create).toHaveBeenCalledTimes(1);
  expect(mocks.admit).toHaveBeenCalledTimes(1);
});

it("offers only organization and current-project trust and scopes project admissions", async () => {
  mocks.includeScopedIssuers = true;
  setup();
  expect(
    screen.getByRole("option", { name: "Workload issuer — Organization" }),
  ).toBeTruthy();
  expect(
    screen.getByRole("option", { name: "Project issuer — Current project" }),
  ).toBeTruthy();
  expect(
    screen.queryByRole("option", { name: /Other project issuer/ }),
  ).toBeNull();
  expect(
    screen.queryByRole("option", { name: /Other organization issuer/ }),
  ).toBeNull();
  fill();
  fireEvent.change(screen.getByLabelText("Workload issuer"), {
    target: { value: "issuer_project" },
  });
  fireEvent.click(
    screen.getByRole("checkbox", {
      name: "Allow inference:execute in Project one. No MCP or administration permissions.",
    }),
  );
  fireEvent.click(
    screen.getByRole("button", { name: "Create inference identity" }),
  );
  await screen.findByRole("status");
  expect(mocks.register).not.toHaveBeenCalled();
  expect(mocks.admit).toHaveBeenCalledWith({
    request: {
      gramProject: "project_one",
      admitWorkloadSubjectForm: {
        projectScoped: true,
        issuer: issuer.issuer,
        subject: "workload:worker",
        matchKind: "exact",
        agentId: "agent_one",
        tags: [],
      },
    },
  });
});

it.each([
  { selected: "issuer_project", existingTier: "", blocked: false },
  { selected: "issuer_one", existingTier: "project_one", blocked: false },
  { selected: "issuer_project", existingTier: "project_one", blocked: true },
  { selected: "issuer_one", existingTier: "", blocked: true },
])(
  "matches duplicates only in selected issuer tier: $selected / $existingTier",
  async ({ selected, existingTier, blocked }) => {
    mocks.includeScopedIssuers = true;
    mocks.duplicateProject = existingTier;
    setup();
    fill();
    fireEvent.change(screen.getByLabelText("Workload issuer"), {
      target: { value: selected },
    });
    fireEvent.click(
      screen.getByRole("checkbox", {
        name: "Allow inference:execute in Project one. No MCP or administration permissions.",
      }),
    );
    fireEvent.click(
      screen.getByRole("button", { name: "Create inference identity" }),
    );
    if (blocked) {
      expect((await screen.findByRole("alert")).textContent).toContain(
        "already connected",
      );
      expect(mocks.create).not.toHaveBeenCalled();
      expect(mocks.register).not.toHaveBeenCalled();
      expect(mocks.admit).not.toHaveBeenCalled();
    } else {
      await screen.findByRole("status");
      expect(mocks.create).toHaveBeenCalledTimes(1);
      expect(mocks.register).not.toHaveBeenCalled();
      expect(mocks.admit).toHaveBeenCalledTimes(1);
    }
  },
);
