import { cleanup, fireEvent, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { OnboardingSteps } from "./Steps";
import { renderWithApp } from "@/test/harness";

const steps = [
  {
    slug: "domain-verification",
    title: "Verify your domain",
    description: "Prove the organization owns its email domain.",
    completion: "fact",
    hidden_by_default: false,
    method_slugs: [],
    requires: [],
  },
  {
    slug: "identity-provider",
    title: "Set up identity provider",
    description: "Connect single sign-on and sync the directory.",
    completion: "children",
    hidden_by_default: false,
    method_slugs: [],
    requires: [],
  },
  {
    slug: "connect-idp",
    title: "Connect identity provider",
    description: "Configure single sign-on for the organization.",
    parent_slug: "identity-provider",
    completion: "fact",
    hidden_by_default: false,
    method_slugs: [],
    requires: ["domain-verification"],
  },
  {
    slug: "directory-sync",
    title: "Set up directory sync",
    description: "Sync people and groups from the identity provider.",
    parent_slug: "identity-provider",
    completion: "fact",
    hidden_by_default: false,
    method_slugs: [],
    requires: ["connect-idp"],
  },
  {
    slug: "anthropic-observability",
    title: "Set up Anthropic observability",
    description: "Turn on inference hooks.",
    completion: "manual",
    hidden_by_default: true,
    method_slugs: ["inference"],
    requires: ["connect-idp"],
  },
];
const fetchMock = vi.fn();
let failures = 0;
beforeEach(() => {
  failures = 0;
  fetchMock.mockReset().mockImplementation(async (request: Request) => {
    expect(new URL(request.url).pathname).toBe("/admin/onboarding.steps");
    if (failures > 0) {
      failures -= 1;
      return new Response("{}", { status: 500 });
    }
    return new Response(JSON.stringify({ steps }), {
      headers: { "Content-Type": "application/json" },
    });
  });
  vi.stubGlobal("fetch", fetchMock);
});
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

function rowSlugs(container: HTMLElement): (string | null)[] {
  return Array.from(container.querySelectorAll("tr[data-step]")).map((row) =>
    row.getAttribute("data-step"),
  );
}

describe("OnboardingSteps", () => {
  it("lists top-level steps and expands a group's cards under it", async () => {
    const { container } = await renderWithApp(<OnboardingSteps />);
    await screen.findByText("Set up identity provider");
    expect(
      screen.getAllByRole("columnheader").map((cell) => cell.textContent),
    ).toEqual(["Step", "Description", "Requires"]);
    // Collapsed by default: the top-level list, as the playbook editor sees it.
    expect(rowSlugs(container)).toEqual([
      "domain-verification",
      "identity-provider",
      "anthropic-observability",
    ]);
    const group = screen.getByRole("button", {
      name: "Set up identity provider",
    });
    expect(group.getAttribute("aria-expanded")).toBe("false");
    expect(screen.queryByRole("button", { name: "Verify your domain" })).toBe(
      null,
    );

    fireEvent.click(group);
    expect(group.getAttribute("aria-expanded")).toBe("true");
    expect(rowSlugs(container)).toEqual([
      "domain-verification",
      "identity-provider",
      "connect-idp",
      "directory-sync",
      "anthropic-observability",
    ]);
    const card = container.querySelector('tr[data-step="connect-idp"]');
    expect(card?.getAttribute("data-parent")).toBe("identity-provider");
    const cells = card?.querySelectorAll("td") ?? [];
    // Cards step in under their group; the group row itself does not.
    expect(cells[0]?.className).toContain("pl-10");
    expect(
      container.querySelector('tr[data-step="identity-provider"] td')
        ?.className,
    ).not.toContain("pl-10");
    expect(cells[1]?.textContent).toBe(
      "Configure single sign-on for the organization.",
    );
    expect(cells[1]?.className).toContain("whitespace-normal");
    // Prerequisites are links named by title; no slug is shown anywhere.
    expect(
      screen
        .getByRole("link", { name: "Verify your domain" })
        .getAttribute("href"),
    ).toBe("#step-domain-verification");
    expect(screen.queryByText("domain-verification")).toBeNull();
    expect(screen.queryByText("inference")).toBeNull();

    fireEvent.click(group);
    expect(rowSlugs(container)).toEqual([
      "domain-verification",
      "identity-provider",
      "anthropic-observability",
    ]);
  });

  it("follows a prerequisite link into its collapsed group", async () => {
    const { container } = await renderWithApp(<OnboardingSteps />);
    await screen.findByText("Set up identity provider");
    // The observability step requires a card that is hidden in its group.
    fireEvent.click(
      screen.getByRole("link", { name: "Connect identity provider" }),
    );
    expect(
      screen
        .getByRole("button", { name: "Set up identity provider" })
        .getAttribute("aria-expanded"),
    ).toBe("true");
    const card = container.querySelector('tr[data-step="connect-idp"]');
    expect(card?.getAttribute("id")).toBe("step-connect-idp");
    expect(card?.getAttribute("data-highlighted")).toBe("true");
    expect(
      container
        .querySelector('tr[data-step="domain-verification"]')
        ?.getAttribute("data-highlighted"),
    ).toBeNull();
  });

  it("reports load failures and retries", async () => {
    failures = 1;
    await renderWithApp(<OnboardingSteps />);
    await screen.findByRole("alert");
    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    await screen.findByText("Set up identity provider");
  });
});
