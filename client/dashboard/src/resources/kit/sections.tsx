import { CopyButton } from "@/components/ui/CopyButton";
import { Input } from "@/components/ui/Input";
import { SegmentedControl } from "@/components/ui/SegmentedControl";
import { Switch } from "@/components/ui/Switch";
import { Button } from "@/components/ui/Button";
import { cn } from "@/lib/utils";
import { formatDistanceToNow } from "date-fns/formatDistanceToNow";
import { useId, useState } from "react";
import { actionDef } from "./action-def";
import { ActionButton } from "./actions";
import type {
  ActionHandlers,
  FieldDef,
  ResourceDefinition,
  SectionDef,
  Tone,
} from "./types";
import { ValueView } from "./values";

/** Section overline: the same eyebrow everywhere, with an optional aside. */
function SectionTitle({
  title,
  aside,
}: {
  title: string;
  aside?: React.ReactNode;
}): JSX.Element {
  return (
    <div className="flex min-h-8 items-center gap-3">
      <h2 className="text-eyebrow">{title}</h2>
      {aside && <div className="ml-auto flex items-center gap-2">{aside}</div>}
    </div>
  );
}

export function Section<Row, Rec>({
  section,
  rec,
  definition,
  handlers,
  subject,
  onOpenRelated,
}: {
  section: SectionDef<Rec>;
  rec: Rec;
  definition: ResourceDefinition<Row, Rec>;
  handlers: ActionHandlers;
  subject: string;
  onOpenRelated?: (resource: string, id: string) => void;
}): JSX.Element | null {
  switch (section.kind) {
    case "facts":
      return (
        <dl className="bg-card grid grid-cols-[repeat(auto-fit,minmax(160px,1fr))] border-t border-l">
          {section.facts.map((fact) => (
            <div key={fact.label} className="border-r border-b px-4 py-3">
              <dt className="text-eyebrow">{fact.label}</dt>
              <dd className="mt-1.5 flex min-w-0">
                <ValueView value={fact.value(rec)} />
              </dd>
            </div>
          ))}
        </dl>
      );

    case "checklist": {
      const steps = section.steps(rec);
      const required = steps.filter((s) => !s.optional);
      const done = required.filter((s) => s.done).length;
      if (section.hideWhenDone && done === required.length) return null;
      const current = steps.find((s) => !s.done && !s.optional);
      return (
        <section className="flex flex-col gap-3">
          <SectionTitle
            title={section.title}
            aside={
              <span className="text-eyebrow">
                {done} of {required.length} done
              </span>
            }
          />
          <ol className="bg-card border">
            {steps.map((step, i) => {
              const isCurrent = step.id === current?.id;
              return (
                <li
                  key={step.id}
                  className="flex items-start gap-4 border-b px-4 py-4 last:border-b-0"
                >
                  <span
                    aria-hidden="true"
                    className={cn(
                      "flex size-7 shrink-0 items-center justify-center font-mono text-xs",
                      step.done
                        ? "bg-primary text-primary-foreground"
                        : isCurrent
                          ? "border-foreground border"
                          : "text-muted-foreground border",
                    )}
                  >
                    {step.done ? "✓" : i + 1}
                  </span>
                  <div className="flex min-w-0 flex-1 flex-col gap-1">
                    <span
                      className={cn(
                        step.done && "text-muted-foreground line-through",
                      )}
                    >
                      {step.title}
                      {step.optional && (
                        <span className="text-eyebrow ml-2">Optional</span>
                      )}
                      <span className="sr-only">
                        {step.done ? " (done)" : ""}
                      </span>
                    </span>
                    <span className="text-muted-foreground text-sm">
                      {step.description}
                    </span>
                  </div>
                  {!step.done && step.action && (
                    <ActionButton
                      def={actionDef(definition, step.action)}
                      handlers={handlers}
                      emphasis={isCurrent ? "secondary" : "tertiary"}
                      size="sm"
                      subject={subject}
                    />
                  )}
                </li>
              );
            })}
          </ol>
        </section>
      );
    }

    case "list": {
      const items = section.items(rec);
      const total = section.total?.(rec) ?? items.length;
      return (
        <section className="flex flex-col gap-3">
          <SectionTitle
            title={section.title}
            aside={
              <>
                <span className="text-eyebrow">
                  {total > items.length
                    ? `Showing ${items.length} of ${total}`
                    : `${total}`}
                </span>
                {section.addAction && (
                  <ActionButton
                    def={actionDef(definition, section.addAction)}
                    handlers={handlers}
                    emphasis="tertiary"
                    size="sm"
                    subject={subject}
                  />
                )}
              </>
            }
          />
          {items.length === 0 ? (
            <p className="text-muted-foreground border border-dashed px-4 py-6 text-center text-sm">
              {section.empty}
            </p>
          ) : (
            <ul className="bg-card border">
              {items.map((item) => (
                <li
                  key={item.id}
                  className="flex min-h-12 items-center gap-4 border-b px-4 py-2.5 last:border-b-0"
                >
                  <div className="flex min-w-0 flex-1 flex-col sm:flex-row sm:items-baseline sm:gap-4">
                    {item.opens && onOpenRelated ? (
                      <button
                        type="button"
                        onClick={() =>
                          onOpenRelated(item.opens!.resource, item.opens!.id)
                        }
                        className={cn(
                          "truncate text-left underline-offset-2 hover:underline sm:w-56 sm:shrink-0",
                          item.mono && "font-mono text-sm",
                        )}
                        title={item.title}
                      >
                        {item.title}
                      </button>
                    ) : (
                      <span
                        className={cn(
                          "truncate sm:w-56 sm:shrink-0",
                          item.mono && "font-mono text-sm",
                        )}
                        title={item.title}
                      >
                        {item.title}
                      </span>
                    )}
                    {item.description && (
                      <span
                        className="text-muted-foreground truncate text-sm"
                        title={item.description}
                      >
                        {item.description}
                      </span>
                    )}
                  </div>
                  {section.itemAction && (
                    <ActionButton
                      def={actionDef(definition, section.itemAction)}
                      handlers={handlers}
                      emphasis="tertiary"
                      size="sm"
                      input={item.id}
                    />
                  )}
                </li>
              ))}
            </ul>
          )}
        </section>
      );
    }

    case "code":
      return <CodeSection section={section} rec={rec} />;

    case "settings":
      return (
        <section className="flex flex-col gap-3">
          <SectionTitle title={section.title} />
          <div className="bg-card border">
            {section.fields.map((field) => (
              <FieldRow
                key={field.id}
                field={field}
                rec={rec}
                handlers={handlers}
              />
            ))}
          </div>
        </section>
      );

    case "activity": {
      const entries = section.entries(rec);
      return (
        <section className="flex flex-col gap-3">
          <SectionTitle title={section.title} />
          {entries.length === 0 ? (
            <p className="text-muted-foreground text-sm">No activity yet.</p>
          ) : (
            <ol className="flex flex-col">
              {entries.map((entry) => (
                <li key={entry.id} className="flex gap-4 py-1.5 text-sm">
                  <time
                    dateTime={entry.at.toISOString()}
                    title={entry.at.toLocaleString()}
                    className="text-muted-foreground w-24 shrink-0 font-mono text-xs leading-5"
                  >
                    {formatDistanceToNow(entry.at, { addSuffix: true })}
                  </time>
                  <span className={toneText(entry.tone)}>{entry.text}</span>
                </li>
              ))}
            </ol>
          )}
        </section>
      );
    }
  }
}

function toneText(tone: Tone | undefined): string {
  switch (tone) {
    case "destructive":
      return "text-default-destructive";
    case "warning":
      return "text-default-warning";
    case "success":
      return "text-default-success";
    case "information":
      return "text-default-information";
    case "neutral":
    case undefined:
      return "";
  }
}

function CodeSection<Rec>({
  section,
  rec,
}: {
  section: Extract<SectionDef<Rec>, { kind: "code" }>;
  rec: Rec;
}): JSX.Element {
  const samples = section.samples(rec);
  const [active, setActive] = useState(samples[0]?.id ?? "");
  const sample = samples.find((s) => s.id === active) ?? samples[0];
  return (
    <section className="flex flex-col gap-3">
      <SectionTitle title={section.title} />
      {section.description && (
        <p className="text-muted-foreground -mt-1 text-sm">
          {section.description}
        </p>
      )}
      <div className="bg-card border">
        <div className="flex items-center gap-2 border-b px-3 py-2">
          <SegmentedControl
            ariaLabel="Client"
            value={sample?.id ?? null}
            onChange={setActive}
            options={samples.map((s) => ({ value: s.id, label: s.label }))}
            className="h-8"
          />
          {sample && (
            <span className="ml-auto">
              <CopyButton text={sample.code} size="sm" tooltip="Copy" />
            </span>
          )}
        </div>
        <pre className="bg-muted/40 overflow-x-auto p-4 font-mono text-xs leading-relaxed">
          {sample?.code}
        </pre>
      </div>
    </section>
  );
}

function FieldRow<Rec>({
  field,
  rec,
  handlers,
}: {
  field: FieldDef<Rec>;
  rec: Rec;
  handlers: ActionHandlers;
}): JSX.Element {
  const labelId = useId();
  const state = handlers[field.action];
  const readOnly = state == null;

  return (
    <div className="flex flex-col gap-3 border-b px-4 py-4 last:border-b-0 sm:flex-row sm:items-start sm:gap-6">
      <div className="flex flex-col gap-1 sm:w-56 sm:shrink-0">
        <span id={labelId}>{field.label}</span>
        {field.description && (
          <span className="text-muted-foreground text-sm">
            {field.description}
          </span>
        )}
      </div>
      <div className="min-w-0 flex-1">
        {field.kind === "text" && (
          <TextField
            labelId={labelId}
            value={field.value(rec)}
            readOnly={readOnly}
            pending={state?.pending ?? false}
            validate={field.validate}
            onSave={(v) => state?.run(v)}
          />
        )}
        {field.kind === "toggle" && (
          <Switch
            aria-labelledby={labelId}
            checked={field.value(rec)}
            disabled={readOnly || state?.pending}
            onCheckedChange={(next) => state?.run(String(next))}
          />
        )}
        {field.kind === "choice" && (
          <ChoiceField
            labelId={labelId}
            value={field.value(rec)}
            options={field.options(rec)}
            readOnly={readOnly}
            pending={state?.pending ?? false}
            onChange={(v) => state?.run(v)}
          />
        )}
        {readOnly && (
          <p className="text-muted-foreground mt-2 text-xs">
            You can view this but not change it.
          </p>
        )}
      </div>
    </div>
  );
}

function TextField({
  labelId,
  value,
  readOnly,
  pending,
  validate,
  onSave,
}: {
  labelId: string;
  value: string;
  readOnly: boolean;
  pending: boolean;
  validate?: (value: string) => string | undefined;
  onSave: (value: string) => void;
}): JSX.Element {
  const [draft, setDraft] = useState<string | null>(null);
  const current = draft ?? value;
  const dirty = draft != null && draft !== value;
  const error = dirty ? validate?.(current) : undefined;

  if (readOnly) return <span className="block py-2">{value}</span>;
  return (
    <div className="flex flex-col gap-2">
      <div className="flex items-center gap-2 [&>*:first-child]:flex-1">
        <Input
          aria-labelledby={labelId}
          value={current}
          onChange={setDraft}
          onEnter={() => {
            if (dirty && !error) onSave(current);
          }}
          error={error != null}
          className="flex-1"
        />
        {dirty && (
          <>
            <Button
              size="sm"
              variant="tertiary"
              onClick={() => setDraft(null)}
              disabled={pending}
            >
              Cancel
            </Button>
            <Button
              size="sm"
              onClick={() => onSave(current)}
              disabled={pending || error != null}
            >
              {pending ? "Saving…" : "Save"}
            </Button>
          </>
        )}
      </div>
      {error && <p className="text-default-destructive text-sm">{error}</p>}
    </div>
  );
}

/** Plain radio rows: options inside a section are never cards. */
function ChoiceField({
  labelId,
  value,
  options,
  readOnly,
  pending,
  onChange,
}: {
  labelId: string;
  value: string;
  options: { value: string; label: string; description: string }[];
  readOnly: boolean;
  pending: boolean;
  onChange: (value: string) => void;
}): JSX.Element {
  const name = useId();
  return (
    <div
      role="radiogroup"
      aria-labelledby={labelId}
      aria-busy={pending || undefined}
      className="flex flex-col gap-2"
    >
      {options.map((option) => (
        <label
          key={option.value}
          className={cn(
            "flex cursor-pointer items-start gap-3 py-1",
            (readOnly || pending) && "cursor-default opacity-70",
          )}
        >
          <input
            type="radio"
            name={name}
            value={option.value}
            checked={value === option.value}
            disabled={readOnly || pending}
            onChange={() => onChange(option.value)}
            className="accent-foreground mt-1"
          />
          <span className="flex flex-col">
            <span>{option.label}</span>
            <span className="text-muted-foreground text-sm">
              {option.description}
            </span>
          </span>
        </label>
      ))}
    </div>
  );
}
