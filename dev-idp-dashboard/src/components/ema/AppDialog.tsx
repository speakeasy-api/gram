import { useState } from "react";
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import type { EmaApp } from "@/lib/devidp";
import { generateSigningKey, type GeneratedKey } from "@/lib/keygen";
import {
  useCreateEmaApp,
  useDeleteEmaApp,
  useUpdateEmaApp,
} from "@/hooks/use-ema";

/**
 * How an app authenticates is one choice, not three independent fields, so
 * it is one tab strip. The tab is authoritative: submitting clears the
 * credentials the other tabs own, otherwise a leftover JWKS would keep
 * forcing private_key_jwt after switching to a secret.
 */
type Method = "secret" | "jwt" | "public";

function methodOf(app: EmaApp | undefined): Method {
  if (!app) return "secret";
  if (app.jwks) return "jwt";
  if (app.client_secret) return "secret";
  return "public";
}

/**
 * A client_id that is a URL is a Client ID Metadata Document. The draft
 * forbids pairing one with a shared secret — the document is public, so a
 * secret alongside it would be a credential its readers are not meant to
 * have — and the server rejects the combination, so the tab goes away rather
 * than failing on save.
 */
function isCIMD(clientID: string): boolean {
  return /^https?:\/\/\S+$/.test(clientID);
}

export function AppDialog({
  app,
  onClose,
}: {
  app?: EmaApp;
  onClose: () => void;
}) {
  const editing = app !== undefined;
  const [clientID, setClientID] = useState(app?.client_id ?? "");
  const [name, setName] = useState(app?.name ?? "");
  const [method, setMethod] = useState<Method>(methodOf(app));
  const [clientSecret, setClientSecret] = useState(app?.client_secret ?? "");
  const [jwks, setJwks] = useState(app?.jwks ?? "");

  const create = useCreateEmaApp();
  const update = useUpdateEmaApp();
  const remove = useDeleteEmaApp();
  const pending = create.isPending || update.isPending || remove.isPending;
  const error = create.error ?? update.error ?? remove.error;

  // Submit what was inspected. Deciding CIMD on the trimmed value but sending
  // the raw one registers a padded URL as a public client, which the server
  // then cannot recognise as a metadata document.
  const submittedClientID = clientID.trim();
  const cimd = isCIMD(submittedClientID);
  const effectiveMethod: Method = cimd && method === "secret" ? "jwt" : method;

  // Exactly one credential survives, decided by the tab. A CIMD client stores
  // no JWKS at all: its keys are read from its own document at each request,
  // so rotation is the client republishing rather than an edit here.
  const credentials = {
    client_secret: effectiveMethod === "secret" ? clientSecret : "",
    jwks: effectiveMethod === "jwt" && !cimd ? jwks : "",
  };

  const submit = () => {
    if (editing) {
      update.mutate(
        { id: app.id, client_id: submittedClientID, name, ...credentials },
        { onSuccess: onClose },
      );
    } else {
      create.mutate(
        { client_id: submittedClientID, name: name || undefined, ...credentials },
        { onSuccess: onClose },
      );
    }
  };

  const ready =
    submittedClientID !== "" &&
    (effectiveMethod !== "jwt" || cimd || jwks.trim() !== "") &&
    (effectiveMethod !== "secret" || clientSecret.trim() !== "");

  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="max-w-lg">
        <form
          className="flex flex-col gap-4"
          onSubmit={(e) => {
            e.preventDefault();
            submit();
          }}
        >
          <DialogHeader>
            <DialogTitle>{editing ? "Edit app" : "Register app"}</DialogTitle>
          </DialogHeader>

          <div className="flex flex-col gap-3">
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="app-client-id">Client ID</Label>
              <Input
                id="app-client-id"
                value={clientID}
                onChange={(e) => setClientID(e.target.value)}
                placeholder="my-mcp-client"
                autoFocus
                required
              />
            </div>
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="app-name">Name</Label>
              <Input
                id="app-name"
                value={name}
                onChange={(e) => setName(e.target.value)}
                placeholder="defaults to the client id"
              />
            </div>
          </div>

          <section className="flex flex-col gap-2">
            <Label>Authenticates with</Label>
            <Tabs
              value={effectiveMethod}
              onValueChange={(v) => setMethod(v as Method)}
            >
              <TabsList className="w-full">
                <TabsTrigger
                  value="secret"
                  disabled={cimd}
                  title={
                    cimd
                      ? "A metadata document client_id cannot hold a shared secret"
                      : undefined
                  }
                >
                  Client Secret
                </TabsTrigger>
                <TabsTrigger value="jwt">Private Key JWT</TabsTrigger>
                <TabsTrigger value="public">Public</TabsTrigger>
              </TabsList>

              <TabsContent value="secret">
                <SecretPane value={clientSecret} onChange={setClientSecret} />
              </TabsContent>
              <TabsContent value="jwt">
                {cimd ? (
                  <CIMDPane clientID={submittedClientID} />
                ) : (
                  <PrivateKeyPane value={jwks} onChange={setJwks} />
                )}
              </TabsContent>
              <TabsContent value="public">
                <PublicPane />
              </TabsContent>
            </Tabs>
          </section>

          {error && (
            <div className="text-xs text-destructive">
              {(error as Error).message}
            </div>
          )}

          <DialogFooter className="justify-between">
            {editing ? (
              <Button
                type="button"
                variant="destructive"
                onClick={() =>
                  remove.mutate({ id: app.id }, { onSuccess: onClose })
                }
              >
                Delete
              </Button>
            ) : (
              <span />
            )}
            <div className="flex gap-2">
              <Button type="button" variant="outline" onClick={onClose}>
                Cancel
              </Button>
              <Button type="submit" disabled={pending || !ready}>
                {editing ? "Save" : "Register"}
              </Button>
            </div>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

/**
 * When the client_id is a metadata document there is nothing to paste: the
 * keys are whatever the document publishes, read fresh on each request.
 */
function CIMDPane({ clientID }: { clientID: string }) {
  return (
    <div className="flex flex-col gap-2 pt-3 text-xs text-muted-foreground">
      <p>
        This client_id is a Client ID Metadata Document, so its keys come from
        the document itself — <code className="break-all">{clientID}</code> —
        via its <code>jwks</code> or <code>jwks_uri</code>. Nothing is stored
        here.
      </p>
      <p>
        Rotation is the client republishing its document; this dev-idp re-reads
        it within a minute. A document that publishes no keys describes a
        public client, and the mint leg treats it as one.
      </p>
    </div>
  );
}

function SecretPane({
  value,
  onChange,
}: {
  value: string;
  onChange: (v: string) => void;
}) {
  return (
    <div className="flex flex-col gap-1.5 pt-3">
      <Label htmlFor="app-secret">Client secret</Label>
      <Input
        id="app-secret"
        value={value}
        onChange={(e) => onChange(e.target.value)}
        placeholder="s3cret"
      />
      <p className="text-xs text-muted-foreground">
        Sent as <code>client_secret</code> on the form (client_secret_post).
      </p>
    </div>
  );
}

function PrivateKeyPane({
  value,
  onChange,
}: {
  value: string;
  onChange: (v: string) => void;
}) {
  const [generated, setGenerated] = useState<GeneratedKey | null>(null);
  const [generating, setGenerating] = useState(false);
  const [genError, setGenError] = useState<string | null>(null);

  const generate = async () => {
    setGenerating(true);
    setGenError(null);
    try {
      const key = await generateSigningKey();
      setGenerated(key);
      onChange(key.jwks);
    } catch (e) {
      setGenError(e instanceof Error ? e.message : String(e));
    } finally {
      setGenerating(false);
    }
  };

  return (
    <div className="flex flex-col gap-3 pt-3">
      <div className="flex items-center justify-between gap-2">
        <p className="text-xs text-muted-foreground">
          Register the app's public key; it signs a client assertion to
          authenticate. Paste a JWKS, or generate a keypair here.
        </p>
        <Button
          type="button"
          variant="outline"
          size="sm"
          onClick={generate}
          disabled={generating}
        >
          {generating ? "Generating…" : "Generate"}
        </Button>
      </div>

      {genError && <p className="text-xs text-destructive">{genError}</p>}

      {generated && (
        <div className="flex flex-col gap-1.5 rounded-md border border-[var(--retro-orange)]/40 p-2">
          <div className="flex items-center justify-between gap-2">
            <Label className="text-[var(--retro-orange)]">Private key</Label>
            <CopyButton text={generated.privateKeyPEM} />
          </div>
          <pre className="max-h-40 overflow-auto rounded-sm bg-muted/40 p-2 font-mono text-[10px] leading-tight break-all whitespace-pre-wrap">
            {generated.privateKeyPEM}
          </pre>
          <p className="text-xs text-muted-foreground">
            Generated in your browser and never sent to dev-idp — only the JWKS
            below is. Copy it now; closing this dialog loses it.
          </p>
        </div>
      )}

      <div className="flex flex-col gap-1.5">
        <Label htmlFor="app-jwks">JWKS</Label>
        <textarea
          id="app-jwks"
          value={value}
          onChange={(e) => onChange(e.target.value)}
          rows={5}
          spellCheck={false}
          placeholder='{"keys":[{"kty":"RSA","kid":"…","n":"…","e":"AQAB"}]}'
          className="rounded-md border border-border bg-background p-2 font-mono text-xs"
        />
      </div>
    </div>
  );
}

/**
 * The honest version: a public client is weaker, but it is not the free-for-all
 * it looks like, and the copy should say which part it does and does not gate.
 */
function PublicPane() {
  return (
    <div className="flex flex-col gap-2 pt-3 text-xs text-muted-foreground">
      <p>
        The app presents only its <code>client_id</code>, which is not a secret.
        Anything that can reach this IdP can claim to be it.
      </p>
      <p>
        Minting still needs a valid <code>subject_token</code> — an id_token or
        refresh token this IdP issued — plus an assignment for the user it
        names. So a caller holding no user credential cannot mint either way;
        what client authentication adds is a second barrier if a subject token
        leaks.
      </p>
      <p className="text-[var(--retro-orange)]">
        Real enterprise IdPs generally require one of the other two. Pick this
        for convenience while exercising policy, not when checking what a client
        will meet in production.
      </p>
    </div>
  );
}

function CopyButton({ text }: { text: string }) {
  const [copied, setCopied] = useState(false);
  return (
    <Button
      type="button"
      variant="ghost"
      size="xs"
      onClick={() => {
        void navigator.clipboard.writeText(text).then(() => {
          setCopied(true);
          setTimeout(() => setCopied(false), 1500);
        });
      }}
    >
      {copied ? "Copied" : "Copy"}
    </Button>
  );
}
