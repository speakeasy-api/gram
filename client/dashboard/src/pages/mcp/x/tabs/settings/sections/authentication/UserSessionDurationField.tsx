import { RequireScope } from "@/components/require-scope";
import {
  Field,
  FieldDescription,
  FieldError,
  FieldLabel,
} from "@/components/ui/field";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { toastError } from "@/lib/toast-error";
import type { UserSessionIssuer } from "@gram/client/models/components/usersessionissuer.js";
import { useUpdateUserSessionIssuerMutation } from "@gram/client/react-query/updateUserSessionIssuer.js";
import { invalidateAllUserSessionIssuer } from "@gram/client/react-query/userSessionIssuer.js";
import { invalidateAllUserSessionIssuers } from "@gram/client/react-query/userSessionIssuers.js";
import { Button, Input } from "@/components/ui/moonshine";
import { useQueryClient } from "@tanstack/react-query";
import { useEffect, useState } from "react";
import { toast } from "sonner";

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
      toastError(error, "Failed to update session duration");
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

  const handleUnitChange = (value: string) => {
    setDurationUnit(value as DurationUnit);
  };

  return (
    <Field data-invalid={update.isError ? true : undefined}>
      <FieldLabel htmlFor="mcp-auth-session-duration">
        Session Duration
      </FieldLabel>
      <div className="flex flex-col gap-2 sm:flex-row sm:items-start">
        <Input
          id="mcp-auth-session-duration"
          type="number"
          min="1"
          value={String(durationNumber)}
          onChange={(e) => handleNumberChange(e.target.value)}
          className="w-[100px]"
        />
        <Select value={durationUnit} onValueChange={handleUnitChange}>
          <SelectTrigger className="w-[120px]">
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
        <RequireScope scope="mcp:write" level="component">
          <Button
            variant="primary"
            size="md"
            disabled={!dirty || !valid || update.isPending}
            onClick={handleSave}
          >
            <Button.Text>Save</Button.Text>
          </Button>
        </RequireScope>
      </div>
      <FieldDescription>
        Users authenticate with Speakeasy before using this server. Choose how
        long issued sessions stay valid before re-authentication.
      </FieldDescription>
      {update.isError && <FieldError>{update.error.message}</FieldError>}
    </Field>
  );
}
