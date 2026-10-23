import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { InferenceSetup } from "./InferenceSetup";
import { inferencePolicyGrants } from "./inferenceSetupInstructions";

const mocks = vi.hoisted(() => ({
  create: vi.fn(),
  register: vi.fn(),
  admit: vi.fn(),
  allowed: true,
  admin: false,
}));
vi.mock("@/contexts/Auth", () => ({
  useOrganization: () => ({ id: "org_example" }),
  useProject: () => ({ id: "project_one", name: "Project one" }),
  useSession: () => ({ user: { id: "owner" } }),
}));
vi.mock("@/contexts/Sdk", () => ({
  useSdkClient: () => ({ agents: { create: mocks.create } }),
}));
vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({
    hasScope: (scope: string) =>
      scope === "org:admin" ? mocks.admin : mocks.allowed,
  }),
}));
vi.mock("@/routes", () => ({
  useOrgRoutes: () => ({
    access: { roles: { href: () => "/example/access/roles" } },
  }),
}));
vi.mock("../agent-policy-grants", () => ({ invalidateAgentPolicy: vi.fn() }));
vi.mock("@gram/client/react-query/workloadIdentities.js", () => ({
  useWorkloadIdentities: () => ({ data: { issuers: [], admissions: [] } }),
  invalidateAllWorkloadIdentities: vi.fn(),
}));
vi.mock("@gram/client/react-query/registerWorkloadIssuer.js", () => ({
  useRegisterWorkloadIssuerMutation: () => ({ mutateAsync: mocks.register }),
}));
vi.mock("@gram/client/react-query/admitWorkloadSubject.js", () => ({
  useAdmitWorkloadSubjectMutation: () => ({ mutateAsync: mocks.admit }),
}));
vi.mock("@gram/client/react-query/workloadTokenEndpoints.js", () => ({
  useWorkloadTokenEndpoints: () => ({
    data: {
      items: [
        {
          userSessionIssuerId: "usi_one",
          userSessionIssuerSlug: "shared",
          projectId: "",
          projectName: "",
          issuer: "https://auth.example.test/oauth/usi/usi_one",
          tokenEndpoint: "https://auth.example.test/oauth/usi/usi_one/token",
          mcpHost: "https://mcp.example.test",
        },
      ],
    },
    isPending: false,
    isError: false,
  }),
}));

function setup() {
  return render(
    <QueryClientProvider
      client={
        new QueryClient({ defaultOptions: { queries: { retry: false } } })
      }
    >
      <InferenceSetup onDone={vi.fn<() => void>()} />
    </QueryClientProvider>,
  );
}
function fill() {
  fireEvent.change(screen.getByLabelText("Agent identity name"), {
    target: { value: "Inference worker" },
  });
  fireEvent.change(screen.getByLabelText("Name"), {
    target: { value: "Workload issuer" },
  });
  fireEvent.change(screen.getByLabelText("Issuer URL"), {
    target: { value: "https://issuer.example.test" },
  });
  fireEvent.change(screen.getByLabelText("JWKS URL"), {
    target: { value: "https://issuer.example.test/jwks" },
  });
  fireEvent.change(screen.getByLabelText("Subject"), {
    target: { value: "workload:worker" },
  });
}
afterEach(cleanup);
beforeEach(() => {
  vi.clearAllMocks();
  mocks.allowed = true;
  mocks.admin = false;
  mocks.create.mockResolvedValue({ id: "agent_one" });
  mocks.register.mockResolvedValue({});
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
  it("reuses issuer/admission creation with opt-in, then shows inference-only exchange guidance", async () => {
    setup();
    fill();
    expect(
      (
        screen.getByRole("button", {
          name: "Create inference identity",
        }) as HTMLButtonElement
      ).disabled,
    ).toBe(true);
    fireEvent.click(screen.getByRole("checkbox"));
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
    expect(mocks.register).toHaveBeenCalledWith({
      request: {
        registerWorkloadIssuerForm: {
          name: "Workload issuer",
          description: "Federated from https://issuer.example.test",
          issuer: "https://issuer.example.test",
          jwksUri: "https://issuer.example.test/jwks",
          tags: [],
        },
      },
    });
    expect(mocks.admit).toHaveBeenCalledWith({
      request: {
        admitWorkloadSubjectForm: {
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
    ).toContain("resource=urn:gram:inference\nproject_id=project_one");
    expect(
      screen.getByText("Speakeasy issuer / external JWT audience"),
    ).toBeTruthy();
    expect(screen.getByText("POST chat completions")).toBeTruthy();
    expect(screen.queryByText("MCP host")).toBeNull();
  });
  it("does not create identities when permission is absent", () => {
    mocks.allowed = false;
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
    fireEvent.click(screen.getByRole("checkbox"));
    fireEvent.click(
      screen.getByRole("button", { name: "Create inference identity" }),
    );
    await screen.findByRole("alert");
    fireEvent.click(
      screen.getByRole("button", { name: "Create inference identity" }),
    );
    await waitFor(() => expect(mocks.admit).toHaveBeenCalledTimes(2));
    expect(mocks.create).toHaveBeenCalledTimes(1);
    expect(mocks.register).toHaveBeenCalledTimes(1);
  });
});

it("links administrators without inference access to owner role configuration", () => {
  mocks.allowed = false;
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
