import { GramLogo } from "@/components/gram-logo";
import { Type } from "@/components/ui/type";
import { useSessionData } from "@/contexts/Auth";
import { buildLoginRedirectURL } from "@/lib/utils";
import { useRiskAcknowledgePolicyChallengeMutation } from "@gram/client/react-query/riskAcknowledgePolicyChallenge.js";
import { useRiskDeclinePolicyChallengeMutation } from "@gram/client/react-query/riskDeclinePolicyChallenge.js";
import { useRiskGetPolicyChallengeMutation } from "@gram/client/react-query/riskGetPolicyChallenge.js";
import { Button } from "@/components/ui/button";
import { Icon } from "@/components/ui/icon";
import { Stack } from "@/components/ui/stack";
import { type ComponentProps, useEffect, useRef, useState } from "react";

type IconName = ComponentProps<typeof Icon>["name"];

const ACK_TOKEN_STORAGE_KEY = "riskPolicyChallengeAckToken";

type Outcome = "idle" | "approved" | "declined" | "error";

export function RiskPolicyChallengeAcknowledgeContent(): JSX.Element {
  const { session: sessionInfo, status: sessionStatus } = useSessionData();
  const hasSession = !!sessionInfo;
  const sessionLoading = sessionStatus === "pending";
  const ackToken = getAckToken();
  const [outcome, setOutcome] = useState<Outcome>("idle");

  // The ack token is a bearer credential delivered in the URL fragment (see
  // GeneratePolicyAckURL on the server) so it never hits server logs. Force
  // no-referrer so it also can't leak via the Referer header.
  useEffect(() => {
    const meta = document.createElement("meta");
    meta.name = "referrer";
    meta.content = "no-referrer";
    document.head.appendChild(meta);
    return () => {
      document.head.removeChild(meta);
    };
  }, []);

  // Move the token out of the URL into sessionStorage and strip the fragment so
  // it isn't left in a shared screen, browser history, or a copy-pasted URL.
  useEffect(() => {
    if (!ackToken) return;
    sessionStorage.setItem(ACK_TOKEN_STORAGE_KEY, ackToken);
    window.history.replaceState(
      null,
      "",
      window.location.pathname + window.location.search,
    );
  }, [ackToken]);

  const storedAckToken =
    ackToken ?? sessionStorage.getItem(ACK_TOKEN_STORAGE_KEY);

  useEffect(() => {
    // Wait for the session query to settle before deciding. Otherwise the
    // transient "no session" on first render bounces an already-authenticated
    // user to auth.login, which then drops them on their project home instead
    // of this page.
    if (!storedAckToken || sessionLoading || hasSession) return;
    window.location.href = buildLoginRedirectURL(window.location.pathname);
  }, [storedAckToken, sessionLoading, hasSession]);

  // The peek is a mutation (POST body carries the sensitive token; a query hook
  // would omit the body from its cache key and collide across tokens), so drive
  // it imperatively once and hold the result in state.
  const { mutateAsync: fetchChallenge } = useRiskGetPolicyChallengeMutation();
  const [challengeState, setChallengeState] = useState<
    "idle" | "loading" | "loaded" | "error"
  >("idle");
  const [challengeData, setChallengeData] = useState<{
    acknowledged: boolean;
    message: string;
    policyName?: string | undefined;
    toolName?: string | undefined;
  } | null>(null);

  // Fire the peek exactly once. A ref (not challengeState-in-deps + an active
  // flag) is used deliberately: setting challengeState inside the effect while
  // it is a dependency would re-run the effect, whose cleanup would flip the
  // in-flight active flag and silently drop the resolved result — leaving the
  // spinner up forever.
  const fetchedRef = useRef(false);
  const canFetch = !!storedAckToken && hasSession && outcome === "idle";
  useEffect(() => {
    if (!canFetch || fetchedRef.current) return;
    fetchedRef.current = true;
    setChallengeState("loading");
    fetchChallenge({
      request: {
        acknowledgeRiskPolicyChallengeRequestBody: {
          ackToken: storedAckToken ?? "",
        },
      },
    })
      .then((res) => {
        setChallengeData({
          acknowledged: res.acknowledged,
          message: res.message,
          policyName: res.policyName,
          toolName: res.toolName,
        });
        setChallengeState("loaded");
      })
      .catch(() => {
        setChallengeState("error");
      });
  }, [canFetch, fetchChallenge, storedAckToken]);

  const { mutateAsync: approve, isPending: approving } =
    useRiskAcknowledgePolicyChallengeMutation();
  const { mutateAsync: decline, isPending: declining } =
    useRiskDeclinePolicyChallengeMutation();

  const onApprove = async () => {
    if (!storedAckToken) return;
    try {
      await approve({
        request: {
          acknowledgeRiskPolicyChallengeRequestBody: {
            ackToken: storedAckToken,
          },
        },
      });
      sessionStorage.removeItem(ACK_TOKEN_STORAGE_KEY);
      setOutcome("approved");
    } catch {
      setOutcome("error");
    }
  };

  const onDecline = async () => {
    if (!storedAckToken) return;
    try {
      await decline({
        request: {
          acknowledgeRiskPolicyChallengeRequestBody: {
            ackToken: storedAckToken,
          },
        },
      });
    } catch {
      // Best-effort: even if the decline call fails, the link stays unapproved.
    }
    sessionStorage.removeItem(ACK_TOKEN_STORAGE_KEY);
    setOutcome("declined");
  };

  return (
    <div className="bg-background flex min-h-screen w-full flex-col items-center justify-center p-8">
      <Stack gap={8} align="center" className="w-full max-w-md">
        <GramLogo className="w-25" variant="vertical" />
        {renderBody()}
      </Stack>
    </div>
  );

  function renderBody(): JSX.Element {
    if (outcome === "approved") return <ApprovedView />;
    if (outcome === "declined") return <DeclinedView />;
    if (outcome === "error") {
      return <RetryableError onRetry={() => setOutcome("idle")} />;
    }
    if (!storedAckToken) return <ExpiredView />;
    if (sessionLoading) return <SpinnerView label="Loading..." />;
    if (!hasSession) return <SpinnerView label="Redirecting to sign in..." />;
    if (challengeState === "idle" || challengeState === "loading") {
      return <SpinnerView label="Loading request..." />;
    }
    if (challengeState === "error" || !challengeData) return <ExpiredView />;
    if (challengeData.acknowledged) return <AlreadyApprovedView />;

    return (
      <ChallengeReview
        policyName={challengeData.policyName}
        toolName={challengeData.toolName}
        message={challengeData.message}
        approving={approving}
        declining={declining}
        onApprove={() => void onApprove()}
        onDecline={() => void onDecline()}
      />
    );
  }
}

function getAckToken(): string | null {
  const hashParams = new URLSearchParams(
    window.location.hash.replace(/^#/, ""),
  );
  return hashParams.get("ack_token") ?? hashParams.get("token");
}

function ChallengeReview({
  policyName,
  toolName,
  message,
  approving,
  declining,
  onApprove,
  onDecline,
}: {
  policyName?: string | undefined;
  toolName?: string | undefined;
  message: string;
  approving: boolean;
  declining: boolean;
  onApprove: () => void;
  onDecline: () => void;
}) {
  const busy = approving || declining;
  return (
    <Stack gap={5} align="center" className="w-full">
      <Stack gap={2} align="center">
        <div className="bg-warning/10 flex h-11 w-11 items-center justify-center rounded-full">
          <Icon name="shield-alert" className="text-warning h-5 w-5" />
        </div>
        <Type variant="subheading" className="text-center">
          Approval required
        </Type>
        <Type muted small className="text-center">
          {policyName
            ? `This action was held for review by risk policy "${policyName}".`
            : "This action was held for review by a risk policy."}
          {toolName ? ` Tool: ${toolName}.` : ""}
        </Type>
      </Stack>

      <div className="border-border bg-muted/40 w-full rounded-md border p-3">
        <Type
          small
          className="text-foreground/90 whitespace-pre-wrap break-words font-mono"
        >
          {message}
        </Type>
      </div>

      <Stack direction="horizontal" gap={2} className="w-full justify-center">
        <Button variant="secondary" onClick={onDecline} disabled={busy}>
          <Button.Text>{declining ? "Denying..." : "Deny"}</Button.Text>
        </Button>
        <Button variant="primary" onClick={onApprove} disabled={busy}>
          <Button.Text>{approving ? "Approving..." : "Approve"}</Button.Text>
        </Button>
      </Stack>
      <Type muted small className="text-center">
        Approving lets the agent retry this exact action. Denying keeps it
        blocked.
      </Type>
    </Stack>
  );
}

function ApprovedView() {
  return (
    <StatusView
      icon="check"
      tone="primary"
      title="Approved"
      body="Return to your agent and retry the request. You can close this page."
    />
  );
}

function DeclinedView() {
  return (
    <StatusView
      icon="x"
      tone="muted"
      title="Denied"
      body="The request remains blocked. You can close this page."
    />
  );
}

function AlreadyApprovedView() {
  return (
    <StatusView
      icon="check"
      tone="primary"
      title="Already approved"
      body="This challenge was already approved. Return to your agent and retry the request."
    />
  );
}

function ExpiredView() {
  return (
    <StatusView
      icon="circle-x"
      tone="destructive"
      title="Link expired"
      body="This approval link is no longer valid. Try the action again in your agent to generate a new one."
    />
  );
}

function RetryableError({ onRetry }: { onRetry: () => void }) {
  return (
    <Stack gap={3} align="center">
      <StatusView
        icon="circle-x"
        tone="destructive"
        title="Something went wrong"
        body="We could not record your decision. Check your connection and try again."
      />
      <Button variant="secondary" onClick={onRetry}>
        <Button.LeftIcon>
          <Icon name="refresh-cw" className="h-4 w-4" />
        </Button.LeftIcon>
        <Button.Text>Try again</Button.Text>
      </Button>
    </Stack>
  );
}

function SpinnerView({ label }: { label: string }) {
  return (
    <Stack gap={3} align="center">
      <Icon
        name="loader-circle"
        className="text-muted-foreground h-6 w-6 animate-spin"
      />
      <Type muted small className="text-center">
        {label}
      </Type>
    </Stack>
  );
}

function StatusView({
  icon,
  tone,
  title,
  body,
}: {
  icon: IconName;
  tone: "primary" | "muted" | "destructive";
  title: string;
  body: string;
}) {
  const ring =
    tone === "primary"
      ? "bg-primary/10"
      : tone === "destructive"
        ? "bg-destructive/10"
        : "bg-muted";
  const fg =
    tone === "primary"
      ? "text-primary"
      : tone === "destructive"
        ? "text-destructive"
        : "text-muted-foreground";
  return (
    <Stack gap={3} align="center">
      <div
        className={`flex h-11 w-11 items-center justify-center rounded-full ${ring}`}
      >
        <Icon name={icon} className={`h-5 w-5 ${fg}`} />
      </div>
      <Stack gap={1} align="center">
        <Type variant="subheading" className="text-center">
          {title}
        </Type>
        <Type muted small className="text-center">
          {body}
        </Type>
      </Stack>
    </Stack>
  );
}
