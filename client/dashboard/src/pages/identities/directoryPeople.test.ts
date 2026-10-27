import type { Employee } from "@/components/observe/insightsEmployeesData";
import type { DirectoryPerson } from "@gram/client/models/components/directoryperson.js";
import { describe, expect, it } from "vitest";
import {
  directoryPersonName,
  mergeDirectoryPeople,
  NOT_MEMBER_DIRECTORY_STATE_FILTER,
} from "./directoryPeople";
import { identityHasAccount, identityUrnForEmployee } from "./identityKind";

function employee(overrides: Partial<Employee>): Employee {
  return {
    id: "user_1",
    name: "Ada Lovelace",
    email: "ada@example.com",
    role: "Member",
    inDirectory: true,
    seen: true,
    tokenCount: 10,
    lastActivity: "today",
    lastActivityTimestamp: 1,
    accounts: [],
    mostRecentAccount: null,
    hasPersonalAccount: false,
    roleIds: [],
    department: "",
    teams: [],
    ...overrides,
  };
}

function person(overrides: Partial<DirectoryPerson>): DirectoryPerson {
  return {
    id: "profile_1",
    email: "someone@example.com",
    state: "not_signed_in",
    groups: [],
    ...overrides,
  };
}

describe("mergeDirectoryPeople", () => {
  it("marks a member's row with the member state without adding a row", () => {
    const rows = mergeDirectoryPeople(
      [employee({})],
      [person({ state: "member", userId: "user_1", email: "ada@example.com" })],
    );
    expect(rows).toHaveLength(1);
    expect(rows[0]!.directoryState).toBe("member");
  });

  it("adds a directory-only person as a row with no account", () => {
    const rows = mergeDirectoryPeople(
      [employee({})],
      [
        person({
          email: "New.Person@example.com",
          firstName: "New",
          lastName: "Person",
          groups: [{ id: "group_1", name: "Platform" }],
        }),
      ],
    );
    expect(rows).toHaveLength(2);
    const added = rows[1]!;
    expect(added).toMatchObject({
      id: "usage:new.person@example.com",
      name: "New Person",
      email: "New.Person@example.com",
      inDirectory: true,
      seen: false,
      directoryState: "not_signed_in",
      teams: ["Platform"],
    });
    expect(identityHasAccount(added)).toBe(false);
    expect(identityUrnForEmployee(added)).toBe("email:New.Person@example.com");
  });

  it("joins an activity-only row with the same email instead of duplicating it", () => {
    const usage = employee({
      id: "usage:grace@example.com",
      name: "grace@example.com",
      email: "grace@example.com",
      inDirectory: false,
    });
    const rows = mergeDirectoryPeople(
      [usage],
      [
        person({
          email: "GRACE@example.com",
          firstName: "Grace",
          lastName: "Hopper",
          state: "account_not_member",
          userId: "user_9",
          groups: [{ id: "group_1", name: "Infra" }],
        }),
      ],
    );
    expect(rows).toHaveLength(1);
    expect(rows[0]).toMatchObject({
      name: "Grace Hopper",
      inDirectory: true,
      seen: true,
      directoryState: "account_not_member",
      teams: ["Infra"],
    });
  });

  it("collapses profiles sharing an email into one ambiguous row", () => {
    const rows = mergeDirectoryPeople(
      [],
      [
        person({
          id: "a",
          email: "shared@example.com",
          state: "ambiguous_email",
        }),
        person({
          id: "b",
          email: "shared@example.com",
          state: "ambiguous_email",
        }),
      ],
    );
    expect(rows).toHaveLength(1);
    expect(rows[0]!.directoryState).toBe("ambiguous_email");
  });

  it("flags a member whose address another profile also uses as ambiguous", () => {
    const rows = mergeDirectoryPeople(
      [employee({})],
      [person({ email: "ada@example.com", state: "ambiguous_email" })],
    );
    expect(rows).toHaveLength(1);
    expect(rows[0]!.directoryState).toBe("ambiguous_email");
  });

  it("leaves out a profile with no email", () => {
    expect(mergeDirectoryPeople([], [person({ email: "" })])).toEqual([]);
  });

  it("does not change the rows it was given", () => {
    const original = employee({
      id: "usage:x@example.com",
      email: "x@example.com",
      inDirectory: false,
    });
    mergeDirectoryPeople([original], [person({ email: "x@example.com" })]);
    expect(original.inDirectory).toBe(false);
    expect(original.directoryState).toBeUndefined();
  });
});

describe("directoryPersonName", () => {
  it("shows the email until the directory reports a name", () => {
    expect(directoryPersonName(person({ email: "only@example.com" }))).toBe(
      "only@example.com",
    );
    expect(
      directoryPersonName(person({ firstName: "Ada", lastName: " " })),
    ).toBe("Ada");
  });
});

it("links Team and the SSO tab to every state except member", () => {
  expect(NOT_MEMBER_DIRECTORY_STATE_FILTER).toBe(
    "account_not_member,not_signed_in,ambiguous_email",
  );
});
