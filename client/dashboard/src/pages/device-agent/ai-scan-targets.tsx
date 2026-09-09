import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import { Heading } from "@/components/ui/Heading";
import { Input } from "@/components/ui/Input";
import { MoreActions, type Action } from "@/components/ui/MoreActions";
import { Skeleton } from "@/components/ui/Skeleton";
import { Stack } from "@/components/ui/Stack";
import { Table, type Column } from "@/components/ui/Table";
import { Text } from "@/components/ui/Text";
import { formatRelativeTime } from "@/lib/dates";
import type { AiScanTarget } from "@gram/client/models/components/aiscantarget.js";
import type { UpsertAiScanTargetRequestBody } from "@gram/client/models/components/upsertaiscantargetrequestbody.js";
import { useDeleteDeviceAgentAiScanTargetMutation } from "@gram/client/react-query/deleteDeviceAgentAiScanTarget.js";
import {
  invalidateAllDeviceAgentAiScanTargets,
  useDeviceAgentAiScanTargets,
} from "@gram/client/react-query/deviceAgentAiScanTargets.js";
import { useUpsertDeviceAgentAiScanTargetMutation } from "@gram/client/react-query/upsertDeviceAgentAiScanTarget.js";
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
  draftToUpsertBody,
  emptyDraft,
  signatureSummary,
  type Draft,
} from "./ai-scan-target-draft";

// Rendered inside the Device Agent configuration tab, which is already
// limited to organization admins, the scope the endpoints require.
export function AiScanTargetsSection(): JSX.Element {
  return (
    <Stack gap={6} className="border-border mt-6 border-t pt-8">
      <div>
        <Heading variant="h4" className="mb-2">
          AI scan targets
        </Heading>
        <Text muted small>
          The AI tools this organization's device agents probe for. Speakeasy
          keeps the defaults current; add your own targets or switch a default
          off. Changes reach agents on their next policy poll.
        </Text>
      </div>
      <Catalog />
    </Stack>
  );
}

function errorMessage(err: unknown, fallback: string): string {
  return err instanceof Error && err.message ? err.message : fallback;
}

function isDefault(target: AiScanTarget): boolean {
  return target.origin === "default";
}

type EditorState = { mode: EditorMode; draft: Draft } | null;

function Catalog(): JSX.Element {
  const queryClient = useQueryClient();
  const list = useDeviceAgentAiScanTargets(undefined, undefined, {
    throwOnError: false,
  });
  const [search, setSearch] = useState("");
  const [editor, setEditor] = useState<EditorState>(null);
  const [deleting, setDeleting] = useState<string | null>(null);
  const [pendingId, setPendingId] = useState<string | null>(null);
  const [editorError, setEditorError] = useState<string | null>(null);

  const invalidate = () => invalidateAllDeviceAgentAiScanTargets(queryClient);

  const save = useUpsertDeviceAgentAiScanTargetMutation({
    onSuccess: async (result) => {
      toast.success(
        `Saved ${result.target.id}. List version ${result.listVersion} reaches agents on their next poll.`,
      );
      setEditor(null);
      setEditorError(null);
      await invalidate();
    },
    onError: (err) => {
      setEditorError(errorMessage(err, "Failed to save the target"));
    },
  });
  const toggle = useUpsertDeviceAgentAiScanTargetMutation({
    onSuccess: async (result) => {
      toast.success(
        `${result.target.enabled ? "Enabled" : "Disabled"} ${result.target.id}.`,
      );
      await invalidate();
    },
    onError: (err) => {
      toast.error(errorMessage(err, "Failed to update the target"));
    },
    onSettled: () => setPendingId(null),
  });
  const remove = useDeleteDeviceAgentAiScanTargetMutation({
    onSuccess: async () => {
      toast.success("Target deleted.");
      setDeleting(null);
      await invalidate();
    },
    onError: (err) => {
      toast.error(errorMessage(err, "Failed to delete the target"));
    },
    onSettled: () => setPendingId(null),
  });
  const restore = useDeleteDeviceAgentAiScanTargetMutation({
    onSuccess: async () => {
      toast.success("Speakeasy default restored.");
      await invalidate();
    },
    onError: (err) => {
      toast.error(errorMessage(err, "Failed to restore the default"));
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
    save.isPending ||
    toggle.isPending ||
    remove.isPending ||
    restore.isPending;

  // A default is switched off by customizing it under its own id, and
  // switched back on by dropping that customization so Speakeasy's copy is
  // served again. Organization targets toggle in place.
  const toggleAction = (row: AiScanTarget): Action => {
    const restoresDefault = isDefault(row) && !row.enabled;
    return {
      icon: row.enabled ? "ban" : "play",
      label: row.enabled ? "Disable" : "Enable",
      disabled: mutationPending,
      onClick: () => {
        if (mutationPending) return;
        setPendingId(row.id);
        if (restoresDefault) {
          restore.mutate({
            request: { deleteAiScanTargetRequestBody: { id: row.id } },
          });
          return;
        }
        const body = draftToUpsertBody(draftFromTarget(row));
        toggle.mutate({
          request: {
            upsertAiScanTargetRequestBody: { ...body, enabled: !row.enabled },
          },
        });
      },
    };
  };

  const rowActions = (row: AiScanTarget): Action[] => {
    if (isDefault(row)) {
      return [toggleAction(row)];
    }
    return [
      {
        icon: "pencil",
        label: "Edit",
        disabled: mutationPending,
        onClick: () => {
          setEditorError(null);
          setEditor({ mode: "edit", draft: draftFromTarget(row) });
        },
      },
      toggleAction(row),
      {
        icon: "trash",
        label: "Delete",
        destructive: true,
        disabled: mutationPending,
        onClick: () => setDeleting(row.id),
      },
    ];
  };

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
      key: "source",
      header: "Source",
      width: "150px",
      render: (row) =>
        isDefault(row) ? (
          <Badge variant="neutral" className="shrink-0">
            <Badge.Text>Speakeasy default</Badge.Text>
          </Badge>
        ) : (
          <Badge variant="information" className="shrink-0">
            <Badge.Text>Custom</Badge.Text>
          </Badge>
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
          {formatRelativeTime(row.updatedAt ?? null) ?? "—"}
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

  const submitDraft = (body: UpsertAiScanTargetRequestBody): void => {
    setEditorError(null);
    save.mutate({ request: { upsertAiScanTargetRequestBody: body } });
  };

  if (list.isLoading) {
    return <Skeleton className="h-48 w-full" />;
  }
  if (list.error) {
    return (
      <Text muted className="py-8 text-center">
        Failed to load the scan targets: {list.error.message}
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
        {search.trim() ? " matching filter" : ""}. List version{" "}
        {list.data?.listVersion ?? 0} is what agents echo as{" "}
        <span className="font-mono">target_list_version</span> once they receive
        this list.
      </Text>

      <AiScanTargetEditorSheet
        open={editor !== null}
        mode={editor?.mode ?? "create"}
        initialDraft={editor?.draft ?? emptyDraft()}
        pending={save.isPending}
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
          remove.mutate({
            request: { deleteAiScanTargetRequestBody: { id: targetId } },
          });
        }}
      />
    </div>
  );
}
