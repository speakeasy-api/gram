import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { Checkbox } from "@/components/ui/checkbox";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { Type } from "@/components/ui/type";
import {
  invalidateAllNlPoliciesListSessionVerdicts,
  useNlPoliciesClearSessionVerdictMutation,
  useNlPoliciesListSessionVerdicts,
} from "@gram/client/react-query/index.js";
import type { NLPolicy } from "@gram/client/models/components/nlpolicy.js";
import { useQueryClient } from "@tanstack/react-query";
import { useParams } from "react-router";
import { useState } from "react";

export default function NLPolicyQuarantinesTab({
  policy,
}: {
  policy: NLPolicy;
}) {
  const queryClient = useQueryClient();
  const { orgSlug, projectSlug } = useParams();
  const [activeOnly, setActiveOnly] = useState(true);
  const { data, isLoading } = useNlPoliciesListSessionVerdicts({
    policyId: policy.id,
    activeOnly,
  });
  const verdicts = data?.verdicts ?? [];

  const clear = useNlPoliciesClearSessionVerdictMutation({
    onSuccess: () => invalidateAllNlPoliciesListSessionVerdicts(queryClient),
  });

  return (
    <Card className="space-y-4 p-6">
      <label className="flex items-center gap-2 text-sm">
        <Checkbox
          checked={activeOnly}
          onCheckedChange={(v) => setActiveOnly(v === true)}
        />
        Active only
      </label>

      {isLoading ? (
        <Type small muted>
          Loading session verdicts…
        </Type>
      ) : verdicts.length === 0 ? (
        <Type small muted>
          No quarantined sessions for this policy.
        </Type>
      ) : (
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Session</TableHead>
              <TableHead>Quarantined</TableHead>
              <TableHead>Reason</TableHead>
              <TableHead className="w-[100px]" />
            </TableRow>
          </TableHeader>
          <TableBody>
            {verdicts.map((v) => (
              <TableRow key={v.id}>
                <TableCell className="font-mono text-xs">
                  {orgSlug && projectSlug ? (
                    <a
                      href={`/${orgSlug}/projects/${projectSlug}/agent-sessions?sessionId=${encodeURIComponent(v.sessionId)}`}
                      className="text-primary underline"
                    >
                      {v.sessionId}
                    </a>
                  ) : (
                    v.sessionId
                  )}
                </TableCell>
                <TableCell className="text-xs">
                  {v.quarantinedAt ? v.quarantinedAt.toLocaleString() : "—"}
                </TableCell>
                <TableCell className="max-w-md text-xs">
                  {v.reason ?? "—"}
                </TableCell>
                <TableCell>
                  {!v.clearedAt && (
                    <Button
                      size="sm"
                      variant="outline"
                      onClick={() =>
                        clear.mutate({
                          request: {
                            clearSessionVerdictRequestBody: {
                              verdictId: v.id,
                            },
                          },
                        })
                      }
                      disabled={clear.isPending}
                    >
                      Clear
                    </Button>
                  )}
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      )}
    </Card>
  );
}
