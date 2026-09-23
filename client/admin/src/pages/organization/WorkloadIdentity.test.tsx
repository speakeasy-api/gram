import { cleanup, fireEvent, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";

import type { AdminWorkloadIdentityState } from "@/lib/gramAdminApi";
import { WorkloadIdentity } from "@/pages/organization/WorkloadIdentity";
import { anOrganization } from "@/test/fixtures";
import { renderWithApp } from "@/test/harness";

const mocks = vi.hoisted(() => ({
  getWorkloadIdentity: vi.fn(),
  admitWorkloadSubject: vi.fn(),
}));

vi.mock("@/lib/gramAdminApi", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/lib/gramAdminApi")>();
  return {
    ...actual,
    getWorkloadIdentity: mocks.getWorkloadIdentity,
    admitWorkloadSubject: mocks.admitWorkloadSubject,
  };
});

const ORG = anOrganization();

const ISSUER_ID = "11111111-1111-4111-8111-111111111111";
const AGENT_ID = "22222222-2222-4222-8222-222222222222";

const STATE: AdminWorkloadIdentityState = {
  organization_id: ORG.id,
  issuers: [
    {
      id: ISSUER_ID,
      name: "anthropic",
      issuer: "https://identity.anthropic.com/agents",
      jwks_uri: "https://identity.anthropic.com/agents/jwks.json",
      created_at: "2026-09-23T00:00:00Z",
    },
  ],
  subjects: [],
  authentication_hosts: [],
};

beforeEach(() => {
  mocks.getWorkloadIdentity.mockResolvedValue(STATE);
  mocks.admitWorkloadSubject.mockResolvedValue(STATE);
});

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

const WARNING = /matched in full, literally/i;

// Resolves once the query has settled and the admission form is on the page.
async function renderPage(): Promise<HTMLElement> {
  await renderWithApp(<WorkloadIdentity org={ORG} />);
  await waitFor(() => {
    expect(screen.getByLabelText("Subject (exact)")).toBeTruthy();
  });
  return screen.getByLabelText("Subject (exact)");
}

it("warns that a wildcard in the subject is matched literally", async () => {
  const subject = await renderPage();
  expect(screen.queryByRole("alert")).toBeNull();

  fireEvent.change(subject, {
    target: { value: "wimse://identity.anthropic.com/org/org-abc/agent/*" },
  });

  // An operator expecting glob semantics would otherwise store a rule that
  // admits nothing and reads as correct in the table afterwards.
  const warning = screen.getByRole("alert");
  expect(warning.textContent).toMatch(WARNING);
  // Tied to the input, so a screen reader reaches it from the field.
  expect(subject.getAttribute("aria-describedby")).toBe(warning.id);
});

it("drops the warning once the wildcard is removed", async () => {
  const subject = await renderPage();

  fireEvent.change(subject, { target: { value: "agent/*" } });
  expect(screen.getByRole("alert")).toBeTruthy();

  fireEvent.change(subject, { target: { value: "agent/abc" } });
  expect(screen.queryByRole("alert")).toBeNull();
  expect(subject.getAttribute("aria-describedby")).toBeNull();
});

it("does not warn on a subject without a wildcard", async () => {
  const subject = await renderPage();

  fireEvent.change(subject, {
    target: { value: "wimse://identity.anthropic.com/org/org-abc/agent/a-1" },
  });

  expect(screen.queryByRole("alert")).toBeNull();
});

// The warning is advisory, not a gate: the field still submits, and the server
// is what refuses the rule. Pinned so nobody mistakes it for validation.
it("still submits a wildcard subject", async () => {
  const subject = await renderPage();

  fireEvent.change(subject, { target: { value: "agent/*" } });
  fireEvent.change(screen.getByLabelText("Issuer ID"), {
    target: { value: ISSUER_ID },
  });
  fireEvent.change(screen.getByLabelText("Agent ID"), {
    target: { value: AGENT_ID },
  });
  // Submitted on the form rather than by clicking the button: happy-dom does
  // not dispatch submit from a button activation.
  const form = subject.closest("form");
  expect(form).toBeTruthy();
  fireEvent.submit(form!);

  await waitFor(() => {
    expect(mocks.admitWorkloadSubject.mock.calls.length).toBe(1);
  });
  // First argument only: a mutationFn is called with (variables, context).
  expect(mocks.admitWorkloadSubject.mock.calls[0]?.[0]).toEqual(
    expect.objectContaining({ subject: "agent/*" }),
  );
});
