import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/Sheet";
import { Text } from "@/components/ui/Text";
import type { IntentGuardrail } from "@gram/client/models/components/intentguardrail.js";

export function GuardrailSheet({
  guardrail,
  open,
  onOpenChange,
}: {
  guardrail: IntentGuardrail;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}): JSX.Element {
  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent className="w-[min(640px,100vw)] overflow-y-auto sm:max-w-none">
        <SheetHeader>
          <SheetTitle>Guardrail</SheetTitle>
          <SheetDescription>
            Version {guardrail.version}. Read-only: these rules are built in.
          </SheetDescription>
        </SheetHeader>
        <div className="flex flex-col gap-8 px-4 pb-8">
          <section className="flex flex-col gap-2">
            <h3 className="text-eyebrow">Statements</h3>
            <ol className="flex list-decimal flex-col gap-1.5 pl-5 text-sm">
              {guardrail.statements.map((s) => (
                <li key={s}>{s}</li>
              ))}
            </ol>
          </section>

          <section className="flex flex-col gap-2">
            <h3 className="text-eyebrow">Labels</h3>
            <ul className="flex flex-col divide-y border-y">
              {guardrail.questions.map((q) => (
                <li key={q.key} className="flex flex-col gap-1 py-3">
                  <span className="font-mono text-xs">{q.key}</span>
                  <Text small>{q.instructions}</Text>
                  {q.options.length > 0 && (
                    <ul className="flex flex-col gap-0.5 pl-3">
                      {q.options.map((o) => (
                        <li key={o.key} className="text-xs">
                          <span className="font-mono">{o.key}</span>
                          <span className="text-muted-foreground">
                            {" "}
                            {o.description}
                          </span>
                        </li>
                      ))}
                    </ul>
                  )}
                </li>
              ))}
            </ul>
          </section>

          <section className="flex flex-col gap-2">
            <h3 className="text-eyebrow">Contracts</h3>
            <Text small muted>
              Any block or contract error blocks. Otherwise any clarify asks a
              person. Otherwise the call is permitted.
            </Text>
            <ul className="flex flex-col divide-y border-y">
              {guardrail.contracts.map((c) => (
                <li key={c.id} className="flex flex-col gap-1 py-3">
                  <span className="text-sm">{c.name}</span>
                  <code className="font-mono text-xs break-all text-muted-foreground">
                    {c.expression}
                  </code>
                </li>
              ))}
            </ul>
          </section>
        </div>
      </SheetContent>
    </Sheet>
  );
}
