import { Badge } from "@speakeasy-api/moonshine";

const methodVariants = {
  GET: "information",
  POST: "success",
  PUT: "warning",
  PATCH: "warning",
  DELETE: "destructive",
} as const;

export function MethodBadge({ method }: { method: string }) {
  const upperMethod = method.toUpperCase();
  const variant =
    methodVariants[upperMethod as keyof typeof methodVariants] || "information";

  return (
    <Badge variant={variant} className="font-mono text-xs" background={false}>
      <Badge.Text>{upperMethod}</Badge.Text>
    </Badge>
  );
}
