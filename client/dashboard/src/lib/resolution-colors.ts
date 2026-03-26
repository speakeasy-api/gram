/**
 * Shared color definitions for resolution status indicators.
 * Used across ChatLogs, ChatLogsTable, and other components that display resolution status.
 */

export type ResolutionStatus =
  | "success"
  | "failure"
  | "partial"
  | "abandoned"
  | "unresolved";

/**
 * Background colors for status indicators (dots, badges, etc.)
 */
export const resolutionBgColors: Record<ResolutionStatus, string> = {
  success: "bg-emerald-500",
  failure: "bg-rose-500",
  partial: "bg-amber-500",
  abandoned: "bg-slate-400",
  unresolved: "bg-slate-400",
};

/**
 * Stroke colors for score rings (SVG stroke)
 */
export const resolutionStrokeColors: Record<ResolutionStatus, string> = {
  success: "stroke-emerald-500",
  failure: "stroke-rose-500",
  partial: "stroke-amber-500",
  abandoned: "stroke-slate-400",
  unresolved: "stroke-slate-400",
};

/**
 * Muted stroke colors for ring backgrounds
 */
export const resolutionStrokeMutedColors: Record<ResolutionStatus, string> = {
  success: "stroke-emerald-500/15",
  failure: "stroke-rose-500/15",
  partial: "stroke-amber-500/15",
  abandoned: "stroke-slate-400/15",
  unresolved: "stroke-slate-400/15",
};

/**
 * Text colors for status labels
 */
export const resolutionTextColors: Record<ResolutionStatus, string> = {
  success: "text-emerald-500",
  failure: "text-rose-500",
  partial: "text-amber-500",
  abandoned: "text-slate-400",
  unresolved: "text-slate-400",
};

/**
 * Score thresholds for determining color based on numeric score
 */
export function getScoreColor(
  score: number,
): "success" | "partial" | "failure" {
  if (score >= 80) return "success";
  if (score >= 50) return "partial";
  return "failure";
}
