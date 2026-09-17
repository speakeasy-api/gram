import { Icon } from "@/components/ui/Icon";
import { Input } from "@/components/ui/Input";
import { Label } from "@/components/ui/Label";
import { Stack } from "@/components/ui/Stack";
import { Switch } from "@/components/ui/Switch";
import { Text } from "@/components/ui/Text";
import { useId } from "react";

export type AnnotationHints = {
  title: string;
  readOnly: boolean;
  destructive: boolean;
  idempotent: boolean;
  openWorld: boolean;
};

function AnnotationToggle({
  label,
  description,
  checked,
  onCheckedChange,
}: {
  label: string;
  description: string;
  checked: boolean;
  onCheckedChange: (value: boolean) => void;
}): JSX.Element {
  const descriptionId = useId();

  return (
    <div className="flex items-center justify-between">
      <div>
        <p className="text-sm">{label}</p>
        <p id={descriptionId} className="text-muted-foreground text-xs">
          {description}
        </p>
      </div>
      <Switch
        checked={checked}
        onCheckedChange={onCheckedChange}
        aria-label={`${label} hint`}
        aria-describedby={descriptionId}
      />
    </div>
  );
}

export function AnnotationsFields({
  hints,
  onChange,
}: {
  hints: AnnotationHints;
  onChange: (hints: AnnotationHints) => void;
}): JSX.Element {
  const set = (patch: Partial<AnnotationHints>) =>
    onChange({ ...hints, ...patch });
  return (
    <Stack gap={4}>
      <div className="space-y-2">
        <Label className="text-sm font-medium">Title</Label>
        <Input
          value={hints.title}
          onChange={(title) => set({ title })}
          placeholder="Display name override"
        />
      </div>
      <div className="space-y-3">
        <Label className="text-sm font-medium">Behavior Hints</Label>
        <div className="space-y-2">
          <AnnotationToggle
            label="Read-only"
            description="Tool does not modify its environment"
            checked={hints.readOnly}
            onCheckedChange={(readOnly) => set({ readOnly })}
          />
          <AnnotationToggle
            label="Destructive"
            description="Tool may perform destructive updates"
            checked={hints.destructive}
            onCheckedChange={(destructive) => set({ destructive })}
          />
          <AnnotationToggle
            label="Idempotent"
            description="Repeated calls with same arguments have no additional effect"
            checked={hints.idempotent}
            onCheckedChange={(idempotent) => set({ idempotent })}
          />
          <AnnotationToggle
            label="Open-world"
            description="Tool interacts with external entities"
            checked={hints.openWorld}
            onCheckedChange={(openWorld) => set({ openWorld })}
          />
        </div>
      </div>
    </Stack>
  );
}

export function OriginalValueNote({
  label,
  value,
}: {
  label: string;
  value: string | undefined;
}): JSX.Element {
  return (
    <Stack className="border-border/70 border p-2">
      <Text small muted className="inline font-medium">
        <Icon
          name="layers-2"
          size="small"
          className="text-muted-foreground/70 inline align-text-bottom"
        />{" "}
        {label}
      </Text>
      <Text small muted>
        {value}
      </Text>
    </Stack>
  );
}
