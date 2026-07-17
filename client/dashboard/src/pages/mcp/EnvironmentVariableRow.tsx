import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
} from "@/components/ui/select";
import { SimpleTooltip } from "@/components/ui/tooltip";
import { type Action, MoreActions } from "@/components/ui/more-actions";
import { cn } from "@/lib/utils";
import { Badge, Button } from "@speakeasy-api/moonshine";
import { Eye, EyeOff, Pencil } from "lucide-react";
import { useEffect, useState } from "react";
import {
  environmentHasValue,
  EnvironmentVariable,
  EnvVarState,
  getHeaderDisplayName,
  getValueForEnvironment,
  hasEntryInEnvironment,
  hasHeaderOverride,
  isSecretInEnvironment,
} from "./environmentVariableUtils";

// Stands in for a value that is set but not on display, matching the
// environment page.
const MASK = "••••••••••••";

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
  editingState: Map<string, { headerDisplayName?: string }>;
  editingHeaderId: string | null;
  hasUnsavedChanges: boolean;
  onStateChange: (id: string, state: EnvVarState) => void;
  onEditValue: (envVar: EnvironmentVariable) => void;
  onDeleteValue: (envVar: EnvironmentVariable) => void;
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
  environmentConfigs,
  editingState,
  editingHeaderId,
  hasUnsavedChanges,
  onStateChange,
  onEditValue,
  onDeleteValue,
  onEditHeaderName,
  onHeaderDisplayNameChange,
  onHeaderBlur,
}: EnvironmentVariableRowProps): JSX.Element {
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

  // Values are saved by the dialog rather than typed into the row, so what the
  // row shows is always what the server last returned.
  const storedValue = getValueForEnvironment(envVar, selectedEnvironmentView);
  const hasValue = environmentHasValue(envVar, selectedEnvironmentView);

  const isDisabled = mcpAttachedEnvironmentSlug
    ? selectedEnvironmentView !== mcpAttachedEnvironmentSlug
    : false; // When no environment is attached, allow editing on any environment

  // A secret only ever loads as a redacted preview, so showing it would only
  // put a mask on screen. A non-secret value is stored in cleartext and meant
  // to be read back. A variable this environment has no entry for defaults to
  // secret, matching what the server will store, but it gets no badge: nothing
  // is stored yet to describe.
  const isSecret = isSecretInEnvironment(envVar, selectedEnvironmentView);
  const isStored = hasEntryInEnvironment(envVar, selectedEnvironmentView);
  const showSensitiveBadge = isSecret && envVar.state === "system" && isStored;

  // Values are masked until asked for, as on the environment page. Revealing a
  // secret shows its redacted preview, which is all the server ever returns;
  // revealing a non-secret shows the value itself.
  const [revealed, setRevealed] = useState(false);
  const canReveal = envVar.state === "system" && isStored;
  // Switching environments swaps the value under a mounted row, so a reveal
  // must not carry over to a different environment's value.
  useEffect(() => {
    setRevealed(false);
  }, [selectedEnvironmentView]);

  const { valueDisplay, isValueSet } = getValueDisplay(
    envVar.state,
    isStored,
    revealed,
    storedValue,
  );

  const actions: Action[] = [
    {
      label: isStored ? "Edit" : "Set value",
      onClick: () => onEditValue(envVar),
      icon: "pencil",
      disabled: isDisabled,
    },
  ];
  // A secret hands back only its redacted preview, so copying one would put a
  // useless string on the clipboard. Keep the row shape steady by showing the
  // action locked rather than dropping it.
  actions.push({
    label: "Copy to Clipboard",
    onClick: () => {
      void navigator.clipboard.writeText(storedValue);
    },
    icon: isSecret ? "lock" : "copy",
    disabled: isSecret || !isStored,
  });
  if (isStored) {
    actions.push({
      label: "Delete",
      onClick: () => onDeleteValue(envVar),
      icon: "trash",
      destructive: true,
      disabled: isDisabled,
    });
  }

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
            {showSensitiveBadge && (
              <Badge
                variant="neutral"
                size="sm"
                className="h-4 shrink-0 px-1 text-xs"
              >
                Sensitive
              </Badge>
            )}
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
            disabled={isDisabled}
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

          {/* Value display or status text */}
          <div className="flex h-full w-48 items-center gap-1 pr-1">
            <span
              className={cn(
                "text-muted-foreground flex-1 truncate px-3 font-mono text-xs",
                !isValueSet && "italic",
              )}
            >
              {valueDisplay}
            </span>
            {canReveal && (
              <Button
                variant="tertiary"
                size="sm"
                className="h-7 w-7 flex-shrink-0"
                onClick={() => setRevealed(!revealed)}
                aria-label={
                  revealed ? `Hide ${envVar.key}` : `View ${envVar.key}`
                }
              >
                <Button.LeftIcon>
                  {revealed ? (
                    <EyeOff className="h-3.5 w-3.5" />
                  ) : (
                    <Eye className="h-3.5 w-3.5" />
                  )}
                </Button.LeftIcon>
              </Button>
            )}
          </div>
        </div>

        {/* Kept in the layout for every state so switching modes doesn't
            shift the value input; only the system state shows the menu. */}
        <div
          className={cn(
            "ml-3 shrink-0",
            envVar.state !== "system" && "invisible",
          )}
        >
          <MoreActions actions={actions} />
        </div>
      </div>
    </div>
  );
}

// What the value cell reads for a variable. A stored value stays masked until
// it is revealed; the states that store nothing say so instead.
function getValueDisplay(
  state: EnvVarState,
  isStored: boolean,
  revealed: boolean,
  storedValue: string,
): { valueDisplay: string; isValueSet: boolean } {
  switch (state) {
    case "user-provided":
      return { valueDisplay: "Set at runtime", isValueSet: false };
    case "omitted":
      return { valueDisplay: "Not included", isValueSet: false };
    case "system":
      if (!isStored) return { valueDisplay: "Not set", isValueSet: false };
      return {
        valueDisplay: revealed ? storedValue : MASK,
        isValueSet: true,
      };
  }
}
