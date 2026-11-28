import { cleanup, fireEvent, screen, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import type { AdminBulkDisableOrganizationsResult } from "@gram/admin-client/models/components/adminbulkdisableorganizationsresult";
import type { AdminResolveOrganizationsResult } from "@gram/admin-client/models/components/adminresolveorganizationsresult";
import type { BulkDisableOrganizationsRequestBody } from "@gram/admin-client/models/components/bulkdisableorganizationsrequestbody";
import type { ResolveOrganizationsRequestBody } from "@gram/admin-client/models/components/resolveorganizationsrequestbody";
import { renderWithApp } from "@/test/harness";

import { BulkDisable } from "./BulkDisable";

const mocks = vi.hoisted(() => ({
  resolveOrganizations:
    vi.fn<
      (
        body: ResolveOrganizationsRequestBody,
      ) => Promise<AdminResolveOrganizationsResult>
    >(),
  bulkDisableOrganizations:
    vi.fn<
      (
        body: BulkDisableOrganizationsRequestBody,
      ) => Promise<AdminBulkDisableOrganizationsResult>
    >(),
}));

vi.mock("@/lib/gramAdminClient", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/lib/gramAdminClient")>();
  return {
    ...actual,
    resolveOrganizations: mocks.resolveOrganizations,
    bulkDisableOrganizations: mocks.bulkDisableOrganizations,
  };
});

const announce = vi.fn<(text: string) => void>();
const showFailure = vi.fn<(text: string | null) => void>();
const REPORTER = { announce, showFailure };

const RESOLVED: AdminResolveOrganizationsResult = {
  organizations: [
    {
      id: "org_placeholder_a",
      slug: "placeholder-a",
      name: "Placeholder A",
      accountType: "pro",
      whitelisted: true,
    },
    {
      id: "org_placeholder_b",
      slug: "placeholder-b",
      name: "Placeholder B",
      accountType: "free",
      whitelisted: false,
      disabledAt: new Date("2026-01-01T00:00:00Z"),
    },
  ],
  missingIds: ["org_gone"],
  missingSlugs: ["ghost"],
};

function upload(csv: string): void {
  fireEvent.change(screen.getByLabelText("CSV file"), {
    target: { files: [new File([csv], "orgs.csv", { type: "text/csv" })] },
  });
}

async function openDialog(): Promise<void> {
  await renderWithApp(<BulkDisable reporter={REPORTER} />);
  fireEvent.click(screen.getByRole("button", { name: "Bulk disable" }));
}

beforeEach(() => {
  mocks.resolveOrganizations.mockReset();
  mocks.bulkDisableOrganizations.mockReset();
  announce.mockReset();
  showFailure.mockReset();
});

afterEach(cleanup);

describe("BulkDisable", () => {
  it("refuses a CSV without an id or slug column and sends nothing", async () => {
    await openDialog();
    upload("name\nAcme\n");

    expect((await screen.findByRole("alert")).textContent).toContain(
      'CSV needs an "id" or "slug" column',
    );
    expect(mocks.resolveOrganizations).not.toHaveBeenCalled();
    expect(
      screen.getByRole<HTMLButtonElement>("button", { name: "Disable" })
        .disabled,
    ).toBe(true);
  });

  it("reviews what was found and missed, then disables the reviewed ids", async () => {
    mocks.resolveOrganizations.mockResolvedValue(RESOLVED);
    mocks.bulkDisableOrganizations.mockResolvedValue({
      disabledIds: ["org_placeholder_a", "org_placeholder_b"],
      missingIds: [],
    });
    await openDialog();
    upload("id,slug\norg_placeholder_a,\norg_gone,\n,placeholder-b\n,ghost\n");

    const table = await screen.findByRole("table");
    expect(mocks.resolveOrganizations).toHaveBeenCalledWith({
      ids: ["org_placeholder_a", "org_gone"],
      slugs: ["placeholder-b", "ghost"],
    });
    const rows = within(table).getAllByRole("row").slice(1);
    expect(rows.map((row) => row.textContent)).toEqual([
      "org_placeholder_aplaceholder-aPlaceholder AproActive",
      "org_placeholder_bplaceholder-bPlaceholder BfreeAlready disabled",
    ]);
    expect(
      screen.getByText(/2 organizations from the CSV could not be found/),
    ).toBeTruthy();
    expect(screen.getByText("org_gone")).toBeTruthy();
    expect(screen.getByText("ghost")).toBeTruthy();
    // Reviewing writes nothing.
    expect(mocks.bulkDisableOrganizations).not.toHaveBeenCalled();

    fireEvent.click(
      screen.getByRole("button", { name: "Disable 2 organizations" }),
    );

    expect(
      await screen.findByText(
        "Disabled 2 organizations and removed them from the whitelist.",
      ),
    ).toBeTruthy();
    // The reviewed ids, not the file's slugs.
    expect(mocks.bulkDisableOrganizations).toHaveBeenCalledWith({
      ids: ["org_placeholder_a", "org_placeholder_b"],
    });
    expect(announce).toHaveBeenCalledWith("Disabled 2 organizations.");
  });

  it("keeps the review open with the reason when the disable fails", async () => {
    mocks.resolveOrganizations.mockResolvedValue({
      organizations: [RESOLVED.organizations[0]!],
      missingIds: [],
      missingSlugs: [],
    });
    mocks.bulkDisableOrganizations.mockRejectedValue(new Error("boom"));
    await openDialog();
    upload("id\norg_placeholder_a\n");

    fireEvent.click(
      await screen.findByRole("button", { name: "Disable 1 organization" }),
    );

    expect((await screen.findByRole("alert")).textContent).toBe(
      "Nothing was disabled: boom",
    );
    expect(mocks.bulkDisableOrganizations).toHaveBeenCalledWith({
      ids: ["org_placeholder_a"],
    });
    expect(screen.getByRole("table")).toBeTruthy();
    expect(announce).toHaveBeenCalledWith(
      "Could not disable organizations: boom",
    );
  });

  it("clears an earlier page failure when the disable lands", async () => {
    mocks.resolveOrganizations.mockResolvedValue({
      organizations: [RESOLVED.organizations[0]!],
      missingIds: [],
      missingSlugs: [],
    });
    mocks.bulkDisableOrganizations.mockResolvedValue({
      disabledIds: ["org_placeholder_a"],
      missingIds: [],
    });
    await openDialog();
    upload("id\norg_placeholder_a\n");
    fireEvent.click(
      await screen.findByRole("button", { name: "Disable 1 organization" }),
    );

    await screen.findByText(/Disabled 1 organization/);
    expect(showFailure).toHaveBeenCalledWith(null);
  });

  it("re-reads the same file when it is picked again", async () => {
    await openDialog();
    const input = screen.getByLabelText<HTMLInputElement>("CSV file");
    upload("name\nAcme\n");
    await screen.findByRole("alert");

    // The picker opening clears the input, so the browser fires change even
    // for the file that was already chosen.
    fireEvent.click(input);
    expect(input.value).toBe("");
  });
});
