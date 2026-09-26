import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { SlackDirectoryMember } from "@gram/client/models/components/slackdirectorymember.js";
import type { OrganizationUser } from "@gram/client/models/components/organizationuser.js";
import { TooltipProvider } from "@/components/ui/Tooltip";
import { SlackPersonnelPicker } from "./SlackPersonnelPicker";

const mocks = vi.hoisted(() => ({
  orgSlug: "example",
  member: {} as SlackDirectoryMember,
  people: [] as OrganizationUser[],
  mutate: vi.fn(),
  reset: vi.fn(),
  invalidate: vi.fn(),
  pending: false,
  error: null as Error | null,
}));
vi.mock("@/contexts/Auth", () => ({
  useOrganization: () => ({ slug: mocks.orgSlug }),
}));
vi.mock("@gram/client/react-query/slackDirectoryMember.js", () => ({
  invalidateAllSlackDirectoryMember: vi.fn(),
}));
vi.mock("@gram/client/react-query/slackDirectoryMembers.js", () => ({
  invalidateAllSlackDirectoryMembers: () => mocks.invalidate(),
}));
vi.mock("@gram/client/react-query/setSlackIdentityMapping.js", () => ({
  useSetSlackIdentityMappingMutation: () => ({
    mutate: mocks.mutate,
    isPending: mocks.pending,
    isError: Boolean(mocks.error),
    error: mocks.error,
    reset: mocks.reset,
  }),
}));
vi.mock("@/components/ui/Combobox", () => ({
  Combobox: ({
    id,
    items,
    selected,
    onSelectionChange,
    disabledMessage,
  }: {
    id: string;
    items: Array<{ value: string; label: string; description?: string }>;
    selected?: string;
    disabledMessage?: string;
    onSelectionChange: (item: unknown) => void;
  }) => (
    <select
      id={id}
      aria-label="Personnel"
      title={disabledMessage}
      disabled={Boolean(disabledMessage)}
      value={selected ?? ""}
      onChange={(e) =>
        onSelectionChange(items.find((item) => item.value === e.target.value))
      }
    >
      <option value="">Not mapped</option>
      {items.map((item) => (
        <option key={item.value} value={item.value}>
          {item.label} {item.description}
        </option>
      ))}
    </select>
  ),
}));

const person = (id: string, name: string, email: string): OrganizationUser => ({
  id: `membership-${id}`,
  userId: id,
  name,
  email,
  organizationId: "org_synthetic",
  createdAt: new Date(),
  updatedAt: new Date(),
});
beforeEach(() => {
  mocks.orgSlug = "example";
  mocks.pending = false;
  mocks.error = null;
  mocks.member = {
    id: "00000000-0000-4000-8000-000000000001",
    connectionId: "00000000-0000-4000-8000-000000000002",
    workspaceId: "TEXAMPLE01",
    workspaceName: "Example Engineering",
    slackUserId: "UEXAMPLE01",
    displayName: "Synthetic Person",
    email: " Synthetic@demo.getgram.ai ",
    status: "active",
    memberType: "person",
    lastSeenAt: new Date(),
    observedInLastSync: true,
    mappingRevision: 0,
    mappingStatus: "unmapped",
    observationToken: "current-evidence",
  };
  mocks.people = [
    person("user_synthetic_1", "Synthetic One", "synthetic@demo.getgram.ai"),
    person("user_synthetic_2", "Synthetic Two", "other@demo.getgram.ai"),
  ];
});
afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});
function show(canEdit = true) {
  render(
    <QueryClientProvider client={new QueryClient()}>
      <TooltipProvider>
        <SlackPersonnelPicker
          member={mocks.member}
          people={mocks.people}
          canEdit={canEdit}
        />
      </TooltipProvider>
    </QueryClientProvider>,
  );
}
const picker = () => screen.getByLabelText("Personnel") as HTMLSelectElement;
function mapped() {
  mocks.member.mapping = {
    id: "mapping-example",
    userId: "user_synthetic_1",
    displayName: "Synthetic One",
    email: "synthetic@demo.getgram.ai",
    active: true,
  };
  mocks.member.mappingRevision = 7;
  mocks.member.mappingStatus = "mapped";
}
const body = () =>
  mocks.mutate.mock.calls[0]?.[0].request.setSlackIdentityMappingRequestBody;

it("maps as soon as a person is picked, with the row's revision and evidence", () => {
  show();
  expect(
    screen.getByRole("option", { name: /Synthetic One .*Email match/ }),
  ).toBeTruthy();
  expect(mocks.mutate).not.toHaveBeenCalled();
  fireEvent.change(picker(), { target: { value: "user_synthetic_2" } });
  expect(body()).toEqual({
    id: mocks.member.id,
    userId: "user_synthetic_2",
    mappingRevision: 0,
    observationToken: "current-evidence",
  });
});
it("unmaps immediately when Not mapped is picked", () => {
  mapped();
  show();
  fireEvent.change(picker(), { target: { value: "__unmapped" } });
  expect(body()).toMatchObject({ mappingRevision: 7, userId: undefined });
});
it("does nothing when the current person is picked again", () => {
  mapped();
  show();
  fireEvent.change(picker(), { target: { value: "user_synthetic_1" } });
  expect(mocks.mutate).not.toHaveBeenCalled();
});
it("reconfirms the current person to clear a review finding", () => {
  mapped();
  mocks.member.mappingConflictReason = "email_changed";
  mocks.member.mappingStatus = "needs_review";
  show();
  fireEvent.change(picker(), { target: { value: "user_synthetic_1" } });
  expect(body()).toMatchObject({
    userId: "user_synthetic_1",
    mappingRevision: 7,
  });
});
it("offers only removal for a mapped bot and is disabled for an unmapped bot", () => {
  mapped();
  mocks.member.memberType = "bot";
  show();
  expect(screen.queryByRole("option", { name: /Synthetic Two/ })).toBeNull();
  fireEvent.change(picker(), { target: { value: "__unmapped" } });
  expect(body()).toMatchObject({ userId: undefined });
  cleanup();
  mocks.member.mapping = undefined;
  show();
  expect(picker().disabled).toBe(true);
});
const warning = () => screen.queryByRole("img")?.getAttribute("aria-label");
it("shows a rejected stale edit only as a warning icon", () => {
  mocks.error = new Error("This Slack member changed. Reload the member.");
  show();
  expect(screen.queryByText(/This Slack member changed/)).toBeNull();
  expect(warning()).toMatch(/This Slack member changed.*pick again to retry/);
});
it("has no warning icon for a clean mapping", () => {
  mapped();
  show();
  expect(warning()).toBeUndefined();
  expect(screen.queryByText(/Personnel for/)).toBeNull();
});
it("folds findings and a mismatched email into one warning icon", () => {
  mapped();
  mocks.member.mapping!.email = "someone.else@demo.getgram.ai";
  mocks.member.mapping!.active = false;
  mocks.member.mappingStatus = "needs_review";
  show();
  expect(warning()).toMatch(
    /Needs review: Mapped person is no longer active in this organization/,
  );
  expect(warning()).toMatch(
    /Slack email .* differs from Synthetic One’s email/,
  );
});
it("is read-only in the shared demo and for non-admins", () => {
  mocks.orgSlug = "acme-demo";
  mapped();
  show();
  expect(picker().disabled).toBe(true);
  fireEvent.change(picker(), { target: { value: "user_synthetic_2" } });
  expect(mocks.mutate).not.toHaveBeenCalled();
  cleanup();
  mocks.orgSlug = "example";
  show(false);
  expect(picker().disabled).toBe(true);
});
