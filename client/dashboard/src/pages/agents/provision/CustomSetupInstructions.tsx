import { Label } from "@/components/ui/Label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/Select";
import { Stack } from "@/components/ui/Stack";
import { Text } from "@/components/ui/Text";
import { ConsoleRow } from "./ConsoleRow";
import { TextField } from "./SetupFields";
import type { Connection, PlatformSetup } from "./setupInstructions";

/** The key the picker uses for the platform that is not in the catalog. */
export const CUSTOM_PLATFORM_KEY = "custom";

/**
 * Whether this is an https URL the server will also accept.
 *
 * RFC 8414 §2 wants an issuer identifier to be an https URL with no query or
 * fragment, and the server holds both URLs to that. Accepting one it refuses
 * here only means the reader finds out after Connect.
 */
function https(value: string): boolean {
  let url: URL;
  try {
    url = new URL(value);
  } catch {
    return false;
  }
  if (url.protocol !== "https:") return false;
  if (url.username !== "" || url.password !== "") return false;
  // Read off the string rather than the parsed parts, as the server does: a
  // bare "?" or "#" leaves search and hash empty, and an encoded one stays
  // percent-encoded in the path, so the delimiter is the dependable signal.
  return !value.includes("?") && !value.includes("#");
}

/** Whether the subject carries a `*` of the reader's own. */
function starred(subject: string): boolean {
  return subject.includes("*");
}

/**
 * Any OIDC issuer the catalog does not ship: a CI provider, a cluster, an
 * internal identity service.
 *
 * The catalog knows a platform's issuer and key set so nobody has to look
 * them up. Here the reader supplies them, which is the whole difference —
 * everything after is the same two rows and the same token exchange.
 */
export function customSetupInstructions(): PlatformSetup {
  return {
    connection: (values): Connection | null => {
      const name = (values["name"] ?? "").trim();
      const issuer = (values["issuer"] ?? "").trim();
      const jwksUri = (values["jwks_uri"] ?? "").trim();
      const subject = (values["subject"] ?? "").trim();
      const wildcard = values["match_kind"] === "wildcard";
      if (name === "" || subject === "") return null;
      if (!https(issuer) || !https(jwksUri)) return null;
      // The `*` belongs to the match kind, not to the subject: appending one
      // to a starred stem makes `...**`, and an exact rule carrying one is
      // refused outright.
      if (starred(subject)) return null;
      // A wildcard rule is a stem: everything under it is admitted, so the
      // reader writes the stem and the `*` is ours to add.
      return {
        name,
        description: `Federated from ${issuer}`,
        issuer,
        jwksUri,
        subject: wildcard ? `${subject}*` : subject,
        matchKind: wildcard ? "wildcard" : "exact",
      };
    },
    requirements: (
      <div className="space-y-3 text-sm">
        <p>
          The issuer URL and JWKS URL the platform publishes. Both are in its
          OpenID configuration, usually at{" "}
          <code className="text-xs">
            {"<issuer>"}/.well-known/openid-configuration
          </code>
          .
        </p>
        <p>
          The <code className="text-xs">sub</code> claim its tokens carry. Read
          one of its tokens rather than guessing: a subject that does not match
          exactly admits nothing.
        </p>
      </div>
    ),
    steps: [
      {
        id: "issuer",
        short: "Issuer URLs",
        title: "Identity provider",
        phase: "collect",
        complete: ({ values }) =>
          (values["name"] ?? "").trim().length > 0 &&
          https((values["issuer"] ?? "").trim()) &&
          https((values["jwks_uri"] ?? "").trim()),
        body: ({ values, setValue }) => (
          <>
            <TextField
              id="custom-name"
              label="Name"
              value={values["name"] ?? ""}
              onChange={(next) => setValue("name", next)}
              placeholder="CI pipeline"
              help="What this platform is called in Speakeasy."
            />
            <TextField
              id="custom-issuer"
              label="Issuer URL"
              value={values["issuer"] ?? ""}
              onChange={(next) => setValue("issuer", next)}
              placeholder="https://token.example.com"
              help="The iss claim its tokens carry, exactly."
            />
            <TextField
              id="custom-jwks"
              label="JWKS URL"
              value={values["jwks_uri"] ?? ""}
              onChange={(next) => setValue("jwks_uri", next)}
              placeholder="https://token.example.com/.well-known/jwks.json"
              help="Where it publishes the keys its tokens are signed with."
            />
          </>
        ),
      },
      {
        id: "subject",
        short: "Subject",
        title: "Which workloads",
        phase: "create",
        complete: ({ values }) => {
          const subject = (values["subject"] ?? "").trim();
          return subject.length > 0 && !starred(subject);
        },
        body: ({ values, setValue, agentName, ruleConflict }) => (
          <>
            <TextField
              id="custom-subject"
              label="Subject"
              value={values["subject"] ?? ""}
              onChange={(next) => setValue("subject", next)}
              placeholder="repo:acme/api:ref:refs/heads/main"
              help="The sub claim to accept. For a prefix, choose Any subject below and end it on a separator."
            />
            {starred((values["subject"] ?? "").trim()) && (
              <Text small destructive>
                Leave the * out. Choose &ldquo;Any subject starting with
                it&rdquo; below and Speakeasy matches everything under what you
                wrote.
              </Text>
            )}
            <Stack gap={2}>
              <Label htmlFor="custom-match">Match</Label>
              <Select
                value={values["match_kind"] ?? "exact"}
                onValueChange={(next) => setValue("match_kind", next)}
              >
                <SelectTrigger id="custom-match" className="w-full">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="exact">This subject only</SelectItem>
                  <SelectItem value="wildcard">
                    Any subject starting with it
                  </SelectItem>
                </SelectContent>
              </Select>
              <Text muted small>
                A prefix admits every workload under it, so end it where the
                platform&apos;s own separator falls.
              </Text>
            </Stack>
            <div>
              <p className="text-eyebrow">Acts as</p>
              <Text small className="block font-medium">
                {agentName || "this agent"}
              </Text>
              <Text muted small>
                Limited to the servers you selected.
              </Text>
            </div>
            {ruleConflict !== null && (
              <Text small destructive>
                {ruleConflict}
              </Text>
            )}
          </>
        ),
      },
      {
        id: "point",
        short: "Exchange tokens",
        title: "Point it at Speakeasy",
        phase: "connect",
        body: ({ computed, issuerPicker }) => (
          <>
            <p className="text-sm">
              Exchange the platform&apos;s own token for a Speakeasy session at
              this endpoint. Send it as an OAuth token exchange, with the
              platform&apos;s token as the subject token.
            </p>
            <ol className="border-border divide-border divide-y border">
              <ConsoleRow
                number={1}
                label="Exchange its token at this endpoint"
                fields={[
                  {
                    label: "Token endpoint",
                    value: computed.token_endpoint,
                  },
                  {
                    label: "Audience",
                    value: computed.issuer_url,
                    hint: "The token Speakeasy returns is issued for this value.",
                  },
                ]}
              />
              <ConsoleRow
                number={2}
                label="Send the session it gets back to your MCP server"
                fields={[{ label: "MCP host", value: computed.mcp_host }]}
              />
            </ol>
            {issuerPicker}
          </>
        ),
      },
      {
        id: "check",
        short: "Test it",
        title: "Check it worked",
        phase: "connect",
        body: () => (
          <p className="text-sm">
            Run the exchange once from the platform. A workload that signs in
            appears under this agent&apos;s sessions. If it is refused, compare
            the token&apos;s <code className="text-xs">iss</code> and{" "}
            <code className="text-xs">sub</code> with the values you entered — a
            mismatch in either is the usual cause.
          </p>
        ),
      },
    ],
  };
}
