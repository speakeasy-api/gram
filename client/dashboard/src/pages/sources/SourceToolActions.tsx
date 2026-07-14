import { TagsVariationEditor } from "@/components/tool-variation-tags-editor";
import { Dialog } from "@/components/ui/dialog";
import { Label } from "@/components/ui/label";
import { MoreActions } from "@/components/ui/more-actions";
import { Switch } from "@/components/ui/switch";
import { TextArea } from "@/components/ui/textarea";
import { Type } from "@/components/ui/type";
import { ToolUpdateFields } from "@/hooks/useToolUpdate";
import { TOOL_NAME_REGEX } from "@/lib/constants";
import { Tool } from "@/lib/toolTypes";
import { Stack } from "@/components/ui/stack";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Layers2 } from "lucide-react";
import { useMemo, useState } from "react";

type EditMode = "name" | "description" | "annotations" | "tags";
type SourceTool = Extract<Tool, { type: "http" | "function" }>;

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

export function SourceToolActions({
  tool,
  onUpdate,
  isUpdating,
}: {
  tool: SourceTool;
  onUpdate: (updates: ToolUpdateFields) => void | Promise<void>;
  isUpdating?: boolean;
}): JSX.Element {
  const [editDialogOpen, setEditDialogOpen] = useState(false);
  const [editMode, setEditMode] = useState<EditMode>("name");
  const [editValue, setEditValue] = useState("");
  const [error, setError] = useState<string | null>(null);

  const [annotTitle, setAnnotTitle] = useState("");
  const [annotReadOnly, setAnnotReadOnly] = useState(false);
  const [annotDestructive, setAnnotDestructive] = useState(false);
  const [annotIdempotent, setAnnotIdempotent] = useState(false);
  const [annotOpenWorld, setAnnotOpenWorld] = useState(false);

  const [tagsValue, setTagsValue] = useState<string[] | undefined>(undefined);

  // Memoize so TagsVariationEditor's downstream memoization isn't invalidated
  // by a fresh `[]` reference on every render.
  const baseTags = useMemo(() => tool.tags ?? [], [tool.tags]);

  const openEditDialog = (mode: EditMode) => {
    setEditMode(mode);
    setError(null);

    switch (mode) {
      case "annotations":
        setAnnotTitle(tool.variation?.title ?? tool.annotations?.title ?? "");
        setAnnotReadOnly(
          tool.variation?.readOnlyHint ??
            tool.annotations?.readOnlyHint ??
            false,
        );
        setAnnotDestructive(
          tool.variation?.destructiveHint ??
            tool.annotations?.destructiveHint ??
            false,
        );
        setAnnotIdempotent(
          tool.variation?.idempotentHint ??
            tool.annotations?.idempotentHint ??
            false,
        );
        setAnnotOpenWorld(
          tool.variation?.openWorldHint ??
            tool.annotations?.openWorldHint ??
            false,
        );
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

    setEditDialogOpen(true);
  };

  const handleSave = async () => {
    if (editMode === "name" && !TOOL_NAME_REGEX.test(editValue)) {
      setError("Tool name may only contain letters, numbers, and underscores");
      return;
    }

    let updates: ToolUpdateFields;
    switch (editMode) {
      case "annotations":
        updates = {
          title: annotTitle || undefined,
          readOnlyHint: annotReadOnly,
          destructiveHint: annotDestructive,
          idempotentHint: annotIdempotent,
          openWorldHint: annotOpenWorld,
        };
        break;
      case "tags":
        // tags key must always be present so the upsert form spread correctly
        // overwrites any prior variation tags (sending undefined drops the key
        // from the wire body, signalling no override).
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
      await onUpdate(updates);
      setEditDialogOpen(false);
    } catch (err) {
      // Toast is surfaced by useToolUpdate's onError; keep inline message for
      // dialog visibility.
      setError(err instanceof Error ? err.message : "Unknown error");
    }
  };

  const handleCopyName = async () => {
    await navigator.clipboard.writeText(tool.name);
  };

  const actions = [
    {
      label: "Edit name",
      onClick: () => openEditDialog("name"),
      icon: "pencil" as const,
    },
    {
      label: "Edit description",
      onClick: () => openEditDialog("description"),
      icon: "pencil" as const,
    },
    {
      label: "Edit annotations",
      onClick: () => openEditDialog("annotations"),
      icon: "pencil" as const,
    },
    {
      label: "Edit tags",
      onClick: () => openEditDialog("tags"),
      icon: "pencil" as const,
    },
    {
      label: "Copy name",
      onClick: handleCopyName,
      icon: "copy" as const,
    },
  ];

  return (
    <>
      <MoreActions actions={actions} />

      <Dialog open={editDialogOpen} onOpenChange={setEditDialogOpen}>
        <Dialog.Content>
          <Dialog.Header>
            <Dialog.Title>{DIALOG_TITLES[editMode]}</Dialog.Title>
            <Dialog.Description>
              {dialogDescription(editMode, tool.name)}
            </Dialog.Description>
          </Dialog.Header>
          <div className="space-y-4 py-4">
            {editMode === "annotations" && (
              <Stack gap={4}>
                <div className="space-y-2">
                  <Label className="text-sm font-medium">Title</Label>
                  <Input
                    value={annotTitle}
                    onChange={(e) => setAnnotTitle(e.target.value)}
                    placeholder="Display name override"
                  />
                </div>
                <div className="space-y-3">
                  <Label className="text-sm font-medium">Behavior Hints</Label>
                  <div className="space-y-2">
                    <AnnotationToggle
                      label="Read-only"
                      description="Tool does not modify its environment"
                      checked={annotReadOnly}
                      onCheckedChange={setAnnotReadOnly}
                    />
                    <AnnotationToggle
                      label="Destructive"
                      description="Tool may perform destructive updates"
                      checked={annotDestructive}
                      onCheckedChange={setAnnotDestructive}
                    />
                    <AnnotationToggle
                      label="Idempotent"
                      description="Repeated calls with same arguments have no additional effect"
                      checked={annotIdempotent}
                      onCheckedChange={setAnnotIdempotent}
                    />
                    <AnnotationToggle
                      label="Open-world"
                      description="Tool interacts with external entities"
                      checked={annotOpenWorld}
                      onCheckedChange={setAnnotOpenWorld}
                    />
                  </div>
                </div>
              </Stack>
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
                  onChange={(e) => setEditValue(e.target.value)}
                  placeholder="Tool name"
                />
                {tool.variation?.name &&
                  tool.variation?.name !== tool.canonical?.name && (
                    <Stack direction="horizontal" gap={2} align="center">
                      <Layers2 className="text-muted-foreground/70 size-4" />
                      <Type small muted>
                        Original name:
                      </Type>
                      <Type small muted>
                        {tool.canonical?.name}
                      </Type>
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
                {tool.variation?.description &&
                  tool.variation?.description !==
                    tool.canonical?.description && (
                    <Stack className="border-border/70 border p-2">
                      <Type small muted className="inline font-medium">
                        <Layers2 className="text-muted-foreground/70 inline size-4 align-text-bottom" />{" "}
                        Original Description
                      </Type>
                      <Type small muted>
                        {tool.canonical?.description}
                      </Type>
                    </Stack>
                  )}
              </Stack>
            )}
            {error && <p className="text-destructive text-sm">{error}</p>}
          </div>
          <Dialog.Footer>
            <Button
              variant="tertiary"
              onClick={() => setEditDialogOpen(false)}
              disabled={isUpdating}
            >
              Cancel
            </Button>
            <Button onClick={() => void handleSave()} disabled={isUpdating}>
              Save
            </Button>
          </Dialog.Footer>
        </Dialog.Content>
      </Dialog>
    </>
  );
}

function AnnotationToggle({
  label,
  description,
  checked,
  onCheckedChange,
}: {
  label: string;
  description: string;
  checked: boolean;
  onCheckedChange: (value: boolean) => void;
}) {
  return (
    <div className="flex items-center justify-between">
      <div>
        <p className="text-sm">{label}</p>
        <p className="text-muted-foreground text-xs">{description}</p>
      </div>
      <Switch
        checked={checked}
        onCheckedChange={onCheckedChange}
        aria-label={`${label} hint`}
      />
    </div>
  );
}
