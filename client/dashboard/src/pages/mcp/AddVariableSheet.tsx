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
    setEntries([{ key: "", value: "" }]);
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
        className="flex w-[500px] flex-col sm:max-w-[500px]"
      >
        <SheetHeader className="px-6 pt-6 pb-0">
          <SheetTitle className="text-lg font-semibold">
            Add Environment Variable
          </SheetTitle>
        </SheetHeader>

        <div className="flex-1 space-y-6 overflow-y-auto px-6 py-6">
          {/* Load from environment section */}
          {attachedEnvironment && availableEnvVarsFromAttached.length > 0 ? (
            <div className="border-b pb-4">
              <Label className="text-muted-foreground mb-2 block text-xs">
                Load from {attachedEnvironment.name}
              </Label>
              <Popover>
                <PopoverTrigger asChild>
                  <button className="border-input bg-background hover:bg-accent flex h-10 w-full items-center justify-between rounded-md border px-3 text-sm transition-colors">
                    <span className="text-muted-foreground">
                      Select a variable to add...
                    </span>
                    <ChevronDown className="text-muted-foreground h-4 w-4" />
                  </button>
                </PopoverTrigger>
                <PopoverContent
                  align="start"
                  className="max-h-[300px] w-[450px] overflow-y-auto p-1"
                >
                  {availableEnvVarsFromAttached.map((varName) => (
                    <div
                      key={varName}
                      className="hover:bg-accent flex cursor-pointer items-center gap-2 rounded-sm px-3 py-2 text-sm"
                      onClick={() => onLoadFromEnvironment(varName)}
                    >
                      <div className="font-mono">{varName}</div>
                    </div>
                  ))}
                </PopoverContent>
              </Popover>
              <p className="text-muted-foreground mt-2 text-xs">
                Variables from the attached environment that aren't already
                added
              </p>
            </div>
          ) : attachedEnvironment &&
            availableEnvVarsFromAttached.length === 0 ? (
            <div className="border-b pb-4">
              <p className="text-muted-foreground text-xs">
                All variables from {attachedEnvironment.name} are already added
              </p>
            </div>
          ) : null}

          {/* Manual entry section */}
          <div>
            <Label className="text-muted-foreground mb-2 block text-xs">
              {attachedEnvironment && availableEnvVarsFromAttached.length > 0
                ? "Or create a new variable"
                : "Create a new variable"}
            </Label>
          </div>

          {entries.map((entry, index) => (
            <div key={index} className="flex items-end gap-4">
              <div className="flex-1">
                {index === 0 && (
                  <Label className="text-muted-foreground mb-1.5 block text-xs">
                    Key
                  </Label>
                )}
                <input
                  type="text"
                  value={entry.key}
                  onChange={(e) => updateEntry(index, "key", e.target.value)}
                  placeholder="CLIENT_KEY..."
                  className="border-input bg-background placeholder:text-muted-foreground focus:ring-ring h-10 w-full rounded-md border px-3 font-mono text-sm focus:ring-2 focus:outline-none"
                />
              </div>
              <div className="flex-1">
                {index === 0 && (
                  <Label className="text-muted-foreground mb-1.5 block text-xs">
                    Value
                  </Label>
                )}
                <input
                  type="password"
                  value={entry.value}
                  onChange={(e) => updateEntry(index, "value", e.target.value)}
                  placeholder=""
                  className="border-input bg-background placeholder:text-muted-foreground focus:ring-ring h-10 w-full rounded-md border px-3 font-mono text-sm focus:ring-2 focus:outline-none"
                />
              </div>
              {entries.length > 1 && (
                <button
                  type="button"
                  onClick={() => removeEntry(index)}
                  className="text-muted-foreground hover:text-foreground h-10 px-1 transition-colors"
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
            className="text-muted-foreground hover:text-foreground flex items-center gap-2 text-sm transition-colors"
          >
            <Plus className="h-4 w-4" />
            Add Another
          </button>
        </div>

        <SheetFooter className="flex-row items-center justify-between border-t px-6 py-4">
          <button className="text-muted-foreground hover:text-foreground flex items-center gap-2 text-sm transition-colors">
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
          <span className="text-muted-foreground text-xs">
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
