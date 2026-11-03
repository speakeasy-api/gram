import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { ComponentProps } from "react";
import type { WorkloadIssuer } from "@gram/client/models/components/workloadissuer.js";
import { WorkloadIssuerSelector } from "./WorkloadIssuerSelector";
import { customSetupInstructions } from "./CustomSetupInstructions";

const mocks = vi.hoisted(() => ({
  register: vi.fn(),
  canCreate: true,
  slug: "example",
}));
vi.mock("@/contexts/Auth", () => ({
  useProject: () => ({ id: "project_one", slug: mocks.slug }),
}));
vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({
    hasScope: (scope: string) => scope === "workload:write" && mocks.canCreate,
  }),
}));
vi.mock("@gram/client/react-query/registerWorkloadIssuer.js", () => ({
  useRegisterWorkloadIssuerMutation: () => ({ mutateAsync: mocks.register }),
}));
const issuer: WorkloadIssuer = {
  id: "issuer_one",
  name: "Existing issuer",
  issuer: "https://issuer.example.test",
  jwksUri: "https://issuer.example.test/jwks",
  organizationId: "org_example",
  projectId: "",
  description: "",
  tags: [],
  allowWildcardAdmission: false,
  allowAssertionReuse: false,
  createdAt: new Date(0),
  updatedAt: new Date(0),
};
const created = {
  ...issuer,
  id: "issuer_new",
  name: "New issuer",
  projectId: "project_one",
};
function setup(
  overrides: Partial<ComponentProps<typeof WorkloadIssuerSelector>> = {},
) {
  const props = {
    issuers: [issuer],
    selectedId: "",
    loading: false,
    failed: false,
    onRetry: vi.fn(),
    onSelect: vi.fn(),
    onCreated: vi.fn(),
    issuerStep: customSetupInstructions().steps.find(
      (step) => step.id === "issuer",
    )!,
    ...overrides,
  };
  return { ...render(<WorkloadIssuerSelector {...props} />), props };
}
function openAndFill() {
  fireEvent.click(
    screen.getByRole("button", { name: "Create new workload issuer" }),
  );
  for (const [label, value] of [
    ["Name", "New issuer"],
    ["Issuer URL", issuer.issuer],
    ["JWKS URL", issuer.jwksUri],
  ] as const) {
    fireEvent.change(screen.getByLabelText(label), { target: { value } });
  }
}
function submit() {
  fireEvent.click(
    screen.getByRole("button", { name: "Create workload issuer" }),
  );
}
afterEach(cleanup);
beforeEach(() => {
  vi.resetAllMocks();
  mocks.canCreate = true;
  mocks.slug = "example";
  mocks.register.mockResolvedValue({ issuers: [created], admissions: [] });
});
describe("WorkloadIssuerSelector", () => {
  it("offers existing trust and creation, selecting without a mutation", () => {
    const { props, rerender } = setup();
    expect(
      screen.getByRole("button", { name: "Create new workload issuer" }),
    ).toBeTruthy();
    expect(
      screen.getByRole("option", { name: "Existing issuer — Organization" }),
    ).toBeTruthy();
    fireEvent.change(screen.getByLabelText("Workload issuer"), {
      target: { value: issuer.id },
    });
    expect(props.onSelect).toHaveBeenCalledWith(issuer);
    expect(mocks.register).not.toHaveBeenCalled();
    rerender(<WorkloadIssuerSelector {...props} selectedId={issuer.id} />);
    expect(screen.getByText(issuer.issuer)).toBeTruthy();
    expect(screen.getByText(issuer.jwksUri)).toBeTruthy();
  });
  it("shows only creation when no issuers exist and selects a successful creation", async () => {
    const { props } = setup({ issuers: [] });
    expect(screen.queryByRole("combobox")).toBeNull();
    openAndFill();
    submit();
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    expect(props.onCreated).toHaveBeenCalledWith(
      { issuers: [created], admissions: [] },
      created,
    );
    expect(props.onSelect).not.toHaveBeenCalled();
    expect(mocks.register).toHaveBeenCalledWith({
      request: {
        gramProject: "example",
        registerWorkloadIssuerForm: {
          name: "New issuer",
          issuer: issuer.issuer,
          jwksUri: issuer.jwksUri,
          description: `Federated from ${issuer.issuer}`,
          projectScoped: true,
          tags: [],
        },
      },
    });
  });
  it("sends the actual selected project rather than a default project", async () => {
    mocks.slug = "selected-project";
    setup();
    openAndFill();
    submit();
    await waitFor(() => expect(mocks.register).toHaveBeenCalled());
    expect(mocks.register.mock.calls[0]?.[0].request.gramProject).toBe(
      "selected-project",
    );
    expect(
      mocks.register.mock.calls[0]?.[0].request.registerWorkloadIssuerForm
        .projectScoped,
    ).toBe(true);
  });
  it("selects the registered name and tier when the server normalizes its issuer URL", async () => {
    const normalized = { ...created, issuer: `${issuer.issuer}/` };
    mocks.register.mockResolvedValue({ issuers: [normalized], admissions: [] });
    const { props } = setup();
    openAndFill();
    submit();
    await waitFor(() =>
      expect(props.onCreated).toHaveBeenCalledWith(
        { issuers: [normalized], admissions: [] },
        normalized,
      ),
    );
    expect(screen.queryByRole("dialog")).toBeNull();
  });
  it("creates organization-scoped trust without an explicit project override", async () => {
    const organizationIssuer = { ...created, projectId: "" };
    mocks.register.mockResolvedValue({
      issuers: [organizationIssuer],
      admissions: [],
    });
    const { props } = setup();
    openAndFill();
    fireEvent.change(screen.getByLabelText("Available to"), {
      target: { value: "organization" },
    });
    submit();
    await waitFor(() =>
      expect(props.onCreated).toHaveBeenCalledWith(
        expect.anything(),
        organizationIssuer,
      ),
    );
    expect(mocks.register).toHaveBeenCalledWith({
      request: {
        registerWorkloadIssuerForm: {
          name: "New issuer",
          issuer: issuer.issuer,
          jwksUri: issuer.jwksUri,
          description: `Federated from ${issuer.issuer}`,
          projectScoped: false,
          tags: [],
        },
      },
    });
  });
  it("cancels without registering and clears the form on reopening", () => {
    setup();
    openAndFill();
    fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
    expect(screen.queryByRole("dialog")).toBeNull();
    expect(mocks.register).not.toHaveBeenCalled();
    fireEvent.click(
      screen.getByRole("button", { name: "Create new workload issuer" }),
    );
    expect((screen.getByLabelText("Name") as HTMLInputElement).value).toBe("");
    expect(
      (
        screen.getByRole("button", {
          name: "Create workload issuer",
        }) as HTMLButtonElement
      ).disabled,
    ).toBe(true);
  });
  it("renders loading and retryable query errors instead of selection", () => {
    const { props, rerender } = setup({ loading: true });
    expect(screen.getByRole("status").textContent).toContain(
      "Loading workload issuers",
    );
    expect(screen.queryByRole("combobox")).toBeNull();
    rerender(<WorkloadIssuerSelector {...props} loading={false} failed />);
    expect(screen.getByRole("alert").textContent).toContain(
      "Could not load workload issuers",
    );
    fireEvent.click(
      screen.getByRole("button", { name: "Retry workload issuers" }),
    );
    expect(props.onRetry).toHaveBeenCalledOnce();
  });
  it("disables duplicate submissions and cancellation while creation is pending", async () => {
    let resolve!: (value: unknown) => void;
    mocks.register.mockImplementation(
      () =>
        new Promise((done) => {
          resolve = done;
        }),
    );
    setup();
    openAndFill();
    submit();
    const busy = screen.getByRole("button", {
      name: "Creating issuer…",
    }) as HTMLButtonElement;
    expect(busy.disabled).toBe(true);
    expect(
      (screen.getByRole("button", { name: "Cancel" }) as HTMLButtonElement)
        .disabled,
    ).toBe(true);
    fireEvent.click(busy);
    expect(mocks.register).toHaveBeenCalledOnce();
    resolve({ issuers: [created], admissions: [] });
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
  });
  it.each([
    {
      failure: new Error("Registration unavailable"),
      message: "Registration unavailable",
    },
    {
      failure: "unexpected",
      message: "Could not register the workload issuer.",
    },
  ])(
    "keeps failed creation open for retry: $message",
    async ({ failure, message }) => {
      mocks.register.mockRejectedValueOnce(failure);
      const { props } = setup();
      openAndFill();
      submit();
      expect((await screen.findByRole("alert")).textContent).toBe(message);
      expect(props.onCreated).not.toHaveBeenCalled();
      submit();
      await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
      expect(mocks.register).toHaveBeenCalledTimes(2);
    },
  );
  it("does not select a response missing the newly registered issuer", async () => {
    mocks.register.mockResolvedValue({ issuers: [issuer], admissions: [] });
    const { props } = setup();
    openAndFill();
    submit();
    expect((await screen.findByRole("alert")).textContent).toContain(
      "could not be selected",
    );
    expect(props.onCreated).not.toHaveBeenCalled();
  });
  it("allows existing selection but prevents creation without workload:write", () => {
    mocks.canCreate = false;
    const { props } = setup();
    const button = screen.getByRole("button", {
      name: "Create new workload issuer",
    }) as HTMLButtonElement;
    expect(button.disabled).toBe(true);
    fireEvent.click(button);
    expect(screen.queryByRole("dialog")).toBeNull();
    fireEvent.change(screen.getByLabelText("Workload issuer"), {
      target: { value: issuer.id },
    });
    expect(props.onSelect).toHaveBeenCalledWith(issuer);
    expect(mocks.register).not.toHaveBeenCalled();
  });
  it("blocks project-scoped registration without a project slug", () => {
    mocks.slug = "";
    setup();
    openAndFill();
    expect(
      (
        screen.getByRole("button", {
          name: "Create workload issuer",
        }) as HTMLButtonElement
      ).disabled,
    ).toBe(true);
    submit();
    expect(mocks.register).not.toHaveBeenCalled();
  });
});
