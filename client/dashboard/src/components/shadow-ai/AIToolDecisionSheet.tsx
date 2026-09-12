import { Alert, AlertDescription, AlertTitle } from "@/components/ui/Alert";
import { Button } from "@/components/ui/Button";
import { RadioGroup, RadioGroupItem } from "@/components/ui/RadioGroup";
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/Sheet";
import { Text } from "@/components/ui/Text";
import { TextArea } from "@/components/ui/Textarea";
import type { AIDetection } from "@gram/client/models/components/aidetection.js";
import { invalidateAllAiDetections } from "@gram/client/react-query/aiDetections.js";
import { useSetAIToolDecisionMutation } from "@gram/client/react-query/setAIToolDecision.js";
import { useQueryClient } from "@tanstack/react-query";
import { useEffect, useState } from "react";
import { toast } from "sonner";

type Decision = "unreviewed" | "approved" | "blocked";

const OPTIONS: ReadonlyArray<{
  value: Decision;
  label: string;
  description: string;
}> = [
  {
    value: "approved",
    label: "Approved",
    description: "The tool may reach this organization's MCP gateway.",
  },
  {
    value: "blocked",
    label: "Blocked",
    description:
      "The tool is refused when it authenticates, on every MCP server in the organization.",
  },
  {
    value: "unreviewed",
    label: "Unreviewed",
    description: "Clear the decision. Nothing is enforced.",
  },
];

export function AIToolDecisionSheet({
  detection,
  open,
  onOpenChange,
}: {
  detection: AIDetection | null;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}): JSX.Element {
  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent className="flex w-[520px] flex-col gap-0 sm:max-w-[520px]">
        {open && detection ? (
          <DecisionForm
            key={detection.targetId}
            detection={detection}
            onClose={() => onOpenChange(false)}
          />
        ) : null}
      </SheetContent>
    </Sheet>
  );
}

function DecisionForm({
  detection,
  onClose,
}: {
  detection: AIDetection;
  onClose: () => void;
}): JSX.Element {
  const queryClient = useQueryClient();
  const [decision, setDecision] = useState<Decision>(
    (detection.access.decision as Decision) ?? "unreviewed",
  );
  const [rationale, setRationale] = useState(detection.access.rationale ?? "");
  const mutation = useSetAIToolDecisionMutation({
    onSuccess: () => {
      void invalidateAllAiDetections(queryClient);
      toast.success(`Access decision saved for ${detection.displayName}`);
      onClose();
    },
    onError: () => {
      toast.error("The decision could not be saved. Try again.");
    },
  });

  useEffect(() => {
    setDecision((detection.access.decision as Decision) ?? "unreviewed");
    setRationale(detection.access.rationale ?? "");
  }, [detection]);

  const submit = (event: React.FormEvent<HTMLFormElement>): void => {
    event.preventDefault();
    mutation.mutate({
      request: {
        setAIToolDecisionRequestBody: {
          targetId: detection.targetId,
          decision,
          rationale: rationale.trim() === "" ? undefined : rationale.trim(),
        },
      },
    });
  };

  return (
    <form onSubmit={submit} className="flex min-h-0 flex-1 flex-col">
      <SheetHeader className="px-6 pt-6 pb-0">
        <SheetTitle className="text-lg font-semibold">
          Decide access for {detection.displayName}
        </SheetTitle>
        <SheetDescription>
          The decision applies to every MCP server in this organization.
        </SheetDescription>
      </SheetHeader>

      <div className="flex min-h-0 flex-1 flex-col gap-4 overflow-y-auto px-6 py-6">
        {/* An admin blocking a tool deserves to know, at the moment they do
            it, that the failure their colleagues will hit is a dead end: MCP
            clients pick their credential once at discovery and do not fall
            back when authorization refuses them. */}
        {decision === "blocked" && detection.access.enforceable ? (
          <Alert variant="warning" dismissible={false}>
            <AlertTitle>Users cannot work around this</AlertTitle>
            <AlertDescription>
              {detection.displayName} will fail to authenticate with an error
              its client cannot recover from. The error carries a link to
              request access.
            </AlertDescription>
          </Alert>
        ) : null}
        {/* Not a warning about a weak block — there is no block to record.
            The server refuses a decision it could never carry out, and
            saying so before the attempt beats an error afterwards. */}
        {!detection.access.enforceable ? (
          <Alert variant="info" dismissible={false}>
            <AlertTitle>No decision can be recorded for this tool</AlertTitle>
            <AlertDescription>
              {detection.displayName} publishes no client ID metadata document,
              so Gram cannot recognise it at the gateway and has no way to allow
              or refuse it. It stays unreviewed. Add a document to its scan
              target to make a decision possible.
            </AlertDescription>
          </Alert>
        ) : null}

        <RadioGroup
          value={decision}
          onValueChange={(value) => setDecision(value as Decision)}
          className="gap-3"
        >
          {OPTIONS.map((option) => (
            <label
              key={option.value}
              className="flex cursor-pointer items-start gap-3"
            >
              <RadioGroupItem value={option.value} className="mt-1" />
              <span className="space-y-0.5">
                <Text variant="small" className="font-medium">
                  {option.label}
                </Text>
                <Text muted small className="text-xs">
                  {option.description}
                </Text>
              </span>
            </label>
          ))}
        </RadioGroup>

        <div className="space-y-2">
          <Text variant="small" className="font-medium">
            Rationale
          </Text>
          <TextArea
            value={rationale}
            onChange={setRationale}
            placeholder="Why this decision was made. Shown beside it and kept in the audit trail."
            rows={4}
          />
        </div>
      </div>

      <SheetFooter className="flex-row items-center justify-end gap-2 border-t px-6 py-4">
        <Button type="button" variant="secondary" onClick={onClose}>
          Cancel
        </Button>
        <Button
          type="submit"
          disabled={mutation.isPending || !detection.access.enforceable}
        >
          Save decision
        </Button>
      </SheetFooter>
    </form>
  );
}
