import { useInfiniteQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { CopyIcon, ExternalLinkIcon, MoreHorizontalIcon } from "lucide-react";
import { useState, type JSX } from "react";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuTrigger,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuSub,
  DropdownMenuSubTrigger,
  DropdownMenuSubContent,
  DropdownMenuLabel,
} from "@/components/ui/dropdown-menu";
import {
  errorMessage,
  listUserOrganizations,
  openOrganizationDashboard,
  type AdminUser,
  type AdminUserOrganization,
} from "@/lib/gramAdminApi";

function OrganizationItems({ org }: { org: AdminUserOrganization }) {
  return (
    <>
      <DropdownMenuItem asChild>
        <Link to="/organizations/$idOrSlug" params={{ idOrSlug: org.id }}>
          View Organization
        </Link>
      </DropdownMenuItem>
      <DropdownMenuItem
        disabled={Boolean(org.disabled_at)}
        onSelect={() => openOrganizationDashboard(org.id)}
      >
        <ExternalLinkIcon />
        Open in Dashboard
      </DropdownMenuItem>
    </>
  );
}
export function UserActions({ user }: { user: AdminUser }): JSX.Element {
  const [open, setOpen] = useState(false);
  const overflow = user.organization_count > user.organizations.length;
  const memberships = useInfiniteQuery({
    queryKey: [
      "admin",
      "user-organizations-infinite",
      { user_id: user.id, limit: 50 },
    ],
    initialPageParam: 1,
    queryFn: ({ pageParam, signal }) =>
      listUserOrganizations(
        { user_id: user.id, page: pageParam, limit: 50 },
        signal,
      ),
    getNextPageParam: (last) =>
      last.page * last.limit < last.total ? last.page + 1 : undefined,
    enabled: open && overflow,
  });
  // Once loaded, endpoint pagination/counts supersede the bounded preview.
  const orgs = memberships.data
    ? [
        ...new Map(
          memberships.data.pages
            .flatMap((page) => page.organizations)
            .map((org) => [org.id, org]),
        ).values(),
      ]
    : user.organizations;
  const total =
    memberships.data?.pages.at(-1)?.total ?? user.organization_count;
  const copy = async (value: string, label: string) => {
    try {
      await navigator.clipboard.writeText(value);
      toast.success(`${label} copied`);
    } catch (error) {
      toast.error(
        `Could not copy ${label.toLowerCase()}: ${errorMessage(error)}`,
      );
    }
  };
  return (
    <DropdownMenu open={open} onOpenChange={setOpen}>
      <DropdownMenuTrigger asChild>
        <Button
          variant="ghost"
          size="icon"
          aria-label={`Actions for ${user.display_name || user.email}`}
        >
          <MoreHorizontalIcon />
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end">
        <DropdownMenuItem
          disabled={!user.display_name}
          onSelect={() => void copy(user.display_name, "Name")}
        >
          <CopyIcon />
          Copy Name
        </DropdownMenuItem>
        <DropdownMenuItem onSelect={() => void copy(user.email, "Email")}>
          <CopyIcon />
          Copy Email
        </DropdownMenuItem>
        {total > 0 && <DropdownMenuSeparator />}
        {user.organization_count === 1 && !overflow ? (
          orgs.map((org) => <OrganizationItems key={org.id} org={org} />)
        ) : (
          <>
            {total > 0 && (
              <DropdownMenuLabel>
                {orgs.length} of {total} organizations
              </DropdownMenuLabel>
            )}
            {orgs.map((org) => (
              <DropdownMenuSub key={org.id}>
                <DropdownMenuSubTrigger>
                  {org.name} ({org.slug}){org.disabled_at ? " — Disabled" : ""}
                </DropdownMenuSubTrigger>
                <DropdownMenuSubContent>
                  <OrganizationItems org={org} />
                </DropdownMenuSubContent>
              </DropdownMenuSub>
            ))}
            {memberships.isError && (
              <DropdownMenuLabel role="alert">
                Could not load organizations: {errorMessage(memberships.error)}
              </DropdownMenuLabel>
            )}
            {overflow && (
              <DropdownMenuItem
                aria-disabled={
                  memberships.isFetching ||
                  (!memberships.isError && !memberships.hasNextPage)
                }
                onSelect={(event) => {
                  event.preventDefault();
                  if (memberships.isFetching) return;
                  if (memberships.isError)
                    void (memberships.isFetchNextPageError
                      ? memberships.fetchNextPage()
                      : memberships.refetch());
                  else if (memberships.hasNextPage)
                    void memberships.fetchNextPage();
                }}
              >
                {memberships.isFetching
                  ? "Loading organizations…"
                  : memberships.isError
                    ? "Retry organizations"
                    : memberships.hasNextPage
                      ? "Load more organizations"
                      : "All organizations loaded"}
              </DropdownMenuItem>
            )}
          </>
        )}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
