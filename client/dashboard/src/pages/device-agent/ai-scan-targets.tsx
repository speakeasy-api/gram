import { AIToolIcon } from "@/components/ai-tools/AIToolIcon";
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
import { useDeleteAiScanTargetMutation } from "@gram/client/react-query/deleteAiScanTarget.js";
import {
  invalidateAllAiScanTargets,
  useAiScanTargets,
} from "@gram/client/react-query/aiScanTargets.js";
import { useUpsertAiScanTargetMutation } from "@gram/client/react-query/upsertAiScanTarget.js";
import { useQueryClient } from "@tanstack/react-query";
import { ChevronRight } from "lucide-react";
import { useMemo, useState } from "react";
import { cn } from "@/lib/utils";
import { toast } from "sonner";
import {
  AiScanTargetEditorSheet,
  DeleteAiScanTargetDialog,
  type EditorMode,
} from "./ai-scan-target-editor-sheet";
import {
  draftFromTarget,
  TARGET_CATEGORIES,
  draftToUpsertBody,
  emptyDraft,
  type Draft,
} from "./ai-scan-target-draft";

// Its own tab rather than a section under the configuration form: what agents
// probe for is a different question from how the fleet is configured.
export function AiScanTargetsSection(): JSX.Element {
  return (
    <Stack gap={6}>
      <div>
        <Heading variant="h4" className="mb-2">
          AI scan targets
        </Heading>
        <Text muted small>
          The AI tools this organization's device agents probe for. Built-in
          targets are supplied by Speakeasy and kept current — you can switch
          one off, but not edit it. Add your own targets alongside them. Changes
          reach agents on their next policy poll.
        </Text>
      </div>
      <Library />
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

/** Collapsible row per kind, matching Detection Rules' chevron-and-count. */
function CategoryHeader({
  label,
  description,
  count,
  expanded,
  onToggle,
}: {
  label: string;
  description: string;
  count: number;
  expanded: boolean;
  onToggle: () => void;
}): JSX.Element {
  return (
    <button
      type="button"
      onClick={onToggle}
      aria-expanded={expanded}
      className="hover:bg-muted/40 flex w-full items-center gap-3 px-4 py-3 text-left transition-colors"
    >
      <ChevronRight
        className={cn(
          "text-muted-foreground h-4 w-4 shrink-0 transition-transform",
          expanded && "rotate-90",
        )}
      />
      <div className="min-w-0 flex-1">
        <div className="text-sm font-medium">{label}</div>
        <div className="text-muted-foreground line-clamp-1 text-xs">
          {description}
        </div>
      </div>
      <Badge variant="neutral" className="shrink-0">
        <Badge.Text>{String(count)}</Badge.Text>
      </Badge>
    </button>
  );
}

/**
 * One half of the library — the organization's targets or Speakeasy's — with a
 * collapsible row per kind. Two levels because both questions get asked: which
 * side decides what you can do to a target, which kind is how you find one.
 */
function OriginSection({
  title,
  badge,
  emptyMessage,
  targetsByCategory,
  columns,
  expanded,
  defaultExpanded,
  onToggle,
  searching,
}: {
  title: string;
  // Carries the served library version, a fact about Speakeasy's half only.
  badge?: string;
  emptyMessage: string;
  targetsByCategory: Record<string, AiScanTarget[]>;
  columns: Column<AiScanTarget>[];
  expanded: Record<string, boolean>;
  // Custom opens, built-in does not: twenty built-ins would bury the short
  // list somebody came here to work on.
  defaultExpanded: boolean;
  onToggle: (key: string, fallback: boolean) => void;
  searching: boolean;
}): JSX.Element {
  const total = TARGET_CATEGORIES.reduce(
    (sum, category) => sum + (targetsByCategory[category.value]?.length ?? 0),
    0,
  );

  return (
    <div>
      <div className="mb-3 flex items-center gap-2">
        <span className="text-eyebrow">{title}</span>
        {badge === undefined ? null : (
          <Badge variant="neutral">
            <Badge.Text>{badge}</Badge.Text>
          </Badge>
        )}
      </div>
      {total === 0 ? (
        <div className="border-border bg-muted/20 border px-4 py-6">
          <Text muted small>
            {searching ? "No matching targets" : emptyMessage}
          </Text>
        </div>
      ) : (
        <div className="border-border divide-border divide-y border">
          {TARGET_CATEGORIES.map((category) => {
            const rows = targetsByCategory[category.value] ?? [];
            // An empty kind on this side is not worth a header.
            if (rows.length === 0) return null;
            const key = `${title}:${category.value}`;
            const isExpanded = expanded[key] ?? defaultExpanded;
            return (
              <div key={category.value}>
                <CategoryHeader
                  label={category.label}
                  description={category.description}
                  count={rows.length}
                  expanded={isExpanded}
                  onToggle={() => onToggle(key, defaultExpanded)}
                />
                {isExpanded ? (
                  <div className="border-border border-t">
                    <Table
                      columns={columns}
                      data={rows}
                      rowKey={(row) => row.id}
                      noResultsMessage={<Text>No targets</Text>}
                    />
                  </div>
                ) : null}
              </div>
            );
          })}
        </div>
      )}
    </div>
  );
}

function Library(): JSX.Element {
  const queryClient = useQueryClient();
  const list = useAiScanTargets(undefined, undefined, {
    throwOnError: false,
  });
  const [search, setSearch] = useState("");
  const [editor, setEditor] = useState<EditorState>(null);
  const [deleting, setDeleting] = useState<string | null>(null);
  const [pendingId, setPendingId] = useState<string | null>(null);
  const [editorError, setEditorError] = useState<string | null>(null);

  const invalidate = () => invalidateAllAiScanTargets(queryClient);

  const save = useUpsertAiScanTargetMutation({
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
  const toggle = useUpsertAiScanTargetMutation({
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
  const remove = useDeleteAiScanTargetMutation({
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
  const restore = useDeleteAiScanTargetMutation({
    onSuccess: async () => {
      toast.success("Built-in target restored.");
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

  // Independent rather than Detection Rules' single-open accordion; a key
  // absent from the map reads as its side's default.
  const [openCategories, setOpenCategories] = useState<Record<string, boolean>>(
    {},
  );
  const toggleCategory = (key: string, fallback: boolean): void =>
    setOpenCategories((open) => ({ ...open, [key]: !(open[key] ?? fallback) }));

  // Split by origin first, then by kind inside each half. See OriginSection
  // for why both levels are there.
  const grouped = useMemo(() => {
    const custom: Record<string, AiScanTarget[]> = {};
    const builtin: Record<string, AiScanTarget[]> = {};
    for (const target of targets) {
      const side = isDefault(target) ? builtin : custom;
      (side[target.category] ??= []).push(target);
    }
    return { custom, builtin };
  }, [targets]);

  const mutationPending =
    pendingId !== null ||
    save.isPending ||
    toggle.isPending ||
    remove.isPending ||
    restore.isPending;

  // A default toggles by customizing it under its own id, and back by
  // dropping that customization. Organization targets toggle in place.
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
      // View, not Edit: an organization only decides whether to probe.
      return [
        {
          icon: "eye",
          label: "View",
          disabled: false,
          onClick: () => {
            setEditorError(null);
            setEditor({ mode: "view", draft: draftFromTarget(row) });
          },
        },
        toggleAction(row),
      ];
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
        <div className="flex min-w-0 items-center gap-2">
          <AIToolIcon
            targetId={row.id}
            displayName={row.displayName}
            className="size-4 shrink-0"
          />
          <Text variant="body" className="truncate font-medium">
            {row.displayName}
          </Text>
        </div>
      ),
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
    <div className="space-y-6">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="flex flex-wrap items-center gap-3">
          <Input
            value={search}
            onChange={(value: string) => setSearch(value)}
            placeholder="Filter by name…"
            className="max-w-sm"
          />
        </div>
        <Button
          onClick={() => {
            setEditorError(null);
            setEditor({ mode: "create", draft: emptyDraft() });
          }}
        >
          Add target
        </Button>
      </div>
      {/* Custom first, so the Add target button plainly belongs to it. */}
      <OriginSection
        title="Custom"
        emptyMessage="No targets of your own yet. Add one to probe for a tool Speakeasy does not ship."
        targetsByCategory={grouped.custom}
        columns={columns}
        expanded={openCategories}
        defaultExpanded
        onToggle={toggleCategory}
        searching={search.trim() !== ""}
      />
      <OriginSection
        title="Built-in"
        badge={`Library version ${list.data?.listVersion ?? 0}`}
        emptyMessage="No built-in targets are being served."
        targetsByCategory={grouped.builtin}
        columns={columns}
        expanded={openCategories}
        defaultExpanded={false}
        onToggle={toggleCategory}
        searching={search.trim() !== ""}
      />

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
