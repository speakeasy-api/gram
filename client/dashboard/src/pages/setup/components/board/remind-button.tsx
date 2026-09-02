import { Bell } from "lucide-react";
import { Button } from "@/components/ui/Button";
import type { BoardTask } from "./board-store";

function remindDisabledReason(task: BoardTask): string | undefined {
  if (task.status === "done") return "This task is already done";
  if (!task.assignee) return "Assign someone first";
  return undefined;
}

export function RemindButton({
  task,
  isReminding,
  onRemind,
  size = "xs",
}: {
  task: BoardTask;
  isReminding: boolean;
  onRemind: () => void;
  size?: "xs" | "sm";
}): JSX.Element {
  const disabledReason = remindDisabledReason(task);
  return (
    <Button
      variant="tertiary"
      size={size}
      onClick={onRemind}
      disabled={disabledReason !== undefined || isReminding}
      tooltip={disabledReason}
      aria-label={`Send a reminder for ${task.title}`}
      className="shrink-0"
    >
      <Button.LeftIcon>
        <Bell />
      </Button.LeftIcon>
      <Button.Text>{isReminding ? "Sending…" : "Remind"}</Button.Text>
    </Button>
  );
}
