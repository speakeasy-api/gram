function LoadFailed({
  what,
  onRetry,
}: {
  what: string;
  onRetry: () => void;
}): JSX.Element {
  return (
    <span className="flex items-center gap-3 text-sm">
      <span className={MUTED}>{what} couldn't be loaded.</span>
      <Button variant="outline" size="xs" onClick={onRetry}>
        Try again
      </Button>
    </span>
  );
}
