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
  /**
   * The marker colour: a hue from the brand spectrum (see gradient-colors),
   * so an annotation is identifiable at a glance the way a filter chip's
   * dimension square is.
   */
  color: string;
  icon: React.ComponentType<{ className?: string }>;
}[] = [
  {
    key: "read_only",
    label: "Read-only",
    description: "Tools that don't modify their environment",
    color: "hsl(108, 28%, 45%)",
    icon: Shield,
  },
  {
    key: "destructive",
    label: "Destructive",
    description: "Tools that perform destructive updates",
    color: "hsl(4, 45%, 52%)",
    icon: AlertTriangle,
  },
  {
    key: "idempotent",
    label: "Idempotent",
    description: "Repeated calls have no additional effect",
    color: "hsl(214, 48%, 52%)",
    icon: Repeat,
  },
  {
    key: "open_world",
    label: "Open-world",
    description: "Tools that interact with external entities",
    color: "hsl(23, 52%, 52%)",
    icon: Globe,
  },
];
