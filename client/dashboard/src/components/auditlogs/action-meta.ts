import {
  Clock,
  File,
  FileCode,
  FolderOpen,
  Globe,
  KeyRound,
  Link2,
  MessageSquare,
  Package,
  Puzzle,
  Rocket,
  Shield,
  Sparkles,
  Trash2,
  type LucideIcon,
} from "lucide-react";
import { getActionCategory } from "@/lib/audit-log-colors";

/**
 * Icon + semantic dot for an audit action, shared by every surface that
 * renders audit rows (project-home Activity Timeline, org Audit Logs feed)
 * so the two read as one system.
 */
export type ActionMeta = { icon: LucideIcon; dot?: string };

export function getActionMeta(action: string): ActionMeta {
  // Same destructive detection as getActionCategory (substring match within
  // the verb), so toolset:detach_* / plugin:server_remove read as destructive
  // here too.
  if (getActionCategory(action) === "destructive") {
    return { icon: Trash2, dot: "bg-destructive-default" };
  }
  if (action.startsWith("deployments:")) {
    return { icon: Rocket, dot: "bg-information-default" };
  }
  if (action.startsWith("api_key:")) {
    return { icon: KeyRound, dot: "bg-warning-default" };
  }
  if (
    action.startsWith("access_role:") ||
    action.startsWith("access_member:") ||
    action.startsWith("organization_invitation:")
  ) {
    return { icon: Shield, dot: "bg-warning-default" };
  }
  if (action.startsWith("toolset:") && action.includes("oauth")) {
    return { icon: Link2, dot: "bg-information-default" };
  }
  if (action.startsWith("toolset:")) {
    return { icon: Package, dot: "bg-information-default" };
  }
  if (
    action.startsWith("environment:") ||
    action.startsWith("custom_domains:") ||
    action.startsWith("network_ingress:")
  ) {
    return { icon: Globe, dot: "bg-information-default" };
  }
  if (action.startsWith("template:")) {
    return { icon: FileCode, dot: "bg-information-default" };
  }
  if (action.startsWith("project:")) {
    return { icon: FolderOpen, dot: "bg-success-default" };
  }
  if (action.startsWith("asset:")) {
    return { icon: File };
  }
  if (action.startsWith("variation:")) {
    return { icon: Sparkles, dot: "bg-information-default" };
  }
  if (action.startsWith("chat_session:")) {
    return { icon: MessageSquare, dot: "bg-information-default" };
  }
  if (action.startsWith("plugin:")) {
    return { icon: Puzzle, dot: "bg-information-default" };
  }
  return { icon: Clock };
}
