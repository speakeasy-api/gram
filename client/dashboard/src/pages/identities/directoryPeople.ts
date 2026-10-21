import type {
  DirectoryPerson,
  DirectoryPersonState,
} from "@gram/client/models/components/directoryperson.js";

import type { Employee } from "@/components/observe/insightsEmployeesData";
import { accessListDirectoryPeople } from "@gram/client/funcs/accessListDirectoryPeople";
import { unwrapAsync } from "@gram/client/types/fp";

/** How each directory state reads, wherever it is shown. */
export const DIRECTORY_STATE_LABELS: Record<DirectoryPersonState, string> = {
  member: "Member",
  account_not_member: "Account, not a member",
  not_signed_in: "Not signed in",
  ambiguous_email: "Ambiguous email",
};

/** What each state means, for tooltips. */
export const DIRECTORY_STATE_DESCRIPTIONS: Record<
  DirectoryPersonState,
  string
> = {
  member:
    "Linked to, or the only email match of, a member of this organization.",
  account_not_member:
    "Has a Speakeasy account, but is not a member of this organization.",
  not_signed_in: "Has no Speakeasy account yet.",
  ambiguous_email:
    "The email matches more than one directory profile or account, so it is not matched to anyone automatically.",
};

/** The states that describe someone the directory knows but who is not a member. */
export const NOT_MEMBER_DIRECTORY_STATES: DirectoryPersonState[] = [
  "account_not_member",
  "not_signed_in",
  "ambiguous_email",
];

/** The filter value Team and the SSO tab link to. */
export const NOT_MEMBER_DIRECTORY_STATE_FILTER =
  NOT_MEMBER_DIRECTORY_STATES.join(",");

export function directoryPeopleQueryKey(organizationId: string): string[] {
  return ["identities", "directory-people", organizationId];
}

/** Every live directory profile in the organization, across all pages. */
export async function fetchDirectoryPeople(
  client: Parameters<typeof accessListDirectoryPeople>[0],
): Promise<DirectoryPerson[]> {
  const people: DirectoryPerson[] = [];
  let cursor: string | undefined;
  do {
    const page = await unwrapAsync(
      accessListDirectoryPeople(client, { limit: 1000, cursor }),
    );
    people.push(...page.people);
    cursor = page.nextCursor;
  } while (cursor);
  return people;
}

/** The name a directory reports, or its email until it reports one. */
export function directoryPersonName(person: DirectoryPerson): string {
  const name = [person.firstName, person.lastName]
    .map((part) => part?.trim() ?? "")
    .filter(Boolean)
    .join(" ");
  return name || person.email;
}

// When one row stands for several profiles, the state that most needs a
// reader's attention wins: an ambiguous address is never matched, so it says
// more than any single profile's state.
const STATE_PRIORITY: Record<DirectoryPersonState, number> = {
  ambiguous_email: 3,
  account_not_member: 2,
  not_signed_in: 1,
  member: 0,
};

function strongerState(
  current: DirectoryPersonState | undefined,
  next: DirectoryPersonState,
): DirectoryPersonState {
  if (!current) return next;
  return STATE_PRIORITY[next] > STATE_PRIORITY[current] ? next : current;
}

function unionTeams(current: string[], person: DirectoryPerson): string[] {
  return [
    ...new Set([...current, ...person.groups.map((group) => group.name)]),
  ];
}

/**
 * Folds directory people into the roster.
 *
 * A profile in the member state marks its member's row. Any other profile is
 * someone the roster may not know yet: it joins the activity-only row with the
 * same email when there is one, and otherwise becomes a row of its own, keyed
 * by email so its link opens the same identity page. Profiles sharing an email
 * become one row, because the address is what the identity page resolves.
 * A profile with no email has nothing to link to and is left out; the server
 * counts still include it.
 */
export function mergeDirectoryPeople(
  employees: Employee[],
  people: DirectoryPerson[],
): Employee[] {
  const rows = employees.map((employee) => ({ ...employee }));
  const byUserId = new Map<string, Employee>();
  const byEmail = new Map<string, Employee>();
  for (const row of rows) {
    if (!row.registeredAgentId && !row.id.startsWith("usage:")) {
      byUserId.set(row.id, row);
    }
    if (row.email) byEmail.set(row.email.toLowerCase(), row);
  }

  const added: Employee[] = [];
  for (const person of people) {
    if (person.state === "member" && person.userId) {
      const member = byUserId.get(person.userId);
      if (member) {
        member.directoryState = strongerState(member.directoryState, "member");
        continue;
      }
    }
    const email = person.email.trim().toLowerCase();
    if (!email) continue;

    const existing = byEmail.get(email);
    if (existing) {
      existing.inDirectory = true;
      existing.directoryState = strongerState(
        existing.directoryState,
        person.state,
      );
      existing.teams = unionTeams(existing.teams, person);
      if (existing.name === existing.email || !existing.name) {
        existing.name = directoryPersonName(person);
      }
      continue;
    }

    const row: Employee = {
      id: `usage:${email}`,
      name: directoryPersonName(person),
      email: person.email,
      role: "",
      inDirectory: true,
      seen: false,
      directoryState: person.state,
      tokenCount: 0,
      lastActivity: "\u2014",
      lastActivityTimestamp: null,
      accounts: [],
      mostRecentAccount: null,
      hasPersonalAccount: false,
      roleIds: [],
      department: "",
      teams: person.groups.map((group) => group.name),
    };
    byEmail.set(email, row);
    added.push(row);
  }
  return [...rows, ...added];
}
