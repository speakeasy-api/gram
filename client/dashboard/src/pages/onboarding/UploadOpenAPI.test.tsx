import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import { MemoryRouter, useLocation } from "react-router";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import UploadOpenAPI from "./UploadOpenAPI";

const state = vi.hoisted(() => ({
  search: "",
  sources: [] as { name: string; slug: string; kind: string }[],
  isLoading: false,
  isError: false,
  provider: vi.fn(),
  stepper: {
    state: "error",
    meta: {
      current: {
        existingDocument: null as { name: string; slug: string } | null,
        deployment: { id: "deployment" } as { id: string } | null,
      },
    },
    reset: vi.fn(),
  },
}));
vi.mock("@/contexts/Auth", () => ({ useProject: () => ({ id: "project" }) }));
vi.mock("@/components/page-templates", () => ({
  FormPage: ({ children }: { children: ReactNode }) => <>{children}</>,
}));
vi.mock("@/components/upload-asset/deploy-step", () => ({
  default: () => null,
}));
vi.mock("@/components/upload-asset/name-deployment-step", () => ({
  default: () => null,
}));
vi.mock("@/components/upload-asset/review-changes-step", () => ({
  default: () => null,
}));
vi.mock("@/components/upload-asset/upload-file-step", () => ({
  default: () => null,
}));
vi.mock("@/components/upload-asset/step", () => {
  const Pass = ({ children }: { children: ReactNode }) => <>{children}</>;
  return {
    default: Object.assign(Pass, {
      Indicator: () => null,
      Header: () => null,
      Content: Pass,
    }),
  };
});
vi.mock("@/components/upload-asset/stepper", () => {
  const Pass = ({ children }: { children: ReactNode }) => <>{children}</>;
  return {
    default: {
      Provider: (props: {
        children: ReactNode;
        existingDocument?: unknown;
      }) => {
        state.provider(props.existingDocument);
        return <>{props.children}</>;
      },
      Frame: Pass,
    },
  };
});
vi.mock("@/components/upload-asset/stepper/use-stepper", () => ({
  useStepper: () => state.stepper,
}));
vi.mock("@/contexts/Sdk", () => ({
  useSlugs: () => ({ orgSlug: "org", projectSlug: "project" }),
}));
function LocationProbe() {
  return <output data-testid="location">{useLocation().pathname}</output>;
}
function Router({ children }: { children: ReactNode }) {
  return (
    <MemoryRouter>
      {children}
      <LocationProbe />
    </MemoryRouter>
  );
}
const location = () => screen.getByTestId("location").textContent;
afterEach(cleanup);
beforeEach(() => {
  vi.clearAllMocks();
  state.search = "";
  state.sources = [];
  state.isLoading = false;
  state.isError = false;
  state.stepper.meta.current.existingDocument = null;
  state.stepper.state = "error";
  state.stepper.meta.current.deployment = { id: "deployment" };
});
it("offers deployment logs and a reset after a failed deployment", () => {
  render(<UploadOpenAPI />, { wrapper: Router });
  fireEvent.click(screen.getByRole("button", { name: "View Logs" }));
  expect(location()).toBe("/org/projects/project/deployments/deployment");
});
it("preserves standalone Continue", () => {
  state.stepper.state = "completed";
  render(<UploadOpenAPI />, { wrapper: Router });
  fireEvent.click(screen.getByRole("button", { name: "Continue" }));
  expect(location()).toBe("/org/projects/project/mcp");
});

vi.mock("react-router", async (importOriginal) => ({
  ...(await importOriginal<typeof import("react-router")>()),
  useSearchParams: () => [new URLSearchParams(state.search)],
}));
vi.mock("@/components/sources/source-list", () => ({
  useProjectSources: () => ({
    sources: state.sources,
    isLoading: state.isLoading,
    isError: state.isError,
  }),
}));

it("retains the existing source after lookup succeeds", () => {
  state.search = "slug=original";
  state.sources = [{ name: "Original API", slug: "original", kind: "openapi" }];
  render(<UploadOpenAPI />, { wrapper: Router });
  expect(state.provider).toHaveBeenCalledWith({
    name: "Original API",
    slug: "original",
  });
});
it.each(["loading", "error"])(
  "does not mount an upload before a safe source lookup: %s",
  (status) => {
    state.search = "slug=original";
    state.isLoading = status === "loading";
    state.isError = status === "error";
    render(<UploadOpenAPI />, { wrapper: Router });
    expect(state.provider).not.toHaveBeenCalled();
  },
);
it("returns standalone source updates to sources", () => {
  state.stepper.state = "completed";
  state.stepper.meta.current.existingDocument = {
    name: "Original API",
    slug: "original",
  };
  render(<UploadOpenAPI />, { wrapper: Router });
  fireEvent.click(screen.getByRole("button", { name: "Continue" }));
  expect(location()).toBe("/org/projects/project/mcp/sources");
});
