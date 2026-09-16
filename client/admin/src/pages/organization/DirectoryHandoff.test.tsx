import { QueryClient } from "@tanstack/react-query";
import {
  cleanup,
  fireEvent,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { DirectoryHandoffSection } from "@/pages/organization/DirectoryHandoff";
import type {
  DirectoryHandoff,
  DirectoryHandoffResult,
} from "@/lib/gramAdminApi";
import { anOrganization } from "@/test/fixtures";
import { renderWithApp } from "@/test/harness";

const mocks = vi.hoisted(() => ({
  getOrganizationDirectoryHandoff: vi.fn(),
  setOrganizationDirectoryHandoff: vi.fn(),
  clearOrganizationDirectoryHandoff: vi.fn(),
}));

vi.mock("@/lib/gramAdminApi", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/lib/gramAdminApi")>();
  return {
    ...actual,
    getOrganizationDirectoryHandoff: mocks.getOrganizationDirectoryHandoff,
    setOrganizationDirectoryHandoff: mocks.setOrganizationDirectoryHandoff,
    clearOrganizationDirectoryHandoff: mocks.clearOrganizationDirectoryHandoff,
  };
});

// Invented throughout, like every other fixture in this app: the repository is
// public and no test names a real organization, directory or operator.
const ORG = anOrganization({ workos_id: "org_workos_placeholder" });

const ENDPOINT = "https://api.example.test/scim/v2/placeholder";
const TOKEN = "placeholder-bearer-token";

const STORED: DirectoryHandoff = {
  organization_id: ORG.id,
  scim_base_url: ENDPOINT,
  token_fingerprint: "a1b2c3d4",
  workos_directory_id: "directory_placeholder",
  workos_directory_state: "linked",
  set_by: "operator@example.test",
  updated_at: "2026-02-01T00:00:00Z",
};

function result(
  overrides: Partial<DirectoryHandoffResult> = {},
): DirectoryHandoffResult {
  return { handoff: null, workos_environment: "production", ...overrides };
}

function freshClient(): QueryClient {
  return new QueryClient({ defaultOptions: { queries: { retry: false } } });
}

async function mount(): Promise<void> {
  await renderWithApp(<DirectoryHandoffSection org={ORG} />, {
    queryClient: freshClient(),
  });
}

/**
 * The section asks before it writes, the way every other admin write does. The
 * confirmation carries the same words as the control that opened it, so the
 * press is scoped to the dialog rather than left to find one of the two.
 */
async function confirmWith(label: string): Promise<void> {
  const dialog = await screen.findByRole("dialog");
  fireEvent.click(within(dialog).getByRole("button", { name: label }));
}

function tokenInput(): HTMLInputElement {
  return screen.getByLabelText("Bearer token") as HTMLInputElement;
}

function endpointInput(): HTMLInputElement {
  return screen.getByLabelText("Directory endpoint") as HTMLInputElement;
}

beforeEach(() => {
  for (const mock of Object.values(mocks)) mock.mockReset();
  mocks.getOrganizationDirectoryHandoff.mockResolvedValue(result());
  mocks.setOrganizationDirectoryHandoff.mockResolvedValue(STORED);
  mocks.clearOrganizationDirectoryHandoff.mockResolvedValue(undefined);
});

afterEach(() => {
  cleanup();
});

describe("DirectoryHandoffSection", () => {
  it("names the environment the server's key belongs to", async () => {
    await mount();

    expect(
      await screen.findByText(/WorkOS Production environment/),
    ).toBeTruthy();
    // No environment id is guessed into the link: nothing this app reads
    // carries one, so it stops at the organizations list.
    const link = screen.getByRole("link", {
      name: "Open WorkOS organizations",
    });
    expect(link.getAttribute("href")).toBe(
      "https://dashboard.workos.com/organizations",
    );
  });

  it("says so rather than guessing when the environment is unknown", async () => {
    mocks.getOrganizationDirectoryHandoff.mockResolvedValue(
      result({ workos_environment: "unknown" }),
    );
    await mount();

    expect(
      await screen.findByText(/does not report which WorkOS environment/),
    ).toBeTruthy();
  });

  it("keeps the token in a password field that is never prefilled", async () => {
    mocks.getOrganizationDirectoryHandoff.mockResolvedValue(
      result({ handoff: STORED }),
    );
    await mount();

    // Stored handoff on screen, and still nothing in the field: no read this
    // page makes answers with a token, so there is nothing to prefill from.
    expect(await screen.findByText(STORED.token_fingerprint)).toBeTruthy();
    expect(tokenInput().getAttribute("type")).toBe("password");
    expect(tokenInput().value).toBe("");
  });

  it("sends both values and then holds neither", async () => {
    await mount();
    await screen.findByRole("button", { name: "Store handoff" });

    fireEvent.change(endpointInput(), { target: { value: ENDPOINT } });
    fireEvent.change(tokenInput(), { target: { value: TOKEN } });
    fireEvent.click(screen.getByRole("button", { name: "Store handoff" }));
    await confirmWith("Store handoff");

    // The first argument alone: React Query hands a mutation function its
    // variables and then its own context, and this test is about the request.
    await waitFor(() => {
      expect(mocks.setOrganizationDirectoryHandoff.mock.calls[0]?.[0]).toEqual({
        organization_id: ORG.id,
        scim_base_url: ENDPOINT,
        scim_token: TOKEN,
      });
    });
    // The request was the last place the token existed in this tab.
    await waitFor(() => {
      expect(tokenInput().value).toBe("");
    });
    expect(endpointInput().value).toBe("");
    expect(document.body.textContent).not.toContain(TOKEN);
  });

  it("refuses an endpoint that is not https without asking the server", async () => {
    await mount();
    await screen.findByRole("button", { name: "Store handoff" });

    fireEvent.change(endpointInput(), {
      target: { value: "http://api.example.test/scim/v2/placeholder" },
    });
    fireEvent.change(tokenInput(), { target: { value: TOKEN } });
    fireEvent.click(screen.getByRole("button", { name: "Store handoff" }));

    expect(await screen.findByRole("alert")).toBeTruthy();
    expect(screen.getByRole("alert").textContent).toContain("https");
    expect(mocks.setOrganizationDirectoryHandoff).not.toHaveBeenCalled();
  });

  it("clears the stored handoff once the operator confirms", async () => {
    mocks.getOrganizationDirectoryHandoff.mockResolvedValue(
      result({ handoff: STORED }),
    );
    await mount();

    fireEvent.click(
      await screen.findByRole("button", { name: "Clear handoff" }),
    );
    await confirmWith("Clear handoff");

    await waitFor(() => {
      expect(mocks.clearOrganizationDirectoryHandoff).toHaveBeenCalledWith(
        ORG.id,
      );
    });
  });

  it("reports a directory WorkOS does not have rather than leaving it blank", async () => {
    mocks.getOrganizationDirectoryHandoff.mockResolvedValue(
      result({
        handoff: {
          ...STORED,
          workos_directory_id: undefined,
          workos_directory_state: undefined,
        },
      }),
    );
    await mount();

    expect(await screen.findByText(/Not found in WorkOS/)).toBeTruthy();
  });
});
