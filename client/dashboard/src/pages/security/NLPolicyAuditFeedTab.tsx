import { Badge } from "@/components/ui/badge";
import { Card } from "@/components/ui/card";
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/sheet";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { Type } from "@/components/ui/type";
import { useNlPoliciesListDecisions } from "@gram/client/react-query/index.js";
import type { NLPolicy } from "@gram/client/models/components/nlpolicy.js";
import type { NLPolicyDecision } from "@gram/client/models/components/nlpolicydecision.js";
import { useParams } from "react-router";
import { useState } from "react";

const decisionVariant = (
  d: NLPolicyDecision["decision"],
): "default" | "destructive" | "secondary" => {
  if (d === "BLOCK") return "destructive";
  if (d === "JUDGE_ERROR") return "secondary";
  return "default";
};

export default function NLPolicyAuditFeedTab({ policy }: { policy: NLPolicy }) {
  const { orgSlug, projectSlug } = useParams();
  const { data, isLoading } = useNlPoliciesListDecisions({
    policyId: policy.id,
  });
  const decisions = data?.decisions ?? [];
  const [selected, setSelected] = useState<NLPolicyDecision | null>(null);

  if (isLoading) {
    return (
      <Card className="p-6">
        <Type small muted>
          Loading decisions…
        </Type>
      </Card>
    );
  }

  if (decisions.length === 0) {
    return (
      <Card className="p-6">
        <Type small muted>
          No decisions recorded yet for this policy.
        </Type>
      </Card>
    );
  }

  return (
    <Card className="p-6">
      <Type small muted className="mb-4">
        Last {decisions.length} decisions, newest first.
      </Type>
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>Time</TableHead>
            <TableHead>Decision</TableHead>
            <TableHead>Tool</TableHead>
            <TableHead>Mode</TableHead>
            <TableHead>Decided by</TableHead>
            <TableHead>Reason</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {decisions.map((d) => (
            <TableRow
              key={d.id}
              onClick={() => setSelected(d)}
              className="cursor-pointer"
            >
              <TableCell className="text-xs">
                {d.createdAt.toLocaleTimeString()}
              </TableCell>
              <TableCell>
                <Badge variant={decisionVariant(d.decision)}>
                  {d.decision}
                </Badge>
              </TableCell>
              <TableCell className="font-mono text-xs">{d.toolUrn}</TableCell>
              <TableCell className="text-xs">{d.mode}</TableCell>
              <TableCell className="text-xs">{d.decidedBy}</TableCell>
              <TableCell className="max-w-md truncate text-xs">
                {d.reason ?? "—"}
              </TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
      <Sheet open={!!selected} onOpenChange={(o) => !o && setSelected(null)}>
        <SheetContent className="overflow-y-auto sm:max-w-xl">
          {selected && (
            <>
              <SheetHeader>
                <SheetTitle>Decision detail</SheetTitle>
                <SheetDescription>
                  {selected.createdAt.toLocaleString()}
                </SheetDescription>
              </SheetHeader>
              <div className="space-y-4 px-4 pb-6">
                <div className="flex flex-wrap items-center gap-2">
                  <Badge variant={decisionVariant(selected.decision)}>
                    {selected.decision}
                  </Badge>
                  <Badge variant="outline">{selected.mode}</Badge>
                  <Badge variant="outline">by {selected.decidedBy}</Badge>
                  {selected.enforced && (
                    <Badge variant="destructive">Enforced</Badge>
                  )}
                </div>
                <div>
                  <Type small muted>
                    Tool URN
                  </Type>
                  <div className="bg-muted mt-1 rounded p-2 font-mono text-xs break-all">
                    {selected.toolUrn}
                  </div>
                </div>
                {selected.reason && (
                  <div>
                    <Type small muted>
                      Reason
                    </Type>
                    <div className="bg-muted mt-1 rounded p-2 text-sm">
                      {selected.reason}
                    </div>
                  </div>
                )}
                <div className="grid grid-cols-2 gap-3 text-xs">
                  <div>
                    <Type small muted>
                      Policy version
                    </Type>
                    <div>v{selected.nlPolicyVersion}</div>
                  </div>
                  {selected.judgeLatencyMs != null && (
                    <div>
                      <Type small muted>
                        Judge latency
                      </Type>
                      <div>{selected.judgeLatencyMs}ms</div>
                    </div>
                  )}
                </div>
                {selected.sessionId && orgSlug && projectSlug && (
                  <a
                    className="text-primary text-sm underline"
                    href={`/${orgSlug}/projects/${projectSlug}/agent-sessions?sessionId=${encodeURIComponent(selected.sessionId)}`}
                  >
                    Open session →
                  </a>
                )}
              </div>
            </>
          )}
        </SheetContent>
      </Sheet>
    </Card>
  );
}
