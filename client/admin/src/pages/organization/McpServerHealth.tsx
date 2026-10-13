function LoggingOff({ idOrSlug }: { idOrSlug: string }): JSX.Element {
  return (
    <section className={CARD} aria-labelledby="tool-call-outcomes">
      <div className={CARD_HEAD}>
        <h2 id="tool-call-outcomes" className={CARD_TITLE}>
          Tool call outcomes
        </h2>
      </div>
      <div className="flex flex-wrap items-center justify-between gap-6 px-5 py-7">
        <div className="flex max-w-3xl flex-col gap-1.5">
          <span className="text-[0.9375rem] font-medium">
            Logging is off for this organization
          </span>
          <span className={cn(MUTED, "text-sm leading-normal")}>
            Tool calls are not recorded while the logs feature is off, so this
            server's outcomes are unknown. Treat it as unverified, not healthy.
            Turning logging on only records calls from that point.
          </span>
        </div>
        <Button asChild size="sm">
          <Link to="/organizations/$idOrSlug/features" params={{ idOrSlug }}>
            Review features
          </Link>
        </Button>
      </div>
    </section>
  );
}
