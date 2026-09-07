import { AlertTriangle, Globe, Repeat, Shield } from "lucide-react";

export type ToolAnnotation =
  | "read_only"
  | "destructive"
  | "idempotent"
  | "open_world";

export const TOOL_ANNOTATIONS: readonly ToolAnnotation[] = [
  "read_only",
  "destructive",
  "idempotent",
  "open_world",
];

export const ANNOTATION_OPTIONS: {
  key: ToolAnnotation;
  label: string;
  description: string;
  icon: React.ComponentType<{ className?: string }>;
}[] = [
  {
    key: "read_only",
    label: "Read-only",
    description: "Tools that don't modify their environment",
    icon: Shield,
  },
  {
    key: "destructive",
    label: "Destructive",
    description: "Tools that perform destructive updates",
    icon: AlertTriangle,
  },
  {
    key: "idempotent",
    label: "Idempotent",
    description: "Repeated calls have no additional effect",
    icon: Repeat,
  },
  {
    key: "open_world",
    label: "Open-world",
    description: "Tools that interact with external entities",
    icon: Globe,
  },
];
