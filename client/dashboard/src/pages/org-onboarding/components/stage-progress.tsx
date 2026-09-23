/**
 * Two plain bars, one per stage of the wizard, that fill as the reader moves
 * forward. They carry no visible labels: the rail names the screens, and the
 * bars only show how far each stage has come. The use case screen sits
 * between the stages and belongs to neither bar.
 */
export function StageProgress({
  stack,
  steps,
}: {
  /** How much of stage one, the organization's stack, is done, 0 to 1. */
  stack: number;
  /** How much of stage two, the use case's steps, is done, 0 to 1. */
  steps: number;
}): JSX.Element {
  return (
    <div className="flex w-full gap-2">
      <Bar label="Your stack" value={stack} />
      <Bar label="Your use case's steps" value={steps} />
    </div>
  );
}

function Bar({ label, value }: { label: string; value: number }): JSX.Element {
  const percent = Math.round(Math.min(1, Math.max(0, value)) * 100);
  return (
    <div
      role="progressbar"
      aria-label={label}
      aria-valuemin={0}
      aria-valuemax={100}
      aria-valuenow={percent}
      className="bg-border h-1.5 min-w-0 flex-1 overflow-hidden"
    >
      <div
        className="bg-foreground h-full transition-[width] duration-300 ease-out"
        style={{ width: `${percent}%` }}
      />
    </div>
  );
}
