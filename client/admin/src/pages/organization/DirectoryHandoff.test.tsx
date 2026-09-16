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
import { anOrganization } from "@/test/fixtures";
import { renderWithApp } from "@/test/harness";

// Stubbed at the fetch layer rather than the module, because these three
// endpoints go through the generated client: a module mock would leave the
// generated decode untested, and the decode is what catches the server and the
// model disagreeing about a field name.
const mocks = vi.hoisted(() => ({ fetch: vi.fn() }));

// Invented throughout, like every other fixture in this app: the repository is
// public and no test names a real organization, directory or operator.
const ORG = anOrganization({ workos_id: "org_workos_placeholder" });

const ENDPOINT = "https://api.example.test/scim/v2/placeholder";
const TOKEN = "placeholder-bearer-token";

/** The wire shape, which is snake_case; the model the section reads is not. */
const STORED = {
  organization_id: ORG.id,
  scim_base_url: ENDPOINT,
  token_fingerprint: "a1b2c3d4",
  workos_directory_id: "directory_placeholder",
  workos_directory_state: "linked",
  set_by: "operator@example.test",
  updated_at: "2026-02-01T00:00:00Z",
};

type StoredHandoff = Partial<typeof STORED>;

function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

// One handler for all three endpoints, so a test says what is stored and the
// routing stays in one place.
function serve(options: {
  handoff?: StoredHandoff | null;
  environment?: string;
}): void {
  const { handoff = null, environment = "production" } = options;
  mocks.fetch.mockImplementation(async (input: RequestInfo | URL) => {
    const request = input as Request;
    const { pathname } = new URL(request.url);
    switch (pathname) {
      case "/admin/organization.directoryHandoff":
        return jsonResponse({
          ...(handoff ? { handoff } : {}),
          workos_environment: environment,
        });
      case "/admin/organization.setDirectoryHandoff":
        return jsonResponse(STORED);
      case "/admin/organization.clearDirectoryHandoff":
        return new Response(null, { status: 204 });
      default:
        throw new Error(`unexpected request to ${pathname}`);
    }
  });
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

function requestsTo(pathname: string): Request[] {
  return mocks.fetch.mock.calls
    .map((call) => call[0] as Request)
    .filter((request) => new URL(request.url).pathname === pathname);
}

beforeEach(() => {
  mocks.fetch.mockReset();
  serve({});
  vi.stubGlobal("fetch", mocks.fetch);
});

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
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
    serve({ environment: "unknown" });
    await mount();

    expect(
      await screen.findByText(/does not report which WorkOS environment/),
    ).toBeTruthy();
  });

  it("keeps the token in a password field that is never prefilled", async () => {
    serve({ handoff: STORED });
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

    await waitFor(() => {
      expect(
        requestsTo("/admin/organization.setDirectoryHandoff"),
      ).toHaveLength(1);
    });
    const sent = requestsTo("/admin/organization.setDirectoryHandoff")[0];
    expect(await sent?.clone().json()).toEqual({
      organization_id: ORG.id,
      scim_base_url: ENDPOINT,
      scim_token: TOKEN,
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
    expect(requestsTo("/admin/organization.setDirectoryHandoff")).toHaveLength(
      0,
    );
  });

  it("clears the stored handoff once the operator confirms", async () => {
    serve({ handoff: STORED });
    await mount();

    fireEvent.click(
      await screen.findByRole("button", { name: "Clear handoff" }),
    );
    await confirmWith("Clear handoff");

    await waitFor(() => {
      expect(
        requestsTo("/admin/organization.clearDirectoryHandoff"),
      ).toHaveLength(1);
    });
    const sent = requestsTo("/admin/organization.clearDirectoryHandoff")[0];
    expect(await sent?.clone().json()).toEqual({ organization_id: ORG.id });
  });

  it("reports a directory WorkOS does not have rather than leaving it blank", async () => {
    const { workos_directory_id, workos_directory_state, ...rest } = STORED;
    void workos_directory_id;
    void workos_directory_state;
    serve({ handoff: rest });
    await mount();

    expect(await screen.findByText(/Not found in WorkOS/)).toBeTruthy();
  });
});
