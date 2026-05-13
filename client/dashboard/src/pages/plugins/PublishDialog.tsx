import { Input } from "@/components/moon/input";
import { Label } from "@/components/moon/label";
import { Dialog } from "@/components/ui/dialog";
import { Button, Icon } from "@speakeasy-api/moonshine";
import { memo, useCallback, useState } from "react";

interface PublishDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onPublish: (githubUsernames: string[]) => void;
  isPending: boolean;
}

// GitHub username rules: 1-39 chars, alphanumeric or hyphen, cannot start with hyphen.
const GITHUB_USERNAME_RE = /^[a-zA-Z0-9][a-zA-Z0-9-]{0,38}$/;

export const PublishDialog = memo(function PublishDialog({
  open,
  onOpenChange,
  onPublish,
  isPending,
}: PublishDialogProps) {
  const [usernames, setUsernames] = useState<string[]>([]);
  const [draft, setDraft] = useState("");
  const [error, setError] = useState<string | null>(null);

  const commitDraft = useCallback((value: string): boolean => {
    const trimmed = value.trim().replace(/^@/, "");
    if (!trimmed) return true;
    if (!GITHUB_USERNAME_RE.test(trimmed)) {
      setError(`"${trimmed}" is not a valid GitHub username.`);
      return false;
    }
    setUsernames((prev) =>
      prev.includes(trimmed) ? prev : [...prev, trimmed],
    );
    setError(null);
    return true;
  }, []);

  const handleKeyDown = useCallback(
    (e: React.KeyboardEvent<HTMLInputElement>) => {
      if (e.key === "Enter" || e.key === "," || e.key === " ") {
        e.preventDefault();
        if (commitDraft(draft)) setDraft("");
      } else if (e.key === "Backspace" && draft === "" && usernames.length) {
        setUsernames((prev) => prev.slice(0, -1));
      }
    },
    [draft, usernames.length, commitDraft],
  );

  const handlePaste = useCallback(
    (e: React.ClipboardEvent<HTMLInputElement>) => {
      const text = e.clipboardData.getData("text");
      if (!/[\s,]/.test(text)) return;
      e.preventDefault();
      const parts = text.split(/[\s,]+/).filter(Boolean);
      let ok = true;
      for (const p of parts) {
        if (!commitDraft(p)) {
          ok = false;
          break;
        }
      }
      if (ok) setDraft("");
    },
    [commitDraft],
  );

  const removeAt = useCallback((idx: number) => {
    setUsernames((prev) => prev.filter((_, i) => i !== idx));
  }, []);

  const handleSubmit = useCallback(
    (e: React.FormEvent<HTMLFormElement>) => {
      e.preventDefault();
      const all = [...usernames];
      const trimmed = draft.trim().replace(/^@/, "");
      if (trimmed) {
        if (!GITHUB_USERNAME_RE.test(trimmed)) {
          setError(`"${trimmed}" is not a valid GitHub username.`);
          return;
        }
        if (!all.includes(trimmed)) all.push(trimmed);
      }
      onPublish(all);
      // Dialog close is driven by the parent's mutation onSuccess so the
      // pending state stays visible during the publish.
    },
    [draft, usernames, onPublish],
  );

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <Dialog.Content>
        <Dialog.Header>
          <Dialog.Title>Publish Plugins</Dialog.Title>
          <Dialog.Description>
            Publish all plugins to a GitHub repository. Optionally add
            collaborators who will receive read access to the repo.
          </Dialog.Description>
          <Dialog.Description>
            At least one user in your organization will need to be given access
            to connect the generated repository with Claude, Cursor, or Codex.
          </Dialog.Description>
        </Dialog.Header>
        <form onSubmit={handleSubmit} className="flex flex-col gap-4">
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="githubUsernames">GitHub Usernames</Label>
            <div className="border-input focus-within:border-ring focus-within:ring-ring/50 flex min-h-9 flex-wrap items-center gap-1.5 rounded-md border bg-transparent px-2 py-1 focus-within:ring-[3px]">
              {usernames.map((u, idx) => (
                <span
                  key={u}
                  className="bg-primary/10 text-primary inline-flex items-center gap-1 rounded-md px-2 py-0.5 text-xs"
                >
                  {u}
                  <button
                    type="button"
                    onClick={() => removeAt(idx)}
                    aria-label={`Remove ${u}`}
                    className="hover:opacity-70"
                  >
                    <Icon name="x" className="h-3 w-3" />
                  </button>
                </span>
              ))}
              <Input
                id="githubUsernames"
                value={draft}
                onChange={(e) => {
                  setDraft(e.target.value);
                  if (error) setError(null);
                }}
                onKeyDown={handleKeyDown}
                onPaste={handlePaste}
                onBlur={() => {
                  if (commitDraft(draft)) setDraft("");
                }}
                placeholder={
                  usernames.length === 0 ? "e.g. octocat, hubot" : ""
                }
                className="h-7 flex-1 border-0 bg-transparent p-0 shadow-none focus-visible:ring-0"
              />
            </div>
            <p className="text-muted-foreground text-xs">
              Press Enter, Space, or comma to add. Paste a list to add several
              at once.
            </p>
            {error && <p className="text-destructive text-xs">{error}</p>}
          </div>
          <Dialog.Footer>
            <Button
              variant="secondary"
              onClick={() => onOpenChange(false)}
              type="button"
            >
              Cancel
            </Button>
            <Button type="submit" disabled={isPending}>
              {isPending ? "Publishing..." : "Publish"}
            </Button>
          </Dialog.Footer>
        </form>
      </Dialog.Content>
    </Dialog>
  );
});
