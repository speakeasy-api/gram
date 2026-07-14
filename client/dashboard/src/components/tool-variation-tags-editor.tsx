import { Badge } from "@/components/ui/badge";
import { Label } from "@/components/ui/label";
import { MultiSelect } from "@/components/ui/multi-select";
import { useMemo } from "react";

interface TagsVariationEditorProps {
  /** Source tags from the underlying tool. Rendered as quick-add suggestions. */
  baseTags: string[];
  /**
   * Current variation value (tri-state):
   *   undefined  no override; effective tags come from baseTags
   *   []         explicit empty override
   *   [...]      explicit override
   */
  value: string[] | undefined;
  onChange: (value: string[] | undefined) => void;
  /** Optional id for the MultiSelect input (used as the Label htmlFor target). */
  id?: string;
}

export function TagsVariationEditor({
  baseTags,
  value,
  onChange,
  id,
}: TagsVariationEditorProps): JSX.Element {
  const overridden = value !== undefined;

  const tagOptions = useMemo(
    () => baseTags.map((tag) => ({ label: tag, value: tag })),
    [baseTags],
  );
  const tagsDefaultValue = useMemo(() => value ?? [], [value]);

  const handleAddBaseTag = (tag: string) => {
    if (!overridden) return;
    if (value.includes(tag)) return;
    onChange([...value, tag]);
  };

  return (
    <div className="space-y-2">
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-2">
          <Label htmlFor={id} className="text-sm font-medium">
            Tags
          </Label>
          {overridden && (
            <Badge variant="neutral" background={false} className="text-xs">
              Overridden
            </Badge>
          )}
        </div>
        {overridden && (
          <button
            type="button"
            onClick={() => onChange(undefined)}
            className="text-muted-foreground hover:text-foreground text-xs underline-offset-2 hover:underline"
          >
            Reset to source
          </button>
        )}
      </div>
      <MultiSelect
        id={id}
        options={tagOptions}
        defaultValue={tagsDefaultValue}
        onValueChange={(values) => onChange(values)}
        placeholder="Add tags..."
        creatable
        hideSelectAll
        autoSize
      />
      {baseTags.length > 0 && (
        <div className="flex flex-wrap items-center gap-1.5">
          <span className="text-muted-foreground text-xs">From source:</span>
          {baseTags.map((tag) => {
            // Disabled when not in override mode (source tags are already
            // effective; nothing to "add") or when already present in the
            // explicit override. Avoids the trap where clicking one chip in
            // the no-override state silently narrows the effective set.
            const isDisabled = !overridden || value.includes(tag);
            return (
              <button
                key={tag}
                type="button"
                onClick={() => handleAddBaseTag(tag)}
                disabled={isDisabled}
                aria-label={`Add tag "${tag}" from source`}
                className="disabled:cursor-not-allowed disabled:opacity-50"
              >
                <Badge
                  variant="neutral"
                  background={false}
                  className="text-xs capitalize"
                >
                  {tag}
                </Badge>
              </button>
            );
          })}
        </div>
      )}
    </div>
  );
}
