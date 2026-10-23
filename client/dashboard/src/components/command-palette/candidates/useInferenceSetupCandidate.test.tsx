import { cleanup, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { DEMO_ORG_SLUG } from "@/lib/demo";
import { useInferenceSetupCandidate } from "./useInferenceSetupCandidate";
const mocks = vi.hoisted(() => ({
  workloadWrite: true,
  orgSlug: "example",
  organizationOverride: false,
  scopeOverride: null as string | null,
  inferenceProject: "project_one",
  adminOrg: "",
  impersonatorEmail: "",
  navigate: vi.fn(),
}));
vi.mock("@/contexts/Auth", () => ({
  useProject: () => ({ id: "project_one" }),
  useOrganization: () => ({ id: "org_one", slug: mocks.orgSlug }),
  useSession: () => ({
    organizationOverride: mocks.organizationOverride,
    impersonatorEmail: mocks.impersonatorEmail,
  }),
  useIsPlatformAdmin: () => false,
}));
vi.mock("@/components/dev-toolbar-utils", () => ({
  getRBACScopeOverrideHeader: () => mocks.scopeOverride,
}));
vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({
    hasScope: (scope: string, resourceId?: string, projectId?: string) => {
      if (scope === "workload:write") return mocks.workloadWrite;
      if (scope === "inference:execute")
        return resourceId === "*" && projectId === mocks.inferenceProject;
      if (scope === "org:admin") return resourceId === mocks.adminOrg;
      return false;
    },
  }),
}));
vi.mock("react-router", () => ({ useNavigate: () => mocks.navigate }));
vi.mock("@/routes", () => ({
  useRoutes: () => ({
    identities: {
      agents: {
        new: { href: () => "/example/projects/main/identities/agents/new" },
      },
    },
  }),
  useOrgRoutes: () => ({
    access: { roles: { href: () => "/example/access/roles" } },
  }),
}));
afterEach(cleanup);
beforeEach(() => {
  mocks.workloadWrite = true;
  mocks.orgSlug = "example";
  mocks.organizationOverride = false;
  mocks.scopeOverride = null;
  mocks.inferenceProject = "project_one";
  mocks.adminOrg = "";
  mocks.impersonatorEmail = "";
  vi.clearAllMocks();
});
it("lets workload administrators discover setup before inference permission is granted", () => {
  mocks.inferenceProject = "";
  const { result } = renderHook(() =>
    useInferenceSetupCandidate({ enabled: true }),
  );
  expect(result.current.map((candidate) => candidate.id)).toEqual([
    "inference-workload-setup",
  ]);
  void result.current[0]?.run("open");
  expect(mocks.navigate).toHaveBeenCalledWith(
    "/example/projects/main/identities/agents/new?capability=inference",
  );
});
it("offers role administrators setup and a canonical role configuration alias", () => {
  mocks.workloadWrite = false;
  mocks.inferenceProject = "";
  mocks.adminOrg = "org_one";
  const { result } = renderHook(() =>
    useInferenceSetupCandidate({ enabled: true }),
  );
  expect(result.current.map((candidate) => candidate.id)).toEqual([
    "inference-workload-setup",
    "inference-role-permissions",
  ]);
  void result.current[1]?.run("open");
  expect(mocks.navigate).toHaveBeenCalledWith("/example/access/roles");
});
it("offers the role alias without a project shell", () => {
  mocks.adminOrg = "org_one";
  const { result } = renderHook(() =>
    useInferenceSetupCandidate({ enabled: true, inProject: false }),
  );
  expect(result.current.map((candidate) => candidate.id)).toEqual([
    "inference-role-permissions",
  ]);
});
it("does not confuse inference callers or another organization administrator with configurators", () => {
  mocks.workloadWrite = false;
  mocks.adminOrg = "other_org";
  const { result } = renderHook(() =>
    useInferenceSetupCandidate({ enabled: true }),
  );
  expect(result.current).toEqual([]);
});
it("hides both candidates when closed or impersonating", () => {
  mocks.adminOrg = "org_one";
  const { result, rerender } = renderHook(
    ({ enabled }) => useInferenceSetupCandidate({ enabled }),
    { initialProps: { enabled: false } },
  );
  expect(result.current).toEqual([]);
  mocks.impersonatorEmail = "staff@example.test";
  rerender({ enabled: true });
  expect(result.current).toEqual([]);
});

it.each(["demo", "support", "override"])(
  "hides inference discovery in %s sessions",
  (restriction) => {
    mocks.adminOrg = "org_one";
    if (restriction === "demo") mocks.orgSlug = DEMO_ORG_SLUG;
    if (restriction === "support") mocks.organizationOverride = true;
    if (restriction === "override") mocks.scopeOverride = "org:admin";
    const { result } = renderHook(() =>
      useInferenceSetupCandidate({ enabled: true }),
    );
    expect(result.current).toEqual([]);
  },
);
