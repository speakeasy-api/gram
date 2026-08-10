import { Page } from "@/components/page-layout";
import { DotRow } from "@/components/ui/DotRow";
import { DotTable } from "@/components/ui/DotTable";
import { Text } from "@/components/ui/Text";
import { useIsPlatformAdmin } from "@/contexts/Auth";
import { useOrgRoutes } from "@/routes";
import type { GlobalRemoteSessionIssuer } from "@gram/client/models/components/globalremotesessionissuer.js";
import { invalidateAllGlobalRemoteSessionIssuer } from "@gram/client/react-query/globalRemoteSessionIssuer.js";
import {
  invalidateAllGlobalRemoteSessionIssuers,
  useGlobalRemoteSessionIssuersInfinite,
} from "@gram/client/react-query/globalRemoteSessionIssuers.js";
import { useRefreshGlobalRemoteSessionIssuerMetadataMutation } from "@gram/client/react-query/refreshGlobalRemoteSessionIssuerMetadata.js";
import { Button } from "@/components/ui/Button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/Dropdown";
import { Icon } from "@/components/ui/Icon";
import { useQueryClient } from "@tanstack/react-query";
import { MoreHorizontal, Plus } from "lucide-react";
import { useState } from "react";
import { Outlet } from "react-router";
import { toast } from "sonner";
import { issuerDisplayName } from "../remote-identity-providers/issuerDisplay";
import { CreatePlatformIssuerSheet } from "./CreatePlatformIssuerSheet";
import { DeletePlatformIssuerDialog } from "./DeletePlatformIssuerDialog";
import { PlatformAdminOnly } from "./PlatformAdminOnly";

// The platform catalog is a separate route subtree from the tenant Remote
// Identity Providers page rather than an admin-only section inside it. The two
// surfaces answer different questions — "what can my organization use" versus
// "what does Speakeasy publish to everyone" — and they disagree on the numbers:
// the tenant listing counts the viewing org's own clients on a shared issuer,
// this one counts the catalog's. Keeping them apart also means no admin branch
// ever renders inside a tenant component, so there is no conditional that can
// leak platform controls to a customer, and the catalog is reachable when it is
// still empty (the tenant page's platform section only renders once it is not).
export function PlatformRemoteIdentityProvidersRoot(): JSX.Element {
  return <Outlet />;
}

export function PlatformRemoteIdentityProvidersPage(): JSX.Element {
  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs />
      </Page.Header>
      <Page.Body>
        <PlatformAdminOnly feature="Platform Remote Identity Providers">
          <PlatformRemoteIdentityProvidersOverview />
        </PlatformAdminOnly>
      </Page.Body>
    </Page>
  );
}

function PlatformRemoteIdentityProvidersOverview() {
  const queryClient = useQueryClient();
  const isPlatformAdmin = useIsPlatformAdmin();
  // Infinite rather than single-shot so a catalog that outgrows the server's
  // page size stays fully reachable — rows past the first page would otherwise
  // be silently unlistable (and thus uneditable) here.
  const {
    data,
    isLoading,
    isError,
    hasNextPage,
    fetchNextPage,
    isFetchingNextPage,
  } = useGlobalRemoteSessionIssuersInfinite(
    {},
    undefined,
    // The endpoint refuses non-admins outright; there is no point spending a
    // request to be told so. PlatformAdminOnly already blocks the render, this
    // keeps a stray mount from firing the query too.
    { enabled: isPlatformAdmin },
  );
  const [deleteTarget, setDeleteTarget] =
    useState<GlobalRemoteSessionIssuer | null>(null);
  const [createOpen, setCreateOpen] = useState(false);

  const items = data?.pages.flatMap((page) => page.result.items) ?? [];

  const refreshMetadata = useRefreshGlobalRemoteSessionIssuerMetadataMutation({
    onSuccess: async (result) => {
      await Promise.all([
        invalidateAllGlobalRemoteSessionIssuers(queryClient, {
          refetchType: "all",
        }),
        // The detail page reads the singular query, which the list
        // invalidation above does not cover. Without this, opening a provider
        // right after refreshing it shows the pre-refresh endpoints.
        invalidateAllGlobalRemoteSessionIssuer(queryClient, {
          refetchType: "all",
        }),
      ]);
      const label = issuerDisplayName(result.issuer);
      if (result.discoveryWarnings.length > 0) {
        // The refresh did persist — these describe RFC 8414 deviations worth
        // an operator's attention, not a failure. Anything severe enough to
        // distrust the document fails the request instead.
        toast.warning(`Refreshed ${label} with warnings`, {
          description: result.discoveryWarnings.join(" "),
        });
        return;
      }
      toast.success(`Refreshed discoverable metadata for ${label}`);
    },
    onError: (error) => {
      toast.error(
        error instanceof Error ? error.message : "Failed to refresh metadata",
      );
    },
  });

  return (
    <>
      <Page.Section>
        <Page.Section.Title>
          Platform Remote Identity Providers
        </Page.Section.Title>
        <Page.Section.CTA>
          <Button size="sm" onClick={() => setCreateOpen(true)}>
            <Button.LeftIcon>
              <Plus />
            </Button.LeftIcon>
            <Button.Text>New Platform Provider</Button.Text>
          </Button>
        </Page.Section.CTA>
        <Page.Section.Description className="max-w-2xl">
          Well-known upstream identity providers curated by Speakeasy and
          inherited by every organization. Editing one here changes it for every
          tenant that has adopted it.
        </Page.Section.Description>
        <Page.Section.Body>
          <PlatformIssuerTable
            items={items}
            isLoading={isLoading}
            isError={isError}
            onDelete={setDeleteTarget}
            onRefreshMetadata={(item) =>
              refreshMetadata.mutate({
                request: { riskIDRequestBody: { id: item.issuer.id } },
              })
            }
            refreshPending={refreshMetadata.isPending}
          />
          {hasNextPage && (
            <div className="flex justify-center pt-2">
              <Button
                variant="tertiary"
                size="sm"
                disabled={isFetchingNextPage}
                onClick={() => void fetchNextPage()}
              >
                <Button.Text>
                  {isFetchingNextPage ? "Loading…" : "Load more"}
                </Button.Text>
              </Button>
            </div>
          )}
        </Page.Section.Body>
      </Page.Section>

      <CreatePlatformIssuerSheet
        open={createOpen}
        onOpenChange={setCreateOpen}
      />

      {deleteTarget && (
        <DeletePlatformIssuerDialog
          issuerId={deleteTarget.issuer.id}
          issuerLabel={issuerDisplayName(deleteTarget.issuer)}
          globalClientCount={deleteTarget.globalClientCount}
          tenantClientCount={deleteTarget.tenantClientCount}
          onClose={() => setDeleteTarget(null)}
        />
      )}
    </>
  );
}

function PlatformIssuerTable({
  items,
  isLoading,
  isError,
  onDelete,
  onRefreshMetadata,
  refreshPending,
}: {
  items: GlobalRemoteSessionIssuer[];
  isLoading: boolean;
  isError: boolean;
  onDelete: (item: GlobalRemoteSessionIssuer) => void;
  onRefreshMetadata: (item: GlobalRemoteSessionIssuer) => void;
  refreshPending: boolean;
}) {
  const orgRoutes = useOrgRoutes();

  if (isError) {
    return (
      <Text className="text-destructive py-8 text-center">
        Failed to load the platform catalog.
      </Text>
    );
  }

  if (!isLoading && items.length === 0) {
    return (
      <Text muted className="py-8 text-center">
        No platform identity providers yet.
      </Text>
    );
  }

  return (
    <DotTable
      headers={[
        { label: "Provider" },
        { label: "Platform Clients" },
        { label: "Tenant Clients" },
        { label: "" },
      ]}
    >
      {items.map((item) => (
        <DotRow
          key={item.issuer.id}
          icon={
            <Icon
              name="fingerprint"
              className="text-muted-foreground h-5 w-5"
            />
          }
          href={orgRoutes.platformRemoteIdentityProviders.issuerDetail.href(
            item.issuer.id,
          )}
          ariaLabel={`View platform identity provider ${issuerDisplayName(item.issuer)}`}
        >
          <td className="px-3 py-3">
            <Text
              variant="subheading"
              as="div"
              className="group-hover:text-primary truncate text-sm transition-colors group-hover:underline"
            >
              {issuerDisplayName(item.issuer)}
            </Text>
            <Text small muted as="div" className="truncate">
              {item.issuer.issuer}
            </Text>
          </td>
          <td className="px-3 py-3">
            <Text small muted>
              {item.globalClientCount}{" "}
              {item.globalClientCount === 1 ? "client" : "clients"}
            </Text>
          </td>
          {/* Tenant clients are what organizations registered against this
              shared issuer. A platform admin cannot see or remove them, but
              they block a delete, so the count is shown to explain a refusal
              before it happens rather than after. */}
          <td className="px-3 py-3">
            <Text small muted>
              {item.tenantClientCount}{" "}
              {item.tenantClientCount === 1 ? "client" : "clients"}
            </Text>
          </td>
          <td className="px-3 py-3 text-right">
            <PlatformRowActions
              issuerId={item.issuer.id}
              issuerLabel={issuerDisplayName(item.issuer)}
              onDelete={() => onDelete(item)}
              onRefreshMetadata={() => onRefreshMetadata(item)}
              refreshPending={refreshPending}
            />
          </td>
        </DotRow>
      ))}
    </DotTable>
  );
}

function PlatformRowActions({
  issuerId,
  issuerLabel,
  onDelete,
  onRefreshMetadata,
  refreshPending,
}: {
  issuerId: string;
  issuerLabel: string;
  onDelete: () => void;
  onRefreshMetadata: () => void;
  refreshPending: boolean;
}) {
  const orgRoutes = useOrgRoutes();

  return (
    <div className="relative z-20" onClick={(e) => e.stopPropagation()}>
      {/*
        Non-modal for the same reason as the tenant listing: a mutation
        invalidates the issuers query and reorders the rows, unmounting this
        menu mid-close. A modal menu would then leave Radix's
        `pointer-events: none` body lock stuck and the page unclickable.
      */}
      <DropdownMenu modal={false}>
        <DropdownMenuTrigger asChild>
          {/* Icon-only, so it needs its own accessible name — and naming the
              provider keeps the menus distinguishable when several rows are
              present. */}
          <Button
            variant="tertiary"
            size="sm"
            aria-label={`Actions for ${issuerLabel}`}
          >
            <Button.LeftIcon>
              <MoreHorizontal className="h-4 w-4" />
            </Button.LeftIcon>
          </Button>
        </DropdownMenuTrigger>
        <DropdownMenuContent align="end">
          <DropdownMenuItem
            onClick={onRefreshMetadata}
            disabled={refreshPending}
          >
            Refresh Discoverable Metadata
          </DropdownMenuItem>
          <DropdownMenuSeparator />
          <DropdownMenuItem
            onClick={() =>
              orgRoutes.platformRemoteIdentityProviders.issuerDetail.settings.goTo(
                issuerId,
              )
            }
          >
            Edit
          </DropdownMenuItem>
          <DropdownMenuSeparator />
          <DropdownMenuItem onClick={onDelete}>Delete</DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>
    </div>
  );
}
