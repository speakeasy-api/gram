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
let optionsFailures = 0;
beforeEach(() => {
  saved = structuredClone(recorded);
  optionsFailures = 0;
  fetchMock.mockReset().mockImplementation(async (request: Request) => {
    const path = new URL(request.url).pathname;
    if (path === "/admin/onboarding.stackOptions") {
      if (optionsFailures > 0) {
        optionsFailures -= 1;
        return new Response("{}", { status: 500 });
      }
      return json(options);
    }
    expect(path).toBe("/admin/organization.onboardingStack");
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

  it("reports load failures and retries", async () => {
    optionsFailures = 1;
    await renderWithApp(<OnboardingStack organizationId="org_stack_test" />);
    await screen.findByText("Unable to load the stack.");
    fireEvent.click(screen.getByRole("button", { name: "Retry stack" }));
    await screen.findByRole("checkbox", { name: "Anthropic" });
  });
});
