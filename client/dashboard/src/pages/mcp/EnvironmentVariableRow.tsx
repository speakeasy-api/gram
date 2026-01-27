import { SimpleTooltip } from "@/components/ui/tooltip";
import { cn } from "@/lib/utils";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
} from "@/components/ui/select";
import { Eye, EyeOff, Pencil, Trash2 } from "lucide-react";
import { useState } from "react";
import {
  EnvironmentVariable,
  EnvVarState,
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
  onStateChange: (id: string, state: EnvVarState) => void;
  onValueChange: (id: string, value: string) => void;
  onDelete: (id: string) => void;
  onEditHeaderName: (id: string) => void;
  onHeaderDisplayNameChange: (id: string, value: string) => void;
  onHeaderBlur: () => void;
}

const MODE_OPTIONS: Array<{
  value: EnvVarState;
  label: string;
  description: string;
}> = [
  {
    value: "system",
    label: "System",
    description: "Value is stored securely and injected by the system",
  },
  {
    value: "user-provided",
    label: "User",
    description: "User must provide this value when connecting",
  },
  {
    value: "omitted",
    label: "Omit",
    description: "Variable is not included in the configuration",
  },
];

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
  onStateChange,
  onValueChange,
  onDelete,
  onEditHeaderName,
  onHeaderDisplayNameChange,
  onHeaderBlur,
}: EnvironmentVariableRowProps) {
  const [isPasswordVisible, setIsPasswordVisible] = useState(false);
  const isEditingHeader = editingHeaderId === envVar.id;
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
  // Check if there's a value - use editing state if actively editing, otherwise saved state
  const savedHasValue = environmentHasValue(envVar, selectedEnvironmentView);
  const isActivelyEditing = editingState.has(envVar.id);
  const editingHasValue =
    isActivelyEditing && !!editingState.get(envVar.id)!.value;
  // When actively editing, use editing value to determine indicator; otherwise use saved
  const hasValue = isActivelyEditing ? editingHasValue : savedHasValue;

  const isDisabled =
    selectedEnvironmentView !==
    (mcpAttachedEnvironmentSlug || defaultEnvironmentSlug || "default");

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
          {envVar.state === "omitted" ? (
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
            tabIndex={-1}
            onClick={() => onDelete(envVar.id)}
            className="absolute inset-0 opacity-0 group-hover:opacity-100 transition-opacity flex items-center justify-center text-muted-foreground hover:text-destructive"
          >
            <Trash2 className="h-4 w-4" />
          </button>
        )}
      </div>

      {/* Variable Info */}
      <div className="min-w-0">
        {isEditingHeader ? (
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
                  tabIndex={-1}
                  onClick={() => onEditHeaderName(envVar.id)}
                  className="flex items-center justify-center text-muted-foreground hover:text-foreground transition-colors"
                >
                  <Pencil className="h-3.5 w-3.5" />
                </button>
              </SimpleTooltip>
            ) : (
              <button
                tabIndex={-1}
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

      {/* Combined Mode + Value Input */}
      <div className="flex items-center">
        <div
          className={cn(
            "flex items-center h-9 rounded-md border border-input bg-background overflow-hidden",
            "focus-within:ring-2 focus-within:ring-ring",
          )}
        >
          {/* Mode Dropdown */}
          <Select
            value={envVar.state}
            onValueChange={(value) =>
              onStateChange(envVar.id, value as EnvVarState)
            }
            disabled={isDisabled}
          >
            <SelectTrigger
              tabIndex={-1}
              className="h-full border-0 border-r rounded-none bg-muted/50 focus:ring-0 focus-visible:ring-0 shadow-none w-[90px] text-xs uppercase font-mono gap-0.5"
            >
              <span>
                {MODE_OPTIONS.find((o) => o.value === envVar.state)?.label}
              </span>
            </SelectTrigger>
            <SelectContent>
              {MODE_OPTIONS.map((option) => (
                <SelectItem
                  key={option.value}
                  value={option.value}
                  textValue={option.label}
                >
                  <div className="flex flex-col gap-0.5 py-1">
                    <span className="font-medium">{option.label}</span>
                    <span className="text-xs text-muted-foreground">
                      {option.description}
                    </span>
                  </div>
                </SelectItem>
              ))}
            </SelectContent>
          </Select>

          {/* Value Input or Status Text */}
          <div className="w-48 h-full">
            {envVar.state === "user-provided" ? (
              <div className="h-full flex items-center px-3 text-xs text-muted-foreground font-mono">
                Set at runtime
              </div>
            ) : envVar.state === "omitted" ? (
              <div className="h-full flex items-center px-3 text-xs text-muted-foreground font-mono">
                Not included
              </div>
            ) : (
              <div className="relative h-full">
                <input
                  type={isPasswordVisible ? "text" : "password"}
                  value={editingValue}
                  onChange={(e) => onValueChange(envVar.id, e.target.value)}
                  placeholder="Enter value..."
                  className="w-full h-full px-3 pr-9 bg-transparent text-sm font-mono placeholder:text-muted-foreground focus:outline-none"
                />
                <button
                  type="button"
                  tabIndex={-1}
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
    </div>
  );
}
