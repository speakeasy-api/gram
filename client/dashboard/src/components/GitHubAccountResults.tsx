import type { ReactNode } from "react";
import type { GithubUserResult } from "@/hooks/useGitHubAccountSearch";
import { Spinner } from "@/components/ui/Spinner";

interface GitHubAccountResultsProps {
  id: string;
  label: string;
  results: GithubUserResult[];
  activeIndex: number;
  onActiveIndexChange: (index: number) => void;
  onSelect: (account: GithubUserResult) => void;
  loading: boolean;
  emptyMessage: ReactNode;
}

export function GitHubAccountResults({
  id,
  label,
  results,
  activeIndex,
  onActiveIndexChange,
  onSelect,
  loading,
  emptyMessage,
}: GitHubAccountResultsProps) {
  return (
    <div className="bg-popover text-popover-foreground absolute z-50 mt-1 max-h-56 w-full overflow-y-auto border p-1 shadow-lg">
      {results.length > 0 ? (
        <div id={id} role="listbox" aria-label={label}>
          {results.map((account, index) => (
            <button
              key={account.login}
              id={`${id}-${index}`}
              type="button"
              role="option"
              tabIndex={-1}
              aria-selected={index === activeIndex}
              onMouseDown={(event) => event.preventDefault()}
              onMouseEnter={() => onActiveIndexChange(index)}
              onClick={() => onSelect(account)}
              className="flex w-full items-center gap-2 px-2 py-1 text-left aria-selected:bg-accent aria-selected:text-accent-foreground"
            >
              <img
                src={account.avatar_url}
                alt=""
                className="h-5 w-5 flex-shrink-0 rounded-full"
                loading="lazy"
              />
              <span className="text-foreground text-sm">{account.login}</span>
            </button>
          ))}
        </div>
      ) : (
        <div
          role="status"
          className="text-muted-foreground px-2 py-1.5 text-xs"
        >
          {loading ? (
            <span className="flex items-center gap-2">
              <Spinner className="mr-0 h-3 w-3" />
              Searching GitHub…
            </span>
          ) : (
            emptyMessage
          )}
        </div>
      )}
    </div>
  );
}
