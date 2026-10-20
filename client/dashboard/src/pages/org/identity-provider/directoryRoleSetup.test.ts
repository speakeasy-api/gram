import { describe, expect, it } from "vitest";

import type { DirectoryRoleSetupPreviewItem } from "@gram/client/models/components/directoryrolesetuppreviewitem.js";
import type { DirectoryRoleSetupPreviewResult } from "@gram/client/models/components/directoryrolesetuppreviewresult.js";

import {
  newRoleNames,
  reusedRoleUrns,
  setupRequest,
  setupResultSummary,
  setupRows,
} from "./directoryRoleSetup";

function groupItem(
  id: string,
  name: string,
  overrides: Partial<DirectoryRoleSetupPreviewItem> = {},
): DirectoryRoleSetupPreviewItem {
  return {
    source: { sourceKind: "group", directoryGroupId: id },
    sourceName: name,
    directoryPeople: 3,
    existingRoleUrns: [],
    proposedRoles: [
      { roleAction: "create", roleName: name, currentGrants: [] },
    ],
    version: `v-${id}`,
    ...overrides,
  };
}

function preview(
  items: DirectoryRoleSetupPreviewItem[],
): DirectoryRoleSetupPreviewResult {
  return { items, hiddenAttributeKeys: [], maxValuesPerAttributeKey: 100 };
}

const attributeItem: DirectoryRoleSetupPreviewItem = {
  source: {
    sourceKind: "attribute",
    attributeKey: "department",
    attributeValue: "Sales",
  },
  sourceName: "department=Sales",
  directoryPeople: 2,
  existingRoleUrns: [],
  proposedRoles: [
    {
      roleAction: "reuse",
      roleId: "role-sales",
      roleUrn: "role:sales",
      roleName: "Sales",
      currentGrants: [],
    },
  ],
  version: "v-sales",
};

describe("setupRows", () => {
  it("offers unmapped sources only, groups first and selected", () => {
    const rows = setupRows(
      preview([
        attributeItem,
        groupItem("g2", "Support"),
        groupItem("g1", "Engineering"),
        groupItem("g3", "Mapped", { existingRoleUrns: ["role:x"] }),
      ]),
    );
    expect(rows.map((row) => [row.item.sourceName, row.selected])).toEqual([
      ["Engineering", true],
      ["Support", true],
      ["department=Sales", false],
    ]);
    expect(rows[2]?.choice).toEqual({ kind: "reuse", roleUrn: "role:sales" });
  });

  it("keeps choices and selection across a refreshed preview", () => {
    const first = setupRows(preview([groupItem("g1", "Engineering")]));
    const edited = first.map((row) => ({
      ...row,
      selected: false,
      choice: { kind: "reuse" as const, roleUrn: "role:eng" },
    }));
    const refreshed = setupRows(
      preview([
        groupItem("g1", "Engineering", { version: "v-new" }),
        groupItem("g2", "Support"),
      ]),
      edited,
    );
    expect(refreshed[0]).toMatchObject({
      selected: false,
      choice: { kind: "reuse", roleUrn: "role:eng" },
    });
    expect(refreshed[0]?.item.version).toBe("v-new");
    expect(refreshed[1]?.selected).toBe(true);
  });

  // Someone else created "Engineering" after the first preview, so the
  // refreshed preview proposes reusing it. Creating it again would conflict on
  // every apply.
  const reuseEngineering = groupItem("g1", "Engineering", {
    proposedRoles: [
      {
        roleAction: "reuse",
        roleId: "role-eng",
        roleUrn: "role:eng",
        roleName: "Engineering",
        currentGrants: [],
      },
    ],
    version: "v-new",
  });

  it("follows a refreshed proposal when the choice was never changed", () => {
    const first = setupRows(preview([groupItem("g1", "Engineering")]));
    expect(first[0]?.choice).toEqual({ kind: "create", name: "Engineering" });
    const refreshed = setupRows(preview([reuseEngineering]), first);
    expect(refreshed[0]?.choice).toEqual({
      kind: "reuse",
      roleUrn: "role:eng",
    });
    expect(setupRequest(refreshed).items[0]?.roles).toEqual([
      { roleUrn: "role:eng" },
    ]);
  });

  it("drops a chosen create once the refreshed preview reuses an existing role", () => {
    const first = setupRows(
      preview([
        groupItem("g1", "Engineering", {
          proposedRoles: [
            {
              roleAction: "reuse",
              roleId: "role-dev",
              roleUrn: "role:dev",
              roleName: "Developers",
              currentGrants: [],
            },
          ],
        }),
      ]),
    );
    const edited = first.map((row) => ({
      ...row,
      choice: { kind: "create" as const, name: "Engineering" },
    }));
    const refreshed = setupRows(preview([reuseEngineering]), edited);
    expect(refreshed[0]?.choice).toEqual({
      kind: "reuse",
      roleUrn: "role:eng",
    });
  });
});

describe("setupRequest", () => {
  // Two groups whose names make the same role, plus an attribute value.
  const rows = setupRows(
    preview([
      groupItem("g1", "Engineering"),
      groupItem("g2", "Engineering EU", {
        proposedRoles: [
          { roleAction: "create", roleName: "engineering", currentGrants: [] },
        ],
      }),
      attributeItem,
    ]),
  ).map((row) => ({ ...row, selected: true }));

  it("sends each selected source with its version and one role", () => {
    const grantSet = [{ scope: "org:read" as const, selectors: [] }];
    expect(setupRequest(rows, grantSet)).toEqual({
      items: [
        {
          source: { sourceKind: "group", directoryGroupId: "g1" },
          version: "v-g1",
          roles: [{ createName: "Engineering" }],
        },
        {
          source: { sourceKind: "group", directoryGroupId: "g2" },
          version: "v-g2",
          roles: [{ createName: "engineering" }],
        },
        {
          source: attributeItem.source,
          version: "v-sales",
          roles: [{ roleUrn: "role:sales" }],
        },
      ],
      grantSet,
    });
  });

  it("leaves out the grant set when nothing is created", () => {
    const reuseOnly = rows.map((row) => ({
      ...row,
      choice: { kind: "reuse" as const, roleUrn: "role:sales" },
    }));
    expect(
      setupRequest(reuseOnly, [{ scope: "org:read", selectors: [] }]).grantSet,
    ).toBeUndefined();
  });

  it("counts a new role once however many sources share it", () => {
    expect(newRoleNames(rows)).toEqual(["Engineering"]);
    expect(reusedRoleUrns(rows)).toEqual(["role:sales"]);
  });
});

describe("setupResultSummary", () => {
  it("reports role sync as requested, not done", () => {
    expect(
      setupResultSummary(
        {
          createdRoles: [],
          mappings: [],
          sideEffects: [
            {
              kind: "workos_role_sync",
              requestStatus: "requested",
              roleId: "r1",
            },
          ],
        },
        2,
      ),
    ).toBe(
      "Mapped 2 sources. Syncing 1 new role to WorkOS has been requested and can take a few minutes.",
    );
  });
});
