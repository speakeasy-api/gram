import { useMemo, type JSX } from "react";
import { useQuery } from "@tanstack/react-query";
import { useParams } from "@tanstack/react-router";
import { createColumnHelper, useTable } from "@tanstack/react-table";

import {
  dataTableFeatures,
  DataTable as Table,
  type DataTableFeatures,
} from "@/components/data-table";
import {
  organizationMembersQuery,
  organizationQuery,
} from "@/lib/adminQueries";
import type {
  AdminOrganization,
  AdminOrganizationMember,
} from "@/lib/gramAdminApi";
import { byOldestFirst, fmtDateShort } from "@/lib/utils";

// Two letters where the name gives two words, one where it does not, and the
// email when the record carries no name at all. It is decoration beside the
// name it was taken from, so it is hidden from a screen reader rather than read
// out twice, once as initials and once as words.
function monogram(member: AdminOrganizationMember): string {
  const words = member.display_name.trim().split(/\s+/).filter(Boolean);
  const letters =
    words.length > 1
      ? `${words[0]?.[0] ?? ""}${words[words.length - 1]?.[0] ?? ""}`
      : (words[0]?.slice(0, 2) ?? member.email.slice(0, 2));
  return letters.toUpperCase();
}

// A member with no name is still a member. The email is what the operator has
// to go on, and an empty cell reads as a broken row.
function memberName(member: AdminOrganizationMember): string {
  return member.display_name.trim() || member.email;
}

const memberColumn = createColumnHelper<
  DataTableFeatures,
  AdminOrganizationMember
>();

const MEMBER_COLUMNS = memberColumn.columns([
  memberColumn.accessor("display_name", {
    header: "Member",
    // Wrapping, against the table's own `whitespace-nowrap`. A name and an
    // email have no length the column can be sized for, and the border around
    // the table clips rather than scrolls, so a squeezed column has to fold the
    // text instead of pushing it out of reach.
    meta: { cellClassName: "whitespace-normal" },
    cell: ({ row }) => (
      <span className="flex items-center gap-2 text-sm">
        <span
          aria-hidden="true"
          className="bg-muted flex size-6 shrink-0 items-center justify-center rounded-full text-[10px] font-medium"
        >
          {monogram(row.original)}
        </span>
        {memberName(row.original)}
      </span>
    ),
  }),
  memberColumn.accessor("email", {
    header: "Email",
    // `break-all` rather than `break-words`: an email is one word, so a rule
    // that only breaks between words never fires on it.
    meta: { cellClassName: "whitespace-normal break-all" },
    cell: ({ row }) => (
      <span className="text-muted-foreground text-sm">
        {row.original.email}
      </span>
    ),
  }),
  memberColumn.accessor("last_login", {
    header: "Last active",
    // "Never", not the dash every other absent date draws. A member who has
    // never signed in is a fact about the account, and the dash that means
    // "nothing recorded" hides it among the fields that merely have no value.
    cell: ({ row }) =>
      row.original.last_login ? (
        <span className="text-sm">{fmtDateShort(row.original.last_login)}</span>
      ) : (
        <span className="text-muted-foreground text-sm">Never</span>
      ),
  }),
  memberColumn.accessor("created_at", {
    header: "Joined",
    cell: ({ row }) => (
      <span className="text-sm">{fmtDateShort(row.original.created_at)}</span>
    ),
  }),
]);

// `isPending`, not `isLoading`: React Query makes the second of those
// `isPending && isFetching`, so a paused read is neither loading nor errored and
// falls through to the sentence that says the organization has none.
function membersMessage(isPending: boolean, isError: boolean): string {
  if (isPending) return "Loading...";
  if (isError) return "Unable to load members";
  return "No members in this organization";
}

export function MembersRoute(): JSX.Element | null {
  const { idOrSlug } = useParams({ from: "/organizations/$idOrSlug" });
  const { data } = useQuery(organizationQuery(idOrSlug));
  if (!data) return null;
  return <Members org={data} />;
}

export function Members({ org }: { org: AdminOrganization }): JSX.Element {
  const { data, isPending, isError } = useQuery({
    ...organizationMembersQuery(org.id),
    enabled: !!org.id,
  });

  // Sorted here rather than trusted from the endpoint, which promises no order,
  // and a fresh array each render would rebuild the row model every time.
  const members = useMemo(
    () => [...(data?.members ?? [])].sort(byOldestFirst),
    [data],
  );

  const table = useTable({
    features: dataTableFeatures,
    columns: MEMBER_COLUMNS,
    data: members,
    getRowId: (member) => member.id,
  });

  const rows = table.getRowModel().rows;

  return (
    <div className="flex flex-col gap-3">
      {/* Only once the read has answered with rows. A count over a pending read
          is a number nobody has established, and "0 members" beside the
          sentence that says there are none says it twice. */}
      {rows.length > 0 && (
        <p className="text-sm font-medium">
          {rows.length === 1 ? "1 member" : `${rows.length} members`}
        </p>
      )}
      {/* `overflow-clip`, not `overflow-hidden`: hidden makes this a scroll
          container and the `sticky top-0` header would pin to it, not the page. */}
      <div className="overflow-clip rounded-lg border">
        <Table cellPadding="condensed">
          <Table.Header table={table} />
          <Table.Body>
            {isPending || rows.length === 0 ? (
              <Table.NoResultsMessage>
                <span className="text-muted-foreground text-sm">
                  {membersMessage(isPending, isError)}
                </span>
              </Table.NoResultsMessage>
            ) : (
              rows.map((row) => <Table.Row key={row.id} row={row} />)
            )}
          </Table.Body>
        </Table>
      </div>
    </div>
  );
}
