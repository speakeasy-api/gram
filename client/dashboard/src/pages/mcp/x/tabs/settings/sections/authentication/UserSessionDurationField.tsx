import { RequireScope } from "@/components/require-scope";
import { FieldError } from "@/components/ui/Field";
import { Input } from "@/components/ui/Input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/Select";
import type { UserSessionIssuer } from "@gram/client/models/components/usersessionissuer.js";
import { useUpdateUserSessionIssuerMutation } from "@gram/client/react-query/updateUserSessionIssuer.js";
import { invalidateAllUserSessionIssuer } from "@gram/client/react-query/userSessionIssuer.js";
import { invalidateAllUserSessionIssuers } from "@gram/client/react-query/userSessionIssuers.js";
import { Button } from "@/components/ui/Button";
import { useQueryClient } from "@tanstack/react-query";
import { Loader2 } from "lucide-react";
import { useEffect, useState } from "react";
import { toast } from "sonner";
import { AuthRow, RowSave } from "./AuthRow";

type DurationUnit = "hour" | "day" | "week";

const DURATION_UNIT_HOURS: Record<DurationUnit, number> = {
  hour: 1,
  day: 24,
  week: 24 * 7,
};

const DURATION_UNIT_OPTIONS: ReadonlyArray<{
  value: DurationUnit;
  label: string;
}> = [
  { value: "hour", label: "Hours" },
  { value: "day", label: "Days" },
  { value: "week", label: "Weeks" },
];

function splitIntoUnit(hours: number): {
  number: number;
  unit: DurationUnit;
} {
  if (hours > 0 && hours % DURATION_UNIT_HOURS.week === 0) {
    return { number: hours / DURATION_UNIT_HOURS.week, unit: "week" };
  }
  if (hours > 0 && hours % DURATION_UNIT_HOURS.day === 0) {
    return { number: hours / DURATION_UNIT_HOURS.day, unit: "day" };
  }
  return { number: Math.max(0, hours), unit: "hour" };
}

export function UserSessionDurationField({
  userSessionIssuer,
}: {
  userSessionIssuer: UserSessionIssuer;
}): JSX.Element {
  const queryClient = useQueryClient();
  const initialSplit = splitIntoUnit(userSessionIssuer.sessionDurationHours);
  const [durationNumber, setDurationNumber] = useState(initialSplit.number);
  const [durationUnit, setDurationUnit] = useState<DurationUnit>(
    initialSplit.unit,
  );

  useEffect(() => {
    const split = splitIntoUnit(userSessionIssuer.sessionDurationHours);
    setDurationNumber(split.number);
    setDurationUnit(split.unit);
  }, [userSessionIssuer.sessionDurationHours]);

  const update = useUpdateUserSessionIssuerMutation({
    onSuccess: async () => {
      await Promise.all([
        invalidateAllUserSessionIssuers(queryClient, { refetchType: "all" }),
        invalidateAllUserSessionIssuer(queryClient, { refetchType: "all" }),
      ]);
      toast.success("Session duration updated");
    },
    onError: (error) => {
      toast.error(
        error instanceof Error
          ? error.message
          : "Failed to update session duration",
      );
    },
  });

  const draftHours = durationNumber * DURATION_UNIT_HOURS[durationUnit];
  const dirty = draftHours !== userSessionIssuer.sessionDurationHours;
  const valid = draftHours > 0;

  const handleSave = () => {
    update.mutate({
      request: {
        updateUserSessionIssuerForm: {
          id: userSessionIssuer.id,
          sessionDurationHours: draftHours,
        },
      },
    });
  };

  const handleNumberChange = (raw: string) => {
    const parsed = parseInt(raw, 10);
    setDurationNumber(Number.isFinite(parsed) && parsed >= 0 ? parsed : 0);
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
          value={String(durationNumber)}
          onChange={handleNumberChange}
          className="w-[90px]"
        />
        <Select
          value={durationUnit}
          onValueChange={(value) => setDurationUnit(value as DurationUnit)}
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

      {dirty && !valid && (
        <FieldError>Enter a duration of at least one hour.</FieldError>
      )}
      {update.isError && <FieldError>{update.error.message}</FieldError>}

      <RowSave visible={dirty}>
        <RequireScope scope="project:write" level="component">
          {({ disabled }) => (
            <Button
              variant="primary"
              size="md"
              disabled={disabled || !valid || update.isPending}
              onClick={handleSave}
            >
              {update.isPending && (
                <Button.LeftIcon>
                  <Loader2 aria-hidden="true" className="size-4 animate-spin" />
                </Button.LeftIcon>
              )}
              <Button.Text>{update.isPending ? "Saving" : "Save"}</Button.Text>
            </Button>
          )}
        </RequireScope>
      </RowSave>
    </AuthRow>
  );
}
