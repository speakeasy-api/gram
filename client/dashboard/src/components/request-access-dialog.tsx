import { RequestDurationSelect } from "@/components/request-duration-select";
import { Button } from "@/components/ui/Button";
import { Dialog } from "@/components/ui/Dialog";
import { TextArea } from "@/components/ui/Textarea";
import { Text } from "@/components/ui/Text";
import type { RequestedDurationKey } from "@/pages/access/requestedDuration";
import { useEffect, useId, useState } from "react";

/**
 * The longest note the server accepts with a request, in characters (Unicode
 * code points, as the server counts them, not UTF-16 units).
 */
const MAX_MESSAGE_LENGTH = 1000;

/** Keeps the first max characters of text, counting code points. */
function limitCharacters(text: string, max: number): string {
  const characters = Array.from(text);
  return characters.length > max ? characters.slice(0, max).join("") : text;
}

/** What a requester fills in before a request is sent. */
export type AccessRequestDraft = {
  duration: RequestedDurationKey;
  message: string;
};

/**
 * Asks the organization's administrators for access the viewer was denied:
 * what is being asked for, how long it is needed, and a note. How long is
 * offered only while requests are stored, since only a stored request keeps
 * it; a note is always sent.
 */
export function RequestAccessDialog({
  open,
  onOpenChange,
  permissions,
  resourceName,
  askDuration,
  sending,
  onSubmit,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  /** The permissions being asked for, as the role editor names them. */
  permissions: string[];
  resourceName?: string;
  /** Offer "How long do you need it?". */
  askDuration: boolean;
  sending: boolean;
  onSubmit: (draft: AccessRequestDraft) => void;
}): React.JSX.Element {
  const [duration, setDuration] = useState<RequestedDurationKey>("none");
  const [message, setMessage] = useState("");
  const messageId = useId();
  // Each opening starts a new request: nothing typed or picked before a
  // cancel carries over. Callers key the dialog by what is asked for, so a
  // different permission or resource starts afresh too.
  useEffect(() => {
    if (open) {
      setDuration("none");
      setMessage("");
    }
  }, [open]);

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <Dialog.Content className="max-w-md">
        <Dialog.Header>
          <Dialog.Title>Request access</Dialog.Title>
          <Dialog.Description>
            Your organization administrators are asked to give you this access.
            They can approve it for a limited time.
          </Dialog.Description>
        </Dialog.Header>
        <form
          className="space-y-4"
          onSubmit={(event) => {
            event.preventDefault();
            onSubmit({ duration, message: message.trim() });
          }}
        >
          <div className="bg-muted/25 space-y-1 border px-4 py-3 text-sm">
            <div className="flex items-start justify-between gap-4">
              <span className="text-muted-foreground">Permission</span>
              <span className="text-right">{permissions.join(", ")}</span>
            </div>
            <div className="flex items-start justify-between gap-4">
              <span className="text-muted-foreground">Resource</span>
              <span className="text-right">
                {resourceName || "Every resource"}
              </span>
            </div>
          </div>
          {askDuration && (
            <RequestDurationSelect
              value={duration}
              onChange={setDuration}
              disabled={sending}
            />
          )}
          <div className="space-y-1.5">
            <label htmlFor={messageId} className="text-sm">
              Why do you need it?{" "}
              <span className="text-muted-foreground">(optional)</span>
            </label>
            <TextArea
              id={messageId}
              value={message}
              onChange={(value) =>
                setMessage(limitCharacters(value, MAX_MESSAGE_LENGTH))
              }
              disabled={sending}
              rows={3}
              placeholder="For example: investigating a failed tool call"
            />
            {Array.from(message).length >= MAX_MESSAGE_LENGTH && (
              <Text muted small>
                Notes are limited to {MAX_MESSAGE_LENGTH} characters.
              </Text>
            )}
          </div>
          <Dialog.Footer>
            <Button
              type="button"
              variant="secondary"
              onClick={() => onOpenChange(false)}
              disabled={sending}
            >
              Cancel
            </Button>
            <Button type="submit" variant="primary" disabled={sending}>
              {sending ? "Sending…" : "Send request"}
            </Button>
          </Dialog.Footer>
        </form>
      </Dialog.Content>
    </Dialog>
  );
}
