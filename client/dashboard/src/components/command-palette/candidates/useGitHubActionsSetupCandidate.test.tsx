import { cleanup, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { DEMO_ORG_SLUG } from "@/lib/demo";
import { useGitHubActionsSetupCandidate } from "./useGitHubActionsSetupCandidate";

const mocks = vi.hoisted(() => ({
  workloadWrite: true,
  orgSlug: "example",
  projectId: "project_one",
  impersonatorEmail: "",
  platformEnabled: true,
  queryEnabled: [] as boolean[],
  navigate: vi.fn(),
}));

vi.mock("@/contexts/Auth", () => ({
  useProject: () => ({ id: mocks.projectId }),
  useOrganization: () => ({ id: "org_one", slug: mocks.orgSlug }),
  useSession: () => ({
    organizationOverride: false,
    impersonatorEmail: mocks.impersonatorEmail,
  }),
  useIsPlatformAdmin: () => false,
}));
vi.mock("@/components/dev-toolbar-utils", () => ({
  getRBACScopeOverrideHeader: () => null,
}));
vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({
    hasScope: (scope: string) =>
      scope === "workload:write" && mocks.workloadWrite,
  }),
}));
vi.mock("@gram/client/react-query/workloadPlatforms.js", () => ({
  useWorkloadPlatforms: (
    _request: unknown,
    _security: unknown,
    options: { enabled: boolean },
  ) => {
    mocks.queryEnabled.push(options.enabled);
    return options.enabled
      ? {
          data: {
            platforms: [
              { key: "gke", enabled: true },
              { key: "github-actions", enabled: mocks.platformEnabled },
            ],
          },
        }
      : { data: undefined };
  },
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
}));

afterEach(cleanup);
beforeEach(() => {
  mocks.workloadWrite = true;
  mocks.orgSlug = "example";
  mocks.projectId = "project_one";
  mocks.impersonatorEmail = "";
  mocks.platformEnabled = true;
  mocks.queryEnabled = [];
  vi.clearAllMocks();
});

it("opens the agent wizard on GitHub Actions, findable by GitHub keywords", () => {
  const { result } = renderHook(() =>
    useGitHubActionsSetupCandidate({ enabled: true }),
  );
  expect(result.current).toHaveLength(1);
  const [candidate] = result.current;
  expect(candidate?.title).toBe("Set up GitHub Actions");
  expect(candidate?.keywords).toEqual(
    expect.arrayContaining(["GitHub", "GitHub Actions", "OIDC"]),
  );
  void candidate?.run("open");
  expect(mocks.navigate).toHaveBeenCalledWith(
    "/example/projects/main/identities/agents/new?platform=github-actions",
  );
});

it("reads nothing while the palette is closed", () => {
  const { result } = renderHook(() =>
    useGitHubActionsSetupCandidate({ enabled: false }),
  );
  expect(result.current).toEqual([]);
  expect(mocks.queryEnabled.every((enabled) => !enabled)).toBe(true);
});

it("is not offered without permission to manage workload identities", () => {
  mocks.workloadWrite = false;
  const { result } = renderHook(() =>
    useGitHubActionsSetupCandidate({ enabled: true }),
  );
  expect(result.current).toEqual([]);
  expect(mocks.queryEnabled.every((enabled) => !enabled)).toBe(true);
});

it("is not offered while the catalog has the platform turned off", () => {
  mocks.platformEnabled = false;
  const { result } = renderHook(() =>
    useGitHubActionsSetupCandidate({ enabled: true }),
  );
  expect(result.current).toEqual([]);
});

it("is not offered outside a project, in the demo org or while impersonating", () => {
  for (const setup of [
    () => (mocks.projectId = ""),
    () => (mocks.orgSlug = DEMO_ORG_SLUG),
    () => (mocks.impersonatorEmail = "staff@example.com"),
  ]) {
    mocks.projectId = "project_one";
    mocks.orgSlug = "example";
    mocks.impersonatorEmail = "";
    setup();
    const { result } = renderHook(() =>
      useGitHubActionsSetupCandidate({ enabled: true }),
    );
    expect(result.current).toEqual([]);
  }
  const { result } = renderHook(() =>
    useGitHubActionsSetupCandidate({ enabled: true, inProject: false }),
  );
  expect(result.current).toEqual([]);
});
