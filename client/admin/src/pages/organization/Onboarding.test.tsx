import {
  act,
  cleanup,
  fireEvent,
  screen,
  waitFor,
} from "@testing-library/react";
import { QueryClient } from "@tanstack/react-query";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { Onboarding } from "./Onboarding";
import { renderWithApp } from "@/test/harness";
import { onboardingPresets } from "@/lib/onboardingPresets";

const api = vi.hoisted(() => ({
  assignment: vi.fn(),
  steps: vi.fn(),
  templates: vi.fn(),
  update: vi.fn(),
  create: vi.fn(),
  assign: vi.fn(),
  remove: vi.fn(),
}));
vi.mock("./OnboardingStack", () => ({
  OnboardingStack: () => <p>Stack editor</p>,
}));
vi.mock("@/lib/gramAdminClient", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/lib/gramAdminClient")>()),
  organizationOnboardingPlaybookQuery: (id: string) => ({
    queryKey: ["assignment", id],
    queryFn: api.assignment,
  }),
  onboardingStepsQuery: () => ({ queryKey: ["steps"], queryFn: api.steps }),
  onboardingPlaybooksQuery: (id?: string) => ({
    queryKey: ["templates", id],
    queryFn: api.templates,
  }),
  onboardingUseCasesQuery: () => ({
    queryKey: ["use-cases"],
    queryFn: async () => ({ useCases: [] }),
  }),
  updateAdminOnboardingPlaybook: api.update,
  createAdminOnboardingPlaybook: api.create,
  assignAdminOrganizationOnboardingPlaybook: api.assign,
  deleteAdminOnboardingPlaybook: api.remove,
}));
const group = { slug: "setup", title: "Set up tools" };
const second = { slug: "invite", title: "Invite people" };
const catalog = [
  group,
  { slug: "connect", title: "Connect a tool", parentSlug: "setup" },
  second,
];
const owned = {
  id: "owned",
  organizationId: "org_test",
  name: "Our wizard",
  description: "Keep description",
  isDefault: false,
  steps: [group, second],
};
const template = {
  id: "shared",
  name: "Shared wizard",
  description: "",
  isDefault: true,
  steps: [group],
};
beforeEach(() => {
  vi.resetAllMocks();
  api.assignment.mockResolvedValue({
    organizationId: "org_test",
    applicability: [],
  });
  api.steps.mockResolvedValue({ steps: catalog });
  api.templates.mockResolvedValue({ playbooks: [template, owned] });
  api.create.mockImplementation(async (request: { stepSlugs: string[] }) => {
    const created = {
      ...owned,
      id: "created",
      steps: request.stepSlugs.map((slug) =>
        catalog.find((step) => step.slug === slug)!,
      ),
    };
    api.templates.mockResolvedValue({ playbooks: [template, owned, created] });
    return created;
  });
  api.update.mockResolvedValue(owned);
  api.assign.mockResolvedValue({});
  api.remove.mockResolvedValue({});
});
afterEach(cleanup);
const render = (queryClient?: QueryClient) =>
  renderWithApp(
    <Onboarding
      organizationId="org_test"
      organizationName="Example organization"
    />,
    { queryClient },
  );
const edit = async () =>
  fireEvent.click(
    await screen.findByRole("button", { name: "Edit wizard steps" }),
  );
const chooseTemplate = async () => {
  fireEvent.click(
    await screen.findByRole("combobox", { name: "Onboarding template" }),
  );
  fireEvent.click(await screen.findByRole("option", { name: "Shared wizard" }));
  fireEvent.click(screen.getByRole("button", { name: "Review template" }));
};

describe("tenant onboarding", () => {
  it("explains default fallback and exposes only shared templates", async () => {
    await render();
    await screen.findByText(/saved task selection and catalog defaults/);
    fireEvent.click(
      await screen.findByRole("combobox", { name: "Onboarding template" }),
    );
    expect(
      await screen.findByRole("option", { name: "Shared wizard" }),
    ).toBeTruthy();
    expect(screen.queryByRole("option", { name: "Our wizard" })).toBeNull();
    fireEvent.keyDown(screen.getByRole("listbox"), { key: "Escape" });
    expect(
      screen
        .getByRole("link", { name: "Manage shared templates" })
        .getAttribute("href"),
    ).toBe("/onboarding-playbooks");
    expect(screen.getByText("Stack editor")).toBeTruthy();
    expect(screen.queryByRole("button", { name: /reset|delete/i })).toBeNull();
  });
  it.each(onboardingPresets)(
    "stages and saves the $name preset in order",
    async (preset) => {
      api.assignment.mockResolvedValue({ playbook: owned });
      api.steps.mockResolvedValue({
        steps: [
          ...catalog,
          ...[
            ...new Set(
              onboardingPresets.flatMap((item) => [...item.stepSlugs]),
            ),
          ].map((slug) => ({ slug, title: slug })),
        ],
      });
      await render();
      fireEvent.click(
        await screen.findByRole("combobox", { name: "Onboarding template" }),
      );
      fireEvent.click(await screen.findByRole("option", { name: preset.name }));
      expect(screen.getByText(preset.description)).toBeTruthy();
      fireEvent.click(screen.getByRole("button", { name: "Review preset" }));
      await screen.findByRole("heading", { name: `Review ${preset.name}` });
      expect(api.update).not.toHaveBeenCalled();
      expect(api.create).not.toHaveBeenCalled();
      expect(api.assign).not.toHaveBeenCalled();
      fireEvent.click(
        screen.getByRole("button", { name: "Save wizard steps" }),
      );
      await screen.findByText("Onboarding saved.");
      expect(api.update).toHaveBeenCalledExactlyOnceWith({
        organizationId: "org_test",
        playbookId: owned.id,
        name: owned.name,
        description: owned.description,
        stepSlugs: [...preset.stepSlugs],
      });
      expect(api.assign).not.toHaveBeenCalled();
    },
  );
  it("disables presets when the step catalog is missing required steps", async () => {
    await render();
    fireEvent.click(
      await screen.findByRole("combobox", { name: "Onboarding template" }),
    );
    for (const preset of onboardingPresets) {
      expect(
        (await screen.findByRole("option", { name: preset.name })).getAttribute(
          "aria-disabled",
        ),
      ).toBe("true");
    }
    expect(api.update).not.toHaveBeenCalled();
  });
  it("shows group children and stages ordered edits until save", async () => {
    api.assignment.mockResolvedValue({ playbook: owned });
    await render();
    await screen.findByText("Includes Connect a tool");
    await edit();
    fireEvent.click(
      screen.getByRole("button", { name: "Move Invite people up" }),
    );
    expect(api.update).not.toHaveBeenCalled();
    expect(
      screen.queryByRole("button", { name: "Remove Connect a tool" }),
    ).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Save wizard steps" }));
    await screen.findByText("Onboarding saved.");
    expect(api.update).toHaveBeenCalledExactlyOnceWith({
      organizationId: "org_test",
      playbookId: "owned",
      name: "Our wizard",
      description: "Keep description",
      stepSlugs: ["invite", "setup"],
    });
    expect(api.assign).not.toHaveBeenCalled();
    expect(api.remove).not.toHaveBeenCalled();
  });
  it("discards without writes", async () => {
    api.assignment.mockResolvedValue({ playbook: owned });
    await render();
    await edit();
    fireEvent.click(
      screen.getByRole("button", { name: "Remove Invite people" }),
    );
    fireEvent.click(screen.getByRole("button", { name: "Discard changes" }));
    await screen.findByText("Invite people");
    expect(api.update).not.toHaveBeenCalled();
  });
  it("requires confirmation before assigning a shared copy", async () => {
    await render();
    await chooseTemplate();
    expect(
      screen.getByText(/Later shared-template changes will not sync/),
    ).toBeTruthy();
    expect(api.assign).not.toHaveBeenCalled();
    fireEvent.click(
      screen.getByRole("button", { name: "Confirm template assignment" }),
    );
    await screen.findByText("Onboarding saved.");
    expect(api.assign).toHaveBeenCalledExactlyOnceWith({
      organizationId: "org_test",
      playbookId: "shared",
    });
    expect(api.update).not.toHaveBeenCalled();
  });
  it("retains template confirmation and error on assignment failure", async () => {
    api.assign.mockRejectedValue(new Error("Assignment unavailable"));
    await render();
    await chooseTemplate();
    fireEvent.click(
      screen.getByRole("button", { name: "Confirm template assignment" }),
    );
    await screen.findByText(/Unable to save onboarding/);
    expect(
      screen.getByRole("button", { name: "Confirm template assignment" }),
    ).toBeTruthy();
    expect(api.remove).not.toHaveBeenCalled();
  });
  it("recognizes a shared-template copy after a committed assignment response is lost", async () => {
    api.assign.mockImplementationOnce(async () => {
      api.assignment.mockResolvedValue({
        playbook: {
          ...template,
          id: "copied",
          organizationId: "org_test",
          isDefault: false,
        },
      });
      throw new Error("Response lost");
    });
    await render();
    await chooseTemplate();
    fireEvent.click(
      screen.getByRole("button", { name: "Confirm template assignment" }),
    );
    await screen.findByText("Onboarding saved.");
    expect(api.assign).toHaveBeenCalledTimes(1);
    expect(api.create).not.toHaveBeenCalled();
  });
  it("recognizes a committed template copy on retry after reconciliation reads fail", async () => {
    api.assign.mockImplementationOnce(async () => {
      api.assignment.mockRejectedValue(new Error("Read unavailable"));
      throw new Error("Response lost");
    });
    await render();
    await chooseTemplate();
    fireEvent.click(
      screen.getByRole("button", { name: "Confirm template assignment" }),
    );
    await screen.findByText(/Your draft is retained/);
    api.assignment.mockResolvedValue({
      playbook: {
        ...template,
        id: "copied",
        organizationId: "org_test",
        isDefault: false,
      },
    });
    fireEvent.click(screen.getByRole("button", { name: "Retry wizard steps" }));
    fireEvent.click(
      await screen.findByRole("button", {
        name: "Confirm template assignment",
      }),
    );
    await screen.findByText("Onboarding saved.");
    expect(api.assign).toHaveBeenCalledTimes(1);
  });
  it.each([
    { organizationId: "org_other" },
    { name: "Another wizard" },
    { description: "Changed description" },
    { steps: [second] },
  ])(
    "does not accept a mismatched copy after ambiguous template assignment: %j",
    async (change) => {
      api.assign.mockImplementationOnce(async () => {
        api.assignment.mockResolvedValue({
          playbook: {
            ...template,
            id: "copied",
            organizationId: "org_test",
            ...change,
          },
        });
        throw new Error("Response lost");
      });
      await render();
      await chooseTemplate();
      fireEvent.click(
        screen.getByRole("button", { name: "Confirm template assignment" }),
      );
      await screen.findByText(/Your draft is retained/);
      expect(screen.queryByText("Onboarding saved.")).toBeNull();
    },
  );
  it("recovers a created plan after its response is lost without creating another", async () => {
    api.assignment.mockResolvedValue({ playbook: template });
    api.create.mockImplementationOnce(
      async (request: { stepSlugs: string[] }) => {
        api.templates.mockResolvedValue({
          playbooks: [
            template,
            owned,
            {
              ...owned,
              id: "recovered",
              name: "Example organization onboarding",
              description: "Tenant-specific setup wizard steps",
              steps: request.stepSlugs.map((slug) =>
                catalog.find((step) => step.slug === slug)!,
              ),
            },
          ],
        });
        throw new Error("Response lost");
      },
    );
    await render();
    await edit();
    fireEvent.click(screen.getByRole("button", { name: "Save wizard steps" }));
    await screen.findByText(/Your draft is retained/);
    fireEvent.click(screen.getByRole("button", { name: "Save wizard steps" }));
    await screen.findByText("Onboarding saved.");
    expect(api.create).toHaveBeenCalledTimes(1);
    expect(api.assign).toHaveBeenCalledExactlyOnceWith({
      organizationId: "org_test",
      playbookId: "recovered",
    });
  });
  it("does not repeat ambiguous creation while its result cannot be discovered", async () => {
    api.assignment.mockResolvedValue({ playbook: template });
    api.create.mockRejectedValueOnce(new Error("Response lost"));
    await render();
    await edit();
    fireEvent.click(screen.getByRole("button", { name: "Save wizard steps" }));
    await screen.findByText(/Your draft is retained/);
    fireEvent.click(screen.getByRole("button", { name: "Save wizard steps" }));
    await screen.findByText(/Creation could not be verified/);
    expect(api.create).toHaveBeenCalledTimes(1);
    expect(api.assign).not.toHaveBeenCalled();
  });
  it("does not reuse a pre-existing identical plan after ambiguous creation", async () => {
    const identical = {
      ...owned,
      name: "Example organization onboarding",
      description: "Tenant-specific setup wizard steps",
      steps: [group],
    };
    api.templates.mockResolvedValue({ playbooks: [template, identical] });
    api.assignment.mockResolvedValue({ playbook: template });
    api.create.mockRejectedValueOnce(new Error("Response lost"));
    await render();
    await edit();
    fireEvent.click(screen.getByRole("button", { name: "Save wizard steps" }));
    await screen.findByText(/Your draft is retained/);
    fireEvent.click(screen.getByRole("button", { name: "Save wizard steps" }));
    await screen.findByText(/Creation could not be verified/);
    expect(api.create).toHaveBeenCalledTimes(1);
    expect(api.assign).not.toHaveBeenCalled();
  });
  it("keeps template load errors separate from the assigned editor", async () => {
    api.templates.mockRejectedValue(new Error("Templates unavailable"));
    api.assignment.mockResolvedValue({ playbook: owned });
    await render();
    await screen.findByText(/Unable to load templates/);
    await edit();
    expect(
      screen.getByRole("button", { name: "Save wizard steps" }),
    ).toBeTruthy();
  });
  it("does not present failed assignment reads as fallback or allow writes", async () => {
    api.assignment.mockRejectedValue(new Error("Assignment unavailable"));
    await render();
    await screen.findByText(/Unable to load current wizard steps/);
    expect(
      screen.queryByText(/saved task selection and catalog defaults/),
    ).toBeNull();
    expect(
      screen.queryByRole("button", { name: "Edit wizard steps" }),
    ).toBeNull();
    api.assignment.mockResolvedValue({ playbook: owned });
    fireEvent.click(screen.getByRole("button", { name: "Retry wizard steps" }));
    await screen.findByText("Our wizard");
  });
  it("hides stale assignment on refresh errors", async () => {
    const client = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    api.assignment.mockResolvedValue({ playbook: owned });
    await render(client);
    await screen.findByText("Our wizard");
    api.assignment.mockRejectedValue(new Error("Refresh unavailable"));
    await act(() => client.invalidateQueries({ queryKey: ["assignment"] }));
    await screen.findByText(/Unable to load current wizard steps/);
    expect(screen.queryByText("Our wizard")).toBeNull();
  });
  it("retains a failed update draft for retry", async () => {
    api.assignment.mockResolvedValue({ playbook: owned });
    api.update.mockRejectedValueOnce(new Error("Update unavailable"));
    await render();
    await edit();
    fireEvent.click(
      screen.getByRole("button", { name: "Move Invite people up" }),
    );
    fireEvent.click(screen.getByRole("button", { name: "Save wizard steps" }));
    await screen.findByText(/Your draft is retained/);
    fireEvent.click(screen.getByRole("button", { name: "Save wizard steps" }));
    await screen.findByText("Onboarding saved.");
    expect(api.update).toHaveBeenCalledTimes(2);
  });
  it("preserves a custom copy and reuses it if assignment fails", async () => {
    // A shared assignment is copied rather than mutated, preserving shared ownership.
    api.assignment.mockResolvedValue({ playbook: template });
    api.assign.mockRejectedValue(new Error("Not applicable"));
    await render();
    await edit();
    fireEvent.click(screen.getByRole("button", { name: "Save wizard steps" }));
    await screen.findByText(/Unable to save onboarding/);
    expect(api.create).toHaveBeenCalledWith(
      expect.objectContaining({
        organizationId: "org_test",
        stepSlugs: ["setup"],
      }),
    );
    expect(api.assign).toHaveBeenCalledWith({
      organizationId: "org_test",
      playbookId: "created",
    });
    expect(api.remove).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button", { name: "Save wizard steps" }));
    await waitFor(() => expect(api.assign).toHaveBeenCalledTimes(2));
    expect(api.create).toHaveBeenCalledTimes(1);
    expect(api.update).not.toHaveBeenCalled();
  });
  it("verifies success when the assignment response is lost after commit", async () => {
    api.assignment.mockResolvedValue({ playbook: template });
    api.assign.mockImplementation(async () => {
      api.assignment.mockResolvedValue({
        playbook: { ...owned, id: "created", steps: [group] },
      });
      throw new Error("Response lost");
    });
    await render();
    await edit();
    fireEvent.click(screen.getByRole("button", { name: "Save wizard steps" }));
    await screen.findByText("Onboarding saved.");
    expect(api.remove).not.toHaveBeenCalled();
    expect(api.create).toHaveBeenCalledTimes(1);
  });
  it("preserves the new plan when reconciliation also fails", async () => {
    api.assignment.mockResolvedValue({ playbook: template });
    api.assign.mockImplementation(async () => {
      api.assignment.mockRejectedValue(new Error("Read unavailable"));
      throw new Error("Response lost");
    });
    await render();
    await edit();
    fireEvent.click(screen.getByRole("button", { name: "Save wizard steps" }));
    await screen.findByText(/new plan is preserved, not deleted/);
    expect(api.remove).not.toHaveBeenCalled();
    expect(screen.queryByText("Onboarding saved.")).toBeNull();
  });
  it.each([
    { ...owned, name: "New name" },
    { ...owned, description: "New description" },
    { ...owned, steps: [second, group] },
    { ...owned, id: "replacement" },
  ])("refuses an observed stale custom save: %j", async (changed) => {
    api.assignment.mockResolvedValue({ playbook: owned });
    await render();
    await edit();
    api.assignment.mockResolvedValue({ playbook: changed });
    fireEvent.click(screen.getByRole("button", { name: "Save wizard steps" }));
    await screen.findByText(/current plan changed since editing began/);
    expect(api.update).not.toHaveBeenCalled();
    expect(api.assign).not.toHaveBeenCalled();
    expect(screen.queryByText("Onboarding saved.")).toBeNull();
    fireEvent.click(
      screen.getByRole("button", {
        name: "Reload current plan and discard draft",
      }),
    );
    await screen.findByRole("button", { name: "Edit wizard steps" });
  });
  it("refuses template assignment when its confirmation base changed", async () => {
    api.assignment.mockResolvedValue({ playbook: owned });
    await render();
    await chooseTemplate();
    api.assignment.mockResolvedValue({
      playbook: { ...owned, id: "replacement" },
    });
    fireEvent.click(
      screen.getByRole("button", { name: "Confirm template assignment" }),
    );
    await screen.findByText(/current plan changed since editing began/);
    expect(api.assign).not.toHaveBeenCalled();
    expect(
      screen.getByRole("button", { name: "Confirm template assignment" }),
    ).toBeTruthy();
  });
  it("updates the retained plan to edited steps before retrying assignment", async () => {
    api.assignment.mockResolvedValue({ playbook: owned });
    // An unassigned custom plan starts from the same two selected steps.
    api.assignment.mockResolvedValue({
      playbook: { ...owned, organizationId: undefined },
    });
    api.assign.mockRejectedValueOnce(new Error("Not applicable"));
    api.update.mockResolvedValue({ ...owned, id: "created", steps: [group] });
    await render();
    await edit();
    fireEvent.click(screen.getByRole("button", { name: "Save wizard steps" }));
    await screen.findByText(/new plan is preserved, not deleted/);
    fireEvent.click(
      screen.getByRole("button", { name: "Remove Invite people" }),
    );
    fireEvent.click(screen.getByRole("button", { name: "Save wizard steps" }));
    await screen.findByText("Onboarding saved.");
    expect(api.create).toHaveBeenCalledTimes(1);
    expect(api.update).toHaveBeenCalledExactlyOnceWith({
      organizationId: "org_test",
      playbookId: "created",
      name: "Our wizard",
      description: "Keep description",
      stepSlugs: ["setup"],
    });
    expect(api.assign).toHaveBeenCalledTimes(2);
    expect(api.update.mock.invocationCallOrder[0]).toBeLessThan(
      api.assign.mock.invocationCallOrder[1]!,
    );
  });
  it("does not report success for an edited draft when the previous selection committed", async () => {
    api.assignment.mockResolvedValue({
      playbook: { ...owned, organizationId: undefined },
    });
    api.assign.mockRejectedValue(new Error("Response lost"));
    await render();
    await edit();
    fireEvent.click(screen.getByRole("button", { name: "Save wizard steps" }));
    await screen.findByText(/new plan is preserved, not deleted/);
    fireEvent.click(
      screen.getByRole("button", { name: "Remove Invite people" }),
    );
    api.assignment.mockResolvedValue({ playbook: { ...owned, id: "created" } });
    fireEvent.click(screen.getByRole("button", { name: "Save wizard steps" }));
    await screen.findByText(/previously saved selection is already assigned/);
    expect(screen.queryByText("Onboarding saved.")).toBeNull();
    expect(api.update).not.toHaveBeenCalled();
    expect(api.assign).toHaveBeenCalledTimes(1);
    expect(api.create).toHaveBeenCalledTimes(1);
  });
  it.each(["changed", "deleted"])(
    "refuses an externally %s retained plan on retry",
    async (change) => {
      api.assignment.mockResolvedValue({
        playbook: { ...owned, organizationId: undefined },
      });
      api.assign.mockRejectedValueOnce(new Error("Not applicable"));
      await render();
      await edit();
      fireEvent.click(
        screen.getByRole("button", { name: "Save wizard steps" }),
      );
      await screen.findByText(/new plan is preserved, not deleted/);
      api.templates.mockResolvedValue({
        playbooks:
          change === "deleted"
            ? [template]
            : [
                template,
                { ...owned, id: "created", name: "Externally edited" },
              ],
      });
      fireEvent.click(
        screen.getByRole("button", { name: "Remove Invite people" }),
      );
      fireEvent.click(
        screen.getByRole("button", { name: "Save wizard steps" }),
      );
      await screen.findByText(
        /reviewed template or retained plan changed or was deleted/,
      );
      expect(api.update).not.toHaveBeenCalled();
      expect(api.assign).toHaveBeenCalledTimes(1);
      expect(api.create).toHaveBeenCalledTimes(1);
    },
  );
  it("refuses to assign an unseen change to a reviewed shared template", async () => {
    await render();
    await chooseTemplate();
    api.templates.mockResolvedValue({
      playbooks: [{ ...template, steps: [second] }],
    });
    fireEvent.click(
      screen.getByRole("button", { name: "Confirm template assignment" }),
    );
    await screen.findByText(
      /reviewed template or retained plan changed or was deleted/,
    );
    expect(api.assign).not.toHaveBeenCalled();
    expect(
      screen.getByRole("button", { name: "Confirm template assignment" }),
    ).toBeTruthy();
  });
  it("creates and assigns a new tenant plan after choosing a top-level group", async () => {
    await render();
    await edit();
    fireEvent.click(screen.getByRole("combobox", { name: "Add a step" }));
    expect(screen.queryByRole("option", { name: "Connect a tool" })).toBeNull();
    fireEvent.click(
      await screen.findByRole("option", { name: "Set up tools" }),
    );
    expect(screen.getByText("Connect a tool")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Save wizard steps" }));
    await screen.findByText("Onboarding saved.");
    expect(api.create).toHaveBeenCalledWith(
      expect.objectContaining({
        organizationId: "org_test",
        stepSlugs: ["setup"],
      }),
    );
    expect(api.assign).toHaveBeenCalledWith({
      organizationId: "org_test",
      playbookId: "created",
    });
  });
  it("retains the draft when creation fails without assigning or deleting", async () => {
    api.assignment.mockResolvedValue({ playbook: template });
    api.create.mockRejectedValue(new Error("Creation unavailable"));
    await render();
    await edit();
    fireEvent.click(screen.getByRole("button", { name: "Save wizard steps" }));
    await screen.findByText(/Your draft is retained/);
    expect(api.assign).not.toHaveBeenCalled();
    expect(api.remove).not.toHaveBeenCalled();
  });
  it("guards dirty navigation and lets staff stay or explicitly leave", async () => {
    const { router } = await render();
    await edit();
    act(() => {
      void router.navigate({ to: "/onboarding-playbooks" });
    });
    await screen.findByRole("dialog", { name: "Leave onboarding changes?" });
    fireEvent.click(screen.getByRole("button", { name: "Stay on page" }));
    expect(router.state.location.pathname).toBe("/");
    act(() => {
      void router.navigate({ to: "/onboarding-playbooks" });
    });
    await screen.findByRole("dialog", { name: "Leave onboarding changes?" });
    fireEvent.click(
      screen.getByRole("button", { name: "Leave without saving" }),
    );
    await waitFor(() =>
      expect(router.state.location.pathname).toBe("/onboarding-playbooks"),
    );
    expect(api.create).not.toHaveBeenCalled();
  });
  it("guards repeated saves while pending", async () => {
    api.assignment.mockResolvedValue({ playbook: owned });
    let resolve: (value: unknown) => void = () => {};
    api.update.mockReturnValue(
      new Promise((done) => {
        resolve = done;
      }),
    );
    await render();
    await edit();
    fireEvent.click(screen.getByRole("button", { name: "Save wizard steps" }));
    expect(
      (
        (await screen.findByRole("button", {
          name: "Saving…",
        })) as HTMLButtonElement
      ).disabled,
    ).toBe(true);
    await waitFor(() => expect(api.update).toHaveBeenCalledTimes(1));
    expect(api.update).toHaveBeenCalledTimes(1);
    expect(
      (
        screen.getByRole("button", {
          name: "Discard changes",
        }) as HTMLButtonElement
      ).disabled,
    ).toBe(true);
    await act(async () => {
      resolve(owned);
    });
    await waitFor(() =>
      expect(screen.getByText("Onboarding saved.")).toBeTruthy(),
    );
  });
});
