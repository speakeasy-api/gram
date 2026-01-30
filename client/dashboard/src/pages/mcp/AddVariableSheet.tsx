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
import { ChevronDown, Plus } from "lucide-react";
import { useState } from "react";
import { EnvVarState } from "./environmentVariableUtils";

interface Environment {
  id: string;
  slug: string;
  name: string;
}

interface AddVariableSheetProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  attachedEnvironment: Environment | null;
  availableEnvVarsFromAttached: string[];
  onAddVariable: (key: string, value: string, state: EnvVarState) => void;
  onLoadFromEnvironment: (varKey: string) => void;
}

export function AddVariableSheet({
  open,
  onOpenChange,
  attachedEnvironment,
  availableEnvVarsFromAttached,
  onAddVariable,
  onLoadFromEnvironment,
}: AddVariableSheetProps) {
  const [newKey, setNewKey] = useState("");
  const [newValue, setNewValue] = useState("");
  const [newState] = useState<EnvVarState>("system");
  const [newValueVisible] = useState(false);

  const handleAddVariable = () => {
    if (!newKey.trim()) return;

    const varKey = newKey.toUpperCase().replace(/\s+/g, "_");
    onAddVariable(varKey, newValue, newState);

    // Reset form
    setNewKey("");
    setNewValue("");
    onOpenChange(false);
  };

  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
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

          {/* Key and Value inputs side by side */}
          <div className="flex gap-4">
            <div className="flex-1">
              <Label className="text-xs text-muted-foreground mb-1.5 block">
                Key
              </Label>
              <input
                type="text"
                value={newKey}
                onChange={(e) => setNewKey(e.target.value.toUpperCase())}
                placeholder="CLIENT_KEY..."
                className="w-full h-10 px-3 rounded-md border border-input bg-background text-sm font-mono placeholder:text-muted-foreground focus:outline-none focus:ring-2 focus:ring-ring"
              />
            </div>
            <div className="flex-1">
              <Label className="text-xs text-muted-foreground mb-1.5 block">
                Value
              </Label>
              <input
                type={newValueVisible ? "text" : "password"}
                value={newValue}
                onChange={(e) => setNewValue(e.target.value)}
                placeholder=""
                disabled={newState !== "system"}
                className="w-full h-10 px-3 rounded-md border border-input bg-background text-sm font-mono placeholder:text-muted-foreground focus:outline-none focus:ring-2 focus:ring-ring disabled:bg-muted disabled:cursor-not-allowed"
              />
            </div>
          </div>

          {/* Add Another button */}
          <button className="flex items-center gap-2 text-sm text-muted-foreground hover:text-foreground transition-colors">
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
          <Button onClick={handleAddVariable} disabled={!newKey.trim()}>
            Save
          </Button>
        </SheetFooter>
      </SheetContent>
    </Sheet>
  );
}
