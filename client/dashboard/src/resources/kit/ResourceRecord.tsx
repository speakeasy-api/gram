import { InlineEmptyState } from "@/components/inline-empty-state";
import { Button } from "@/components/ui/Button";
import { Skeleton } from "@/components/ui/Skeleton";
import { SkeletonTable } from "@/components/ui/SkeletonTable";
import { useEffect, useState } from "react";
import { actionDef } from "./action-def";
import { ActionButton } from "./actions";
import { useReportPageTitle } from "./page-title";
import { Section } from "./sections";
import type { ResourceModule } from "./types";
import { MarkTile, StatusText } from "./values";

/**
 * The record template: identity and state first, then the one primary action,
 * then sections in the order the definition lists them, then the danger
 * section. A resource chooses sections; it cannot change how they look.
 */
export function ResourceRecord<Row, Rec>({
  module,
  id,
  onBack,
  onOpenRelated,
  hideBackLink = false,
}: {
  module: ResourceModule<Row, Rec>;
  id: string;
  onBack: () => void;
  /** The host draws its own breadcrumb back to the list. */
  hideBackLink?: boolean;
  onOpenRelated?: (resource: string, id: string) => void;
}): JSX.Element {
  const { definition, data } = module;
  const query = data.useRecord(id);
  const rec = query.status === "ready" ? query.data : undefined;
  const handlers = data.useRecordActions(rec);
  useReportPageTitle(rec ? definition.record.header(rec).identity.title : null);
  // A `leavesRecord` action (delete) can make the record vanish before the
  // action settles; leave as soon as that happens instead of showing an error.
  const [leaving, setLeaving] = useState(false);
  const gone = query.status === "error";
  useEffect(() => {
    if (leaving && gone) onBack();
  }, [leaving, gone, onBack]);
  const leaveProps = (actionId: string) =>
    actionDef(definition, actionId).leavesRecord
      ? { onStart: () => setLeaving(true), onDone: onBack }
      : {};

  if (query.status === "loading") {
    return (
      <div
        className="flex flex-col gap-8"
        role="status"
        aria-label={`Loading ${definition.noun.one}`}
      >
        <div className="bg-card flex gap-6 border p-6">
          <Skeleton>
            <div className="size-[72px]" />
          </Skeleton>
          <div className="flex flex-1 flex-col gap-3">
            <Skeleton>
              <div className="h-3 w-32" />
            </Skeleton>
            <Skeleton>
              <div className="h-9 w-64" />
            </Skeleton>
            <Skeleton>
              <div className="h-4 w-full max-w-md" />
            </Skeleton>
          </div>
        </div>
        <SkeletonTable />
      </div>
    );
  }

  if (query.status === "error" || !rec) {
    return (
      <InlineEmptyState
        icon="triangle-alert"
        heading={`Couldn't open this ${definition.noun.one}`}
        description={
          query.status === "error" ? query.message : "It may have been removed."
        }
        action={
          <div className="flex gap-2">
            {query.status === "error" && (
              <Button variant="secondary" size="sm" onClick={query.retry}>
                Try again
              </Button>
            )}
            <Button variant="tertiary" size="sm" onClick={onBack}>
              Back to {definition.noun.many}
            </Button>
          </div>
        }
      />
    );
  }

  const header = definition.record.header(rec);
  const primaryId = definition.record.primaryAction?.(rec);
  const secondary = definition.record.secondaryActions ?? [];
  const danger = (definition.record.dangerActions ?? []).filter(
    (a) => handlers[a] != null,
  );

  return (
    <div className="flex flex-col gap-10">
      <div className="flex flex-col gap-5">
        {!hideBackLink && (
          <button
            type="button"
            onClick={onBack}
            className="text-muted-foreground hover:text-foreground self-start text-sm"
          >
            ← {definition.list.title}
          </button>
        )}
        <article className="bg-card flex flex-wrap border">
          <div className="bg-muted/40 flex w-full items-center justify-center border-b p-6 sm:w-36 sm:border-r sm:border-b-0">
            <MarkTile mark={header.identity.mark} size={72} />
          </div>
          <div className="flex min-w-0 flex-1 flex-col gap-2 p-6">
            <div className="text-eyebrow">{header.kicker}</div>
            <h1 className="text-display-sm font-thin break-words">
              {header.identity.title}
            </h1>
            {header.description && (
              <p className="text-muted-foreground max-w-prose">
                {header.description}
              </p>
            )}
            {header.status && (
              <div className="pt-1">
                <StatusText status={header.status} />
              </div>
            )}
          </div>
        </article>

        {(primaryId || secondary.length > 0) && (
          <div className="flex flex-wrap items-center gap-2">
            {primaryId && (
              <ActionButton
                def={actionDef(definition, primaryId)}
                handlers={handlers}
                emphasis="primary"
                subject={header.identity.title}
                {...leaveProps(primaryId)}
              />
            )}
            {secondary
              .filter((a) => a !== primaryId)
              .map((a) => (
                <ActionButton
                  key={a}
                  def={actionDef(definition, a)}
                  handlers={handlers}
                  emphasis="secondary"
                  subject={header.identity.title}
                  {...leaveProps(a)}
                />
              ))}
          </div>
        )}
      </div>

      {definition.record.sections
        .filter((section) => section.when?.(rec) ?? true)
        .map((section) => (
          <Section
            key={section.id}
            section={section}
            rec={rec}
            definition={definition}
            handlers={handlers}
            subject={header.identity.title}
            onOpenRelated={onOpenRelated}
          />
        ))}

      {danger.length > 0 && (
        <section className="flex flex-col gap-3">
          <h2 className="text-eyebrow text-default-destructive">Danger zone</h2>
          <div className="bg-card border-destructive/30 border">
            {danger.map((a) => {
              const def = actionDef(definition, a);
              return (
                <div
                  key={a}
                  className="flex flex-wrap items-center gap-4 border-b px-4 py-4 last:border-b-0"
                >
                  <div className="flex min-w-0 flex-1 flex-col gap-1">
                    <span>{def.label}</span>
                    {def.confirm && (
                      <span className="text-muted-foreground text-sm">
                        {def.confirm.description}
                      </span>
                    )}
                  </div>
                  <ActionButton
                    def={def}
                    handlers={handlers}
                    emphasis="danger"
                    label={def.confirm?.confirmLabel}
                    {...leaveProps(a)}
                  />
                </div>
              );
            })}
          </div>
        </section>
      )}
    </div>
  );
}
