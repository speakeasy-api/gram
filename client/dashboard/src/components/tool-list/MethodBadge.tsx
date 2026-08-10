import { Badge } from "@/components/ui/Badge";

export function MethodBadge({ method }: { method: string }): JSX.Element {
  const upperMethod = method.toUpperCase();
  // Editorial idiom: neutral ink for every verb; red is reserved for DELETE.
  const variant = upperMethod === "DELETE" ? "destructive" : "neutral";

  return (
    <Badge variant={variant} className="font-mono text-xs" background={false}>
      <Badge.Text>{upperMethod}</Badge.Text>
    </Badge>
  );
}
