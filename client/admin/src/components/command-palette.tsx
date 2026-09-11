import { keepPreviousData, useQuery } from "@tanstack/react-query";
import {
  detectPlatform,
  formatForDisplay,
  useHotkey,
} from "@tanstack/react-hotkeys";
import { useMatchRoute, useNavigate } from "@tanstack/react-router";
import {
  BuildingIcon,
  CreditCardIcon,
  ExternalLinkIcon,
  FolderIcon,
  HistoryIcon,
  SearchIcon,
  SlidersHorizontalIcon,
  UsersIcon,
} from "lucide-react";
import { Fragment, useRef, useState, type JSX } from "react";

import { Button } from "@/components/ui/button";
import {
  Command,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
  CommandShortcut,
} from "@/components/ui/command";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { useOnUnmount } from "@/hooks/useOnUnmount";
import { organizationQuery, organizationsListQuery } from "@/lib/adminQueries";
import { ADMIN_NAV } from "@/lib/adminNav";
import {
  openOrganizationDashboard,
  type AdminOrganization,
} from "@/lib/gramAdminApi";
import { LEAVES_THE_APP } from "@/lib/impersonation";
import { DISABLED_STATES } from "@/lib/organizationFilters";
import { useOpenOrganization } from "@/pages/organizations/rowActions";

const SEARCH_DEBOUNCE_MS = 200;

// Enough to recognise the record among its near-namesakes, few enough that the
// list does not scroll. An operator who needs the rest wants the table, and the
// Organizations item below this group is what takes them there.
const RESULT_LIMIT = 8;

const NO_ORGS: AdminOrganization[] = [];

// The record's own views, in the order the sidebar reads them. Held at module
// scope because none of it depends on which record is open: the address is the
// one thing the palette supplies per render.
const RECORD_PAGES = [
  { to: "/organizations/$idOrSlug", label: "Overview", icon: BuildingIcon },
  {
    to: "/organizations/$idOrSlug/activity",
    label: "Activity",
    icon: HistoryIcon,
  },
  {
    to: "/organizations/$idOrSlug/projects",
    label: "Projects",
    icon: FolderIcon,
  },
  {
    to: "/organizations/$idOrSlug/billing",
    label: "Billing",
    icon: CreditCardIcon,
  },
  {
    to: "/organizations/$idOrSlug/features",
    label: "Features",
    icon: SlidersHorizontalIcon,
  },
  { to: "/organizations/$idOrSlug/members", label: "Members", icon: UsersIcon },
] as const;

// One spelling of the key, registered from it and drawn from it. `Mod` is the
// library's platform-adaptive modifier: Command on a Mac, Control everywhere
// else, resolved the same way by the matcher and by the label below, so the
// hint an operator reads cannot name a key that does not open anything.
const PALETTE_HOTKEY = "Mod+K";

// The palette does its own matching, so this is the whole of it for the static
// items: a substring over the label and the words behind it. cmdk's built-in
// filter is switched off rather than used, because the organization results are
// the server's answer and not every one of them contains what was typed — an
// id or a WorkOS id matches a record exactly and appears nowhere in its name.
// Leaving cmdk to filter would hide exactly the record a pasted id asked for.
function matches(term: string, haystack: string): boolean {
  return haystack.toLowerCase().includes(term.toLowerCase());
}

// The slug, and the account type behind it, because a search on a common word
// returns several records whose names alone do not tell them apart.
function organizationHint(org: AdminOrganization): string {
  return org.slug ? `${org.slug} · ${org.account_type}` : org.account_type;
}

export function CommandPalette(): JSX.Element {
  const navigate = useNavigate();
  const matchRoute = useMatchRoute();
  const openOrganization = useOpenOrganization();
  // On mount rather than at import, for the reason `AdminLayout` gives about
  // the sidebar cookie: no browser global runs at module scope. Both halves
  // come off the same resolution the matcher makes, so neither the label nor
  // the announcement can name a key that opens nothing on this platform.
  const [shortcut] = useState(() => ({
    label: formatForDisplay(PALETTE_HOTKEY),
    announced: detectPlatform() === "mac" ? "Meta+K" : "Control+K",
  }));

  const [open, setOpen] = useState(false);
  // The box holds what has been typed. `query` holds the term a request has
  // been made for, and the draft reaches it debounced.
  const [term, setTerm] = useState("");
  const [query, setQuery] = useState("");
  const termRef = useRef(term);
  const debounce = useRef<ReturnType<typeof setTimeout>>(undefined);
  useOnUnmount(() => clearTimeout(debounce.current));

  const changeOpen = (next: boolean): void => {
    setOpen(next);
    if (next) return;
    // A term left behind is what the next press would open onto, and it would
    // be sitting above results fetched for it a session ago.
    clearTimeout(debounce.current);
    termRef.current = "";
    setTerm("");
    setQuery("");
  };

  // Toggle rather than open: the same press that opens the palette is the one
  // an operator reaches for to dismiss it. Through `changeOpen` rather than
  // `setOpen`, or the shortcut's half of the dismissal would leave the term
  // behind where Escape and the overlay clear it.
  //
  // `open` is read straight off this render: the hook re-syncs its callback on
  // every one of them, so there is no stale closure to route around.
  //
  // `preventDefault` and the platform resolution are the library's defaults,
  // and it lets a Meta combination through from inside a text field, so the
  // palette opens from the search box on the organizations table as well as
  // from the page.
  useHotkey(PALETTE_HOTKEY, () => changeOpen(!open));

  const changeTerm = (value: string): void => {
    termRef.current = value;
    setTerm(value);

    clearTimeout(debounce.current);
    const next = value.trim();
    // Typing a space, or deleting back to the term already fetched, is not a
    // new question.
    if (next === query) return;

    debounce.current = setTimeout(() => {
      // A later keystroke, or a close, supersedes this one.
      if (termRef.current !== value) return;
      setQuery(next);
    }, SEARCH_DEBOUNCE_MS);
  };

  const trimmed = term.trim();
  const searching = trimmed.length > 0;

  const {
    data,
    isPlaceholderData,
    isError: searchFailed,
  } = useQuery({
    ...organizationsListQuery({
      q: query,
      // Both states, where the table defaults to active only. The palette is
      // how an operator reaches a record they already have in mind, and a
      // disabled organization is a leading reason to go looking for one.
      disabled_states: [...DISABLED_STATES],
      limit: RESULT_LIMIT,
    }),
    // Nothing to ask until something has been typed, and nothing to ask for a
    // closed palette: a stale entry would otherwise refetch on every focus.
    enabled: open && query.length > 0,
    // Each term is its own cache entry, so without this the list empties
    // between keystrokes and the rows jump under the operator's cursor.
    placeholderData: keepPreviousData,
  });

  // Three things have to hold before the rows in hand can be called this term's
  // answer, and each closes a different hole.
  //
  // `settled`: `query` trails the box by the debounce.
  //
  // `!isPlaceholderData`: `placeholderData` hands back the previous term's
  // records while the current term's request is still open. Either of those
  // showing through would put another term's records under the highlight, and
  // Enter lands on whatever is highlighted.
  //
  // `data !== undefined`: the first search of a session has no previous term to
  // hold, so React Query has nothing to mark as placeholder and `data` is
  // simply absent while the request is open. Without this the absence reads as
  // an empty result, and the palette tells the operator their record does not
  // exist in the window before its own request answers.
  const settled = query === trimmed;
  const current = settled && !isPlaceholderData && data !== undefined;
  const results = current ? data.organizations : NO_ORGS;

  // What the Organizations group has to say, given it may have no rows to draw.
  // Split out because three of these are empty, and one "No results" over all
  // three tells an operator a record does not exist when the request for it
  // failed, or has not been made yet.
  //
  // Failure is read before staleness: a request that errors never produces the
  // records that would clear `current`, so the other order reports a dead
  // search as one still running.
  const searchState = !searching
    ? "idle"
    : searchFailed && settled
      ? "failed"
      : !current
        ? "searching"
        : results.length === 0
          ? "empty"
          : "results";

  const lowered = trimmed.toLowerCase();
  const navItems = ADMIN_NAV.filter((item) =>
    matches(lowered, `${item.label} ${item.keywords}`),
  );

  // The record the operator is standing in, by the address they are on rather
  // than by `org.slug`: rewriting it would move the record to another cache
  // entry, which is the rule `RecordNav` is written to.
  const record = matchRoute({ to: "/organizations/$idOrSlug", fuzzy: true });
  const idOrSlug = record ? record.idOrSlug : "";

  // The same entry the sidebar and the record layout read, so the palette costs
  // no second request. Disabled off a record for the reason the sidebar gives:
  // an enabled query here asks for the organization named by an empty string on
  // every page in the app.
  const { data: org } = useQuery({
    ...organizationQuery(idOrSlug),
    enabled: !!idOrSlug,
  });

  const recordPages = org
    ? RECORD_PAGES.filter((page) =>
        matches(lowered, `${page.label} ${org.name}`),
      )
    : [];

  return (
    <>
      <Button
        variant="outline"
        size="sm"
        onClick={() => changeOpen(true)}
        // Named for what it opens rather than what it is. The shortcut is on
        // the button as an attribute too, so it is announced and not only drawn.
        aria-label="Search organizations and pages"
        aria-keyshortcuts={shortcut.announced}
        // Square and iconic where the bar is narrow, an input-shaped box where
        // there is room for one. Never hidden outright: the shortcut is the
        // only other way in, and a narrow screen is the one most likely to be
        // attached to no keyboard at all.
        className="text-muted-foreground ml-auto w-9 justify-center gap-2 px-0 font-normal sm:w-60 sm:justify-start sm:px-3"
      >
        <SearchIcon />
        <span className="hidden sm:inline">Search...</span>
        <CommandShortcut aria-hidden="true" className="hidden sm:inline">
          {shortcut.label}
        </CommandShortcut>
      </Button>

      {/* `CommandDialog` is the stock wrapper for this, and it is composed out
          here rather than used because it does not forward `shouldFilter` to
          the `Command` it holds. Editing it is not an option: `ui/` is vendored
          and the registry owns every file in it. */}
      <Dialog open={open} onOpenChange={changeOpen}>
        <DialogContent className="overflow-hidden p-0" showCloseButton={false}>
          {/* The dialog is named for a screen reader and for nothing else: the
              box below it is the whole of what there is to look at. */}
          <DialogHeader className="sr-only">
            <DialogTitle>Command palette</DialogTitle>
            <DialogDescription>
              Search organizations, or jump to a page
            </DialogDescription>
          </DialogHeader>

          <Command
            // cmdk's own matching would drop a record the server matched on its
            // id. See `matches` above.
            shouldFilter={false}
            className="**:data-[slot=command-input-wrapper]:h-12"
          >
            <CommandInput
              value={term}
              onValueChange={changeTerm}
              placeholder="Search organizations, or jump to a page..."
            />
            <CommandList>
              {searchState !== "idle" && (
                <CommandGroup heading="Organizations">
                  {results.map((result) => (
                    <Fragment key={result.id}>
                      <CommandItem
                        value={result.id}
                        onSelect={() => {
                          changeOpen(false);
                          openOrganization(result);
                        }}
                      >
                        <BuildingIcon />
                        <span className="truncate">{result.name}</span>
                        <span className="text-muted-foreground ml-auto truncate pl-2 text-xs">
                          {result.disabled_at
                            ? `Disabled · ${organizationHint(result)}`
                            : organizationHint(result)}
                        </span>
                      </CommandItem>

                      {/* Indented under the record it acts on, and secondary to
                          it: the record itself is what the operator came for,
                          and this is the other place they might have meant.

                          The dashboard is opened rather than navigated to, so
                          this closes the palette but leaves the admin record in
                          the tab behind it. */}
                      <CommandItem
                        value={`${result.id} open in dashboard`}
                        className="text-muted-foreground pl-8 text-xs"
                        onSelect={() => {
                          // Before the close, though the form does not live in
                          // the dialog: the order is what a reader checks first
                          // when a handoff stops firing.
                          openOrganizationDashboard(result.id);
                          changeOpen(false);
                        }}
                      >
                        <ExternalLinkIcon />
                        <span>Open in Dashboard</span>
                        {/* Which record, because every one of these rows reads
                            alike otherwise, and a screen reader hears them one
                            after another with nothing to tell them apart. */}
                        <span className="sr-only">
                          {` for ${result.name}${LEAVES_THE_APP}`}
                        </span>
                      </CommandItem>
                    </Fragment>
                  ))}

                  {searchState !== "results" && (
                    <div className="text-muted-foreground px-2 py-3 text-sm">
                      {searchState === "failed"
                        ? "Could not search organizations."
                        : searchState === "empty"
                          ? "No organizations match."
                          : "Searching..."}
                    </div>
                  )}
                </CommandGroup>
              )}

              {recordPages.length > 0 && org && (
                <CommandGroup heading={org.name}>
                  {recordPages.map(({ to, label, icon: Icon }) => (
                    <CommandItem
                      key={to}
                      value={to}
                      onSelect={() => {
                        changeOpen(false);
                        void navigate({ to, params: { idOrSlug } });
                      }}
                    >
                      <Icon />
                      <span>{label}</span>
                    </CommandItem>
                  ))}
                </CommandGroup>
              )}

              {navItems.length > 0 && (
                <CommandGroup heading="Go to">
                  {navItems.map(({ to, label, icon: Icon }) => (
                    <CommandItem
                      key={to}
                      value={to}
                      onSelect={() => {
                        changeOpen(false);
                        void navigate({ to });
                      }}
                    >
                      <Icon />
                      <span>{label}</span>
                    </CommandItem>
                  ))}
                </CommandGroup>
              )}
            </CommandList>
          </Command>
        </DialogContent>
      </Dialog>
    </>
  );
}
