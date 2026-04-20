import { PrivateInput } from "@/components/ui/private-input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
} from "@/components/ui/select";
import { SimpleTooltip } from "@/components/ui/tooltip";
import { cn } from "@/lib/utils";
import { Pencil } from "lucide-react";
import {
  environmentHasValue,
  EnvironmentVariable,
  EnvVarState,
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
  onEditHeaderName: (id: string) => void;
  onHeaderDisplayNameChange: (id: string, value: string) => void;
  onHeaderBlur: () => void;
  readOnly?: boolean;
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
  environmentConfigs,
  editingState,
  editingHeaderId,
  hasUnsavedChanges,
  onStateChange,
  onValueChange,
  onEditHeaderName,
  onHeaderDisplayNameChange,
  onHeaderBlur,
  readOnly,
}: EnvironmentVariableRowProps) {
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

  const isDisabled = mcpAttachedEnvironmentSlug
    ? selectedEnvironmentView !== mcpAttachedEnvironmentSlug
    : false; // When no environment is attached, allow editing on any environment

  return (
    <div
      className={cn(
        "group relative grid grid-cols-[auto_1fr_auto] items-center gap-4 px-4 py-4 transition-colors",
        index !== totalCount - 1 && "border-b",
        hasUnsavedChanges && "bg-amber-50/50 dark:bg-amber-950/20",
      )}
    >
      {/* Unsaved changes indicator */}
      {hasUnsavedChanges && (
        <div className="absolute top-0 bottom-0 left-0 w-0.5 bg-amber-500" />
      )}
      {/* Status indicator / Delete button */}
      <div className="relative flex items-center">
        {/* Status indicator - visible by default, hidden on hover for non-required */}
        <div
          className={cn(
            !envVar.isRequired && "transition-opacity group-hover:opacity-0",
          )}
        >
          {envVar.state === "omitted" ? (
            <div className="bg-muted-foreground/30 h-2 w-2 rounded-full" />
          ) : hasValue ? (
            <div className="h-2 w-2 rounded-full bg-green-500" />
          ) : envVar.isRequired ? (
            <div className="h-2 w-2 rounded-full bg-yellow-500" />
          ) : (
            <div className="bg-muted-foreground/30 h-2 w-2 rounded-full" />
          )}
        </div>
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
            className="border-input bg-background placeholder:text-muted-foreground focus:ring-ring h-5 w-full rounded border px-1.5 py-0 font-mono text-sm focus:ring-2 focus:outline-none"
          />
        ) : (
          <div className="group/header-edit flex h-6 w-full items-center gap-2">
            <div
              className={cn(
                "truncate text-sm font-medium",
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
                  className="text-muted-foreground hover:text-foreground flex items-center justify-center transition-colors"
                >
                  <Pencil className="h-3.5 w-3.5" />
                </button>
              </SimpleTooltip>
            ) : (
              <button
                tabIndex={-1}
                onClick={() => onEditHeaderName(envVar.id)}
                className="text-muted-foreground hover:text-foreground pointer-events-none flex items-center justify-center opacity-0 transition-opacity group-hover/header-edit:pointer-events-auto group-hover/header-edit:opacity-100"
              >
                <Pencil className="h-3.5 w-3.5" />
              </button>
            )}
          </div>
        )}
        {envVar.description && (
          <div
            className={cn(
              "text-muted-foreground mt-0.5 truncate text-xs",
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
            "border-input bg-background flex h-9 items-center overflow-hidden rounded-md border",
            "focus-within:ring-ring focus-within:ring-2",
          )}
        >
          {/* Mode Dropdown */}
          <Select
            value={envVar.state}
            onValueChange={(value) =>
              onStateChange(envVar.id, value as EnvVarState)
            }
            disabled={isDisabled || readOnly}
          >
            <SelectTrigger
              tabIndex={-1}
              className="bg-muted/50 h-full w-[90px] gap-0.5 rounded-none border-0 border-r font-mono text-xs uppercase shadow-none focus:ring-0 focus-visible:ring-0"
            >
              <span>
                {MODE_OPTIONS.find((o) => o.value === envVar.state)?.label}
              </span>
            </SelectTrigger>
            <SelectContent>
              {MODE_OPTIONS.map((option) => {
                const isUserProvidedOption = option.value === "user-provided";
                const shouldDisable =
                  isUserProvidedOption && !envVar.isRequired;
                const description = shouldDisable
                  ? "Only available for required variables"
                  : option.description;

                return (
                  <SelectItem
                    key={option.value}
                    value={option.value}
                    textValue={option.label}
                    disabled={shouldDisable}
                  >
                    <div className="flex flex-col gap-0.5 py-1">
                      <span className="font-medium">{option.label}</span>
                      <span className="text-muted-foreground text-xs">
                        {description}
                      </span>
                    </div>
                  </SelectItem>
                );
              })}
            </SelectContent>
          </Select>

          {/* Value Input or Status Text */}
          <div className="flex h-full w-48 items-center">
            {envVar.state === "user-provided" ? (
              <div className="text-muted-foreground flex h-full items-center px-3 font-mono text-xs">
                Set at runtime
              </div>
            ) : envVar.state === "omitted" ? (
              <div className="text-muted-foreground flex h-full items-center px-3 font-mono text-xs">
                Not included
              </div>
            ) : (
              <PrivateInput
                value={editingValue}
                onChange={(value) => onValueChange(envVar.id, value)}
                placeholder="Enter value..."
                className={cn(
                  "placeholder:text-muted-foreground h-full w-full border-0 bg-transparent px-3 pr-9 font-mono text-sm shadow-none focus:outline-none focus-visible:ring-0",
                  readOnly && "cursor-not-allowed opacity-50",
                )}
                disabled={readOnly}
              />
            )}
          </div>
        </div>
      </div>
    </div>
  );
}
