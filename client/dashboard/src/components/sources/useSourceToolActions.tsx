import { TagsVariationEditor } from "@/components/tool-variation-tags-editor";
import { Button } from "@/components/ui/Button";
import { Dialog } from "@/components/ui/Dialog";
import { Icon } from "@/components/ui/Icon";
import { Input } from "@/components/ui/Input";
import type { Action } from "@/components/ui/MoreActions";
import { Stack } from "@/components/ui/Stack";
import { Text } from "@/components/ui/Text";
import { TextArea } from "@/components/ui/Textarea";
import type { ToolUpdateFields } from "@/hooks/useToolUpdate";
import { TOOL_NAME_REGEX } from "@/lib/constants";
import { useCallback, useMemo, useState } from "react";
import { toast } from "sonner";
import {
  AnnotationsFields,
  OriginalValueNote,
  type AnnotationHints,
} from "./SourceToolEditFields";
import type { SourceTool } from "./useSourceQueries";

type EditMode = "name" | "description" | "annotations" | "tags";

const DIALOG_TITLES: Record<EditMode, string> = {
  name: "Edit tool name",
  description: "Edit description",
  annotations: "Edit annotations",
  tags: "Edit tags",
};

function dialogDescription(mode: EditMode, toolName: string): string {
  switch (mode) {
    case "name":
      return `Update the name of tool '${toolName}'`;
    case "description":
      return `Update the description of tool '${toolName}'`;
    case "annotations":
      return `Override behavior hints for '${toolName}'`;
    case "tags":
      return `Override tags for '${toolName}'`;
  }
}

const EMPTY_HINTS: AnnotationHints = {
  title: "",
  readOnly: false,
  destructive: false,
  idempotent: false,
  openWorld: false,
};

// The variation wins over the tool's own annotations: it is the override
// being edited, and the base hints are only the starting point.
function annotationHintsFor(tool: SourceTool): AnnotationHints {
  return {
    title: tool.variation?.title ?? tool.annotations?.title ?? "",
    readOnly:
      tool.variation?.readOnlyHint ?? tool.annotations?.readOnlyHint ?? false,
    destructive:
      tool.variation?.destructiveHint ??
      tool.annotations?.destructiveHint ??
      false,
    idempotent:
      tool.variation?.idempotentHint ??
      tool.annotations?.idempotentHint ??
      false,
    openWorld:
      tool.variation?.openWorldHint ?? tool.annotations?.openWorldHint ?? false,
  };
}

export type SourceToolActionsProps = {
  onUpdate: (
    tool: SourceTool,
    updates: ToolUpdateFields,
  ) => void | Promise<void>;
  isUpdating?: boolean;
};

/**
 * The per-tool edit actions for a source's tool table, and the one dialog
 * they all open.
 *
 * Called once for the table rather than once per row: the design-system
 * Table renders cells and context menus through callbacks, so the dialog
 * keeps the tool it was opened for instead of each row keeping its own.
 */
export function useSourceToolActions({
  onUpdate,
  isUpdating,
}: SourceToolActionsProps): {
  actionsFor: (tool: SourceTool) => Action[];
  dialog: JSX.Element | null;
} {
  const [activeTool, setActiveTool] = useState<SourceTool | null>(null);
  const [editMode, setEditMode] = useState<EditMode>("name");
  const [editValue, setEditValue] = useState("");
  const [hints, setHints] = useState<AnnotationHints>(EMPTY_HINTS);
  const [tagsValue, setTagsValue] = useState<string[] | undefined>(undefined);
  const [error, setError] = useState<string | null>(null);

  // Memoized so TagsVariationEditor's downstream memoization isn't
  // invalidated by a fresh `[]` reference on every render.
  const baseTags = useMemo(() => activeTool?.tags ?? [], [activeTool?.tags]);

  // Stable, so the table's memoized columns can close over it: it only
  // touches state setters.
  const openEditDialog = useCallback((tool: SourceTool, mode: EditMode) => {
    setActiveTool(tool);
    setEditMode(mode);
    setError(null);
    switch (mode) {
      case "annotations":
        setHints(annotationHintsFor(tool));
        break;
      case "tags":
        setTagsValue(tool.variation?.tags);
        break;
      case "name":
        setEditValue(tool.name);
        break;
      case "description":
        setEditValue(tool.description);
        break;
    }
  }, []);

  const closeDialog = () => setActiveTool(null);

  const handleSave = async () => {
    if (!activeTool) return;
    if (editMode === "name" && !TOOL_NAME_REGEX.test(editValue)) {
      setError("Tool name may only contain letters, numbers, and underscores");
      return;
    }

    let updates: ToolUpdateFields;
    switch (editMode) {
      case "annotations":
        updates = {
          title: hints.title || undefined,
          readOnlyHint: hints.readOnly,
          destructiveHint: hints.destructive,
          idempotentHint: hints.idempotent,
          openWorldHint: hints.openWorld,
        };
        break;
      case "tags":
        // The tags key must always be present so the upsert form spread
        // overwrites any prior variation tags (an absent key signals no
        // override).
        updates = { tags: tagsValue };
        break;
      case "name":
        updates = { name: editValue };
        break;
      case "description":
        updates = { description: editValue };
        break;
    }

    try {
      await onUpdate(activeTool, updates);
      closeDialog();
    } catch (err) {
      // useToolUpdate toasts the failure; the inline message keeps it in view
      // while the dialog stays open.
      setError(err instanceof Error ? err.message : "Unknown error");
    }
  };

  const actionsFor = useCallback(
    (tool: SourceTool): Action[] => [
      {
        label: "Edit name",
        icon: "pencil",
        onClick: () => openEditDialog(tool, "name"),
      },
      {
        label: "Edit description",
        icon: "pencil",
        onClick: () => openEditDialog(tool, "description"),
      },
      {
        label: "Edit annotations",
        icon: "pencil",
        onClick: () => openEditDialog(tool, "annotations"),
      },
      {
        label: "Edit tags",
        icon: "pencil",
        onClick: () => openEditDialog(tool, "tags"),
      },
      {
        label: "Copy name",
        icon: "copy",
        separatorBefore: true,
        onClick: () => {
          void navigator.clipboard.writeText(tool.name).then(() => {
            toast.success("Tool name copied");
          });
        },
      },
    ],
    [openEditDialog],
  );

  const showOriginalName =
    !!activeTool?.variation?.name &&
    activeTool.variation.name !== activeTool.canonical?.name;
  const showOriginalDescription =
    !!activeTool?.variation?.description &&
    activeTool.variation.description !== activeTool.canonical?.description;

  const dialog = activeTool ? (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open) closeDialog();
      }}
    >
      <Dialog.Content>
        <Dialog.Header>
          <Dialog.Title>{DIALOG_TITLES[editMode]}</Dialog.Title>
          <Dialog.Description>
            {dialogDescription(editMode, activeTool.name)}
          </Dialog.Description>
        </Dialog.Header>
        <div className="space-y-4 py-4">
          {editMode === "annotations" && (
            <AnnotationsFields hints={hints} onChange={setHints} />
          )}
          {editMode === "tags" && (
            <TagsVariationEditor
              baseTags={baseTags}
              value={tagsValue}
              onChange={setTagsValue}
            />
          )}
          {editMode === "name" && (
            <Stack gap={2}>
              <Input
                value={editValue}
                onChange={setEditValue}
                placeholder="Tool name"
              />
              {showOriginalName && (
                <Stack direction="horizontal" gap={2} align="center">
                  <Icon
                    name="layers-2"
                    size="small"
                    className="text-muted-foreground/70"
                  />
                  <Text small muted>
                    Original name:
                  </Text>
                  <Text small muted>
                    {activeTool.canonical?.name}
                  </Text>
                </Stack>
              )}
            </Stack>
          )}
          {editMode === "description" && (
            <Stack gap={2}>
              <TextArea
                value={editValue}
                onChange={setEditValue}
                placeholder="Tool description"
                rows={3}
              />
              {showOriginalDescription && (
                <OriginalValueNote
                  label="Original Description"
                  value={activeTool.canonical?.description}
                />
              )}
            </Stack>
          )}
          {error && <p className="text-destructive text-sm">{error}</p>}
        </div>
        <Dialog.Footer>
          <Button
            variant="tertiary"
            onClick={closeDialog}
            disabled={isUpdating}
          >
            <Button.Text>Cancel</Button.Text>
          </Button>
          <Button onClick={() => void handleSave()} disabled={isUpdating}>
            <Button.Text>Save</Button.Text>
          </Button>
        </Dialog.Footer>
      </Dialog.Content>
    </Dialog>
  ) : null;

  return { actionsFor, dialog };
}
