import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useParams } from "@tanstack/react-router";
import { useState, type JSX } from "react";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Switch } from "@/components/ui/switch";
import {
  organizationQuery,
  organizationWorkloadIdentityQuery,
} from "@/lib/adminQueries";
import {
  admitWorkloadSubject,
  createWorkloadIssuer,
  errorMessage,
  setWorkloadAuthenticationHost,
  teardownWorkloadIssuer,
  type AdminOrganization,
  type AdminWorkloadIdentityState,
} from "@/lib/gramAdminApi";

// Ties the warning to the input it is about, for a screen reader.
const WILDCARD_WARNING_ID = "workload-subject-wildcard-warning";

export function WorkloadIdentityRoute(): JSX.Element | null {
  const { idOrSlug } = useParams({ from: "/organizations/$idOrSlug" });
  const { data } = useQuery(organizationQuery(idOrSlug));
  if (!data) return null;
  return <WorkloadIdentity key={data.id} org={data} />;
}

export function WorkloadIdentity({
  org,
}: {
  org: AdminOrganization;
}): JSX.Element {
  const queryClient = useQueryClient();
  const query = organizationWorkloadIdentityQuery(org.id);
  const { data, isPending, isError, error } = useQuery(query);

  // Every write returns the whole state, so the cache is replaced rather than
  // invalidated. A refetch here would race the next form submission.
  const replace = (updated: AdminWorkloadIdentityState): void => {
    queryClient.setQueryData<AdminWorkloadIdentityState>(
      query.queryKey,
      updated,
    );
  };

  const createIssuer = useMutation({
    mutationFn: createWorkloadIssuer,
    onSuccess: replace,
  });
  const admitSubject = useMutation({
    mutationFn: admitWorkloadSubject,
    onSuccess: replace,
  });
  const setHost = useMutation({
    mutationFn: setWorkloadAuthenticationHost,
    onSuccess: replace,
  });
  const teardown = useMutation({
    mutationFn: teardownWorkloadIssuer,
    onSuccess: replace,
  });

  if (isPending) {
    return (
      <span className="text-muted-foreground text-sm">
        Loading workload identity...
      </span>
    );
  }
  if (isError || !data) {
    return (
      <span className="text-destructive text-sm">{errorMessage(error)}</span>
    );
  }

  return (
    <div className="space-y-8">
      <section className="space-y-2">
        <h2 className="font-medium">Authentication host</h2>
        <p className="text-muted-foreground text-sm">
          A client that refuses a token endpoint sharing a host with the API it
          calls needs this on. Off means this issuer announces the MCP host.
        </p>
        <table className="w-full text-sm">
          <thead>
            <tr className="text-muted-foreground text-left">
              <th className="py-1">User session issuer</th>
              <th className="py-1">Project</th>
              <th className="py-1">Authentication host</th>
            </tr>
          </thead>
          <tbody>
            {data.authentication_hosts.map((host) => (
              <tr key={host.user_session_issuer_id} className="border-t">
                <td className="py-1 font-mono text-xs">
                  {host.user_session_issuer_id}
                </td>
                <td className="py-1 font-mono text-xs">
                  {host.project_id ?? "org-wide"}
                </td>
                <td className="py-1">
                  <Switch
                    checked={host.use_authentication_host}
                    disabled={setHost.isPending}
                    onCheckedChange={(enabled) =>
                      setHost.mutate({
                        organizationID: org.id,
                        userSessionIssuerID: host.user_session_issuer_id,
                        enabled,
                      })
                    }
                  />
                </td>
              </tr>
            ))}
          </tbody>
        </table>
        {data.authentication_hosts.length === 0 && (
          <span className="text-muted-foreground text-sm">
            No user session issuers.
          </span>
        )}
        {setHost.isError && (
          <span className="text-destructive text-sm">
            {errorMessage(setHost.error)}
          </span>
        )}
      </section>

      <section className="space-y-2">
        <h2 className="font-medium">Trusted issuers</h2>
        <table className="w-full text-sm">
          <thead>
            <tr className="text-muted-foreground text-left">
              <th className="py-1">Name</th>
              <th className="py-1">Issuer</th>
              <th className="py-1">JWKS URI</th>
              <th className="py-1">Project</th>
              <th className="py-1"></th>
            </tr>
          </thead>
          <tbody>
            {data.issuers.map((issuer) => (
              <tr key={issuer.id} className="border-t align-top">
                <td className="py-1">{issuer.name}</td>
                <td className="py-1 font-mono text-xs">{issuer.issuer}</td>
                <td className="py-1 font-mono text-xs">{issuer.jwks_uri}</td>
                <td className="py-1 font-mono text-xs">
                  {issuer.project_id ?? "org-wide"}
                </td>
                <td className="py-1">
                  <Button
                    variant="destructive"
                    size="sm"
                    disabled={teardown.isPending}
                    onClick={() =>
                      teardown.mutate({
                        organizationID: org.id,
                        workloadIssuerID: issuer.id,
                      })
                    }
                  >
                    Withdraw
                  </Button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
        {data.issuers.length === 0 && (
          <span className="text-muted-foreground text-sm">
            No trusted issuers. Nothing can exchange a workload assertion here.
          </span>
        )}
        {teardown.isError && (
          <span className="text-destructive text-sm">
            {errorMessage(teardown.error)}
          </span>
        )}
        <CreateIssuerForm
          organizationID={org.id}
          pending={createIssuer.isPending}
          error={createIssuer.isError ? errorMessage(createIssuer.error) : null}
          onSubmit={(input) => createIssuer.mutate(input)}
        />
      </section>

      <section className="space-y-2">
        <h2 className="font-medium">Admitted subjects</h2>
        <p className="text-muted-foreground text-sm">
          Trusting an issuer admits nothing on its own. The subject must be the
          exact value the platform mints, which for some platforms is only
          visible in the log line of a refused exchange.
        </p>
        <table className="w-full text-sm">
          <thead>
            <tr className="text-muted-foreground text-left">
              <th className="py-1">Subject</th>
              <th className="py-1">Issuer</th>
              <th className="py-1">Agent</th>
            </tr>
          </thead>
          <tbody>
            {data.subjects.map((subject) => (
              <tr
                key={`${subject.workload_issuer_id}:${subject.subject}`}
                className="border-t align-top"
              >
                <td className="py-1 font-mono text-xs">{subject.subject}</td>
                <td className="py-1 font-mono text-xs">
                  {subject.workload_issuer_id}
                </td>
                <td className="py-1">
                  {subject.agent_id ? (
                    (subject.agent_name ?? subject.agent_id)
                  ) : (
                    <span className="text-destructive">
                      unassigned, will be refused
                    </span>
                  )}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
        {data.subjects.length === 0 && (
          <span className="text-muted-foreground text-sm">
            No admitted subjects.
          </span>
        )}
        {admitSubject.isError && (
          <span className="text-destructive text-sm">
            {errorMessage(admitSubject.error)}
          </span>
        )}
        <AdmitSubjectForm
          organizationID={org.id}
          issuerIDs={data.issuers.map((issuer) => issuer.id)}
          pending={admitSubject.isPending}
          onSubmit={(input) => admitSubject.mutate(input)}
        />
      </section>
    </div>
  );
}

function CreateIssuerForm({
  organizationID,
  pending,
  error,
  onSubmit,
}: {
  organizationID: string;
  pending: boolean;
  error: string | null;
  onSubmit: (input: {
    organizationID: string;
    projectID?: string;
    name: string;
    issuer: string;
    jwksURI: string;
  }) => void;
}): JSX.Element {
  const [name, setName] = useState("");
  const [issuer, setIssuer] = useState("");
  const [jwksURI, setJwksURI] = useState("");
  const [projectID, setProjectID] = useState("");

  return (
    <form
      className="flex flex-wrap items-end gap-2 border-t pt-3"
      onSubmit={(event) => {
        event.preventDefault();
        onSubmit({
          organizationID,
          projectID: projectID || undefined,
          name,
          issuer,
          jwksURI,
        });
      }}
    >
      <Labelled label="Name">
        <Input
          value={name}
          onChange={(event) => setName(event.target.value)}
          required
        />
      </Labelled>
      <Labelled label="Issuer (https)">
        <Input
          value={issuer}
          onChange={(event) => setIssuer(event.target.value)}
          placeholder="https://issuer.example/agents"
          required
        />
      </Labelled>
      <Labelled label="JWKS URI (https)">
        <Input
          value={jwksURI}
          onChange={(event) => setJwksURI(event.target.value)}
          placeholder="https://issuer.example/agents/jwks.json"
          required
        />
      </Labelled>
      <Labelled label="Project ID (optional)">
        <Input
          value={projectID}
          onChange={(event) => setProjectID(event.target.value)}
        />
      </Labelled>
      <Button type="submit" size="sm" disabled={pending}>
        Trust issuer
      </Button>
      {error && <span className="text-destructive text-sm">{error}</span>}
    </form>
  );
}

function AdmitSubjectForm({
  organizationID,
  issuerIDs,
  pending,
  onSubmit,
}: {
  organizationID: string;
  issuerIDs: string[];
  pending: boolean;
  onSubmit: (input: {
    organizationID: string;
    workloadIssuerID: string;
    subject: string;
    name?: string;
    agentID: string;
  }) => void;
}): JSX.Element {
  const [workloadIssuerID, setWorkloadIssuerID] = useState(issuerIDs[0] ?? "");
  const [subject, setSubject] = useState("");
  const [agentID, setAgentID] = useState("");
  const [name, setName] = useState("");

  // A subject is compared in full, so a `*` is just another character and the
  // rule matches nothing. Silent, and it reads as correct in the table
  // afterwards, which is why this says so while the operator is still typing
  // rather than leaving them to wonder why no exchange is admitted.
  const subjectLooksLikeWildcard = subject.includes("*");

  return (
    <form
      className="flex flex-wrap items-end gap-2 border-t pt-3"
      onSubmit={(event) => {
        event.preventDefault();
        onSubmit({
          organizationID,
          workloadIssuerID,
          subject,
          name: name || undefined,
          agentID,
        });
      }}
    >
      <Labelled label="Issuer ID">
        <Input
          value={workloadIssuerID}
          onChange={(event) => setWorkloadIssuerID(event.target.value)}
          list="workload-issuer-ids"
          required
        />
        <datalist id="workload-issuer-ids">
          {issuerIDs.map((id) => (
            <option key={id} value={id} />
          ))}
        </datalist>
      </Labelled>
      <Labelled label="Subject (exact)">
        <Input
          value={subject}
          onChange={(event) => setSubject(event.target.value)}
          aria-describedby={
            subjectLooksLikeWildcard ? WILDCARD_WARNING_ID : undefined
          }
          required
        />
      </Labelled>
      <Labelled label="Agent ID">
        <Input
          value={agentID}
          onChange={(event) => setAgentID(event.target.value)}
          required
        />
      </Labelled>
      <Labelled label="Label (optional)">
        <Input value={name} onChange={(event) => setName(event.target.value)} />
      </Labelled>
      <Button type="submit" size="sm" disabled={pending}>
        Admit subject
      </Button>
      {/* basis-full so it gets its own row rather than being squeezed between
          two inputs in the wrapping flex layout. */}
      {subjectLooksLikeWildcard && (
        <p
          id={WILDCARD_WARNING_ID}
          role="alert"
          className="text-destructive basis-full text-sm"
        >
          A subject is matched <strong>in full, literally</strong>, including
          the <code>*</code>. This rule admits only a subject containing that
          character, which no platform mints — not every subject beginning with
          it.
        </p>
      )}
    </form>
  );
}

function Labelled({
  label,
  children,
}: {
  label: string;
  children: React.ReactNode;
}): JSX.Element {
  return (
    <label className="flex flex-col gap-1 text-sm">
      <span className="text-muted-foreground text-xs">{label}</span>
      {children}
    </label>
  );
}
