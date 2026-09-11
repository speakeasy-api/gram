import { QueryClient } from "@tanstack/react-query";
import {
  act,
  cleanup,
  fireEvent,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { organizationOnboardingQuery } from "@/lib/gramAdminClient";
import { Onboarding } from "./Onboarding";
import { renderWithApp } from "@/test/harness";

const fixture = {
  organization_id: "org_onboarding_test",
  preset: "gateway",
  presets: [
    {
      key: "gateway",
      visible_task_keys: ["create-marketplace", "distribute-servers"],
    },
    {
      key: "security",
      visible_task_keys: ["create-marketplace", "enable-logging"],
    },
  ],
  tasks: [
    {
      key: "create-marketplace",
      title: "Create marketplace",
      description: "Create a marketplace",
      hidden: false,
    },
    {
      key: "distribute-servers",
      title: "Distribute servers",
      description: "Share servers",
      hidden: true,
    },
    {
      key: "enable-logging",
      title: "Enable logging",
      description: "Configure logs",
      hidden: false,
    },
  ],
};
const fetchMock = vi.fn();
let saved: Omit<typeof fixture, "preset"> & { preset?: string } = fixture;
beforeEach(() => {
  saved = structuredClone(fixture);
  fetchMock.mockReset().mockImplementation(async (request: Request) => {
    expect(new URL(request.url).pathname).toBe(
      "/admin/organization.onboarding",
    );
    if (request.method === "POST") {
      const body = await request.clone().json();
      saved = {
        ...saved,
        preset: body.preset,
        tasks: saved.tasks.map((task) => ({
          ...task,
          hidden: !body.visible_task_keys.includes(task.key),
        })),
      };
    }
    return new Response(JSON.stringify(saved), {
      headers: { "Content-Type": "application/json" },
    });
  });
  vi.stubGlobal("fetch", fetchMock);
});
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});
const writes = () =>
  fetchMock.mock.calls
    .map(([request]) => request as Request)
    .filter((request) => request.method === "POST");

describe("Onboarding", () => {
  it("reconciles an unapplied preset choice when editing tasks", async () => {
    await renderWithApp(
      <Onboarding organizationId={fixture.organization_id} />,
    );
    const preset = await screen.findByRole("combobox", {
      name: "Onboarding preset",
    });
    fireEvent.keyDown(preset, { key: "ArrowDown" });
    fireEvent.click(await screen.findByRole("option", { name: "Security" }));
    expect(preset.textContent).toContain("Security");
    fireEvent.click(
      screen.getByRole("checkbox", { name: "Distribute servers" }),
    );
    expect(preset.textContent).toContain("Gateway");
    fireEvent.click(screen.getByRole("button", { name: "Save onboarding" }));
    await screen.findByText("Onboarding saved.");
    expect((await writes()[0]!.clone().json()).preset).toBe("gateway");
  });

  it("loads effective custom selection, not preset defaults, and saves only on request", async () => {
    await renderWithApp(
      <Onboarding organizationId={fixture.organization_id} />,
    );
    const distribute = await screen.findByRole("checkbox", {
      name: "Distribute servers",
    });
    expect(distribute.getAttribute("data-state")).toBe("unchecked");
    expect(screen.getByText("gateway - customized")).toBeTruthy();
    fireEvent.click(distribute);
    expect(writes()).toHaveLength(0);
    fireEvent.click(screen.getByRole("button", { name: "Save onboarding" }));
    await screen.findByText("Onboarding saved.");
    expect(writes()).toHaveLength(1);
    expect(await writes()[0]!.clone().json()).toEqual({
      organization_id: fixture.organization_id,
      preset: "gateway",
      visible_task_keys: [
        "create-marketplace",
        "enable-logging",
        "distribute-servers",
      ],
    });
  });

  it("requires confirmation before reapplying a preset and permits discarding the draft", async () => {
    await renderWithApp(
      <Onboarding organizationId={fixture.organization_id} />,
    );
    fireEvent.click(
      await screen.findByRole("button", { name: "Apply preset" }),
    );
    await screen.findByRole("dialog");
    fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
    expect(
      screen
        .getByRole("checkbox", { name: "Distribute servers" })
        .getAttribute("data-state"),
    ).toBe("unchecked");
    fireEvent.click(screen.getByRole("button", { name: "Apply preset" }));
    fireEvent.click(
      await screen.findByRole("button", { name: "Apply to draft" }),
    );
    await waitFor(() =>
      expect(
        screen
          .getByRole("checkbox", { name: "Distribute servers" })
          .getAttribute("data-state"),
      ).toBe("checked"),
    );
    expect(
      screen
        .getByRole("checkbox", { name: "Enable logging" })
        .getAttribute("data-state"),
    ).toBe("unchecked");
    expect(writes()).toHaveLength(0);
    fireEvent.click(screen.getByRole("button", { name: "Discard changes" }));
    expect(
      screen
        .getByRole("checkbox", { name: "Distribute servers" })
        .getAttribute("data-state"),
    ).toBe("unchecked");
  });

  it("keeps a failed empty-selection draft and supports retry without a preset", async () => {
    saved = { ...saved, preset: undefined };
    await renderWithApp(
      <Onboarding organizationId={fixture.organization_id} />,
    );
    await screen.findByText("Legacy");
    fireEvent.click(
      screen.getByRole("checkbox", { name: "Create marketplace" }),
    );
    fireEvent.click(screen.getByRole("checkbox", { name: "Enable logging" }));
    fetchMock.mockRejectedValueOnce(new Error("write failed"));
    fireEvent.click(screen.getByRole("button", { name: "Save onboarding" }));
    await screen.findByRole("alert");
    expect(screen.getByText(/0 of 3 tasks selected/)).toBeTruthy();
    expect(screen.queryByText("Onboarding saved.")).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Save onboarding" }));
    await screen.findByText("Onboarding saved.");
    expect(await writes()[1]!.clone().json()).toEqual({
      organization_id: fixture.organization_id,
      visible_task_keys: [],
    });
  });

  it("reports load failures and retries", async () => {
    fetchMock.mockRejectedValueOnce(new Error("read failed"));
    await renderWithApp(
      <Onboarding organizationId={fixture.organization_id} />,
      {
        queryClient: new QueryClient({
          defaultOptions: { queries: { retry: false, throwOnError: true } },
        }),
      },
    );
    fireEvent.click(
      await screen.findByRole("button", { name: "Retry onboarding" }),
    );
    await screen.findByRole("checkbox", { name: "Create marketplace" });
  });

  it("does not lose drafts to a refetch or leak them to another organization", async () => {
    const client = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    const mounted = await renderWithApp(
      <Onboarding organizationId={fixture.organization_id} />,
      { queryClient: client },
    );
    fireEvent.click(
      await screen.findByRole("checkbox", { name: "Distribute servers" }),
    );
    await act(() =>
      client.invalidateQueries(
        organizationOnboardingQuery(fixture.organization_id),
      ),
    );
    expect(
      screen
        .getByRole("checkbox", { name: "Distribute servers" })
        .getAttribute("data-state"),
    ).toBe("checked");
    mounted.unmount();
    await renderWithApp(<Onboarding organizationId="org_other" />, {
      queryClient: client,
    });
    expect(
      (
        await screen.findByRole("checkbox", { name: "Distribute servers" })
      ).getAttribute("data-state"),
    ).toBe("unchecked");
    expect(organizationOnboardingQuery("org_other").queryKey).not.toEqual(
      organizationOnboardingQuery(fixture.organization_id).queryKey,
    );
  });

  it("disables writes and task changes while saving", async () => {
    await renderWithApp(
      <Onboarding organizationId={fixture.organization_id} />,
    );
    fireEvent.click(
      await screen.findByRole("checkbox", { name: "Distribute servers" }),
    );
    fetchMock.mockImplementationOnce(() => new Promise(() => {}));
    fireEvent.click(screen.getByRole("button", { name: "Save onboarding" }));
    expect(
      (
        (await screen.findByRole("button", {
          name: "Saving...",
        })) as HTMLButtonElement
      ).disabled,
    ).toBe(true);
    for (const checkbox of screen.getAllByRole("checkbox"))
      expect((checkbox as HTMLButtonElement).disabled).toBe(true);
    expect(writes()).toHaveLength(1);
  });
});
