import { Badge } from "@/components/ui/badge";
import { Dialog } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import { TextArea } from "@/components/ui/textarea";
import { Tool } from "@/lib/toolTypes";
import { Button } from "@speakeasy-api/moonshine";
import { FileCode, PencilRuler, SquareFunction } from "lucide-react";
import { useEffect, useRef, useState } from "react";

function getToolIcon(tool: Tool) {
  if (tool.type === "http") return FileCode;
  if (tool.type === "function") return SquareFunction;
  return PencilRuler;
}

function getToolSource(
  tool: Tool,
  documentIdToName?: Record<string, string>,
  functionIdToName?: Record<string, string>,
): string {
  if (tool.type === "http") {
    if (tool.packageName) return tool.packageName;
    if (tool.openapiv3DocumentId && documentIdToName) {
      return documentIdToName[tool.openapiv3DocumentId] || "OpenAPI";
    }
    if (tool.deploymentId) return tool.deploymentId;
    return "Custom";
  } else if (tool.type === "function") {
    if (tool.functionId && functionIdToName) {
      return functionIdToName[tool.functionId] || "Functions";
    }
    return "Functions";
  }
  return "Unknown";
}

function getToolTypeLabel(tool: Tool): string {
  if (tool.type === "http") return "HTTP";
  if (tool.type === "function") return "Function";
  if (tool.type === "prompt") return "Prompt";
  return "Unknown";
}

/** Get the effective annotation value: variation override > base annotation > undefined */
function getAnnotationValue(
  tool: Tool,
  field: "readOnlyHint" | "destructiveHint" | "idempotentHint" | "openWorldHint",
): boolean | undefined {
  if (tool.type === "prompt" || tool.type === "external-mcp") return undefined;
  const variationVal = tool.variation?.[field];
  if (variationVal !== undefined) return variationVal;
  return tool.annotations?.[field];
}

export type ToolUpdatePayload = {
  name: string;
  description: string;
  readOnlyHint?: boolean;
  destructiveHint?: boolean;
  idempotentHint?: boolean;
  openWorldHint?: boolean;
};

interface EditToolDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  tool: Tool | null;
  documentIdToName?: Record<string, string>;
  functionIdToName?: Record<string, string>;
  onSave: (updates: ToolUpdatePayload) => void;
  onRemove: () => void;
}

export function EditToolDialog({
  open,
  onOpenChange,
  tool,
  documentIdToName,
  functionIdToName,
  onSave,
  onRemove,
}: EditToolDialogProps) {
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [readOnlyHint, setReadOnlyHint] = useState<boolean | undefined>();
  const [destructiveHint, setDestructiveHint] = useState<boolean | undefined>();
  const [idempotentHint, setIdempotentHint] = useState<boolean | undefined>();
  const [openWorldHint, setOpenWorldHint] = useState<boolean | undefined>();
  const nameInputRef = useRef<HTMLInputElement>(null);

  const hasAnnotations = tool?.type === "http" || tool?.type === "function";

  // Reset form when tool changes
  useEffect(() => {
    if (tool) {
      setName(tool.name);
      setDescription(tool.description || "");
      setReadOnlyHint(getAnnotationValue(tool, "readOnlyHint"));
      setDestructiveHint(getAnnotationValue(tool, "destructiveHint"));
      setIdempotentHint(getAnnotationValue(tool, "idempotentHint"));
      setOpenWorldHint(getAnnotationValue(tool, "openWorldHint"));
    }
  }, [tool]);

  // Focus name input when dialog opens
  useEffect(() => {
    if (open && nameInputRef.current) {
      // Small delay to ensure dialog animation completes
      setTimeout(() => {
        nameInputRef.current?.focus();
      }, 50);
    }
  }, [open]);

  // Keyboard shortcuts
  useEffect(() => {
    if (!open) return;

    const handleKeyDown = (e: KeyboardEvent) => {
      // Cmd+Enter or Ctrl+Enter to save
      if ((e.metaKey || e.ctrlKey) && e.key === "Enter") {
        e.preventDefault();
        handleSave();
      }
    };

    window.addEventListener("keydown", handleKeyDown);
    return () => window.removeEventListener("keydown", handleKeyDown);
  }, [open, name, description, readOnlyHint, destructiveHint, idempotentHint, openWorldHint]);

  const handleSave = () => {
    if (!tool) return;
    onSave({
      name,
      description,
      ...(hasAnnotations && {
        readOnlyHint,
        destructiveHint,
        idempotentHint,
        openWorldHint,
      }),
    });
    onOpenChange(false);
  };

  const handleRemove = () => {
    onRemove();
    onOpenChange(false);
  };

  const handleClose = () => {
    onOpenChange(false);
  };

  if (!tool) return null;

  const ToolIcon = getToolIcon(tool);
  const source = getToolSource(tool, documentIdToName, functionIdToName);
  const typeLabel = getToolTypeLabel(tool);

  const origReadOnly = getAnnotationValue(tool, "readOnlyHint");
  const origDestructive = getAnnotationValue(tool, "destructiveHint");
  const origIdempotent = getAnnotationValue(tool, "idempotentHint");
  const origOpenWorld = getAnnotationValue(tool, "openWorldHint");

  const hasChanges =
    name !== tool.name ||
    description !== (tool.description || "") ||
    readOnlyHint !== origReadOnly ||
    destructiveHint !== origDestructive ||
    idempotentHint !== origIdempotent ||
    openWorldHint !== origOpenWorld;

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <Dialog.Content className="min-w-2xl max-w-3xl">
        <Dialog.Header>
          <Dialog.Title className="flex items-center gap-2">
            <ToolIcon className="size-4 text-muted-foreground" />
            <span>{source}</span>
            <Badge variant="secondary" className="text-xs">
              {typeLabel}
            </Badge>
          </Dialog.Title>
        </Dialog.Header>

        {/* Editable fields */}
        <div className="py-4 space-y-4">
          <div className="space-y-2">
            <Label htmlFor="tool-name" className="text-sm font-medium">
              Name
            </Label>
            <Input
              id="tool-name"
              ref={nameInputRef}
              value={name}
              onChange={(value) => setName(value)}
              placeholder="Tool name"
            />
          </div>

          <div className="space-y-2">
            <Label htmlFor="tool-description" className="text-sm font-medium">
              Description
            </Label>
            <TextArea
              id="tool-description"
              value={description}
              onChange={(value) => setDescription(value)}
              placeholder="Add a description for this tool..."
              rows={4}
            />
          </div>

          {/* Behavior Hints */}
          {hasAnnotations && (
            <div className="space-y-3">
              <Label className="text-sm font-medium">Behavior Hints</Label>
              <p className="text-xs text-muted-foreground">
                Override how this tool is presented to AI models.
              </p>
              <div className="space-y-2">
                <div className="flex items-center justify-between">
                  <div>
                    <p className="text-sm">Read-only</p>
                    <p className="text-xs text-muted-foreground">
                      Tool does not modify its environment
                    </p>
                  </div>
                  <Switch
                    checked={readOnlyHint ?? false}
                    onCheckedChange={setReadOnlyHint}
                    aria-label="Read-only hint"
                  />
                </div>
                <div className="flex items-center justify-between">
                  <div>
                    <p className="text-sm">Destructive</p>
                    <p className="text-xs text-muted-foreground">
                      Tool may perform destructive updates
                    </p>
                  </div>
                  <Switch
                    checked={destructiveHint ?? false}
                    onCheckedChange={setDestructiveHint}
                    aria-label="Destructive hint"
                  />
                </div>
                <div className="flex items-center justify-between">
                  <div>
                    <p className="text-sm">Idempotent</p>
                    <p className="text-xs text-muted-foreground">
                      Repeated calls with same arguments have no additional
                      effect
                    </p>
                  </div>
                  <Switch
                    checked={idempotentHint ?? false}
                    onCheckedChange={setIdempotentHint}
                    aria-label="Idempotent hint"
                  />
                </div>
                <div className="flex items-center justify-between">
                  <div>
                    <p className="text-sm">Open-world</p>
                    <p className="text-xs text-muted-foreground">
                      Tool interacts with external entities
                    </p>
                  </div>
                  <Switch
                    checked={openWorldHint ?? false}
                    onCheckedChange={setOpenWorldHint}
                    aria-label="Open-world hint"
                  />
                </div>
              </div>
            </div>
          )}
        </div>

        {/* Actions */}
        <div className="flex items-center justify-between pt-4 border-t">
          <Button variant="destructive-secondary" onClick={handleRemove}>
            Remove
          </Button>
          <div className="flex items-center gap-2">
            <Button variant="secondary" onClick={handleClose}>
              Cancel
            </Button>
            <Button onClick={handleSave} disabled={!hasChanges}>
              Save
              {hasChanges && (
                <span className="ml-2 text-xs opacity-60">⌘⏎</span>
              )}
            </Button>
          </div>
        </div>
      </Dialog.Content>
    </Dialog>
  );
}
