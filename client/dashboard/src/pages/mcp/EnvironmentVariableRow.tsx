import { SimpleTooltip } from "@/components/ui/tooltip";
import { cn } from "@/lib/utils";
import { Button } from "@speakeasy-api/moonshine";
import { AlertTriangle, Eye, EyeOff, Pencil, Trash2 } from "lucide-react";
import { useState } from "react";
import {
  EnvironmentVariable,
  environmentHasValue,
  getEditingValue,
  getHeaderDisplayName,
  hasHeaderOverride,
} from "./environmentVariableUtils";

interface EnvironmentVariableRowProps {
  envVar: EnvironmentVariable;
  index: number;
  totalCount: number;
  selectedEnvironmentView: string;
  mcpAttachedEnvironmentSlug: string | null;
  defaultEnvironmentSlug: string;
  environmentConfigs: Array<{
    variableName: string;
    providedBy: string;
    headerDisplayName?: string;
  }>;
  editingState: Map<string, { value: string; headerDisplayName?: string }>;
  editingHeaderId: string | null;
  hasUnsavedChanges: boolean;
  onToggleState: (id: string) => void;
  onValueChange: (id: string, value: string) => void;
  onDelete: (id: string) => void;
  onEditHeaderName: (id: string) => void;
  onHeaderDisplayNameChange: (id: string, value: string) => void;
  onHeaderBlur: () => void;
}

export function EnvironmentVariableRow({
  envVar,
  index,
  totalCount,
  selectedEnvironmentView,
  mcpAttachedEnvironmentSlug,
  defaultEnvironmentSlug,
  environmentConfigs,
  editingState,
  editingHeaderId,
  hasUnsavedChanges,
  onToggleState,
  onValueChange,
  onDelete,
  onEditHeaderName,
  onHeaderDisplayNameChange,
  onHeaderBlur,
}: EnvironmentVariableRowProps) {
  const [isPasswordVisible, setIsPasswordVisible] = useState(false);
  const isEditing = editingHeaderId === envVar.id;
  const headerName = getHeaderDisplayName(
    envVar,
    environmentConfigs,
    editingState,
  );

  const hasOverride = hasHeaderOverride(envVar, environmentConfigs);
  // Check if there's an unsaved header name edit
  const hasUnsavedHeaderEdit =
    editingState.has(envVar.id) &&
    editingState.get(envVar.id)!.headerDisplayName !== undefined;
  const showHeaderName = hasOverride || hasUnsavedHeaderEdit;

  const editingValue = getEditingValue(
    envVar,
    editingState,
    selectedEnvironmentView,
  );
  // Check if there's a value - either from saved state or current editing
  const savedHasValue = environmentHasValue(envVar, selectedEnvironmentView);
  const editingHasValue = editingState.has(envVar.id) && !!editingState.get(envVar.id)!.value;
  const hasValue = savedHasValue || editingHasValue;
  const hasEntry = environmentConfigs.some(
    (e) => e.variableName === envVar.key,
  );

  return (
    <div
      className={cn(
        "group grid grid-cols-[auto_1fr_auto] gap-4 items-center px-4 py-4 transition-colors relative",
        index !== totalCount - 1 && "border-b",
        hasUnsavedChanges && "bg-amber-50/50 dark:bg-amber-950/20",
      )}
    >
      {/* Unsaved changes indicator */}
      {hasUnsavedChanges && (
        <div className="absolute left-0 top-0 bottom-0 w-0.5 bg-amber-500" />
      )}
      {/* Status indicator / Delete button */}
      <div className="relative flex items-center">
        {/* Status indicator - visible by default, hidden on hover for non-required */}
        <div
          className={cn(
            !envVar.isRequired && "group-hover:opacity-0 transition-opacity",
          )}
        >
          {/* Show unmapped indicator for required vars without environment entry */}
          {envVar.isRequired && !hasEntry ? (
            <SimpleTooltip tooltip="Not configured - save to create configuration">
              <AlertTriangle className="w-4 h-4 text-yellow-600" />
            </SimpleTooltip>
          ) : envVar.state === "omitted" ? (
            <div className="w-2 h-2 rounded-full bg-muted-foreground/30" />
          ) : hasValue ? (
            <div className="w-2 h-2 rounded-full bg-green-500" />
          ) : envVar.isRequired ? (
            <div className="w-2 h-2 rounded-full bg-yellow-500" />
          ) : (
            <div className="w-2 h-2 rounded-full bg-muted-foreground/30" />
          )}
        </div>

        {/* Delete button - hidden by default, visible on hover for non-required */}
        {!envVar.isRequired && (
          <button
            onClick={() => onDelete(envVar.id)}
            className="absolute inset-0 opacity-0 group-hover:opacity-100 transition-opacity flex items-center justify-center text-muted-foreground hover:text-destructive"
          >
            <Trash2 className="h-4 w-4" />
          </button>
        )}
      </div>

      {/* Variable Info */}
      <div className="min-w-0">
        {isEditing ? (
          <input
            type="text"
            value={headerName}
            onChange={(e) =>
              onHeaderDisplayNameChange(envVar.id, e.target.value)
            }
            onBlur={onHeaderBlur}
            onKeyDown={(e) => {
              if (e.key === "Enter" || e.key === "Escape") {
                onHeaderBlur();
              }
            }}
            placeholder={`Display name for ${envVar.key}`}
            autoFocus
            className="w-full h-5 px-1.5 py-0 rounded border border-input bg-background text-sm font-mono placeholder:text-muted-foreground focus:outline-none focus:ring-2 focus:ring-ring"
          />
        ) : (
          <div className="w-full h-6 flex items-center gap-2 group/header-edit">
            <div
              className={cn(
                "font-medium text-sm truncate",
                !showHeaderName && "font-mono",
                envVar.state === "omitted" && "text-muted-foreground/50",
              )}
            >
              {showHeaderName && headerName ? headerName : envVar.key}
            </div>
            {showHeaderName && headerName ? (
              <SimpleTooltip tooltip={`Variable name: ${envVar.key}`}>
                <button
                  onClick={() => onEditHeaderName(envVar.id)}
                  className="flex items-center justify-center text-muted-foreground hover:text-foreground transition-colors"
                >
                  <Pencil className="h-3.5 w-3.5" />
                </button>
              </SimpleTooltip>
            ) : (
              <button
                onClick={() => onEditHeaderName(envVar.id)}
                className="flex items-center justify-center text-muted-foreground hover:text-foreground opacity-0 pointer-events-none group-hover/header-edit:opacity-100 group-hover/header-edit:pointer-events-auto transition-opacity"
              >
                <Pencil className="h-3.5 w-3.5" />
              </button>
            )}
          </div>
        )}
        {envVar.description && (
          <div
            className={cn(
              "text-xs text-muted-foreground mt-0.5 truncate",
              envVar.state === "omitted" && "opacity-50",
            )}
          >
            {envVar.description}
          </div>
        )}
      </div>

      {/* Right side: State Button + Value */}
      <div className="flex items-center gap-4">
        {/* State cycle button */}
        <Button
          size="xs"
          variant="secondary"
          onClick={() => onToggleState(envVar.id)}
          disabled={
            selectedEnvironmentView !==
            (mcpAttachedEnvironmentSlug || defaultEnvironmentSlug || "default")
          }
        >
          <Button.Text>
            {envVar.state === "user-provided"
              ? "User Provided"
              : envVar.state === "system"
                ? "System"
                : "Omitted"}
          </Button.Text>
        </Button>

        {/* Value Input or status badge */}
        <div className="w-56">
          {envVar.state === "user-provided" ? (
            <div className="h-9 flex items-center px-3 rounded-md bg-muted text-xs text-muted-foreground font-mono">
              Set at runtime
            </div>
          ) : envVar.state === "omitted" ? (
            <div className="h-9 flex items-center px-3 rounded-md bg-muted text-xs text-muted-foreground font-mono">
              Not included
            </div>
          ) : (
            <div className="relative">
              <input
                type={isPasswordVisible ? "text" : "password"}
                value={editingValue}
                onChange={(e) => onValueChange(envVar.id, e.target.value)}
                placeholder="Enter value..."
                className="w-full h-9 px-3 pr-10 rounded-md border border-input bg-background text-sm font-mono placeholder:text-muted-foreground focus:outline-none focus:ring-2 focus:ring-ring"
              />
              <button
                type="button"
                onClick={() => setIsPasswordVisible(!isPasswordVisible)}
                className="absolute right-2 top-1/2 -translate-y-1/2 text-muted-foreground hover:text-foreground transition-colors"
              >
                {isPasswordVisible ? (
                  <EyeOff className="h-4 w-4" />
                ) : (
                  <Eye className="h-4 w-4" />
                )}
              </button>
            </div>
          )}
        </div>
      </div>
    </div>
  );
}
