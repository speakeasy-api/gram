import { InternalAdminBadge } from "@/components/internal-admin-badge";
import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import { Heading } from "@/components/ui/Heading";
import { Input } from "@/components/ui/Input";
import { MoreActions, type Action } from "@/components/ui/MoreActions";
import { Skeleton } from "@/components/ui/Skeleton";
import { Stack } from "@/components/ui/Stack";
import { Table, type Column } from "@/components/ui/Table";
import { Text } from "@/components/ui/Text";
import { useIsPlatformAdmin } from "@/contexts/Auth";
import { formatRelativeTime } from "@/lib/dates";
import type { AiScanTarget } from "@gram/client/models/components/aiscantarget.js";
import type { UpsertRequestBody2 } from "@gram/client/models/components/upsertrequestbody2.js";
import { usePlatformAiScanTargetsDeleteMutation } from "@gram/client/react-query/platformAiScanTargetsDelete.js";
import {
  invalidateAllPlatformAiScanTargetsList,
  usePlatformAiScanTargetsList,
} from "@gram/client/react-query/platformAiScanTargetsList.js";
import { usePlatformAiScanTargetsSetEnabledMutation } from "@gram/client/react-query/platformAiScanTargetsSetEnabled.js";
import { usePlatformAiScanTargetsUpsertMutation } from "@gram/client/react-query/platformAiScanTargetsUpsert.js";
import { useQueryClient } from "@tanstack/react-query";
import { useMemo, useState } from "react";
import { toast } from "sonner";
import {
  AiScanTargetEditorSheet,
  DeleteAiScanTargetDialog,
  type EditorMode,
} from "./ai-scan-target-editor-sheet";
import {
  categoryLabel,
  draftFromTarget,
  emptyDraft,
  signatureSummary,
  type Draft,
} from "./ai-scan-target-draft";

// Rendered inside the Device Agent configuration tab. The catalog is global
// and Speakeasy-managed, so only platform admins see the section; the list
// endpoint rejects everyone else.
export function AiScanTargetsSection(): JSX.Element | null {
  const isPlatformAdmin = useIsPlatformAdmin();
  if (!isPlatformAdmin) return null;

  return (
    <Stack gap={6} className="border-border mt-6 border-t pt-8">
      <div>
        <Heading variant="h4" className="mb-2 flex items-center gap-2">
          AI scan targets
          <InternalAdminBadge />
        </Heading>
        <Text muted small>
          The Shadow AI catalog every enrolled device agent probes for, in every
          organization. Changes reach agents on their next policy poll, without
          an agent release.
        </Text>
      </div>
      <Catalog />
    </Stack>
  );
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
      await invalidateAllPlatformAiScanTargetsList(queryClient);
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
      await invalidateAllPlatformAiScanTargetsList(queryClient);
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
      await invalidateAllPlatformAiScanTargetsList(queryClient);
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
        <span className="font-mono">target_list_version</span> once they receive
        this list.
      </Text>

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
