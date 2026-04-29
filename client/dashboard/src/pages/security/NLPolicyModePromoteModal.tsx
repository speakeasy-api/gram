import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Dialog } from "@/components/ui/dialog";
import { Type } from "@/components/ui/type";
import { useNlPoliciesListDecisions } from "@gram/client/react-query/index.js";
import type { NLPolicy } from "@gram/client/models/components/nlpolicy.js";

type Mode = NLPolicy["mode"];

export default function NLPolicyModePromoteModal({
  policy,
  onClose,
  onConfirm,
}: {
  policy: NLPolicy;
  onClose: () => void;
  onConfirm: (mode: Mode) => void;
}) {
  const since = new Date(Date.now() - 7 * 24 * 60 * 60 * 1000);
  const { data } = useNlPoliciesListDecisions({
    policyId: policy.id,
    since,
  });
  const decisions = data?.decisions ?? [];
  const wouldBlock = decisions.filter((d) => d.decision === "BLOCK").length;
  const wouldAllow = decisions.filter((d) => d.decision === "ALLOW").length;
  const judgeErr = decisions.filter((d) => d.decision === "JUDGE_ERROR").length;

  const confirmAndClose = (mode: Mode) => {
    onConfirm(mode);
    onClose();
  };

  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <Dialog.Content>
        <Dialog.Header>
          <Dialog.Title>Change mode for "{policy.name}"</Dialog.Title>
          <Dialog.Description>
            Currently <strong>{policy.mode}</strong>.
          </Dialog.Description>
        </Dialog.Header>

        <div className="space-y-3">
          <Type small muted>
            In the last 7 days the policy produced these decisions:
          </Type>
          <div className="flex flex-wrap gap-2">
            <Badge variant="destructive">Would BLOCK: {wouldBlock}</Badge>
            <Badge>Would ALLOW: {wouldAllow}</Badge>
            <Badge variant="secondary">JUDGE_ERROR: {judgeErr}</Badge>
          </div>
          <Type small muted>
            Once enforcement is on, blocks become 403s for MCP clients and
            sessions may be quarantined.
          </Type>
        </div>

        <Dialog.Footer className="gap-2">
          <Button variant="outline" onClick={onClose}>
            Cancel
          </Button>
          <Button
            variant="secondary"
            onClick={() => confirmAndClose("disabled")}
          >
            Disable
          </Button>
          <Button variant="secondary" onClick={() => confirmAndClose("audit")}>
            Audit
          </Button>
          <Button onClick={() => confirmAndClose("enforce")}>Enforce</Button>
        </Dialog.Footer>
      </Dialog.Content>
    </Dialog>
  );
}
