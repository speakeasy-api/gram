import type { RemoteMcpServerHeader } from "@gram/client/models/components/remotemcpserverheader.js";
import type { useCreateRemoteMcpServerHeaderMutation } from "@gram/client/react-query/createRemoteMcpServerHeader.js";
import type { useUpdateRemoteMcpServerHeaderMutation } from "@gram/client/react-query/updateRemoteMcpServerHeader.js";
import { useState } from "react";
import { toast } from "sonner";

const REDACTED_SECRET = "***";

export type AgentCredentialFormat =
  | "bearer"
  | "basic"
  | "manual"
  | "client-credentials";

function formatFromHeader(
  header: RemoteMcpServerHeader | undefined,
): AgentCredentialFormat {
  // Nothing configured yet starts on Bearer — the format almost every upstream
  // wants — rather than on Manual, which asks the operator to hand-assemble a
  // header before they have seen the simpler options.
  if (!header) return "bearer";
  const value = header.value ?? "";
  if (value.startsWith("Basic ")) return "basic";
  if (value.startsWith("Bearer ") || value === REDACTED_SECRET) return "bearer";
  return "manual";
}

function encodeBasicCredential(username: string, password: string): string {
  const bytes = new TextEncoder().encode(`${username}:${password}`);
  let binary = "";
  for (const byte of bytes) binary += String.fromCharCode(byte);
  return btoa(binary);
}

/** Bullets rather than the literal secret, capped so a long token can't wrap
 * the preview into a paragraph. */
function mask(value: string): string {
  return value ? "•".repeat(Math.min(value.length, 28)) : "";
}

/** The credential form's own state: what the operator typed, what the upstream
 * would receive, and how it reads on screen. No persistence — the settings
 * section saves it to a header, the creation flows hand it to the create RPC. */
export type AgentCredentialFields = {
  format: AgentCredentialFormat;
  setFormat: (format: AgentCredentialFormat) => void;
  prefix: string;
  setPrefix: (prefix: string) => void;
  token: string;
  setToken: (token: string) => void;
  username: string;
  setUsername: (username: string) => void;
  password: string;
  setPassword: (password: string) => void;
  manualValue: string;
  setManualValue: (value: string) => void;
  reveal: boolean;
  toggleReveal: () => void;
  /** The exact Authorization value the upstream receives, masked unless revealed. */
  preview: string;
  /** The unmasked header value, or "" while the form is incomplete. */
  authorizationValue: string;
  clear: () => void;
};

export function useAgentCredentialFields(
  authorizationHeader?: RemoteMcpServerHeader,
): AgentCredentialFields {
  const [format, setFormat] = useState<AgentCredentialFormat>(() =>
    formatFromHeader(authorizationHeader),
  );
  const initialValue = authorizationHeader?.value ?? "";
  const [prefix, setPrefix] = useState("Bearer");
  const [token, setToken] = useState(
    initialValue.startsWith("Bearer ") ? initialValue.slice(7) : "",
  );
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [manualValue, setManualValue] = useState(
    formatFromHeader(authorizationHeader) === "manual" ? initialValue : "",
  );
  const [reveal, setReveal] = useState(false);

  let authorizationValue = "";
  if (format === "bearer" && token) {
    authorizationValue = prefix.trim() ? `${prefix.trim()} ${token}` : token;
  } else if (format === "basic" && username && password) {
    authorizationValue = `Basic ${encodeBasicCredential(username, password)}`;
  } else if (format === "manual") {
    authorizationValue = manualValue;
  }

  let preview: string;
  if (format === "bearer") {
    const shown = token ? (reveal ? token : mask(token)) : "<token>";
    preview = prefix.trim() ? `${prefix.trim()} ${shown}` : shown;
  } else if (format === "basic") {
    if (username || password) {
      const encoded = encodeBasicCredential(username, password);
      preview = `Basic ${reveal ? encoded : mask(encoded)}`;
    } else {
      preview = "Basic <base64(username:password)>";
    }
  } else {
    preview = manualValue
      ? reveal
        ? manualValue
        : mask(manualValue)
      : "<header value>";
  }

  return {
    format,
    setFormat,
    prefix,
    setPrefix,
    token,
    setToken,
    username,
    setUsername,
    password,
    setPassword,
    manualValue,
    setManualValue,
    reveal,
    toggleReveal: (): void => setReveal((current) => !current),
    preview,
    authorizationValue,
    clear: (): void => {
      setToken("");
      setUsername("");
      setPassword("");
      setManualValue("");
    },
  };
}

export type AgentCredentialDraft = AgentCredentialFields & {
  canSave: boolean;
  saving: boolean;
  save: () => Promise<boolean>;
};

/**
 * Owns the static Authorization credential every caller of this server shares.
 * The value is written to the backing Remote MCP source, so the section's Save
 * drives it rather than a button of its own.
 */
export function useAgentCredentialDraft({
  remoteMcpServerId,
  authorizationHeader,
  onSaved,
  createHeader,
  updateHeader,
}: {
  remoteMcpServerId: string;
  authorizationHeader: RemoteMcpServerHeader | undefined;
  onSaved: () => Promise<boolean>;
  createHeader: ReturnType<typeof useCreateRemoteMcpServerHeaderMutation>;
  updateHeader: ReturnType<typeof useUpdateRemoteMcpServerHeaderMutation>;
}): AgentCredentialDraft {
  const fields = useAgentCredentialFields(authorizationHeader);
  const { authorizationValue, format, clear } = fields;
  const saving = createHeader.isPending || updateHeader.isPending;

  const save = async (): Promise<boolean> => {
    if (format === "client-credentials" || authorizationValue.trim() === "") {
      return false;
    }
    try {
      if (authorizationHeader) {
        const preserveRedacted = authorizationValue === REDACTED_SECRET;
        await updateHeader.mutateAsync({
          request: {
            updateServerHeaderForm: {
              id: authorizationHeader.id,
              name: "Authorization",
              isRequired: true,
              isSecret: true,
              value: preserveRedacted ? undefined : authorizationValue,
            },
          },
        });
      } else {
        await createHeader.mutateAsync({
          request: {
            createServerHeaderForm: {
              remoteMcpServerId,
              name: "Authorization",
              isRequired: true,
              isSecret: true,
              value: authorizationValue,
            },
          },
        });
      }
      const refreshed = await onSaved();
      if (!refreshed) {
        toast.warning("Credential saved, but headers could not be refreshed.");
        return true;
      }
      clear();
      toast.success("Agent Identity updated");
      return true;
    } catch (error) {
      toast.error(
        error instanceof Error
          ? error.message
          : "Failed to update Agent Identity",
      );
      return false;
    }
  };

  return {
    ...fields,
    canSave:
      format !== "client-credentials" && authorizationValue.trim() !== "",
    saving,
    save,
  };
}
