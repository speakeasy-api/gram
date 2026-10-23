import { cleanup, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { useInferenceSetupCandidate } from "./useInferenceSetupCandidate";
const mocks = vi.hoisted(() => ({
  allowed: true,
  impersonatorEmail: "",
  navigate: vi.fn(),
}));
vi.mock("@/contexts/Auth", () => ({
  useProject: () => ({ id: "project_one" }),
  useOrganization: () => ({ slug: "example" }),
  useSession: () => ({ impersonatorEmail: mocks.impersonatorEmail }),
  useIsPlatformAdmin: () => false,
}));
vi.mock("@/components/dev-toolbar-utils", () => ({
  getRBACScopeOverrideHeader: () => null,
}));
vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({ hasScope: () => mocks.allowed }),
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
  mocks.allowed = true;
  mocks.impersonatorEmail = "";
  vi.clearAllMocks();
});
it("opens the inference capability setup, not MCP setup", () => {
  const { result } = renderHook(() =>
    useInferenceSetupCandidate({ enabled: true }),
  );
  expect(result.current[0]?.title).toBe("Set up inference workload identity");
  void result.current[0]?.run("open");
  expect(mocks.navigate).toHaveBeenCalledWith(
    "/example/projects/main/identities/agents/new?capability=inference",
  );
});
it("hides setup when closed, unauthorized, or impersonating", () => {
  const { result, rerender } = renderHook(
    ({ enabled }) => useInferenceSetupCandidate({ enabled }),
    { initialProps: { enabled: false } },
  );
  expect(result.current).toEqual([]);
  mocks.allowed = false;
  rerender({ enabled: true });
  expect(result.current).toEqual([]);
  mocks.allowed = true;
  mocks.impersonatorEmail = "staff@example.test";
  rerender({ enabled: true });
  expect(result.current).toEqual([]);
});
