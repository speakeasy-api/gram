import { Type } from "@/components/ui/type";
import { Button, Combobox } from "@speakeasy-api/moonshine";
import { FolderPlus, Layers } from "lucide-react";
import { AnimatePresence, motion } from "motion/react";

interface CommandBarProps {
  selectedCount: number;
  projects: { value: string; label: string }[];
  currentProjectSlug: string;
  onSelectProject: (slug: string) => void;
  onCreateProject: (name: string) => void;
  onAdd: () => void;
  onClear: () => void;
}

export function CommandBar({
  selectedCount,
  projects,
  currentProjectSlug,
  onSelectProject,
  onCreateProject,
  onAdd,
  onClear,
}: CommandBarProps) {
  return (
    <AnimatePresence>
      {selectedCount > 0 && (
        <motion.div
          className="fixed bottom-6 left-1/2 -translate-x-1/2 z-50"
          initial={{ opacity: 0, y: 8 }}
          animate={{ opacity: 1, y: 0 }}
          exit={{ opacity: 0, y: 8 }}
          transition={{ duration: 0.2 }}
        >
          <div className="bg-background border rounded-lg shadow-lg p-2 flex items-center gap-2">
            <div className="flex items-center gap-1.5 px-2">
              <Layers className="w-3.5 h-3.5 text-muted-foreground" />
              <Type small muted>
                {selectedCount} {selectedCount === 1 ? "server" : "servers"}
              </Type>
            </div>
            <div className="flex items-center gap-0 bg-stone-200 dark:bg-stone-800 rounded-md">
              <Type variant="small" className="px-2">
                Project
              </Type>
              <div className="[&_button]:w-36 [&_button]:justify-between [&_button]:overflow-hidden [&_button_span]:truncate">
                <Combobox
                  options={projects}
                  value={currentProjectSlug}
                  onValueChange={(v) => v && onSelectProject(v)}
                  size="sm"
                  searchable
                  placeholder="Select project..."
                  createOptions={{
                    handleCreate: onCreateProject,
                  }}
                />
              </div>
            </div>
            <Button variant="secondary" size="sm" onClick={onAdd}>
              <Button.LeftIcon>
                <FolderPlus className="w-3.5 h-3.5" />
              </Button.LeftIcon>
              <Button.Text>Add</Button.Text>
            </Button>
            <Button variant="tertiary" size="sm" onClick={onClear}>
              <Button.Text>Clear</Button.Text>
            </Button>
          </div>
        </motion.div>
      )}
    </AnimatePresence>
  );
}
