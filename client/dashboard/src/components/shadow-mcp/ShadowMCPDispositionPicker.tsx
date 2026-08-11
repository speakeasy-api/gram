import { RadioGroup, RadioGroupItem } from "@/components/ui/RadioGroup";
import { Text } from "@/components/ui/Text";
import { cn } from "@/lib/utils";
import type { ShadowMCPDisposition } from "@/pages/security/policy-shadow-mcp-setup";

const DISPOSITION_OPTIONS: {
  value: ShadowMCPDisposition;
  title: string;
  description: string;
}[] = [
  {
    value: "block_all",
    title: "Block all servers",
    description:
      "Every Shadow MCP server is blocked unless you allow it. Best for locked-down environments.",
  },
  {
    value: "allow_all",
    title: "Allow all servers",
    description:
      "Every Shadow MCP server stays available unless you block it. Best for rolling out enforcement gradually.",
  },
];

export type ShadowMCPDispositionPickerProps = {
  value: ShadowMCPDisposition;
  onChange: (next: ShadowMCPDisposition) => void;
  /** Edit mode: the disposition is immutable after create, so render the
   * choice read-only with an explanation. */
  readOnly: boolean;
};

export function ShadowMCPDispositionPicker({
  value,
  onChange,
  readOnly,
}: ShadowMCPDispositionPickerProps): JSX.Element {
  return (
    <section
      aria-labelledby="shadow-mcp-disposition-picker-title"
      className="space-y-3"
    >
      <div>
        <Text
          id="shadow-mcp-disposition-picker-title"
          variant="body"
          className="font-medium"
        >
          Default behavior
        </Text>
        <Text muted small className="mt-1">
          How this policy treats Shadow MCP servers that have no rule.
        </Text>
      </div>
      <RadioGroup
        value={value}
        onValueChange={(next) => {
          if (readOnly) return;
          onChange(next as ShadowMCPDisposition);
        }}
        className="space-y-2.5"
      >
        {DISPOSITION_OPTIONS.map((opt) => {
          const selected = value === opt.value;
          const disabled = readOnly && !selected;

          return (
            <label
              key={opt.value}
              htmlFor={`shadow-mcp-disposition-${opt.value}`}
              className={cn(
                "grid grid-cols-[auto_1fr] items-center gap-x-3 border p-3.5 transition-colors",
                disabled
                  ? "border-border cursor-not-allowed opacity-60"
                  : selected
                    ? "border-foreground bg-muted/40 cursor-default"
                    : "border-border hover:bg-muted/30 cursor-pointer",
              )}
            >
              <RadioGroupItem
                value={opt.value}
                id={`shadow-mcp-disposition-${opt.value}`}
                disabled={readOnly}
              />
              <span className="text-sm font-medium">{opt.title}</span>
              <div className="text-muted-foreground col-start-2 mt-1.5 text-xs">
                {opt.description}
              </div>
            </label>
          );
        })}
      </RadioGroup>
      {readOnly && (
        <Text muted small>
          The disposition is locked after a policy is created. To switch, delete
          this policy (and its rules) and create a new one.
        </Text>
      )}
    </section>
  );
}
