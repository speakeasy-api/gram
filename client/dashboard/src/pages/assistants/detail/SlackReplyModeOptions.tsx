import { Label } from "@/components/ui/Label";
import { RadioGroup, RadioGroupItem } from "@/components/ui/RadioGroup";
import { Text } from "@/components/ui/Text";
import { cn } from "@/lib/utils";
import {
  SLACK_REPLY_MODES,
  isSlackReplyMode,
  type SlackReplyMode,
} from "./slackCapabilities";

/**
 * The "when should it reply in Slack?" choice, shared by the onboarding cards
 * and the trigger editor.
 */
export function SlackReplyModeOptions({
  value,
  onChange,
  idPrefix,
}: {
  value: SlackReplyMode | undefined;
  onChange: (value: SlackReplyMode) => void;
  idPrefix: string;
}): JSX.Element {
  return (
    <div className="space-y-1.5">
      <RadioGroup
        value={value ?? ""}
        onValueChange={(next) => {
          if (isSlackReplyMode(next)) onChange(next);
        }}
        className="gap-1.5"
      >
        {SLACK_REPLY_MODES.map((mode) => {
          const id = `${idPrefix}-${mode.value}`;
          return (
            <div
              key={mode.value}
              className={cn(
                "border-border flex items-start gap-2 border p-3",
                value === mode.value && "border-primary bg-primary/5",
              )}
            >
              <RadioGroupItem value={mode.value} id={id} className="mt-1" />
              <Label
                htmlFor={id}
                className="flex-1 cursor-pointer flex-col items-start gap-0"
              >
                <Text small className="font-medium">
                  {mode.label}
                </Text>
                <Text small muted className="mt-0.5">
                  {mode.description}
                </Text>
              </Label>
            </div>
          );
        })}
      </RadioGroup>
      <Text small muted>
        Direct messages and clicks on its buttons always get a reply.
      </Text>
    </div>
  );
}
