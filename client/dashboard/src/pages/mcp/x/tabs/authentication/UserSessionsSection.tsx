import { RequireScope } from "@/components/require-scope";
import { Heading } from "@/components/ui/heading";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Type } from "@/components/ui/type";
import type { UserSessionIssuer } from "@gram/client/models/components";
import {
  invalidateAllUserSessionIssuer,
  invalidateAllUserSessionIssuers,
  useUpdateUserSessionIssuerMutation,
} from "@gram/client/react-query/index.js";
import { Alert, Button, Stack } from "@speakeasy-api/moonshine";
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

// Pick the largest whole unit that divides the hours value evenly. 168 → 1
// week, 48 → 2 days, 36 → 36 hours. Keeps the inputs readable when the row
// loads a saved value.
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

export function UserSessionsSection({
  userSessionIssuer,
}: {
  userSessionIssuer: UserSessionIssuer;
}) {
  const queryClient = useQueryClient();
  const initialSplit = splitIntoUnit(userSessionIssuer.sessionDurationHours);
  const [durationNumber, setDurationNumber] = useState(initialSplit.number);
  const [durationUnit, setDurationUnit] = useState<DurationUnit>(
    initialSplit.unit,
  );

  // Resync from the saved record whenever it changes — post-save refetch,
  // switching MCP servers, etc. Safe because dirty edits can't have produced
  // a new server value without going through handleSave first.
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

  const handleUnitChange = (value: string) => {
    setDurationUnit(value as DurationUnit);
  };

  return (
    <section>
      <Heading variant="h4" className="mb-3">
        User Sessions
      </Heading>
      <Type muted small className="mb-4">
        The platform issues user sessions for this MCP server. Attach remote
        identity providers to delegate server functionality authentication.
      </Type>
      <Stack gap={4}>
        <Stack gap={2}>
          <Label className="text-muted-foreground text-xs">
            Session Duration
          </Label>
          <Stack direction="horizontal" gap={2} align="center">
            <Input
              type="number"
              min="1"
              value={String(durationNumber)}
              onChange={handleNumberChange}
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
                disabled={!dirty || !valid || update.isPending}
                onClick={handleSave}
              >
                <Button.Text>Save</Button.Text>
              </Button>
            </RequireScope>
          </Stack>
          <Type muted small>
            How long an issued user session stays valid before the user must
            re-authenticate.
          </Type>
        </Stack>

        {update.isError && (
          <Alert variant="error" dismissible={false}>
            {update.error.message}
          </Alert>
        )}
      </Stack>
    </section>
  );
}
