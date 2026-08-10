import { useSessionData } from "@/contexts/Auth";
import { useSdkClient } from "@/contexts/Sdk";
import { cn } from "@/lib/utils";
import { AUTH_BUTTON_CLASSES } from "@/pages/login/components/auth-constants";
import { AuthShell } from "@/pages/login/components/auth-shell";
import { useState } from "react";

interface SwitchOrgProps {
  gate?: boolean;
}

interface OrgRowData {
  id: string;
  name: string;
  slug: string;
  projects: readonly unknown[];
}

// One selectable organization row: avatar square with the org initial, name +
// slug/project-count meta, and either the pinned "Current" chip or a checkmark
// when selected ("2C Switch organization" frame in the design project).
function OrgRow({
  org,
  current,
  selected,
  onSelect,
}: {
  org: OrgRowData;
  current: boolean;
  selected: boolean;
  onSelect: () => void;
}) {
  const displayName = org.name || org.slug;
  const projectCount = org.projects.length;

  return (
    <button
      type="button"
      disabled={current}
      onClick={onSelect}
      className={cn(
        "flex w-full items-center gap-3 border px-3.5 py-3 text-left transition-colors",
        current
          ? "cursor-default border-[var(--edge)] bg-[var(--surface)]"
          : selected
            ? "border-[var(--cta)] bg-[hsl(0,0%,97%)]"
            : "border-[var(--edge)] bg-[var(--card)] hover:border-[hsl(0,0%,60%)]",
      )}
    >
      <span
        className={cn(
          "auth-mono-text flex h-7 w-7 flex-none items-center justify-center text-[12px]",
          current || selected
            ? "bg-[var(--cta)] text-white"
            : "bg-[var(--edge-soft)] text-[var(--muted-strong)]",
        )}
      >
        {displayName.charAt(0).toUpperCase()}
      </span>
      <span className="flex flex-1 flex-col gap-px">
        <span className="text-[15px]">{displayName}</span>
        <span className="auth-mono-text text-[11px] text-[var(--muted)]">
          {org.slug} · {projectCount}{" "}
          {projectCount === 1 ? "project" : "projects"}
        </span>
      </span>
      {current && (
        <span className="auth-mono rounded-full bg-[var(--moss)] px-2.5 py-[3px] text-[10px] text-white">
          Current
        </span>
      )}
      {selected && !current && (
        <span className="auth-mono-text text-[13px] text-black">✓</span>
      )}
    </button>
  );
}

export default function SwitchOrg({
  gate = false,
}: SwitchOrgProps): JSX.Element {
  const client = useSdkClient();
  const { session } = useSessionData();

  const currentOrgId = session?.activeOrganizationId;
  const allOrgs = session?.organizations ?? [];
  // The gate frames the list as the way through, so the blocked org only
  // appears in the no-access banner; the switcher pins it first instead.
  const orgs = gate
    ? allOrgs.filter((org) => org.id !== currentOrgId)
    : [...allOrgs].sort(
        (a, b) => Number(b.id === currentOrgId) - Number(a.id === currentOrgId),
      );

  const [selectedOrgId, setSelectedOrgId] = useState<string>("");
  const [isSwitching, setIsSwitching] = useState(false);

  const handleSwitch = async () => {
    if (!selectedOrgId || selectedOrgId === currentOrgId) return;
    setIsSwitching(true);
    try {
      await client.auth.switchScopes({ organizationId: selectedOrgId });
      window.location.replace("/");
    } finally {
      setIsSwitching(false);
    }
  };

  const handleLogout = async () => {
    await client.auth.logout();
    window.location.href = "/login";
  };

  const currentOrg = allOrgs.find((org) => org.id === currentOrgId);
  const currentOrgName =
    currentOrg?.name || currentOrg?.slug || "This organization";

  return (
    <AuthShell
      page={gate ? "Organization access" : "Switch organization"}
      contentClassName="max-w-[400px]"
      showTerms={false}
      headerAction={
        <button
          type="button"
          onClick={() => void handleLogout()}
          className="auth-mono text-[13px] leading-none text-[var(--muted)] transition-colors hover:text-black"
        >
          Log out
        </button>
      }
    >
      <div className="text-center">
        <p className="text-[16px] tracking-[0.0025em]">
          {gate
            ? `${currentOrgName} doesn't have platform access.`
            : "Switch organization."}
        </p>
        <p className="mt-1.5 text-[14px] tracking-[0.0025em] text-[var(--muted-strong)]">
          {gate
            ? "Switch to another organization to continue, or contact your admin."
            : "Select which organization you'd like to work in."}
        </p>
      </div>

      {gate && (
        <div className="flex w-full items-center gap-2.5 border border-[var(--ember)] bg-[hsl(23,96%,97%)] px-3.5 py-2.5">
          <span className="auth-mono flex-none rounded-full bg-[var(--ember)] px-2.5 py-[3px] text-[10px] text-black">
            No access
          </span>
          <span className="auth-mono-text text-[12px] text-[var(--muted-strong)]">
            {currentOrg?.slug ?? "this organization"} · MCP platform not enabled
          </span>
        </div>
      )}

      <div className="flex w-full flex-col gap-2">
        {orgs.map((org) => (
          <OrgRow
            key={org.id}
            org={org}
            current={org.id === currentOrgId}
            selected={org.id === selectedOrgId}
            onSelect={() => setSelectedOrgId(org.id)}
          />
        ))}
      </div>

      <button
        type="button"
        onClick={() => void handleSwitch()}
        disabled={!selectedOrgId || isSwitching}
        className={cn(
          AUTH_BUTTON_CLASSES,
          "w-full disabled:cursor-not-allowed disabled:opacity-50",
        )}
      >
        {isSwitching ? "Switching…" : "Switch organization"}
      </button>
    </AuthShell>
  );
}
