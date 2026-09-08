import { Page } from "@/components/page-layout";
import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import { Input } from "@/components/ui/Input";
import { MoreActions, type Action } from "@/components/ui/MoreActions";
import { Skeleton } from "@/components/ui/Skeleton";
import { Table, type Column } from "@/components/ui/Table";
import { Text } from "@/components/ui/Text";
import { formatRelativeTime } from "@/lib/dates";
import type { AiScanCatalogRevision } from "@gram/client/models/components/aiscancatalogrevision.js";
import type { AiScanTarget } from "@gram/client/models/components/aiscantarget.js";
import type { UpsertRequestBody2 } from "@gram/client/models/components/upsertrequestbody2.js";
import { usePlatformAiScanTargetsDeleteMutation } from "@gram/client/react-query/platformAiScanTargetsDelete.js";
import {
  invalidateAllPlatformAiScanTargetsList,
  usePlatformAiScanTargetsList,
} from "@gram/client/react-query/platformAiScanTargetsList.js";
import {
  invalidateAllPlatformAiScanTargetsListRevisions,
  usePlatformAiScanTargetsListRevisions,
} from "@gram/client/react-query/platformAiScanTargetsListRevisions.js";
import { usePlatformAiScanTargetsSetEnabledMutation } from "@gram/client/react-query/platformAiScanTargetsSetEnabled.js";
import { usePlatformAiScanTargetsUpsertMutation } from "@gram/client/react-query/platformAiScanTargetsUpsert.js";
import { useQueryClient, type QueryClient } from "@tanstack/react-query";
import { useMemo, useState } from "react";
import { toast } from "sonner";
import {
  AiScanTargetEditorSheet,
  DeleteAiScanTargetDialog,
  type EditorMode,
} from "./AiScanTargetEditorSheet";
import {
  categoryLabel,
  draftFromTarget,
  emptyDraft,
  revisionActionLabel,
  signatureSummary,
  type Draft,
} from "./aiScanTargetDraft";
import { StrictPlatformAdminGate } from "./StrictPlatformAdminGate";

const REVISION_HISTORY_LIMIT = 25;

export default function PlatformAdminAiScanTargets(): JSX.Element {
  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs />
      </Page.Header>
      <Page.Body>
        <Page.Section>
          <Page.Section.Title area="Platform Admin">
            AI Scan Targets
          </Page.Section.Title>
          <Page.Section.Description>
            The Shadow AI catalog every enrolled device agent probes for.
            Changes reach agents on their next policy poll, without an agent
            release.
          </Page.Section.Description>
          <Page.Section.Body>
            <StrictPlatformAdminGate>
              <Catalog />
            </StrictPlatformAdminGate>
          </Page.Section.Body>
        </Page.Section>
      </Page.Body>
    </Page>
  );
}

async function invalidateCatalog(queryClient: QueryClient): Promise<void> {
  await Promise.all([
    invalidateAllPlatformAiScanTargetsList(queryClient),
    invalidateAllPlatformAiScanTargetsListRevisions(queryClient),
  ]);
}

function errorMessage(err: unknown, fallback: string): string {
  return err instanceof Error && err.message ? err.message : fallback;
}

type EditorState = { mode: EditorMode; draft: Draft } | null;

function Catalog(): JSX.Element {
  const queryClient = useQueryClient();
  const list = usePlatformAiScanTargetsList(undefined, undefined, {
    throwOnError: false,
  });
  const [search, setSearch] = useState("");
  const [editor, setEditor] = useState<EditorState>(null);
  const [deleting, setDeleting] = useState<string | null>(null);
  const [pendingId, setPendingId] = useState<string | null>(null);
  const [editorError, setEditorError] = useState<string | null>(null);

  const upsert = usePlatformAiScanTargetsUpsertMutation({
    onSuccess: async (result) => {
      toast.success(
        `Saved ${result.target.id}. Catalog revision ${result.listVersion} reaches agents on their next poll.`,
      );
      setEditor(null);
      setEditorError(null);
      await invalidateCatalog(queryClient);
    },
    onError: (err) => {
      setEditorError(errorMessage(err, "Failed to save the target"));
    },
  });
  const setEnabled = usePlatformAiScanTargetsSetEnabledMutation({
    onSuccess: async (result) => {
      toast.success(
        `${result.target.enabled ? "Enabled" : "Disabled"} ${result.target.id}.`,
      );
      await invalidateCatalog(queryClient);
    },
    onError: (err) => {
      toast.error(errorMessage(err, "Failed to update the target"));
    },
    onSettled: () => setPendingId(null),
  });
  const remove = usePlatformAiScanTargetsDeleteMutation({
    onSuccess: async () => {
      toast.success("Target deleted.");
      setDeleting(null);
      await invalidateCatalog(queryClient);
    },
    onError: (err) => {
      toast.error(errorMessage(err, "Failed to delete the target"));
    },
    onSettled: () => setPendingId(null),
  });

  const targets = useMemo(() => {
    const all = list.data?.targets ?? [];
    const needle = search.trim().toLowerCase();
    if (!needle) return all;
    return all.filter(
      (target) =>
        target.id.toLowerCase().includes(needle) ||
        target.displayName.toLowerCase().includes(needle),
    );
  }, [list.data, search]);

  const mutationPending =
    pendingId !== null ||
    upsert.isPending ||
    setEnabled.isPending ||
    remove.isPending;

  const rowActions = (row: AiScanTarget): Action[] => [
    {
      icon: "pencil",
      label: "Edit",
      disabled: mutationPending,
      onClick: () => {
        setEditorError(null);
        setEditor({ mode: "edit", draft: draftFromTarget(row) });
      },
    },
    {
      icon: row.enabled ? "ban" : "play",
      label: row.enabled ? "Disable" : "Enable",
      disabled: mutationPending,
      onClick: () => {
        if (mutationPending) return;
        setPendingId(row.id);
        setEnabled.mutate({
          request: {
            setEnabledRequestBody: { id: row.id, enabled: !row.enabled },
          },
        });
      },
    },
    {
      icon: "trash",
      label: "Delete",
      destructive: true,
      disabled: mutationPending,
      onClick: () => setDeleting(row.id),
    },
  ];

  const columns: Column<AiScanTarget>[] = [
    {
      key: "target",
      header: "Target",
      render: (row) => (
        <div className="min-w-0">
          <Text variant="body" className="truncate font-medium">
            {row.displayName}
          </Text>
          <Text muted small className="truncate font-mono">
            {row.id}
          </Text>
        </div>
      ),
    },
    {
      key: "category",
      header: "Category",
      width: "130px",
      render: (row) => (
        <Badge variant="neutral" className="shrink-0">
          <Badge.Text>{categoryLabel(row.category)}</Badge.Text>
        </Badge>
      ),
    },
    {
      key: "signatures",
      header: "Signatures",
      width: "260px",
      render: (row) => <Text small>{signatureSummary(row)}</Text>,
    },
    {
      key: "status",
      header: "Status",
      width: "110px",
      render: (row) =>
        row.enabled ? (
          <Badge variant="success" className="shrink-0">
            <Badge.Text>Served</Badge.Text>
          </Badge>
        ) : (
          <Badge variant="neutral" background className="shrink-0">
            <Badge.Text>Disabled</Badge.Text>
          </Badge>
        ),
    },
    {
      key: "updated",
      header: "Updated",
      width: "140px",
      render: (row) => (
        <Text muted small>
          {formatRelativeTime(row.updatedAt) ?? "—"}
        </Text>
      ),
    },
    {
      key: "actions",
      header: "",
      width: "56px",
      render: (row) => (
        <MoreActions
          actions={rowActions(row)}
          triggerLoading={pendingId === row.id}
          triggerDisabled={mutationPending}
        />
      ),
    },
  ];

  const submitDraft = (body: UpsertRequestBody2): void => {
    setEditorError(null);
    upsert.mutate({ request: { upsertRequestBody2: body } });
  };

  if (list.isLoading) {
    return <Skeleton className="h-48 w-full" />;
  }
  if (list.error) {
    return (
      <Text muted className="py-8 text-center">
        Failed to load the scan target catalog: {list.error.message}
      </Text>
    );
  }

  return (
    <div className="space-y-8">
      <div className="space-y-3">
        <div className="flex flex-wrap items-center justify-between gap-3">
          <Input
            value={search}
            onChange={(value: string) => setSearch(value)}
            placeholder="Filter by id or name…"
            className="max-w-sm"
          />
          <Button
            onClick={() => {
              setEditorError(null);
              setEditor({ mode: "create", draft: emptyDraft() });
            }}
          >
            Add target
          </Button>
        </div>
        <Table
          columns={columns}
          data={targets}
          rowKey={(row) => row.id}
          noResultsMessage={<Text>No matching targets</Text>}
        />
        <Text muted small>
          {targets.length} target{targets.length === 1 ? "" : "s"}
          {search.trim() ? " matching filter" : ""}. Catalog revision{" "}
          {list.data?.listVersion ?? 0} is what agents echo as{" "}
          <span className="font-mono">target_list_version</span> once they
          receive this list.
        </Text>
      </div>

      <RevisionHistory />

      <AiScanTargetEditorSheet
        open={editor !== null}
        mode={editor?.mode ?? "create"}
        initialDraft={editor?.draft ?? emptyDraft()}
        pending={upsert.isPending}
        serverError={editorError}
        onOpenChange={(open) => {
          if (!open) {
            setEditor(null);
            setEditorError(null);
          }
        }}
        onSubmit={submitDraft}
      />
      <DeleteAiScanTargetDialog
        targetId={deleting}
        pending={remove.isPending}
        onOpenChange={(open) => {
          if (!open) setDeleting(null);
        }}
        onConfirm={(targetId) => {
          setPendingId(targetId);
          remove.mutate({ request: { deleteRequestBody2: { id: targetId } } });
        }}
      />
    </div>
  );
}

function RevisionHistory(): JSX.Element {
  const revisions = usePlatformAiScanTargetsListRevisions(
    { limit: REVISION_HISTORY_LIMIT },
    undefined,
    { throwOnError: false },
  );

  const columns: Column<AiScanCatalogRevision>[] = [
    {
      key: "revision",
      header: "Revision",
      width: "90px",
      render: (row) => (
        <Text small className="font-mono">
          {row.revision}
        </Text>
      ),
    },
    {
      key: "action",
      header: "Change",
      width: "110px",
      render: (row) => (
        <Badge variant="neutral" className="shrink-0">
          <Badge.Text>{revisionActionLabel(row.action)}</Badge.Text>
        </Badge>
      ),
    },
    {
      key: "target",
      header: "Target",
      width: "180px",
      render: (row) => (
        <Text small className="font-mono">
          {row.targetId}
        </Text>
      ),
    },
    {
      key: "actor",
      header: "By",
      width: "220px",
      render: (row) => (
        <Text small muted={!row.actorEmail}>
          {row.actorEmail ?? "Speakeasy (seed)"}
        </Text>
      ),
    },
    {
      key: "reason",
      header: "Reason",
      render: (row) => (
        <Text
          small
          muted={!row.reason}
          className="truncate"
          title={row.reason ?? undefined}
        >
          {row.reason ?? "—"}
        </Text>
      ),
    },
    {
      key: "when",
      header: "When",
      width: "140px",
      render: (row) => (
        <Text muted small>
          {formatRelativeTime(row.createdAt) ?? "—"}
        </Text>
      ),
    },
  ];

  return (
    <section className="space-y-3">
      <Text className="text-eyebrow">Recent changes</Text>
      {revisions.isLoading ? (
        <Skeleton className="h-24 w-full" />
      ) : revisions.error ? (
        <Text muted small>
          Failed to load catalog history: {revisions.error.message}
        </Text>
      ) : (
        <Table
          columns={columns}
          data={revisions.data?.revisions ?? []}
          rowKey={(row) => String(row.revision)}
          noResultsMessage={<Text>No changes recorded yet</Text>}
        />
      )}
    </section>
  );
}
