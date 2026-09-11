import type { ReactNode } from "react";
import { EyeOff } from "lucide-react";
import { Link } from "react-router";
import { RequireScope } from "@/components/require-scope";
import { Button } from "@/components/ui/Button";
import { Stack } from "@/components/ui/Stack";
import { Text } from "@/components/ui/Text";
import { SimpleTooltip } from "@/components/ui/Tooltip";
import { useOrgRoutes } from "@/routes";
import {
  SESSION_AUDITOR_ROLE_NAME,
  useSessionAuditAccess,
  type SessionAuditAccess,
} from "./session-audit-access";
import { RemoveSessionAuditAccessButton } from "./session-audit-revoke";

const SETTING_TITLE = "Temporarily enable chat access";
const ALREADY_HELD = "You already have the chat:read permission";

type SettingMode = "hidden" | "scim" | "holding" | "offer";

/**
 * Holding a role that reads sessions wins over already having `chat:read`:
 * the role was taken here, so the way to give it back belongs here too. One
 * whose grants were edited away reads nothing, so the offer stands, and
 * taking it repairs the role. Both halves come from the roles query, so they
 * load together — deciding on `canReadSessions` would flash the offer at a
 * holder while grants were still in flight.
 */
function settingMode(access: SessionAuditAccess): SettingMode {
  if (access.scimManaged) return "scim";
  if (access.holdsRole && access.roleReadsSessions) return "holding";
  if (!access.available) return "hidden";
  return "offer";
}

/**
 * Greys a control out while leaving it hoverable, so the tooltip can say why.
 * The `disabled` attribute suppresses the mouse events the tooltip needs —
 * the same reason `RequireScope`'s component gate wraps rather than disables.
 */
function HeldBack({
  reason,
  children,
}: {
  reason: string | undefined;
  children: ReactNode;
}): JSX.Element {
  if (!reason) return <>{children}</>;

  return (
    <SimpleTooltip tooltip={reason}>
      <span
        className="inline-flex cursor-not-allowed opacity-50 **:cursor-not-allowed"
        onClickCapture={(event) => {
          event.preventDefault();
          event.stopPropagation();
        }}
      >
        {children}
      </span>
    </SimpleTooltip>
  );
}

/** The same row the logging switch sits in, with a button instead. */
function SettingRow({
  description,
  control,
}: {
  description: ReactNode;
  control: ReactNode;
}): JSX.Element {
  return (
    <div className="border-border bg-card border p-4">
      <Stack direction="horizontal" justify="space-between" align="center">
        <Stack gap={1}>
          <Stack direction="horizontal" align="center" gap={2}>
            <EyeOff className="text-muted-foreground h-4 w-4" />
            <Text variant="body" className="font-medium">
              {SETTING_TITLE}
            </Text>
          </Stack>
          <Text variant="body" className="text-muted-foreground ml-6 text-sm">
            {description}
          </Text>
        </Stack>
        <div className="shrink-0">{control}</div>
      </Stack>
    </div>
  );
}

/**
 * Sessions are private by default, `chat:read` being a separate permission no
 * system role holds. An inference hook attributes a conversation to whatever
 * email Claude reports, so the admin running setup may not be able to see the
 * one they just sent. This lends them the permission for as long as that
 * takes: grants are read per request, so the next poll runs under it.
 */
export function SessionAuditAccessSetting(): JSX.Element | null {
  const access = useSessionAuditAccess();
  const orgRoutes = useOrgRoutes();
  const mode = settingMode(access);

  if (mode === "hidden") return null;

  if (mode === "holding") {
    return (
      <SettingRow
        description="Chat access is on for you. Disable it once traffic is confirmed below."
        control={
          <RequireScope scope="org:admin" level="component">
            <RemoveSessionAuditAccessButton
              access={access}
              label="Disable"
              pendingLabel="Disabling…"
            />
          </RequireScope>
        }
      />
    );
  }

  // Under directory sync a membership written here is replaced on the next
  // reconciliation, so the role is created and the mapping is asked for where
  // it will hold.
  if (mode === "scim") {
    return (
      <SettingRow
        description={
          <>
            Map a directory group to {SESSION_AUDITOR_ROLE_NAME} in{" "}
            <Link
              to={orgRoutes.identity.href()}
              className="underline underline-offset-2"
            >
              Identity → SCIM → Configure
            </Link>
            .
          </>
        }
        control={
          <RequireScope scope="org:admin" level="component">
            <Button
              variant="secondary"
              size="sm"
              disabled={access.isPending || access.roleExists}
              onClick={access.ensureRole}
            >
              {access.roleExists ? "Created" : "Create role"}
            </Button>
          </RequireScope>
        }
      />
    );
  }

  return (
    <SettingRow
      description="We don't give admins chat access by default. Enable it temporarily here, in order to validate your setup."
      control={
        <RequireScope scope="org:admin" level="component">
          <HeldBack reason={access.canReadSessions ? ALREADY_HELD : undefined}>
            <Button
              size="sm"
              disabled={access.isPending}
              onClick={access.grant}
            >
              {access.isPending ? "Enabling…" : "Enable"}
            </Button>
          </HeldBack>
        </RequireScope>
      }
    />
  );
}
