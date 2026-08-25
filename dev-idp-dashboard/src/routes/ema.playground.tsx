import { useState } from "react";
import { createFileRoute } from "@tanstack/react-router";
import { useMutation } from "@tanstack/react-query";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Section } from "@/components/ema/Section";
import { cn } from "@/lib/utils";
import { useEmaApps, useEmaResources } from "@/hooks/use-ema";

export const Route = createFileRoute("/ema/playground")({
  component: PlaygroundPage,
});

interface Leg {
  name: string;
  request: { url: string; params: Record<string, string> };
  status: number;
  body: unknown;
}

interface ExchangeResult {
  ok: boolean;
  failed_at?: string;
  legs: Leg[];
  id_jag?: { header: unknown; claims: unknown };
  error?: string;
}

function PlaygroundPage() {
  const apps = useEmaApps();
  const resources = useEmaResources();

  const [clientID, setClientID] = useState("");
  const [clientSecret, setClientSecret] = useState("");
  const [audience, setAudience] = useState("");
  const [resource, setResource] = useState("");
  const [scope, setScope] = useState("");
  const [tokenEndpoint, setTokenEndpoint] = useState("");

  const run = useMutation<ExchangeResult, Error, void>({
    mutationFn: async () => {
      const res = await fetch("/api/ema-exchange", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          client_id: clientID,
          client_secret: clientSecret || undefined,
          audience,
          resource: resource || undefined,
          scope: scope || undefined,
          token_endpoint: tokenEndpoint || undefined,
        }),
      });
      return (await res.json()) as ExchangeResult;
    },
  });

  /** Selecting a registered resource fills in the two URLs it implies. */
  const chooseResource = (id: string) => {
    const r = resources.data?.items.find((x) => x.id === id);
    if (!r) return;
    setAudience(r.issuer);
    setResource(r.resource_identifier);
    setTokenEndpoint("");
  };

  return (
    <Section
      title="Playground"
      description="Runs the whole flow as a client would — sign in, exchange the id_token for an ID-JAG, redeem it — and shows every leg. It runs as whichever user is currently signed in on the oauth2-1 provider, so that is the user the app must be assigned to. Point the redeem endpoint somewhere else to exchange a grant with an upstream that is not this dev-idp."
    >
      <div className="grid grid-cols-2 gap-3 rounded-lg border border-border p-3">
        <Field
          id="pg-client"
          label="App client_id"
          value={clientID}
          onChange={setClientID}
          list="pg-apps"
          placeholder="my-mcp-client"
        />
        <datalist id="pg-apps">
          {(apps.data?.items ?? []).map((a) => (
            <option key={a.id} value={a.client_id} />
          ))}
        </datalist>

        <Field
          id="pg-secret"
          label="App client_secret"
          value={clientSecret}
          onChange={setClientSecret}
          placeholder="blank for a public client"
        />

        <div className="col-span-2 flex flex-col gap-1.5">
          <Label htmlFor="pg-pick">Fill from a registered resource</Label>
          <select
            id="pg-pick"
            onChange={(e) => chooseResource(e.target.value)}
            defaultValue=""
            className="h-9 rounded-md border border-border bg-background px-2 text-sm"
          >
            <option value="">Select…</option>
            {(resources.data?.items ?? []).map((r) => (
              <option key={r.id} value={r.id}>
                {r.slug} — {r.resource_identifier}
              </option>
            ))}
          </select>
        </div>

        <Field
          id="pg-audience"
          label="audience (resource AS issuer)"
          value={audience}
          onChange={setAudience}
          placeholder="http://localhost:35291/resource-as/chat"
        />
        <Field
          id="pg-resource"
          label="resource (MCP server)"
          value={resource}
          onChange={setResource}
          placeholder="https://mcp.chat.example/"
        />
        <Field
          id="pg-scope"
          label="scope"
          value={scope}
          onChange={setScope}
          placeholder="blank = everything assigned"
        />
        <Field
          id="pg-endpoint"
          label="redeem at (upstream token endpoint)"
          value={tokenEndpoint}
          onChange={setTokenEndpoint}
          placeholder="defaults to the audience's own /token"
        />

        <div className="col-span-2">
          <Button
            onClick={() => run.mutate()}
            disabled={!clientID || !audience || run.isPending}
          >
            {run.isPending ? "Running…" : "Run the exchange"}
          </Button>
        </div>
      </div>

      {run.error ? (
        <p className="text-sm text-destructive">{String(run.error)}</p>
      ) : null}

      {run.data ? <Result result={run.data} /> : null}
    </Section>
  );
}

function Result({ result }: { result: ExchangeResult }) {
  if (result.error) {
    return <p className="text-sm text-destructive">{result.error}</p>;
  }

  return (
    <div className="flex flex-col gap-3">
      <p
        className={cn(
          "text-sm font-medium",
          result.ok ? "text-foreground" : "text-destructive",
        )}
      >
        {result.ok
          ? "Every leg succeeded — the access token below is the end of the flow."
          : `Stopped at: ${result.failed_at}`}
      </p>

      {result.id_jag ? (
        <div className="rounded-lg border border-border p-3 flex flex-col gap-2">
          <h3 className="text-sm font-medium">Decoded ID-JAG</h3>
          <Json label="header" value={result.id_jag.header} />
          <Json label="claims" value={result.id_jag.claims} />
        </div>
      ) : null}

      {result.legs.map((leg, i) => (
        <div
          key={`${leg.name}-${i}`}
          className="rounded-lg border border-border p-3 flex flex-col gap-2"
        >
          <div className="flex items-center justify-between gap-3">
            <h3 className="text-sm font-medium">{leg.name}</h3>
            <span
              className={cn(
                "text-xs font-mono px-2 py-0.5 rounded-full",
                leg.status >= 200 && leg.status < 400
                  ? "bg-accent text-foreground"
                  : "bg-destructive/10 text-destructive",
              )}
            >
              {leg.status}
            </span>
          </div>
          <p className="text-xs font-mono text-muted-foreground break-all">
            {leg.request.url}
          </p>
          {Object.keys(leg.request.params).length > 0 ? (
            <Json label="request" value={leg.request.params} />
          ) : null}
          <Json label="response" value={leg.body} />
        </div>
      ))}
    </div>
  );
}

function Json({ label, value }: { label: string; value: unknown }) {
  return (
    <div className="flex flex-col gap-1">
      <span className="text-[10px] uppercase tracking-wider text-muted-foreground/80 font-mono">
        {label}
      </span>
      <pre className="text-xs font-mono bg-muted/40 rounded-md p-2 overflow-x-auto whitespace-pre-wrap break-all">
        {JSON.stringify(value, null, 2)}
      </pre>
    </div>
  );
}

function Field({
  id,
  label,
  value,
  onChange,
  placeholder,
  list,
}: {
  id: string;
  label: string;
  value: string;
  onChange: (v: string) => void;
  placeholder?: string;
  list?: string;
}) {
  return (
    <div className="flex flex-col gap-1.5">
      <Label htmlFor={id}>{label}</Label>
      <Input
        id={id}
        value={value}
        onChange={(e) => onChange(e.target.value)}
        placeholder={placeholder}
        list={list}
      />
    </div>
  );
}
