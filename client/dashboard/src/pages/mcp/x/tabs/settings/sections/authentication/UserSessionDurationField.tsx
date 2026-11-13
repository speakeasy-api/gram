import { FieldError } from "@/components/ui/Field";
import { Input } from "@/components/ui/Input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/Select";
import { AuthRow } from "./AuthRow";
import type { DurationUnit, SessionDurationDraft } from "./sessionDuration";

const DURATION_UNIT_OPTIONS: ReadonlyArray<{
  value: DurationUnit;
  label: string;
}> = [
  { value: "hour", label: "Hours" },
  { value: "day", label: "Days" },
  { value: "week", label: "Weeks" },
];

/**
 * The session length row. The draft and its Save live with the panel's other
 * session settings, in `UserIdentitySessionControls`.
 */
export function UserSessionDurationField({
  value,
  onChange,
  invalid,
  readOnly = false,
}: {
  value: SessionDurationDraft;
  onChange: (next: SessionDurationDraft) => void;
  /** The draft is under an hour; shown only once it differs from the saved value. */
  invalid: boolean;
  readOnly?: boolean;
}): JSX.Element {
  const handleNumberChange = (raw: string) => {
    const parsed = parseInt(raw, 10);
    onChange({
      ...value,
      number: Number.isFinite(parsed) && parsed >= 0 ? parsed : 0,
    });
  };

  return (
    <AuthRow
      label="Session length"
      hint="Longest a sign-in lasts before users authenticate again."
      htmlFor="mcp-auth-session-duration"
    >
      <div className="flex flex-wrap items-center gap-2">
        <Input
          id="mcp-auth-session-duration"
          type="number"
          min="1"
          value={String(value.number)}
          onChange={handleNumberChange}
          aria-invalid={invalid ? true : undefined}
          aria-describedby={
            invalid ? "mcp-auth-session-duration-error" : undefined
          }
          className="w-[90px]"
          disabled={readOnly}
        />
        <Select
          value={value.unit}
          onValueChange={(unit) =>
            onChange({ ...value, unit: unit as DurationUnit })
          }
          disabled={readOnly}
        >
          <SelectTrigger className="w-[110px]">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {DURATION_UNIT_OPTIONS.map((option) => (
              <SelectItem key={option.value} value={option.value}>
                {option.label}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>

      {invalid && (
        <FieldError id="mcp-auth-session-duration-error">
          Enter a duration of at least one hour.
        </FieldError>
      )}
    </AuthRow>
  );
}
