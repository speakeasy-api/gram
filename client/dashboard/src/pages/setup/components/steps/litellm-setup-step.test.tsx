import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
} from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { ReactNode } from "react";
import type { LiteLLMInstance } from "@gram/client/models/components/litellminstance.js";
import { LiteLLMSetupStep } from "./litellm-setup-step";

function instance(
  overrides: Partial<Omit<LiteLLMInstance, "diagnostics">> & {
    diagnostics?: Partial<LiteLLMInstance["diagnostics"]>;
  } = {},
): LiteLLMInstance {
  const { diagnostics, ...rest } = overrides;
  return {
    id: "inst-1",
    name: "prod proxy",
    active: true,
    createdAt: new Date("2026-09-01T00:00:00Z"),
    updatedAt: new Date("2026-09-01T00:00:00Z"),
    createdByUserId: "user-1",
    organizationId: "org-one",
    keyPrefix: "gram_ll_",
    failurePosture: "fail_closed",
    project: { id: "p1", name: "Default", slug: "default" },
    diagnostics: { status: "pending", ...diagnostics },
    ...rest,
  } as LiteLLMInstance;
}

const mocks = vi.hoisted(() => ({
  instances: [] as unknown[],
  listCalls: [] as Array<{ gramProject: string }>,
  createDialog: {
    lastProps: null as null | {
      open: boolean;
      projects: Array<{ slug: string }>;
      initialProjectSlug: string;
      onInstanceCreated?: (instance: unknown) => void;
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
vi.mock("@gram/client/react-query/liteLLMInstances.js", () => ({
  useLiteLLMInstances: (request: { gramProject: string }) => {
    mocks.listCalls.push(request);
    return { data: { instances: mocks.instances } };
  },
}));
vi.mock("@/pages/org/litellm-integration-row", () => {
  return {
    HealthBadge: ({ instance }: { instance: LiteLLMInstance }) => (
      <span>health: {instance.diagnostics.status}</span>
    ),
    SetupContent: ({ instance }: { instance: LiteLLMInstance }) => (
      <div>
        Setup for {instance.name} in {instance.project.slug} (
        {instance.failurePosture})
      </div>
    ),
    CreateInstanceDialog: (props: {
      open: boolean;
      projects: Array<{ slug: string }>;
      initialProjectSlug: string;
      onInstanceCreated?: (instance: unknown) => void;
    }) => {
      mocks.createDialog.lastProps = props;
      return props.open ? <div>Create instance dialog</div> : null;
    },
  };
});

beforeEach(() => {
  mocks.instances = [];
  mocks.listCalls = [];
});

afterEach(() => {
  cleanup();
  mocks.createDialog.lastProps = null;
});

function renderStep() {
  return render(<LiteLLMSetupStep onComplete={() => {}} />);
}

describe("LiteLLMSetupStep", () => {
  it("holds back the proxy and traffic sections until an instance exists", () => {
    renderStep();

    expect(screen.getByText("Set up LiteLLM")).toBeTruthy();
    expect(
      screen.getAllByRole("heading", { level: 3 }).map((h) => h.textContent),
    ).toEqual([
      "Create a LiteLLM instance",
      "Configure the proxy",
      "Confirm traffic",
    ]);
    expect(screen.getAllByText(/Create an instance above first/)).toHaveLength(
      2,
    );
    expect(
      screen
        .getByRole("link", { name: "AI Integrations" })
        .getAttribute("href"),
    ).toBe("/acme/ai-integrations");
    expect(mocks.createDialog.lastProps?.initialProjectSlug).toBe("default");
    expect(
      mocks.createDialog.lastProps?.projects.map((project) => project.slug),
    ).toEqual(["default", "zeta"]);
  });

  it("renders the created instance's own setup, project and posture included", () => {
    renderStep();

    fireEvent.click(screen.getByRole("button", { name: "New instance" }));
    expect(screen.getByText("Create instance dialog")).toBeTruthy();

    act(() =>
      mocks.createDialog.lastProps?.onInstanceCreated?.(
        instance({
          id: "inst-zeta",
          name: "zeta proxy",
          failurePosture: "fail_open",
          project: { id: "p2", name: "Zeta", slug: "zeta" },
        }),
      ),
    );

    expect(
      screen.getByText("Setup for zeta proxy in zeta (fail_open)"),
    ).toBeTruthy();
    expect(screen.getByText("health: pending")).toBeTruthy();
    // The list now follows the instance's project, not the dashboard's.
    expect(mocks.listCalls.at(-1)?.gramProject).toBe("zeta");
  });

  it("picks up an instance that already exists and confirms traffic from its diagnostics", () => {
    mocks.instances = [
      instance({
        id: "old",
        name: "old proxy",
        active: false,
        createdAt: new Date("2026-09-05T00:00:00Z"),
      }),
      instance({
        id: "newest",
        name: "newest proxy",
        createdAt: new Date("2026-09-03T00:00:00Z"),
        diagnostics: {
          status: "success",
          lastGuardrailEventAt: new Date("2026-09-04T00:00:00Z"),
        },
      }),
      instance({
        id: "older",
        name: "older proxy",
        createdAt: new Date("2026-09-02T00:00:00Z"),
      }),
    ];

    renderStep();

    // Revoked instances are skipped; the newest active one is used.
    expect(
      screen.getByText("Setup for newest proxy in default (fail_closed)"),
    ).toBeTruthy();
    expect(screen.getByText("health: success")).toBeTruthy();
    expect(screen.queryByText(/Create an instance above first/)).toBeNull();
    expect(screen.queryByText("Not received")).toBeNull();
  });

  it("tracks the proxy's configured badge", () => {
    mocks.instances = [instance()];
    renderStep();

    expect(screen.queryByText("Complete")).toBeNull();
    fireEvent.click(
      screen.getByRole("button", { name: "Mark LiteLLM as configured" }),
    );
    expect(screen.getByText("Complete")).toBeTruthy();
    expect(screen.getByText("LiteLLM is configured.")).toBeTruthy();
  });
});
