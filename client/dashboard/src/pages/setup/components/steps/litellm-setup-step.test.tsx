import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
} from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { ReactNode } from "react";
import { LiteLLMSetupStep } from "./litellm-setup-step";

const mocks = vi.hoisted(() => ({
  createDialog: {
    lastProps: null as null | {
      open: boolean;
      projects: Array<{ slug: string }>;
      initialProjectSlug: string;
      onProjectCreated: (slug: string) => void;
    },
  },
}));

vi.mock("@/contexts/Auth", () => ({
  useOrganization: () => ({
    id: "org-one",
    projects: [
      { id: "p2", name: "Zeta", slug: "zeta" },
      { id: "p1", name: "Default", slug: "default" },
    ],
  }),
}));
vi.mock("@/routes", () => ({
  useOrgRoutes: () => ({
    aiIntegrations: {
      href: () => "/acme/ai-integrations",
      Link: ({
        children,
        className,
      }: {
        children: ReactNode;
        className?: string;
      }) => (
        <a href="/acme/ai-integrations" className={className}>
          {children}
        </a>
      ),
    },
  }),
}));
vi.mock("@/pages/org/litellm-integration-row", () => ({
  CreateInstanceDialog: (props: {
    open: boolean;
    projects: Array<{ slug: string }>;
    initialProjectSlug: string;
    onProjectCreated: (slug: string) => void;
  }) => {
    mocks.createDialog.lastProps = props;
    return props.open ? <div>Create instance dialog</div> : null;
  },
}));
vi.mock("../confirm-traffic-section", () => ({
  ConfirmTrafficSection: ({
    description,
    matchesSource,
  }: {
    description: string;
    matchesSource: (source: string) => boolean;
  }) => (
    <div>
      <p>Confirm traffic section: {description}</p>
      <p>
        matches litellm: {String(matchesSource("litellm"))}, matches cursor:{" "}
        {String(matchesSource("cursor"))}
      </p>
    </div>
  ),
}));
vi.mock("../platform-setup-flow", () => ({
  PlatformSetupFlow: ({
    platformId,
    onStatusChange,
  }: {
    platformId: string;
    onStatusChange: (status: string) => void;
  }) => (
    <div>
      <p>Steps for {platformId}</p>
      <button onClick={() => onStatusChange("complete")}>
        Connect {platformId}
      </button>
    </div>
  ),
}));

afterEach(() => {
  cleanup();
  mocks.createDialog.lastProps = null;
});

function renderStep() {
  return render(<LiteLLMSetupStep onComplete={() => {}} />);
}

describe("LiteLLMSetupStep", () => {
  it("creates the instance first, then configures the proxy, then confirms traffic", () => {
    renderStep();

    expect(screen.getByText("Set up LiteLLM")).toBeTruthy();
    expect(
      screen.getAllByRole("heading", { level: 3 }).map((h) => h.textContent),
    ).toEqual(["Create a LiteLLM instance", "Configure the proxy"]);
    expect(screen.getByText("Steps for litellm")).toBeTruthy();
    expect(
      screen.getByText("matches litellm: true, matches cursor: false"),
    ).toBeTruthy();
    expect(
      screen
        .getByRole("link", { name: "AI Integrations" })
        .getAttribute("href"),
    ).toBe("/acme/ai-integrations");
  });

  it("opens the create-instance dialog on the default project and ticks the section once one is created", () => {
    renderStep();

    expect(mocks.createDialog.lastProps?.open).toBe(false);
    expect(mocks.createDialog.lastProps?.initialProjectSlug).toBe("default");
    expect(
      mocks.createDialog.lastProps?.projects.map((project) => project.slug),
    ).toEqual(["default", "zeta"]);

    fireEvent.click(screen.getByRole("button", { name: "New instance" }));
    expect(screen.getByText("Create instance dialog")).toBeTruthy();

    // The first section's number becomes a check mark once an instance exists.
    expect(screen.getByText("1")).toBeTruthy();
    act(() => mocks.createDialog.lastProps?.onProjectCreated("default"));
    expect(screen.queryByText("1")).toBeNull();
  });

  it("tracks the proxy's connected badge", () => {
    renderStep();

    expect(screen.queryByText("Complete")).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Connect litellm" }));
    expect(screen.getByText("Complete")).toBeTruthy();
  });
});
