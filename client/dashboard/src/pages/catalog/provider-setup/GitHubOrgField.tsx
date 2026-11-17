import { useGitHubAccountSearch } from "@/hooks/useGitHubAccountSearch";
import { type JSX, useId, useState } from "react";
import { Input } from "@/components/ui/Input";
import { Button } from "@/components/ui/Button";
import { Field, FieldError, FieldLabel } from "@/components/ui/Field";
import { normalizeGitHubOrg } from "./github";
import { useSetup } from "./setup-context";

export function GitHubOrgField(): JSX.Element {
  const { s, live } = useSetup();
  const [open, setOpen] = useState(false);
  const [activeIndex, setActiveIndex] = useState(-1);
  const listId = useId();
  const query = s.values.org.trim();
  const canSearch = /^[a-zA-Z0-9][a-zA-Z0-9-]{1,38}$/.test(query);
  const showResults = open && canSearch;
  const search = useGitHubAccountSearch(showResults ? query : "", true);
  const results = search.results.map((item) => item.login);
  const error = s.errors.org;
  const select = (login: string) => {
    live.changeField({ field: "org", value: login });
    setOpen(false);
    setActiveIndex(-1);
  };
  let status = "No organizations found. You can enter the name manually.";
  if (search.loading) status = "Searching GitHub…";
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
          aria-expanded={showResults}
          aria-controls={showResults ? listId : undefined}
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
              select(results[activeIndex]);
            }
          }}
        />
        {showResults && (
          <div
            id={listId}
            role="listbox"
            className="bg-popover absolute top-full right-0 left-0 z-50 border p-1 shadow-md"
            aria-label="GitHub organizations"
          >
            {results.map((login, index) => (
              <Button
                key={login}
                id={`${listId}-${index}`}
                role="option"
                tabIndex={-1}
                variant="tertiary"
                className="w-full justify-start aria-selected:bg-accent"
                aria-selected={index === activeIndex}
                onMouseDown={(event) => event.preventDefault()}
                onClick={() => select(login)}
              >
                {login}
              </Button>
            ))}
            {results.length === 0 && (
              <p
                role="status"
                className="text-muted-foreground px-2 py-3 text-sm"
              >
                {status}
              </p>
            )}
          </div>
        )}
      </div>
      {error && <FieldError id="setup-github-org-error">{error}</FieldError>}
    </Field>
  );
}
