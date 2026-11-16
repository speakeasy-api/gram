import { InlineEmptyState } from "@/components/inline-empty-state";
import { MemberFacepile } from "@/components/member-facepile";
import { Button } from "@/components/ui/Button";
import { SkeletonTable } from "@/components/ui/Skeleton";
import { type Column, Table } from "@/components/ui/Table";
import { Text } from "@/components/ui/Text";
import { pluralize } from "@/lib/format";
import { useOrgRoutes } from "@/routes";
import { useDirectoryRoleMappings } from "@gram/client/react-query/directoryRoleMappings.js";
import { useMembers } from "@gram/client/react-query/members.js";
import { Tag, Users } from "lucide-react";
import { useMemo } from "react";
import { Link } from "react-router";
import {
  type RoleDirectorySource,
  roleDirectorySources,
} from "./directorySourcesForRole";

/** Undefined means the count is not known. */
function userCount(count: number | undefined): string {
  return count === undefined ? "—" : pluralize(count, "user");
}

const linkClassName =
  "text-sm underline decoration-border underline-offset-4 hover:decoration-foreground";

/**
 * Under directory sync the directory decides who holds a role, so the role
 * editor shows the groups and attribute values that give it out in place of a
 * member picker. Each one is changed on the identity provider page.
 */
export function RoleDirectorySources({
  role,
}: {
  /** The role being edited; undefined while creating one. */
  role: { id: string; name: string; principalUrn: string } | undefined;
}): JSX.Element {
  const orgRoutes = useOrgRoutes();
  const mappingsHref = orgRoutes.identity.href();

  return (
    <section
      aria-labelledby="role-directory-sources"
      className="border-border space-y-3 border-t pt-4 pb-4"
    >
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div className="space-y-1">
          <Text
            id="role-directory-sources"
            variant="body"
            className="font-medium"
          >
            Directory mappings
          </Text>
          <Text muted small>
            Directory sync decides who has this role. People in these groups, or
            with these attribute values, get it automatically.
          </Text>
        </div>
        {role && (
          <Link to={mappingsHref} className={linkClassName}>
            Manage on IDP and SSO →
          </Link>
        )}
      </div>
      {role ? (
        <SourcesForRole role={role} mappingsHref={mappingsHref} />
      ) : (
        <Text muted small>
          Once the role is created, you can map a directory group or attribute
          value to it on{" "}
          <Link to={mappingsHref} className={linkClassName}>
            IDP and SSO
          </Link>
          .
        </Text>
      )}
    </section>
  );
}

function SourcesForRole({
  role,
  mappingsHref,
}: {
  role: { id: string; name: string; principalUrn: string };
  mappingsHref: string;
}): JSX.Element {
  const { data, isPending, isError, refetch } = useDirectoryRoleMappings(
    undefined,
    undefined,
    { throwOnError: false },
  );
  const { data: membersData } = useMembers();
  const sources = useMemo(
    () =>
      data ? roleDirectorySources(data, membersData?.members ?? [], role) : [],
    [data, membersData?.members, role],
  );

  if (isPending) return <SkeletonTable />;
  if (isError) {
    return (
      <InlineEmptyState
        icon="triangle-alert"
        heading="Couldn't load directory mappings"
        action={
          <Button size="sm" variant="secondary" onClick={() => void refetch()}>
            Retry
          </Button>
        }
      />
    );
  }

  const columns: Column<RoleDirectorySource>[] = [
    {
      key: "label",
      header: "Source",
      render: (source) => (
        <div className="flex min-w-0 items-center gap-2.5">
          {source.kind === "group" ? (
            <Users className="text-muted-foreground size-4 shrink-0" />
          ) : (
            <Tag className="text-muted-foreground size-4 shrink-0" />
          )}
          <Text className="truncate font-medium">{source.label}</Text>
        </div>
      ),
    },
    {
      key: "kind",
      header: "Type",
      width: "120px",
      render: (source) => (
        <Text muted small>
          {source.kind === "group" ? "Group" : "Attribute"}
        </Text>
      ),
    },
    {
      key: "members",
      header: "Members",
      width: "220px",
      // Faces show who has signed in; the count covers everyone the source
      // matches, so a large group with few signed-in members still reads large.
      render: (source) => (
        <div className="flex items-center gap-2">
          {source.members.length > 0 && (
            <MemberFacepile members={source.members} maxFaces={4} />
          )}
          <Text muted small className="whitespace-nowrap">
            {userCount(source.memberCount)}
          </Text>
        </div>
      ),
    },
    {
      key: "key",
      header: "",
      width: "140px",
      render: () => (
        <div className="text-right">
          <Link to={mappingsHref} className={linkClassName}>
            View mapping →
          </Link>
        </div>
      ),
    },
  ];

  return sources.length > 0 ? (
    <Table columns={columns} data={sources} rowKey={(source) => source.key} />
  ) : (
    <InlineEmptyState
      icon="users"
      heading="No directory group or attribute gives out this role"
      description={`No directory group or attribute is mapped to ${role.name} yet. Map one to give it out.`}
      action={
        <Button asChild size="sm" variant="secondary">
          <Link to={mappingsHref}>Map a group</Link>
        </Button>
      }
    />
  );
}
