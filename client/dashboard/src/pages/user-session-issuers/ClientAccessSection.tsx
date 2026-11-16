import { AgentProviderIcon } from "@/components/agent-providers/AgentProviderIcon";
import { hasAgentProviderIcon } from "@/components/agent-providers/agent-provider-icon-kind";
import {
  FooterSaveButton,
  SettingsSection,
} from "@/components/detail/settings-section";
import { RequireScope } from "@/components/require-scope";
import { Alert } from "@/components/ui/Alert";
import { Button } from "@/components/ui/Button";
import { ConfirmDialog } from "@/components/ui/ConfirmDialog";
import { Dialog } from "@/components/ui/Dialog";
import { FieldError } from "@/components/ui/Field";
import { Label } from "@/components/ui/Label";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/Popover";
import { RadioGroup, RadioGroupItem } from "@/components/ui/RadioGroup";
import { Skeleton } from "@/components/ui/Skeleton";
import { Text } from "@/components/ui/Text";
import type { UserSessionIssuer } from "@gram/client/models/components/usersessionissuer.js";
import type { UserSessionIssuerCimdClient } from "@gram/client/models/components/usersessionissuercimdclient.js";
import { useCimdClientPresets } from "@gram/client/react-query/cimdClientPresets.js";
import { Info, Plus } from "lucide-react";
import { useEffect, useId, useMemo, useState } from "react";
import { ExplainerDialog } from "@/pages/mcp/x/tabs/settings/sections/authentication/AuthRow";
import { AddClientDialog } from "./AddClientDialog";
import { groupKnownClients, monogram, type KnownClient } from "./knownClients";
import {
  SAVE_SCOPE,
  editPathFor,
  settingsFooterHint,
  useSaveProviderSettings,
  type EditPath,
} from "./providerSettings";
import { useOrganizationClients } from "./useOrganizationClients";

type Mode = "presets" | "open";

const MODES: {
  value: Mode;
  label: string;
  explanation: string;
  when: string;
}[] = [
  {
    value: "presets",
    label: "Known clients",
    explanation:
      "Only allow clients your team has specifically allow-listed, starting from Speakeasy's platform-wide baseline of common vendors. Enforced by CIMD.",
    when: "You want to know which clients your people sign in with. Start from the vendors Speakeasy already checks and add your internal tools.",
  },
  {
    value: "open",
    label: "Any client",
    explanation:
      "Allow all clients, whether they identify themselves with CIMD or DCR.",
    when: "Your people try new or niche clients nobody has checked yet, and you are comfortable letting each person approve them at the consent screen.",
  },
];

/** The saved mode as one of this page's choices, or null when it is neither. */
function selectable(saved: string): Mode | null {
  if (saved === "presets") return "presets";
  // "reporting" is a legacy value that admits what "open" admits.
  if (saved === "open" || saved === "reporting") return "open";
  return null;
}

/**
 * Which MCP clients may identify themselves with a published document, saved
 * on its own because changing it can drop connections.
 */
export function ClientAccessSection({
  issuer,
  editableProjectId,
}: {
  issuer: UserSessionIssuer;
  /** The project whose own providers this page may change, if any. */
  editableProjectId?: string;
}): JSX.Element {
  const fieldId = useId();
  const path = editPathFor(issuer, editableProjectId);
  const readOnly = path.kind === "elsewhere";
  const saved = selectable(issuer.clientIdMetadataAdmissionMode);
  const [mode, setMode] = useState<Mode | null>(saved);
  useEffect(() => {
    setMode(selectable(issuer.clientIdMetadataAdmissionMode));
  }, [issuer.id, issuer.clientIdMetadataAdmissionMode]);

  const dirty = !readOnly && mode !== null && mode !== saved;
  const update = useSaveProviderSettings(issuer, path);

  return (
    <SettingsSection id="client-access">
      <SettingsSection.Header>
        <SettingsSection.Title>Client access</SettingsSection.Title>
        <SettingsSection.Description>
          Which MCP clients may connect through this provider.
          <ExplainerDialog title="Client ID Metadata Documents (CIMD)">
            <Text muted small className="block">
              A client can host a small public file — its name, logo and where
              it sends users after sign-in — and hand Speakeasy that URL instead
              of registering first. This setting decides whose files Speakeasy
              accepts.
            </Text>
            <Text muted small className="block">
              Clients that publish no such file register themselves
              automatically (Dynamic Client Registration). That path stays open
              to every client no matter what you pick here.
            </Text>
          </ExplainerDialog>
        </SettingsSection.Description>
      </SettingsSection.Header>
      <SettingsSection.Panel>
        <SettingsSection.Body className="space-y-4">
          {saved === null && (
            <Text small muted className="block">
              Clients can&apos;t identify themselves with a document here right
              now, so each one registers itself. Pick one to change that.
            </Text>
          )}
          <RadioGroup
            aria-label="Client access"
            value={mode ?? ""}
            onValueChange={(next) => setMode(next as Mode)}
            className="flex flex-col gap-4"
            disabled={readOnly}
          >
            {MODES.map((option) => (
              <div key={option.value} className="flex items-start gap-3">
                <RadioGroupItem
                  value={option.value}
                  id={`${fieldId}-${option.value}`}
                  className="mt-0.5"
                />
                <div className="flex min-w-0 flex-col gap-0.5">
                  <div className="flex items-center gap-1.5">
                    <Label
                      htmlFor={`${fieldId}-${option.value}`}
                      className="cursor-pointer text-sm"
                    >
                      {option.label}
                    </Label>
                    <WhenToPick label={option.label} text={option.when} />
                  </div>
                  <Text small muted className="max-w-2xl">
                    {option.explanation}
                  </Text>
                </div>
              </div>
            ))}
          </RadioGroup>
          {update.error && <FieldError>{update.error.message}</FieldError>}
        </SettingsSection.Body>

        {mode === "presets" && (
          <KnownClients
            issuer={issuer}
            path={path}
            canCheck={editableProjectId !== undefined}
          />
        )}

        {dirty && mode === "presets" && (
          <div className="border-t px-6 py-4">
            <Alert
              variant="warning"
              title="Clients not on these lists can't sign in again"
            >
              This covers clients that identify themselves with a published
              document; clients that register themselves aren&apos;t affected.
              People already connected through them stay connected until their
              session ends, then have to sign in again with a listed client.
              Enterprise Managed Auth sign-ins from them stop right away.
            </Alert>
          </div>
        )}

        <SettingsSection.Footer>
          <SettingsSection.FooterHint>
            {settingsFooterHint(path)}
          </SettingsSection.FooterHint>
          {path.kind !== "elsewhere" && (
            <SettingsSection.FooterActions>
              {dirty && (
                <Button
                  variant="tertiary"
                  size="md"
                  onClick={() => setMode(saved)}
                  disabled={update.isPending}
                >
                  <Button.Text>Discard</Button.Text>
                </Button>
              )}
              <RequireScope scope={SAVE_SCOPE[path.kind]} level="component">
                {({ disabled }) => (
                  <FooterSaveButton
                    pending={update.isPending}
                    disabled={disabled || !dirty || update.isPending}
                    onClick={() => {
                      if (mode) {
                        update.save({ clientIdMetadataAdmissionMode: mode });
                      }
                    }}
                  />
                )}
              </RequireScope>
            </SettingsSection.FooterActions>
          )}
        </SettingsSection.Footer>
      </SettingsSection.Panel>
    </SettingsSection>
  );
}

function WhenToPick({
  label,
  text,
}: {
  label: string;
  text: string;
}): JSX.Element {
  return (
    <Popover>
      <PopoverTrigger asChild>
        <button
          type="button"
          aria-label={`When to pick ${label}`}
          className="text-muted-foreground hover:text-foreground inline-flex cursor-pointer items-center"
        >
          <Info className="size-3.5" aria-hidden="true" />
        </button>
      </PopoverTrigger>
      <PopoverContent align="start" className="w-80">
        <div className="text-eyebrow mb-1">When to pick it</div>
        <Text small>{text}</Text>
      </PopoverContent>
    </Popover>
  );
}

/**
 * The clients Known clients admits: Speakeasy's catalog, then the ones this
 * organization added for this provider.
 */
function KnownClients({
  issuer,
  path,
  canCheck,
}: {
  issuer: UserSessionIssuer;
  path: EditPath;
  canCheck: boolean;
}): JSX.Element {
  const presets = useCimdClientPresets(undefined, undefined, {
    throwOnError: false,
  });
  const known = useMemo(
    () => groupKnownClients(presets.data?.items ?? []),
    [presets.data],
  );
  const clients = useOrganizationClients(issuer, path);
  const [adding, setAdding] = useState(false);
  const [removing, setRemoving] = useState<UserSessionIssuerCimdClient | null>(
    null,
  );

  return (
    <>
      <div className="flex flex-col gap-3 border-t px-6 py-5">
        <div className="flex flex-col gap-0.5">
          <Text className="text-sm">Allowed by Speakeasy</Text>
          <Text small muted>
            Clients Speakeasy has checked and keeps current.
          </Text>
        </div>
        {presets.isLoading ? (
          <Skeleton className="h-10 w-full" />
        ) : presets.isError ? (
          <InlineRow>
            <Text small className="text-destructive">
              Couldn&apos;t load the clients Speakeasy allows.
            </Text>
            <Button
              variant="tertiary"
              size="sm"
              onClick={() => void presets.refetch()}
            >
              <Button.Text>Retry</Button.Text>
            </Button>
          </InlineRow>
        ) : (
          <SpeakeasyClients known={known} />
        )}
      </div>

      <div className="flex flex-col gap-3 border-t px-6 py-5">
        <div className="flex flex-wrap items-end justify-between gap-4">
          <div className="flex flex-col gap-0.5">
            <Text className="text-sm">Allowed by your organization</Text>
            <Text small muted>
              Clients you add yourself, for this MCP Authentication Provider
              only.
            </Text>
          </div>
          {path.kind !== "elsewhere" && (
            <RequireScope scope={SAVE_SCOPE[path.kind]} level="component">
              {({ disabled }) => (
                <Button
                  variant="secondary"
                  size="sm"
                  disabled={disabled}
                  onClick={() => setAdding(true)}
                >
                  <Button.LeftIcon>
                    <Plus className="size-4" />
                  </Button.LeftIcon>
                  <Button.Text>Add</Button.Text>
                </Button>
              )}
            </RequireScope>
          )}
        </div>
        <OrganizationClientList
          path={path}
          clients={clients}
          onRemove={setRemoving}
        />
      </div>

      {path.kind !== "elsewhere" && (
        <AddClientDialog
          open={adding}
          onOpenChange={setAdding}
          clients={clients}
          canCheck={canCheck}
        />
      )}

      <ConfirmDialog
        open={removing !== null}
        onOpenChange={(open) => {
          if (!open) setRemoving(null);
        }}
        title="Remove this client?"
        description="New sign-ins from it are refused right away. People already signed in keep their sessions until they expire."
        confirmLabel="Remove"
        pendingLabel="Removing…"
        isPending={clients.removing}
        onConfirm={() => {
          if (removing) clients.remove(removing.id, () => setRemoving(null));
        }}
      />
    </>
  );
}

function InlineRow({ children }: { children: React.ReactNode }): JSX.Element {
  return <div className="flex items-center gap-3">{children}</div>;
}

function OrganizationClientList({
  path,
  clients,
  onRemove,
}: {
  path: EditPath;
  clients: ReturnType<typeof useOrganizationClients>;
  onRemove: (client: UserSessionIssuerCimdClient) => void;
}): JSX.Element {
  if (path.kind === "elsewhere") {
    return (
      <Text small muted>
        See them in {path.projectName}&apos;s MCP Sessions configuration.
      </Text>
    );
  }
  if (clients.isLoading) return <Skeleton className="h-14 w-full" />;
  if (clients.isError) {
    return (
      <InlineRow>
        <Text small className="text-destructive">
          Couldn&apos;t load the clients your organization added.
        </Text>
        <Button variant="tertiary" size="sm" onClick={clients.retry}>
          <Button.Text>Retry</Button.Text>
        </Button>
      </InlineRow>
    );
  }
  const items = clients.items ?? [];
  if (items.length === 0) {
    return (
      <Text small muted>
        None yet. Add an internal tool or a client Speakeasy hasn&apos;t
        checked.
      </Text>
    );
  }
  return (
    <ul aria-label="Allowed by your organization" className="divide-y border">
      {items.map((client) => (
        <li
          key={client.id}
          className="flex flex-wrap items-center gap-3 px-4 py-3"
        >
          <code className="min-w-0 flex-1 font-mono text-xs break-all">
            {client.clientIdMetadataUri}
          </code>
          <Text small muted className="whitespace-nowrap">
            Added{" "}
            {client.createdAt.toLocaleDateString(undefined, {
              day: "numeric",
              month: "short",
            })}
          </Text>
          <RequireScope scope={SAVE_SCOPE[path.kind]} level="component">
            {({ disabled }) => (
              <Button
                variant="secondary"
                size="sm"
                disabled={disabled}
                onClick={() => onRemove(client)}
                aria-label={`Remove ${client.clientIdMetadataUri}`}
              >
                <Button.Text>Remove</Button.Text>
              </Button>
            )}
          </RequireScope>
        </li>
      ))}
    </ul>
  );
}

/** How many logos the summary shows before "and N more". */
const PREVIEW_COUNT = 6;

/**
 * The clients Speakeasy allows, summarized by a few logos; the full list,
 * with what each one allows, opens in a dialog.
 */
function SpeakeasyClients({ known }: { known: KnownClient[] }): JSX.Element {
  const preview = known.slice(0, PREVIEW_COUNT);
  const more = known.length - preview.length;
  return (
    <Dialog>
      <InlineRow>
        <div className="flex items-center -space-x-1.5" aria-hidden="true">
          {preview.map((client) => (
            <span
              key={client.key}
              title={client.name}
              className="bg-card flex size-8 items-center justify-center border font-mono text-xs"
            >
              <ClientMark name={client.name} />
            </span>
          ))}
        </div>
        <Text small muted>
          {more > 0 ? `+${more} more` : `${known.length} clients`}
        </Text>
        <Dialog.Trigger asChild>
          <Button variant="secondary" size="sm">
            <Button.Text>See all</Button.Text>
          </Button>
        </Dialog.Trigger>
      </InlineRow>
      <Dialog.Content className="max-w-xl">
        <Dialog.Header>
          <Dialog.Title>Allowed by Speakeasy</Dialog.Title>
          <Dialog.Description>
            Clients Speakeasy has checked and keeps current, with the documents
            each one may identify itself with.
          </Dialog.Description>
        </Dialog.Header>
        <ul
          aria-label="Allowed by Speakeasy"
          className="max-h-[60vh] divide-y overflow-y-auto border"
        >
          {known.map((client) => (
            <li key={client.key} className="flex gap-3 px-4 py-3">
              <span className="flex size-8 shrink-0 items-center justify-center border font-mono text-xs">
                <ClientMark name={client.name} />
              </span>
              <div className="flex min-w-0 flex-col gap-1">
                <Text>{client.name}</Text>
                {client.documents.map((document) => (
                  <div key={document.url} className="flex flex-col gap-0.5">
                    {document.name !== client.name && (
                      <Text small>{document.name}</Text>
                    )}
                    <code className="text-muted-foreground font-mono text-xs break-all">
                      {document.url}
                    </code>
                    {document.isPattern && (
                      <Text small muted>
                        Covers every address that fills each * with one path
                        segment.
                      </Text>
                    )}
                  </div>
                ))}
              </div>
            </li>
          ))}
        </ul>
      </Dialog.Content>
    </Dialog>
  );
}

function ClientMark({ name }: { name: string }): JSX.Element {
  return hasAgentProviderIcon(name) ? (
    <AgentProviderIcon source={name} className="size-5" />
  ) : (
    <span aria-hidden="true">{monogram(name)}</span>
  );
}
