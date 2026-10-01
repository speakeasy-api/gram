import { QueryClient } from "@tanstack/react-query";
import { act, cleanup, fireEvent, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { OnboardingPlaybook } from "./OnboardingPlaybook";
import { renderWithApp } from "@/test/harness";

type Assigned = {
  organization_id: string;
  playbook?: {
    id: string;
    use_case_id: string;
    use_case_slug: string;
    use_case_name: string;
    name: string;
    description: string;
    is_default: boolean;
    organization_id?: string;
    steps: { slug: string; title: string }[];
  };
  applicability: {
    slug: string;
    title: string;
    applies: boolean;
    reason: string;
  }[];
};
const fetchMock = vi.fn();
let assigned: Assigned = { organization_id: "org_pb_test", applicability: [] };
let failures = 0;
beforeEach(() => {
  assigned = { organization_id: "org_pb_test", applicability: [] };
  failures = 0;
  fetchMock.mockReset().mockImplementation(async (request: Request) => {
    expect(new URL(request.url).pathname).toBe(
      "/admin/organization.onboardingPlaybook",
    );
    expect(request.method).toBe("GET");
    if (failures > 0) {
      failures -= 1;
      return new Response("{}", { status: 500 });
    }
    return new Response(JSON.stringify(assigned), {
      headers: { "Content-Type": "application/json" },
    });
  });
  vi.stubGlobal("fetch", fetchMock);
});
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

describe("OnboardingPlaybook", () => {
  it("links the assigned playbook by name", async () => {
    assigned = {
      organization_id: "org_pb_test",
      playbook: {
        id: "pb-custom",
        use_case_id: "uc-identity",
        use_case_slug: "identity",
        use_case_name: "Identity",
        name: "Ours",
        description: "",
        is_default: false,
        organization_id: "org_pb_test",
        steps: [{ slug: "domain-verification", title: "Verify your domain" }],
      },
      applicability: [
        {
          slug: "domain-verification",
          title: "Verify your domain",
          applies: true,
          reason: "",
        },
      ],
    };
    await renderWithApp(<OnboardingPlaybook organizationId="org_pb_test" />);
    // Just the name, and the name is the link out.
    const link = await screen.findByRole("link", { name: "Ours" });
    expect(link.getAttribute("href")).toBe(
      "/onboarding-playbooks?organization=org_pb_test",
    );
    expect(screen.queryByText(/Identity/)).toBeNull();
    expect(screen.queryByText("Verify your domain")).toBeNull();
    expect(screen.queryByRole("button")).toBeNull();
  });

  it("links out even when nothing is assigned", async () => {
    await renderWithApp(<OnboardingPlaybook organizationId="org_pb_test" />);
    const link = await screen.findByRole("link", { name: "Not assigned" });
    expect(link.getAttribute("href")).toBe(
      "/onboarding-playbooks?organization=org_pb_test",
    );
  });

  it("reports load failures and retries", async () => {
    failures = 1;
    await renderWithApp(<OnboardingPlaybook organizationId="org_pb_test" />);
    await screen.findByText("Unable to load the playbook.");
    fireEvent.click(screen.getByRole("button", { name: "Retry playbook" }));
    await screen.findByText("Not assigned");
  });

  it("hides a stale name when a refresh fails and retries", async () => {
    const client = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    await renderWithApp(<OnboardingPlaybook organizationId="org_pb_test" />, {
      queryClient: client,
    });
    await screen.findByRole("link", { name: "Not assigned" });
    failures = 1;
    await act(() => client.invalidateQueries());
    await screen.findByText("Unable to load the playbook.");
    expect(screen.queryByRole("link")).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Retry playbook" }));
    await screen.findByRole("link", { name: "Not assigned" });
  });
});
