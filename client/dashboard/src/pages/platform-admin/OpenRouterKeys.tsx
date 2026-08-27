import { Page } from "@/components/page-layout";
import { Badge } from "@/components/ui/Badge";
import { Input } from "@/components/ui/Input";
import { MoreActions, type Action } from "@/components/ui/MoreActions";
import { Skeleton } from "@/components/ui/Skeleton";
import { Table, type Column } from "@/components/ui/Table";
import { TablePagination } from "@/components/ui/TablePagination";
import { usePagedRows } from "@/components/ui/TablePagination/usePagedRows";
import { Text } from "@/components/ui/Text";
import { SimpleTooltip } from "@/components/ui/Tooltip";
import type { AdminOpenRouterKey } from "@gram/client/models/components/adminopenrouterkey.js";
import {
  invalidateAllAdminOpenRouterKeys,
  useAdminOpenRouterKeys,
} from "@gram/client/react-query/adminOpenRouterKeys.js";
import { useAdminOpenRouterKeyUsage } from "@gram/client/react-query/adminOpenRouterKeyUsage.js";
import { useDisableAdminOpenRouterKeyMutation } from "@gram/client/react-query/disableAdminOpenRouterKey.js";
import { useEnableAdminOpenRouterKeyMutation } from "@gram/client/react-query/enableAdminOpenRouterKey.js";
import { useQueryClient } from "@tanstack/react-query";
import { useMemo, useState } from "react";
import { toast } from "sonner";
import { useIsPlatformAdmin } from "@/contexts/Auth";
import { causeLabels, keyAction } from "./openRouterKeyState";

// Rows per page; also the ceiling on concurrent live usage fetches, since
// only mounted rows request usage.
const PAGE_SIZE = 25;

export default function PlatformAdminOpenRouterKeys(): JSX.Element {
  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs />
      </Page.Header>
      <Page.Body>
        <Page.Section>
          <Page.Section.Title area="Platform Admin">
            OpenRouter Keys
          </Page.Section.Title>
          <Page.Section.Description>
            Platform-issued OpenRouter keys across every organization: credit
            limits and live usage.
          </Page.Section.Description>
          <Page.Section.Body>
            <StrictPlatformAdminGate>
              <KeysTable />
            </StrictPlatformAdminGate>
          </Page.Section.Body>
        </Page.Section>
      </Page.Body>
    </Page>
  );
}

// Unlike PlatformAdminGate, no local-dev bypass: this page manages live
// upstream OpenRouter credentials across every organization, so it stays
// strictly admin-gated like Remote Identity Providers. Local developers can
// still reach it through the impersonation toggle on the Overview page, which
// flips the platform-admin flag itself.
function StrictPlatformAdminGate({
  children,
}: {
  children: React.ReactNode;
}): JSX.Element {
  const isPlatformAdmin = useIsPlatformAdmin();

  if (!isPlatformAdmin) {
    return (
      <Text muted className="py-8 text-center">
        This page is available to platform admins only.
      </Text>
    );
  }

  return <>{children}</>;
}

// Usage is fetched live per visible row rather than stored: nothing in the
// database records current spend, and the periodic credits monitor only emits
// metrics for alerting.
function UsageCell({ row }: { row: AdminOpenRouterKey }): JSX.Element {
  const usage = useAdminOpenRouterKeyUsage(
    {
      organizationId: row.organizationId,
      keyType: row.keyType === "internal" ? "internal" : "chat",
    },
    undefined,
    {
      enabled: !row.disabled,
      staleTime: 5 * 60 * 1000,
      retry: false,
      throwOnError: false,
    },
  );

  if (row.disabled) {
    return (
      <SimpleTooltip tooltip="Disabled keys are not polled for usage.">
        <Text muted small>
          —
        </Text>
      </SimpleTooltip>
    );
  }
  if (usage.isLoading) {
    return <Skeleton className="h-4 w-20" />;
  }
  if (usage.isError || !usage.data) {
    return (
      <SimpleTooltip tooltip="OpenRouter did not return usage for this key.">
        <Text muted small>
          unavailable
        </Text>
      </SimpleTooltip>
    );
  }

  return (
    <SimpleTooltip tooltip="Fetched live from OpenRouter when this row is displayed and cached for 5 minutes in this session. Reflects OpenRouter's own month-to-date accounting at fetch time; the alerting pipeline polls separately every 5 minutes.">
      <Text small>
        ${usage.data.creditsUsed.toFixed(2)} of ${String(row.monthlyCredits)}
      </Text>
    </SimpleTooltip>
  );
}

function KeysTable(): JSX.Element {
  const queryClient = useQueryClient();
  const { data, isLoading, error } = useAdminOpenRouterKeys();
  const [search, setSearch] = useState("");

  const invalidate = () => {
    void invalidateAllAdminOpenRouterKeys(queryClient);
  };

  const disable = useDisableAdminOpenRouterKeyMutation({
    onSuccess: (key) => {
      toast.success(
        `Admin lock added to the ${key.keyType} key for ${key.organizationName}.`,
      );
      invalidate();
    },
    onError: (err) => {
      toast.error(
        err instanceof Error ? err.message : "Failed to disable the key",
      );
    },
  });
  const enable = useEnableAdminOpenRouterKeyMutation({
    onSuccess: (key) => {
      toast.success(
        `Admin lock removed from the ${key.keyType} key for ${key.organizationName}.`,
      );
      invalidate();
    },
    onError: (err) => {
      toast.error(
        err instanceof Error ? err.message : "Failed to enable the key",
      );
    },
  });

  const keys = useMemo(() => {
    const all = data?.keys ?? [];
    const needle = search.trim().toLowerCase();
    if (!needle) return all;
    return all.filter(
      (key) =>
        key.organizationName.toLowerCase().includes(needle) ||
        key.organizationSlug.toLowerCase().includes(needle) ||
        key.organizationId.toLowerCase().includes(needle),
    );
  }, [data, search]);

  // Paginate client-side so only the rendered page's rows mount UsageCells:
  // each cell issues a live OpenRouter usage request, and an unpaginated
  // render would fan out one request per key in the whole fleet.
  const { page, pageRows, setPage } = usePagedRows({
    rows: keys,
    pageSize: PAGE_SIZE,
    resetOn: [search],
  });

  const rowActions = (row: AdminOpenRouterKey): Action[] => {
    const keyType = row.keyType === "internal" ? "internal" : "chat";
    const body = { organizationId: row.organizationId, keyType } as const;
    const actions: Action[] = [];
    if (keyAction(row.disableCauses) === "remove-admin-lock") {
      actions.push({
        icon: "play",
        label: "Remove admin lock",
        disabled: enable.isPending,
        onClick: () =>
          enable.mutate({
            request: { enableOpenRouterKeyRequestBody: body },
          }),
      });
    } else {
      actions.push({
        icon: "ban",
        label: "Disable key",
        destructive: true,
        disabled: disable.isPending,
        onClick: () =>
          disable.mutate({
            request: { disableOpenRouterKeyRequestBody: body },
          }),
      });
    }
    return actions;
  };

  const columns: Column<AdminOpenRouterKey>[] = [
    {
      key: "organization",
      header: "Organization",
      render: (row) => (
        <div className="min-w-0">
          <Text variant="body" className="truncate font-medium">
            {row.organizationName}
          </Text>
          <Text muted small className="truncate">
            {row.organizationSlug}
          </Text>
        </div>
      ),
    },
    {
      key: "keyType",
      header: "Key type",
      width: "110px",
      render: (row) => (
        <Badge variant="neutral" className="shrink-0">
          <Badge.Text>{row.keyType}</Badge.Text>
        </Badge>
      ),
    },
    {
      key: "accountType",
      header: "Account",
      width: "110px",
      render: (row) => <Text small>{row.gramAccountType}</Text>,
    },
    {
      key: "limit",
      header: "Limit",
      width: "110px",
      render: (row) => <Text small>${String(row.monthlyCredits)}/mo</Text>,
    },
    {
      key: "usage",
      header: "Usage",
      width: "160px",
      render: (row) => <UsageCell row={row} />,
    },
    {
      key: "status",
      header: "Status",
      width: "110px",
      render: (row) =>
        row.disabled ? (
          <Badge variant="destructive" background className="shrink-0">
            <Badge.Text>Disabled</Badge.Text>
          </Badge>
        ) : (
          <Badge variant="success" className="shrink-0">
            <Badge.Text>Enabled</Badge.Text>
          </Badge>
        ),
    },
    {
      key: "causes",
      header: "Causes",
      width: "180px",
      render: (row) => {
        const labels = causeLabels(row.disableCauses);
        return labels.length > 0 ? (
          <div className="space-y-1">
            {labels.map((label) => (
              <Text key={label} small>
                {label}
              </Text>
            ))}
          </div>
        ) : (
          <Text muted small>
            —
          </Text>
        );
      },
    },
    {
      key: "actions",
      header: "",
      width: "56px",
      render: (row) => <MoreActions actions={rowActions(row)} />,
    },
  ];

  if (isLoading) {
    return <Skeleton className="h-48 w-full" />;
  }
  if (error) {
    return (
      <Text muted className="py-8 text-center">
        Failed to load OpenRouter keys: {error.message}
      </Text>
    );
  }

  return (
    <div className="space-y-3">
      <Input
        value={search}
        onChange={(value: string) => setSearch(value)}
        placeholder="Filter by organization name, slug, or id…"
        className="max-w-sm"
      />
      <Table
        columns={columns}
        data={pageRows}
        rowKey={(row) => `${row.organizationId}/${row.keyType}`}
      />
      <TablePagination
        page={page}
        pageSize={PAGE_SIZE}
        totalItems={keys.length}
        onPageChange={setPage}
      />
      <Text muted small>
        {keys.length} key{keys.length === 1 ? "" : "s"}
        {search.trim() ? " matching filter" : ""}. One row per organization per
        key type; keys are minted on an organization's first completion.
      </Text>
    </div>
  );
}
