import {
  useGitHubAccountSearch,
  MIN_GITHUB_SEARCH_LENGTH,
  type GithubUserResult,
} from "@/hooks/useGitHubAccountSearch";
import { GitHubAccountResults } from "@/components/GitHubAccountResults";
import { Input } from "@/components/moon/input";
import { Label } from "@/components/moon/label";
import { Dialog } from "@/components/ui/Dialog";
import { Button } from "@/components/ui/Button";
import { Icon } from "@/components/ui/Icon";
import { memo, useCallback, useEffect, useRef, useState } from "react";

interface PublishDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onPublish: (githubUsernames: string[]) => void;
  isPending: boolean;
  mode?: "publish" | "manage";
}

// GitHub username rules: 1-39 chars, alphanumeric or hyphen, cannot start with hyphen.
const GITHUB_USERNAME_RE = /^[a-zA-Z0-9][a-zA-Z0-9-]{0,38}$/;
export const PublishDialog = memo(function PublishDialog({
  open,
  onOpenChange,
  onPublish,
  isPending,
  mode = "publish",
}: PublishDialogProps) {
  const isManage = mode === "manage";
  const [usernames, setUsernames] = useState<string[]>([]);
  const [draft, setDraft] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [focused, setFocused] = useState(false);
  const [activeIndex, setActiveIndex] = useState(0);
  const containerRef = useRef<HTMLDivElement | null>(null);

  const search = useGitHubAccountSearch(
    open ? draft.trim().replace(/^@/, "") : "",
  );
  const results = search.results.filter(
    (item) => !usernames.includes(item.login),
  );
  const searchLoading = search.loading;

  // Close dropdown on outside click.
  useEffect(() => {
    if (!focused) return;
    const handler = (e: MouseEvent) => {
      if (!containerRef.current) return;
      if (!containerRef.current.contains(e.target as Node)) {
        setFocused(false);
      }
    };
    document.addEventListener("mousedown", handler);
    return () => document.removeEventListener("mousedown", handler);
  }, [focused]);

  const addUsername = useCallback((login: string): boolean => {
    const trimmed = login.trim().replace(/^@/, "");
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

  const selectResult = useCallback(
    (user: GithubUserResult) => {
      if (addUsername(user.login)) {
        setDraft("");
        setActiveIndex(0);
      }
    },
    [addUsername],
  );

  const handleKeyDown = useCallback(
    (e: React.KeyboardEvent<HTMLInputElement>) => {
      // Two distinct conditions: arrow nav only makes sense with results; Escape
      // should close any visible panel (loading + empty-results states too).
      const hasResultsForNav = focused && results.length > 0;
      const panelVisible =
        focused &&
        (results.length > 0 ||
          searchLoading ||
          draft.trim().length >= MIN_GITHUB_SEARCH_LENGTH);

      if (hasResultsForNav && e.key === "ArrowDown") {
        e.preventDefault();
        setActiveIndex((i) => (i + 1) % results.length);
        return;
      }
      if (hasResultsForNav && e.key === "ArrowUp") {
        e.preventDefault();
        setActiveIndex((i) => (i - 1 + results.length) % results.length);
        return;
      }
      if (e.key === "Enter") {
        e.preventDefault();
        if (hasResultsForNav && results[activeIndex]) {
          selectResult(results[activeIndex]);
        } else if (addUsername(draft)) {
          setDraft("");
        }
        return;
      }
      if (e.key === "Escape" && panelVisible) {
        e.preventDefault();
        setFocused(false);
        return;
      }
      if (e.key === "," || e.key === " " || e.key === "Tab") {
        if (draft.trim()) {
          e.preventDefault();
          if (addUsername(draft)) setDraft("");
        }
        return;
      }
      if (e.key === "Backspace" && draft === "" && usernames.length) {
        setUsernames((prev) => prev.slice(0, -1));
      }
    },
    [
      focused,
      results,
      activeIndex,
      draft,
      usernames.length,
      addUsername,
      selectResult,
      searchLoading,
    ],
  );

  const handlePaste = useCallback(
    (e: React.ClipboardEvent<HTMLInputElement>) => {
      const text = e.clipboardData.getData("text");
      if (!/[\s,]/.test(text)) return;
      e.preventDefault();
      const parts = text.split(/[\s,]+/).filter(Boolean);
      let ok = true;
      for (const p of parts) {
        if (!addUsername(p)) {
          ok = false;
          break;
        }
      }
      if (ok) setDraft("");
    },
    [addUsername],
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

  const dropdownOpen =
    focused &&
    (results.length > 0 ||
      searchLoading ||
      (draft.trim().length >= MIN_GITHUB_SEARCH_LENGTH && !searchLoading));

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <Dialog.Content>
        <Dialog.Header>
          <Dialog.Title>
            {isManage ? "Add collaborators" : "Publish Plugins"}
          </Dialog.Title>
          {isManage ? (
            <>
              <Dialog.Description>
                Add new GitHub users as collaborators on the marketplace repo.
                Existing collaborators are not shown here — anyone you add below
                is granted access in addition to those already attached to the
                repo.
              </Dialog.Description>
              <Dialog.Description>
                Collaborators are added as repository admins, which is what
                platform marketplaces such as Cursor require to serve these
                plugins to your team. Adding someone who already has access
                upgrades them to admin.
              </Dialog.Description>
              <Dialog.Description>
                To view or remove existing collaborators, open the repository on
                GitHub. ZIP recipients and Claude Code users installing through
                the Speakeasy marketplace proxy do not each need to be GitHub
                collaborators.
              </Dialog.Description>
              <Dialog.Description>
                <strong className="text-foreground font-semibold">
                  New collaborators may receive a GitHub invitation. Accept it
                  in GitHub or via the notification before accessing the private
                  repository directly. Existing collaborators need no new
                  invitation.
                </strong>
              </Dialog.Description>
            </>
          ) : (
            <>
              <Dialog.Description>
                Publish all plugins to a GitHub repository. Optionally add
                collaborators, who become admins on the repo so they can connect
                it to Cursor, Claude and other marketplaces.
              </Dialog.Description>
              <Dialog.Description>
                The person importing this private repository into a vendor
                marketplace needs GitHub repository access and permission to
                authorize the vendor's GitHub App. ZIP recipients and Claude
                Code users installing through the Speakeasy marketplace proxy do
                not each need to be GitHub collaborators.
              </Dialog.Description>
              <Dialog.Description>
                <strong className="text-foreground font-semibold">
                  New collaborators may receive a GitHub invitation. Accept it
                  in GitHub or via the notification before accessing the private
                  repository directly. Existing collaborators need no new
                  invitation.
                </strong>
              </Dialog.Description>
            </>
          )}
        </Dialog.Header>
        <form onSubmit={handleSubmit} className="flex flex-col gap-4">
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="githubUsernames">GitHub Usernames</Label>
            <div ref={containerRef} className="relative">
              <div className="border-input focus-within:border-ring focus-within:ring-ring/50 flex min-h-9 flex-wrap items-center gap-1.5 border bg-transparent px-2 py-1 focus-within:ring-[3px]">
                {usernames.map((u, idx) => (
                  <span
                    key={u}
                    className="bg-primary/10 text-primary inline-flex items-center gap-1 px-2 py-0.5 text-xs"
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
                    setActiveIndex(0);
                    if (error) setError(null);
                  }}
                  onKeyDown={handleKeyDown}
                  onPaste={handlePaste}
                  onFocus={() => setFocused(true)}
                  placeholder={
                    usernames.length === 0 ? "Search GitHub users…" : ""
                  }
                  autoComplete="off"
                  autoCorrect="off"
                  autoCapitalize="off"
                  spellCheck={false}
                  data-lpignore="true"
                  data-1p-ignore="true"
                  data-bwignore="true"
                  data-form-type="other"
                  role="combobox"
                  aria-expanded={dropdownOpen && results.length > 0}
                  aria-controls={
                    dropdownOpen && results.length
                      ? "github-user-results"
                      : undefined
                  }
                  aria-activedescendant={
                    dropdownOpen && results[activeIndex]
                      ? `github-user-results-${activeIndex}`
                      : undefined
                  }
                  aria-autocomplete="list"
                  className="h-7 flex-1 border-0 bg-transparent p-0 shadow-none focus-visible:ring-0"
                />
              </div>

              {dropdownOpen && (
                <GitHubAccountResults
                  id="github-user-results"
                  label="GitHub accounts"
                  results={results}
                  activeIndex={activeIndex}
                  onActiveIndexChange={setActiveIndex}
                  onSelect={selectResult}
                  loading={searchLoading}
                  emptyMessage={
                    <>
                      No matches. Press Enter to add{" "}
                      <code className="text-foreground">
                        {draft.trim().replace(/^@/, "")}
                      </code>{" "}
                      anyway.
                    </>
                  }
                />
              )}
            </div>
            <p className="text-muted-foreground text-xs">
              Start typing to search GitHub. Click a result, or press Enter to
              add a handle you've typed.
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
              {isPending
                ? isManage
                  ? "Adding..."
                  : "Publishing..."
                : isManage
                  ? "Add collaborators"
                  : "Publish"}
            </Button>
          </Dialog.Footer>
        </form>
      </Dialog.Content>
    </Dialog>
  );
});
