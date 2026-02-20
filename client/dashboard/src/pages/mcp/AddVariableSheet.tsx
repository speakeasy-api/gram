import { Label } from "@/components/ui/label";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/popover";
import {
  Sheet,
  SheetContent,
  SheetFooter,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/sheet";
import { Button } from "@speakeasy-api/moonshine";
import { ChevronDown, Plus, X } from "lucide-react";
import { useCallback, useState } from "react";
import { EnvVarState } from "./environmentVariableUtils";

interface Environment {
  id: string;
  slug: string;
  name: string;
}

export interface VariableEntry {
  key: string;
  value: string;
  state: EnvVarState;
}

interface AddVariableSheetProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  attachedEnvironment: Environment | null;
  availableEnvVarsFromAttached: string[];
  onAddVariables: (entries: VariableEntry[]) => void;
  onLoadFromEnvironment: (varKey: string) => void;
}

export function AddVariableSheet({
  open,
  onOpenChange,
  attachedEnvironment,
  availableEnvVarsFromAttached,
  onAddVariables,
  onLoadFromEnvironment,
}: AddVariableSheetProps) {
  const emptyEntry = { key: "", value: "" };
  const [entries, setEntries] = useState([{ ...emptyEntry }]);

  const resetForm = useCallback(() => {
    setEntries([{ ...emptyEntry }]);
  }, []);

  const handleSave = () => {
    const validEntries = entries
      .filter((e) => e.key.trim())
      .map((e) => ({
        key: e.key.toUpperCase().replace(/\s+/g, "_"),
        value: e.value,
        state: "system" as EnvVarState,
      }));
    if (validEntries.length === 0) return;

    onAddVariables(validEntries);
    resetForm();
    onOpenChange(false);
  };

  const updateEntry = (index: number, field: "key" | "value", val: string) => {
    setEntries((prev) =>
      prev.map((e, i) =>
        i === index
          ? { ...e, [field]: field === "key" ? val.toUpperCase() : val }
          : e,
      ),
    );
  };

  const addEntry = () => {
    setEntries((prev) => [...prev, { ...emptyEntry }]);
  };

  const removeEntry = (index: number) => {
    setEntries((prev) =>
      prev.length <= 1 ? prev : prev.filter((_, i) => i !== index),
    );
  };

  const hasValidEntry = entries.some((e) => e.key.trim());

  return (
    <Sheet
      open={open}
      onOpenChange={(isOpen) => {
        if (!isOpen) resetForm();
        onOpenChange(isOpen);
      }}
    >
      <SheetContent
        side="right"
        className="w-[500px] sm:max-w-[500px] flex flex-col"
      >
        <SheetHeader className="px-6 pt-6 pb-0">
          <SheetTitle className="text-lg font-semibold">
            Add Environment Variable
          </SheetTitle>
        </SheetHeader>

        <div className="flex-1 overflow-y-auto px-6 py-6 space-y-6">
          {/* Load from environment section */}
          {attachedEnvironment && availableEnvVarsFromAttached.length > 0 ? (
            <div className="pb-4 border-b">
              <Label className="text-xs text-muted-foreground mb-2 block">
                Load from {attachedEnvironment.name}
              </Label>
              <Popover>
                <PopoverTrigger asChild>
                  <button className="w-full h-10 px-3 rounded-md border border-input bg-background text-sm flex items-center justify-between hover:bg-accent transition-colors">
                    <span className="text-muted-foreground">
                      Select a variable to add...
                    </span>
                    <ChevronDown className="h-4 w-4 text-muted-foreground" />
                  </button>
                </PopoverTrigger>
                <PopoverContent
                  align="start"
                  className="w-[450px] p-1 max-h-[300px] overflow-y-auto"
                >
                  {availableEnvVarsFromAttached.map((varName) => (
                    <div
                      key={varName}
                      className="px-3 py-2 text-sm rounded-sm cursor-pointer hover:bg-accent flex items-center gap-2"
                      onClick={() => onLoadFromEnvironment(varName)}
                    >
                      <div className="font-mono">{varName}</div>
                    </div>
                  ))}
                </PopoverContent>
              </Popover>
              <p className="text-xs text-muted-foreground mt-2">
                Variables from the attached environment that aren't already
                added
              </p>
            </div>
          ) : attachedEnvironment &&
            availableEnvVarsFromAttached.length === 0 ? (
            <div className="pb-4 border-b">
              <p className="text-xs text-muted-foreground">
                All variables from {attachedEnvironment.name} are already added
              </p>
            </div>
          ) : null}

          {/* Manual entry section */}
          <div>
            <Label className="text-xs text-muted-foreground mb-2 block">
              {attachedEnvironment && availableEnvVarsFromAttached.length > 0
                ? "Or create a new variable"
                : "Create a new variable"}
            </Label>
          </div>

          {entries.map((entry, index) => (
            <div key={index} className="flex gap-4 items-end">
              <div className="flex-1">
                {index === 0 && (
                  <Label className="text-xs text-muted-foreground mb-1.5 block">
                    Key
                  </Label>
                )}
                <input
                  type="text"
                  value={entry.key}
                  onChange={(e) => updateEntry(index, "key", e.target.value)}
                  placeholder="CLIENT_KEY..."
                  className="w-full h-10 px-3 rounded-md border border-input bg-background text-sm font-mono placeholder:text-muted-foreground focus:outline-none focus:ring-2 focus:ring-ring"
                />
              </div>
              <div className="flex-1">
                {index === 0 && (
                  <Label className="text-xs text-muted-foreground mb-1.5 block">
                    Value
                  </Label>
                )}
                <input
                  type="password"
                  value={entry.value}
                  onChange={(e) => updateEntry(index, "value", e.target.value)}
                  placeholder=""
                  className="w-full h-10 px-3 rounded-md border border-input bg-background text-sm font-mono placeholder:text-muted-foreground focus:outline-none focus:ring-2 focus:ring-ring"
                />
              </div>
              {entries.length > 1 && (
                <button
                  type="button"
                  onClick={() => removeEntry(index)}
                  className="h-10 px-1 text-muted-foreground hover:text-foreground transition-colors"
                >
                  <X className="h-4 w-4" />
                </button>
              )}
            </div>
          ))}

          {/* Add Another button */}
          <button
            type="button"
            onClick={addEntry}
            className="flex items-center gap-2 text-sm text-muted-foreground hover:text-foreground transition-colors"
          >
            <Plus className="h-4 w-4" />
            Add Another
          </button>
        </div>

        <SheetFooter className="px-6 py-4 border-t flex-row justify-between items-center">
          <button className="flex items-center gap-2 text-sm text-muted-foreground hover:text-foreground transition-colors">
            <svg
              className="h-4 w-4"
              fill="none"
              stroke="currentColor"
              viewBox="0 0 24 24"
              strokeWidth="2"
            >
              <path d="M4 16v1a3 3 0 003 3h10a3 3 0 003-3v-1m-4-8l-4-4m0 0L8 8m4-4v12" />
            </svg>
            Import .env
          </button>
          <span className="text-xs text-muted-foreground">
            or paste .env contents in Key input
          </span>
          <Button onClick={handleSave} disabled={!hasValidEntry}>
            Save
          </Button>
        </SheetFooter>
      </SheetContent>
    </Sheet>
  );
}
