import { cleanup, fireEvent, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { OnboardingStack } from "./OnboardingStack";
import { renderWithApp } from "@/test/harness";

const options = {
  vendors: [
    {
      vendor: "Anthropic",
      plans: [
        { slug: "anthropic-team", name: "Team" },
        { slug: "anthropic-enterprise", name: "Enterprise" },
      ],
      platforms: [
        {
          slug: "claude-code-cli",
          name: "Claude Code · CLI",
          family: "Claude Code",
          surface: "CLI",
        },
        {
          slug: "claude-chat-web",
          name: "Claude Chat · Web",
          family: "Claude Chat",
          surface: "Web",
        },
      ],
    },
    {
      vendor: "OpenCode",
      plans: [],
      platforms: [
        {
          slug: "opencode-app",
          name: "OpenCode",
          family: "OpenCode",
          surface: "App",
        },
      ],
    },
  ],
  mdm_vendors: [
    { slug: "jamf", name: "Jamf Pro" },
    { slug: "other", name: "Other" },
  ],
};
const recorded = {
  organization_id: "org_stack_test",
  vendors: [{ vendor: "Anthropic", plan_slug: "anthropic-team" }],
  mdm_vendor: "jamf",
};
type Saved = {
  organization_id: string;
  vendors: { vendor: string; plan_slug?: string }[];
  mdm_vendor?: string;
  mdm_vendor_name?: string;
};
const fetchMock = vi.fn();
let saved: Saved = recorded;
/** How many of the next requests of each kind answer 500. */
const failures = { options: 0, stack: 0, save: 0 };
function failing(kind: keyof typeof failures): boolean {
  if (failures[kind] === 0) return false;
  failures[kind] -= 1;
  return true;
}
beforeEach(() => {
  saved = structuredClone(recorded);
  failures.options = 0;
  failures.stack = 0;
  failures.save = 0;
  fetchMock.mockReset().mockImplementation(async (request: Request) => {
    const path = new URL(request.url).pathname;
    if (path === "/admin/onboarding.stackOptions") {
      if (failing("options")) return new Response("{}", { status: 500 });
      return json(options);
    }
    expect(path).toBe("/admin/organization.onboardingStack");
    if (failing(request.method === "POST" ? "save" : "stack")) {
      return new Response("{}", { status: 500 });
    }
    if (request.method === "POST") {
      const body = await request.clone().json();
      saved = {
        organization_id: body.organization_id,
        vendors: body.vendors,
        mdm_vendor: body.mdm_vendor,
        mdm_vendor_name: body.mdm_vendor_name,
      };
    }
    return json(saved);
  });
  vi.stubGlobal("fetch", fetchMock);
});
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});
function json(value: unknown): Response {
  return new Response(JSON.stringify(value), {
    headers: { "Content-Type": "application/json" },
  });
}
const writes = () =>
  fetchMock.mock.calls
    .map(([request]) => request as Request)
    .filter((request) => request.method === "POST");
const lastWrite = async () => writes().at(-1)!.clone().json();

describe("OnboardingStack", () => {
  it("shows the recorded stack against the catalog", async () => {
    await renderWithApp(<OnboardingStack organizationId="org_stack_test" />);
    expect(
      (await screen.findByRole("checkbox", { name: "Anthropic" })).getAttribute(
        "aria-checked",
      ),
    ).toBe("true");
    expect(
      screen
        .getByRole("checkbox", { name: "OpenCode" })
        .getAttribute("aria-checked"),
    ).toBe("false");
    expect(
      screen.getByRole("combobox", { name: "Anthropic plan" }).textContent,
    ).toContain("Team");
    expect(
      screen
        .getByRole("checkbox", { name: "Uses device management software" })
        .getAttribute("aria-checked"),
    ).toBe("true");
    expect(
      (screen.getByRole("button", { name: "Save stack" }) as HTMLButtonElement)
        .disabled,
    ).toBe(true);
  });

  it("needs a plan for a vendor that sells them, and none for one that does not", async () => {
    await renderWithApp(<OnboardingStack organizationId="org_stack_test" />);
    const anthropic = await screen.findByRole("checkbox", {
      name: "Anthropic",
    });
    fireEvent.click(anthropic);
    fireEvent.click(anthropic);
    const save = screen.getByRole("button", {
      name: "Save stack",
    }) as HTMLButtonElement;
    expect(save.disabled).toBe(true);
    expect(screen.getByText("Choose the plan for Anthropic.")).toBeTruthy();

    const plan = screen.getByRole("combobox", { name: "Anthropic plan" });
    fireEvent.keyDown(plan, { key: "ArrowDown" });
    fireEvent.click(await screen.findByRole("option", { name: "Enterprise" }));
    fireEvent.click(screen.getByRole("checkbox", { name: "OpenCode" }));
    expect(save.disabled).toBe(false);

    fireEvent.click(save);
    await screen.findByText("Stack saved.");
    expect(await lastWrite()).toEqual({
      organization_id: "org_stack_test",
      vendors: [
        { vendor: "Anthropic", plan_slug: "anthropic-enterprise" },
        { vendor: "OpenCode" },
      ],
      mdm_vendor: "jamf",
    });
  });

  it("records other device management with its name", async () => {
    await renderWithApp(<OnboardingStack organizationId="org_stack_test" />);
    const software = await screen.findByRole("combobox", {
      name: "Device management software",
    });
    expect(software.textContent).toContain("Jamf Pro");
    fireEvent.keyDown(software, { key: "ArrowDown" });
    fireEvent.click(await screen.findByRole("option", { name: "Other" }));
    const save = screen.getByRole("button", {
      name: "Save stack",
    }) as HTMLButtonElement;
    expect(save.disabled).toBe(true);
    expect(
      screen.getByText("Name the device management software."),
    ).toBeTruthy();

    fireEvent.change(
      screen.getByRole("textbox", { name: "Device management software name" }),
      { target: { value: " Fleet " } },
    );
    expect(save.disabled).toBe(false);
    fireEvent.click(save);
    await screen.findByText("Stack saved.");
    const body = await lastWrite();
    expect(body.mdm_vendor).toBe("other");
    expect(body.mdm_vendor_name).toBe("Fleet");
    expect(body.vendors).toEqual([
      { vendor: "Anthropic", plan_slug: "anthropic-team" },
    ]);
  });

  it("clears device management when the organization has none", async () => {
    await renderWithApp(<OnboardingStack organizationId="org_stack_test" />);
    fireEvent.click(
      await screen.findByRole("checkbox", {
        name: "Uses device management software",
      }),
    );
    expect(
      screen.queryByRole("combobox", { name: "Device management software" }),
    ).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Save stack" }));
    await screen.findByText("Stack saved.");
    const body = await lastWrite();
    expect(body.mdm_vendor).toBe("none");
    expect(body.mdm_vendor_name).toBeUndefined();
  });

  it("discards a draft", async () => {
    await renderWithApp(<OnboardingStack organizationId="org_stack_test" />);
    fireEvent.click(await screen.findByRole("checkbox", { name: "OpenCode" }));
    fireEvent.click(screen.getByRole("button", { name: "Discard" }));
    expect(
      screen
        .getByRole("checkbox", { name: "OpenCode" })
        .getAttribute("aria-checked"),
    ).toBe("false");
    expect(writes()).toHaveLength(0);
  });

  it("keeps the name of other software within the server's limit", async () => {
    await renderWithApp(<OnboardingStack organizationId="org_stack_test" />);
    const software = await screen.findByRole("combobox", {
      name: "Device management software",
    });
    fireEvent.keyDown(software, { key: "ArrowDown" });
    fireEvent.click(await screen.findByRole("option", { name: "Other" }));
    const name = screen.getByRole("textbox", {
      name: "Device management software name",
    });
    expect(name.getAttribute("maxlength")).toBe("200");
    fireEvent.change(name, { target: { value: "x".repeat(201) } });
    const save = screen.getByRole("button", {
      name: "Save stack",
    }) as HTMLButtonElement;
    expect(save.disabled).toBe(true);
    expect(
      screen.getByText(
        "Keep the device management software name to 200 characters.",
      ),
    ).toBeTruthy();
    fireEvent.change(name, { target: { value: "x".repeat(200) } });
    expect(save.disabled).toBe(false);
    expect(writes()).toHaveLength(0);
  });

  it("reports a failed save, keeps the draft and saves it on retry", async () => {
    failures.save = 1;
    await renderWithApp(<OnboardingStack organizationId="org_stack_test" />);
    fireEvent.click(await screen.findByRole("checkbox", { name: "OpenCode" }));
    fireEvent.click(screen.getByRole("button", { name: "Save stack" }));
    await screen.findByRole("alert");
    expect(screen.queryByText("Stack saved.")).toBeNull();
    expect(
      screen
        .getByRole("checkbox", { name: "OpenCode" })
        .getAttribute("aria-checked"),
    ).toBe("true");
    fireEvent.click(screen.getByRole("button", { name: "Save stack" }));
    await screen.findByText("Stack saved.");
    expect(writes()).toHaveLength(2);
    expect((await lastWrite()).vendors).toEqual([
      { vendor: "Anthropic", plan_slug: "anthropic-team" },
      { vendor: "OpenCode" },
    ]);
  });

  it.each(["options", "stack"] as const)(
    "reports a failed %s load and retries",
    async (kind) => {
      failures[kind] = 1;
      await renderWithApp(<OnboardingStack organizationId="org_stack_test" />);
      await screen.findByText("Unable to load the stack.");
      fireEvent.click(screen.getByRole("button", { name: "Retry stack" }));
      await screen.findByRole("checkbox", { name: "Anthropic" });
    },
  );
});
