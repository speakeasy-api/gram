import type { OnboardingOption } from "@gram/client/models/components/onboardingoption.js";
import { Icon } from "@/components/ui/Icon";
import type { IconName } from "@/components/ui/Icon/names";
import { RadioCard, RadioCardGroup } from "@/components/ui/RadioCard";

const DETAILS: Record<string, { icon: IconName; note: string }> = {
  observability: {
    icon: "activity",
    note: "See every session and tool call from the products you picked.",
  },
  "cost-tracking": {
    icon: "credit-card",
    note: "Know what each person and product spends, then set budgets.",
  },
  security: {
    icon: "shield-check",
    note: "Scan traffic against policies and find AI tools you did not approve.",
  },
  "mcp-gateway": {
    icon: "network",
    note: "Route MCP servers through one gateway and put it in your agents' hands.",
  },
};

/** The use case question: exactly one outcome, the first aha moment. */
export function UseCasePicker({
  useCases,
  value,
  onChange,
  disabled = false,
}: {
  useCases: OnboardingOption[];
  value: string | null;
  onChange: (useCase: string) => void;
  disabled?: boolean;
}): JSX.Element {
  return (
    <RadioCardGroup value={value} onValueChange={onChange} disabled={disabled}>
      {useCases.map((useCase) => {
        const detail = DETAILS[useCase.slug];
        return (
          <RadioCard
            key={useCase.slug}
            value={useCase.slug}
            title={useCase.name}
            leading={
              detail ? (
                <Icon
                  name={detail.icon}
                  className="text-muted-foreground h-5 w-5"
                />
              ) : undefined
            }
          >
            {detail?.note ?? ""}
          </RadioCard>
        );
      })}
    </RadioCardGroup>
  );
}
