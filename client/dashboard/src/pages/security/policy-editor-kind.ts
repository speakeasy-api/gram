import {
  type LucideIcon,
  MonitorCheck,
  Shield,
  ShieldCheck,
  Sparkles,
} from "lucide-react";

/** Which editor a policy opens in. Standard and prompt policies choose their
 *  detectors; a Shadow AI policy's detector is fixed, and it is told apart
 *  by what it decides instead. */
export type PolicyEditorKind = "prompt" | "standard" | "shadow_ai" | "approval";

export const POLICY_EDITOR_KIND_META: Record<
  PolicyEditorKind,
  {
    icon: LucideIcon;
    label: string;
    placeholder: string;
    /** Under the name while the policy is still being created. */
    createHint: string;
    /** After the version while the policy is turned off: what it stops. */
    inactiveNote: string;
  }
> = {
  prompt: {
    icon: Sparkles,
    label: "Prompt-based (LLM judge)",
    placeholder: "Untitled prompt policy",
    createHint:
      "Describe the behavior to catch. The name is generated for you.",
    inactiveNote: "new messages are not scanned",
  },
  standard: {
    icon: Shield,
    label: "Built-in detector",
    placeholder: "Untitled standard policy",
    createHint: "Leave the name blank to auto-generate one from the detectors.",
    inactiveNote: "new messages are not scanned",
  },
  shadow_ai: {
    icon: MonitorCheck,
    label: "Shadow AI",
    placeholder: "Untitled Shadow AI policy",
    createHint:
      "Decide which AI tools may connect. Decisions apply when the policy is created.",
    inactiveNote: "new messages are not scanned",
  },
  approval: {
    icon: ShieldCheck,
    label: "Tool approval",
    placeholder: "Untitled approval policy",
    createHint:
      "Choose the tools that need approval, then who it applies to and who approves.",
    inactiveNote: "tool calls are not held for approval",
  },
};
