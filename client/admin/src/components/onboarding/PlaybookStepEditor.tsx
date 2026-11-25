import { ArrowDown, ArrowUp, X } from "lucide-react";
import type { JSX } from "react";
import type { AdminOnboardingStep } from "@gram/admin-client/models/components/adminonboardingstep";

import { Button } from "@/components/ui/button";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";

/**
 * The ordered top-level steps of a playbook. Cards under a group come with
 * the group, so only top-level steps are offered; the server checks that
 * every prerequisite is present and earlier.
 */
export function PlaybookStepEditor({
  steps,
  value,
  onChange,
  disabled = false,
}: {
  /** Every step the server mirrors, groups and cards. */
  steps: AdminOnboardingStep[];
  /** Selected top-level slugs in walking order. */
  value: string[];
  onChange: (next: string[]) => void;
  disabled?: boolean;
}): JSX.Element {
  const topLevel = steps.filter((step) => !step.parentSlug);
  const bySlug = new Map(topLevel.map((step) => [step.slug, step]));
  const cardsOf = (slug: string) =>
    steps.filter((step) => step.parentSlug === slug).map((step) => step.title);
  const available = topLevel.filter((step) => !value.includes(step.slug));
  const move = (index: number, delta: number) => {
    const next = [...value];
    const target = index + delta;
    if (target < 0 || target >= next.length) return;
    [next[index], next[target]] = [next[target]!, next[index]!];
    onChange(next);
  };

  return (
    <div className="space-y-3">
      {value.length === 0 ? (
        <p className="text-muted-foreground text-sm">No steps yet.</p>
      ) : (
        <ol className="space-y-2">
          {value.map((slug, index) => {
            const step = bySlug.get(slug);
            const cards = cardsOf(slug);
            return (
              <li
                key={slug}
                className="border-border flex items-start gap-3 rounded-md border p-2"
              >
                <span className="text-muted-foreground w-5 pt-1 text-right text-sm">
                  {index + 1}
                </span>
                <div className="min-w-0 flex-1">
                  <p className="text-sm font-medium">{step?.title ?? slug}</p>
                  {cards.length > 0 ? (
                    <p className="text-muted-foreground text-xs">
                      {cards.join(" · ")}
                    </p>
                  ) : null}
                </div>
                <div className="flex gap-1">
                  <Button
                    type="button"
                    variant="ghost"
                    size="icon"
                    aria-label={`Move ${step?.title ?? slug} up`}
                    disabled={disabled || index === 0}
                    onClick={() => move(index, -1)}
                  >
                    <ArrowUp />
                  </Button>
                  <Button
                    type="button"
                    variant="ghost"
                    size="icon"
                    aria-label={`Move ${step?.title ?? slug} down`}
                    disabled={disabled || index === value.length - 1}
                    onClick={() => move(index, 1)}
                  >
                    <ArrowDown />
                  </Button>
                  <Button
                    type="button"
                    variant="ghost"
                    size="icon"
                    aria-label={`Remove ${step?.title ?? slug}`}
                    disabled={disabled}
                    onClick={() => onChange(value.filter((s) => s !== slug))}
                  >
                    <X />
                  </Button>
                </div>
              </li>
            );
          })}
        </ol>
      )}
      <Select
        value=""
        onValueChange={(slug) => onChange([...value, slug])}
        disabled={disabled || available.length === 0}
      >
        <SelectTrigger aria-label="Add a step" className="w-72">
          <SelectValue placeholder="Add a step" />
        </SelectTrigger>
        <SelectContent>
          {available.map((step) => (
            <SelectItem key={step.slug} value={step.slug}>
              {step.title}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
    </div>
  );
}
