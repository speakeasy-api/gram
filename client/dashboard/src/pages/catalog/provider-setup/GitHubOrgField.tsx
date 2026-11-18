import { useGitHubAccountSearch } from "@/hooks/useGitHubAccountSearch";
import { type JSX, useId, useState } from "react";
import { Input } from "@/components/ui/Input";
import { GitHubAccountResults } from "@/components/GitHubAccountResults";
import { Field, FieldError, FieldLabel } from "@/components/ui/Field";
import { normalizeGitHubOrg } from "./github";
import { useSetup } from "./setup-context";

export function GitHubOrgField(): JSX.Element {
  const { s, live } = useSetup();
  const [open, setOpen] = useState(false);
  const [activeIndex, setActiveIndex] = useState(-1);
  const listId = useId();
  const query = s.values.org.trim();
  const canSearch = /^[a-zA-Z0-9][a-zA-Z0-9-]{0,38}$/.test(query);
  const showResults = open && canSearch;
  const search = useGitHubAccountSearch(showResults ? query : "", true);
  const results = search.results;
  const listOpen = showResults && results.length > 0;
  const error = s.errors.org;
  const select = (login: string) => {
    live.changeField({ field: "org", value: login });
    setOpen(false);
    setActiveIndex(-1);
  };
  let status = "No organizations found. You can enter the name manually.";
  if (search.failed)
    status = "Search unavailable. You can enter the name manually.";

  return (
    <Field className="mt-6" data-invalid={!!error}>
      <FieldLabel htmlFor="setup-github-org">GitHub organization</FieldLabel>
      <div className="relative">
        <Input
          id="setup-github-org"
          role="combobox"
          icon="search"
          placeholder="Search or paste a GitHub organization URL"
          value={s.values.org}
          noAutofill
          error={!!error}
          aria-autocomplete="list"
          aria-expanded={listOpen}
          aria-controls={listOpen ? listId : undefined}
          aria-activedescendant={
            showResults && results[activeIndex]
              ? `${listId}-${activeIndex}`
              : undefined
          }
          aria-invalid={!!error}
          aria-describedby={error ? "setup-github-org-error" : undefined}
          onFocus={() => setOpen(true)}
          onBlur={() => {
            setOpen(false);
            setActiveIndex(-1);
          }}
          onChange={(value) => {
            live.changeField({
              field: "org",
              value: normalizeGitHubOrg(value),
            });
            setOpen(true);
            setActiveIndex(-1);
          }}
          onKeyDown={(event) => {
            if (event.key === "Escape" && open) {
              event.preventDefault();
              event.stopPropagation();
              setOpen(false);
              setActiveIndex(-1);
            }
            if (["ArrowDown", "ArrowUp"].includes(event.key)) {
              event.preventDefault();
              setOpen(true);
              if (results.length)
                setActiveIndex((index) => {
                  if (index === -1)
                    return event.key === "ArrowDown" ? 0 : results.length - 1;
                  return (
                    (index +
                      (event.key === "ArrowDown" ? 1 : results.length - 1)) %
                    results.length
                  );
                });
            }
            if (event.key === "Enter" && showResults && results[activeIndex]) {
              event.preventDefault();
              select(results[activeIndex].login);
            }
          }}
        />
        {showResults && (
          <GitHubAccountResults
            id={listId}
            label="GitHub organizations"
            results={results}
            activeIndex={activeIndex}
            onActiveIndexChange={setActiveIndex}
            onSelect={(account) => select(account.login)}
            loading={search.loading}
            emptyMessage={status}
          />
        )}
      </div>
      {error && <FieldError id="setup-github-org-error">{error}</FieldError>}
    </Field>
  );
}
