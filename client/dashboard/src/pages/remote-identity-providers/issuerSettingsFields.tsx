import { Input } from "@/components/ui/Input";
import { Label } from "@/components/ui/Label";
import { Text } from "@/components/ui/Text";
import type { ReactNode } from "react";
import { useId } from "react";

// The layout primitives shared by the issuer Settings tabs (tenant and platform
// catalog). They live beside the form builder so the two tiers cannot drift
// apart visually. Kept in this folder rather than components/ui because they
// are specific to these settings surfaces, not a general dashboard pattern.

export function SettingsSection({
  title,
  description,
  children,
}: {
  title: string;
  description?: string;
  children: ReactNode;
}): JSX.Element {
  return (
    <div className="flex flex-col gap-4 border-b pb-6 last:border-b-0 last:pb-0">
      <div className="flex flex-col gap-1">
        <Text className="font-medium">{title}</Text>
        {description && (
          <Text small muted>
            {description}
          </Text>
        )}
      </div>
      {children}
    </div>
  );
}

export function SettingsField({
  label,
  value,
  onChange,
}: {
  label: string;
  value: string;
  onChange: (value: string) => void;
}): JSX.Element {
  const id = useId();
  return (
    <div className="flex flex-col gap-1.5">
      <Label htmlFor={id}>{label}</Label>
      <Input id={id} value={value} onChange={onChange} />
    </div>
  );
}
