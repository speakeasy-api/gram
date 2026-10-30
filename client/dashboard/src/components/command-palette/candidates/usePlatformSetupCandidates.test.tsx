import { cleanup, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { DEMO_ORG_SLUG } from "@/lib/demo";
import { usePlatformSetupCandidates } from "./usePlatformSetupCandidates";

const mocks = vi.hoisted(() => ({
  workloadWrite: true,
  orgSlug: "example",
  inProject: true,
  impersonatorEmail: "",
  scopeOverride: null as string | null,
  githubEnabled: true,
  catalogEnabled: [] as boolean[],
  navigate: vi.fn(),
}));

// The palette is mounted above the project's provider, so it never sees a
// project id: the hook must not depend on one.
vi.mock("@/contexts/Auth", () => ({
  useProject: () => ({ id: "" }),
  useOrganization: () => ({ id: "org_one", slug: mocks.orgSlug }),
  useSession: () => ({
    organizationOverride: false,
    impersonatorEmail: mocks.impersonatorEmail,
  }),
}));
vi.mock("@/hooks/useRBACScopeOverride", () => ({
  useRBACScopeOverride: () => mocks.scopeOverride,
}));
vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({
    hasScope: (scope: string) =>
      scope === "workload:write" && mocks.workloadWrite,
  }),
}));
// The catalog the server ships: GitHub Actions and GKE have guided setups,
// and an entry with none stays out.
vi.mock("@/pages/agents/credentials/workloadCatalog", () => ({
  useCatalogEntries: (enabled: boolean) => {
    mocks.catalogEnabled.push(enabled);
    const entry = (key: string, displayName: string, on = true) => ({
      key,
      displayName,
      description: `${displayName} description`,
      enabled: on,
    });
    return {
      entries: enabled
        ? [
            entry("github-actions", "GitHub Actions", mocks.githubEnabled),
            entry("gke", "GKE"),
            entry("unknown", "Unknown"),
          ]
        : [],
    };
  },
}));
vi.mock("@/pages/agents/credentials/platformSetups", () => ({
  hasPlatformSetup: (entry: { key: string; enabled: boolean }) =>
    entry.enabled && entry.key !== "unknown",
  platformSetup: (entry: { key: string }) =>
    entry.key === "github-actions" ? { keywords: ["CI", "workflow"] } : {},
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
  mocks.inProject = true;
  mocks.impersonatorEmail = "";
  mocks.scopeOverride = null;
  mocks.githubEnabled = true;
  mocks.catalogEnabled = [];
  vi.clearAllMocks();
});

it("offers each platform with a guided setup, opening the wizard on it", () => {
  const { result } = renderHook(() =>
    usePlatformSetupCandidates({ enabled: true }),
  );
  expect(result.current.map((candidate) => candidate.title)).toEqual([
    "Set up GitHub Actions",
    "Set up GKE",
  ]);
  const [github] = result.current;
  expect(github?.keywords).toEqual(
    expect.arrayContaining(["GitHub Actions", "OIDC", "CI", "workflow"]),
  );
  void github?.run("open");
  expect(mocks.navigate).toHaveBeenCalledWith(
    "/example/projects/main/identities/agents/new?platform=github-actions",
  );
});

it("leaves out a platform the catalog has turned off", () => {
  mocks.githubEnabled = false;
  const { result } = renderHook(() =>
    usePlatformSetupCandidates({ enabled: true }),
  );
  expect(result.current.map((candidate) => candidate.title)).toEqual([
    "Set up GKE",
  ]);
});

it("reads nothing while the palette is closed", () => {
  const { result } = renderHook(() =>
    usePlatformSetupCandidates({ enabled: false }),
  );
  expect(result.current).toEqual([]);
  expect(mocks.catalogEnabled.every((enabled) => !enabled)).toBe(true);
});

it.each([
  [
    "without permission to manage workload identities",
    () => {
      mocks.workloadWrite = false;
    },
  ],
  [
    "outside a project",
    () => {
      mocks.inProject = false;
    },
  ],
  [
    "in the demo org",
    () => {
      mocks.orgSlug = DEMO_ORG_SLUG;
    },
  ],
  [
    "while impersonating",
    () => {
      mocks.impersonatorEmail = "staff@example.com";
    },
  ],
  [
    "under a scope override",
    () => {
      mocks.scopeOverride = "workload:read";
    },
  ],
])("offers nothing %s", (_, arrange) => {
  arrange();
  const { result } = renderHook(() =>
    usePlatformSetupCandidates({ enabled: true, inProject: mocks.inProject }),
  );
  expect(result.current).toEqual([]);
  expect(mocks.catalogEnabled.every((enabled) => !enabled)).toBe(true);
});
