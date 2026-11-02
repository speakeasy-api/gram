import type {
  CoverageBucket,
  ManagedDevice,
  PolicyState,
} from "@gram/client/models/components/manageddevice.js";
import {
  CheckCircle2,
  CircleSlash,
  HelpCircle,
  Laptop,
  MailX,
  MonitorX,
  ShieldCheck,
  ShieldOff,
  TriangleAlert,
  UserX,
} from "lucide-react";

// Coverage joins the MDM-reported fleet against device agent heartbeats, in
// one of two modes the server selects per org. Under device-level matching
// (hardware serial) an active heartbeat proves THIS machine ran the agent.
// Under user-level matching it proves only "this device's assigned user runs
// the agent somewhere" — never "this device is monitored".
//
// Copy below must hold under BOTH modes, because the client is not told which
// one produced the counts. That is why the agent_active detail speaks of "the
// device agent" without asserting whose machine it ran on, while
// agent_other_device — which only device-level matching can produce — is free
// to be specific.
export type CoverageDisplay = {
  label: string;
  variant: "destructive" | "neutral" | "success" | "warning";
  icon: React.ComponentType<{ className?: string }>;
  detail: string;
};

const FALLBACK: CoverageDisplay = {
  label: "Unknown",
  variant: "neutral",
  icon: HelpCircle,
  detail: "Unrecognized coverage state.",
};

export const BUCKETS: Record<CoverageBucket, CoverageDisplay> = {
  agent_active: {
    label: "Agent active",
    variant: "success",
    icon: CheckCircle2,
    detail: "The device agent reported a heartbeat within the active window.",
  },
  agent_stale: {
    label: "Agent stale",
    variant: "warning",
    icon: TriangleAlert,
    detail:
      "The agent has gone quiet while the MDM still sees the device checking in — the drift case. The agent may be disabled or removed.",
  },
  agent_other_device: {
    label: "Agent on another device",
    variant: "warning",
    icon: Laptop,
    detail:
      "The assigned user runs the device agent, but not on this machine. Install it here — unlike No agent, the user is already onboarded.",
  },
  no_agent: {
    label: "No agent",
    variant: "destructive",
    icon: CircleSlash,
    detail:
      "The assigned user has never reported a device agent heartbeat. This device's user is not running the agent.",
  },
  no_email: {
    label: "No email in MDM",
    variant: "neutral",
    icon: MailX,
    detail:
      "The MDM record has no assigned-user email, so agent coverage cannot be attested. Set the user's email on the device record in your MDM.",
  },
  unresolved_email: {
    label: "Unknown user",
    variant: "neutral",
    icon: HelpCircle,
    detail:
      "The MDM-assigned email does not match any member of this organization.",
  },
  missing: {
    label: "Missing from MDM",
    variant: "neutral",
    icon: MonitorX,
    detail:
      "The device stopped appearing in the MDM inventory. It may have been unenrolled or retired.",
  },
};

// The policy states split agent_active: the agent runs, and this says whether
// it enforces the policy Speakeasy last sent. "Not reporting" stays neutral
// rather than a warning, because every agent built before state reports reads
// that way until it updates, and a fleet-wide warning on rollout day would
// say something is broken when nothing changed.
export const POLICY_STATES: Record<PolicyState, CoverageDisplay> = {
  attested: {
    label: "Enforcing policy",
    variant: "success",
    icon: ShieldCheck,
    detail:
      "The agent reported that it applied the policy Speakeasy last sent it.",
  },
  outdated: {
    label: "Policy behind",
    variant: "warning",
    icon: TriangleAlert,
    detail:
      "The agent is running, but it has not reported applying the policy Speakeasy last sent it. A tool that cannot apply the new one is usually what holds it back.",
  },
  unreported: {
    label: "No policy report",
    variant: "neutral",
    icon: HelpCircle,
    detail:
      "The agent is running but has not said which policy it enforces in the last hour. Agent versions without state reports never do; update the agent.",
  },
  halted: {
    label: "Enforcement paused",
    variant: "destructive",
    icon: ShieldOff,
    detail:
      "The agent reported running a version your organization blocked, so it stopped enforcing policy. Update that agent to resume.",
  },
};

export const UNMANAGED_USERS: CoverageDisplay = {
  label: "Agent users w/o device",
  variant: "neutral",
  icon: UserX,
  detail:
    "Users reporting device agent heartbeats whose email matches no managed device in the MDM inventory.",
};

// One key per thing the Coverage column can say: an agent_active device is
// shown by its policy state when the server sent one.
export type CoverageKey = Exclude<CoverageBucket, "agent_active"> | PolicyState;

export function deviceCoverageKey(
  device: ManagedDevice,
): CoverageKey | "agent_active" {
  if (device.coverageBucket === "agent_active" && device.policyState) {
    return device.policyState;
  }
  return device.coverageBucket;
}

// The coverage filter matches what the Coverage column shows, and
// agent_active also matches every active device, whatever its policy state.
export function matchesCoverageFilter(
  device: ManagedDevice,
  selected: readonly string[] | undefined,
): boolean {
  if (!selected || selected.length === 0) return true;
  return (
    selected.includes(deviceCoverageKey(device)) ||
    (device.coverageBucket === "agent_active" &&
      selected.includes("agent_active"))
  );
}

// Safe lookup: the SDK enums are open, so a value from a newer server must
// degrade to a neutral badge, not a crash.
export function coverageDisplay(key: string): CoverageDisplay {
  // Own keys only, so a value like "constructor" falls back too.
  const policy = Object.hasOwn(POLICY_STATES, key)
    ? POLICY_STATES[key as PolicyState]
    : undefined;
  const bucket = Object.hasOwn(BUCKETS, key)
    ? BUCKETS[key as CoverageBucket]
    : undefined;
  return policy ?? bucket ?? FALLBACK;
}

const TOOL_LABELS: Record<string, string> = {
  claude_code: "Claude Code",
  codex: "Codex",
  copilot: "GitHub Copilot CLI",
  cursor: "Cursor",
  gemini: "Gemini CLI",
  openclaw: "OpenClaw",
  opencode: "OpenCode",
};

export function toolLabel(tool: string): string {
  // Own keys only: an id like "constructor" must fall through, not render
  // an inherited function.
  const label = Object.hasOwn(TOOL_LABELS, tool)
    ? TOOL_LABELS[tool]
    : undefined;
  if (label) return label;
  const words = tool.replaceAll("_", " ");
  return words.charAt(0).toUpperCase() + words.slice(1);
}

const OS_LABELS: Record<string, string> = {
  darwin: "macOS",
  linux: "Linux",
  windows: "Windows",
};

export function osLabel(os: string): string {
  return (Object.hasOwn(OS_LABELS, os) ? OS_LABELS[os] : undefined) ?? os;
}

// Command K links a device with ?device=<id> and carries the device it
// matched as navigation state.
export const LINKED_DEVICE_PARAM = "device";

export type LinkedDeviceState = { device?: ManagedDevice };
