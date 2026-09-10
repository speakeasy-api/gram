import type { BoardTask } from "./board-store";

export function remindDisabledReason(task: BoardTask): string | undefined {
  if (task.status === "done") return "This task is already done";
  if (!task.assignee) return "Assign someone first";
  return undefined;
}
