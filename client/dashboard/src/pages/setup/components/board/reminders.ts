import { type Assignee, assigneeLabel } from "./board-store";

/**
 * Sends the assignee an email asking them to finish a task.
 *
 * Stubbed: the real version will call a management API endpoint that emails
 * the assignee a link back to the task on this board. Resolving after a short
 * delay lets the UI exercise the pending state it will need once the request
 * is real.
 */
export async function sendTaskReminder(input: {
  taskTitle: string;
  assignee: Assignee;
}): Promise<{ recipient: string }> {
  await new Promise<void>((resolve) => {
    setTimeout(resolve, 400);
  });
  return { recipient: assigneeLabel(input.assignee) };
}
