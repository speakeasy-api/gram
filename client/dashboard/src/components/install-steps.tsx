import { CodeBlock } from "@/components/code";

export type InstallStep = {
  title: React.ReactNode;
  description?: React.ReactNode;
  code?: string;
  language?: string;
  children?: React.ReactNode;
};

/**
 * A numbered, connected step list for install/setup instructions. Each step
 * gets a filled number badge, a connecting line down to the next step, and an
 * optional syntax-highlighted code snippet (via the shared CodeBlock) instead
 * of a plain gray box.
 */
export function InstallSteps({
  steps,
}: {
  steps: InstallStep[];
}): React.JSX.Element {
  return (
    <ol className="list-none">
      {steps.map((step, i) => (
        <li key={i} className="relative flex gap-4 pb-6 last:pb-0">
          {i < steps.length - 1 && (
            <div
              aria-hidden="true"
              className="bg-border absolute top-7 bottom-0 left-[13px] w-px"
            />
          )}
          <div className="bg-foreground text-background relative z-10 flex h-7 w-7 shrink-0 items-center justify-center rounded-full text-xs font-semibold">
            {i + 1}
          </div>
          <div className="min-w-0 flex-1 space-y-2 pt-0.5">
            <h4 className="text-foreground text-sm font-semibold">
              {step.title}
            </h4>
            {step.description && (
              <p className="text-muted-foreground text-sm leading-relaxed">
                {step.description}
              </p>
            )}
            {step.code && (
              <CodeBlock language={step.language ?? "bash"} className="mt-1">
                {step.code}
              </CodeBlock>
            )}
            {step.children}
          </div>
        </li>
      ))}
    </ol>
  );
}
