# admin

## 0.2.2

### Patch Changes

- 3a8f15f: Add pay-as-you-go as a first-class billing tier across server entitlements, management API tier data, and dashboard and admin tier controls. PAYG organizations receive enterprise feature access with capped PAYG billing behavior, and Stripe-authoritative tier state cannot be overwritten by stale Polar data.

## 0.2.1

### Patch Changes

- d375c27: Platform admins can now create an organization from the organizations list. The
  endpoint shipped without a caller, so opening an organization still meant leaving
  Gram for the WorkOS dashboard. A Create organization button sits at the end of
  the toolbar, takes a name, and creates the organization.

  What it creates is deliberately plain: no members, not whitelisted, no trial, and
  on the free tier. The server validates the name exactly as self-serve signup
  validates it, so an operator cannot name an organization something a customer
  could not have named it.

  The list is fetched again after a create rather than having the new row written
  into the page on screen. Where that row belongs depends on the sort, the filters
  and the page the operator is on, so putting it anywhere by hand would put it
  somewhere the server would not have. For the same reason the confirmation names
  the organization that was created and says the list may not show it: a list
  filtered to running trials is right to leave a new free-tier organization out,
  and the operator still needs to be told the write landed.

  A refused name leaves the dialog open with the name still in it and the server's
  reason beside it, because a rejected name is one the operator wants to edit
  rather than retype. A deployment with no WorkOS configuration refuses this way
  too, so it reports that it cannot create organizations instead of failing
  silently. A refusal also leaves the list as it found it, so pressing Create while
  the list is still loading cannot leave the table saying there are no
  organizations. While a create is in flight, the confirm button, the cancel button, the
  close control and the Escape key are all held, so one press creates one
  organization.

- 06d079f: An organization record's Overview now reads as three named groups rather than
  one flat list of facts. Identity carries the name, slug, organization id, WorkOS
  id and the created and updated dates. Plan carries the account type and the
  trial. Access carries Whitelisted and Disabled at.

  Whitelisted keeps its name and sits on its own, because it gates the
  organization's access to the platform rather than expressing a preference.

  The slug, the organization id and the WorkOS id can each be copied with one
  click, the same control the organizations list already uses when you peek at a
  record. An organization with no WorkOS id gets no copy button rather than a
  button that copies a dash.

  The Members row is gone from the Overview. The record's own nav already counts
  members, so the row said the same thing twice.

- a595516: An organization record's editable facts are now committed one at a time. The
  save bar that sat under the whole record is gone, along with the draft it wrote
  into, so the record no longer holds an unsaved change.

  Changing the account type, or the Whitelisted switch, raises a confirmation
  naming that one change and nothing else: "Account type: pro → enterprise." The
  write that follows carries only the field that was changed. A change that is
  not confirmed is never written, and the control goes back to reading the record
  on its own.

  Both controls are held while a write is in flight, so one record cannot take
  two writes at once. Every write now reports through the record's own reporter:
  spoken on success, and both spoken and shown on failure, in place of the error
  line the save bar used to carry.

- 234bbcb: An organization in the admin app is now a record rather than a page. The
  sidebar drops the global nav while an operator is inside one and shows the
  record instead: a row back to all organizations, the organization's name with
  its account type and trial state under it, and Overview, Projects, Features and
  Members, with a count beside Projects and Members. Features is the one item that
  leaves the admin app: it opens that organization's feature list in the Gram
  dashboard, in a new tab, and the row is marked so an operator can tell before
  clicking. An organization with one project
  carries no count and its Projects item opens that project. The breadcrumb above
  reads Organizations, then the organization by name, then the view.

  Each of those views has its own address, so a link to one organization's
  members opens on its members, a refresh stays where it was, and the back button
  walks back through the views rather than out of the record. A project opened
  from the list stays inside the record it belongs to, and the record's name,
  trial and actions stay on screen above it.

  The record's name, account type and trial now sit in a header at the top of
  every view, beside Open in Gram and Disable. An organization on a live trial
  also gets a callout naming the day the trial ends, with Extend trial beside the
  date it acts on. An organization that never trialled shows no trial mark at all.

  The facts themselves are unchanged, apart from one thing worth knowing before
  you read a date: dates in the moved content are now the server's day in UTC with
  no clock time, the same reading the organizations list has always shown. The
  Members table's Last login and Overview's Updated used to carry a time of day in
  the reader's own zone and no longer do.

- 038c7eb: Opening a project from an organization record could hang forever. Every link
  into a project now addresses it by id, and `project.get` resolves a slug to one
  project before it reads that project's detail.

  Project slugs are unique only within an organization, so a slug the whole
  platform uses matches one project per organization. The detail read counts six
  child tables for every row it matches, and two of those counts have no index on
  `project_id` to use, so a common slug cost one full table scan per organization
  and the read never returned. It now resolves the slug to a single id first and
  counts once.

  `project.get` also takes the organization now. Inside an organization record the
  project is read scoped to it, so a slug names one project rather than an
  arbitrary one, and a project outside that organization is reported as not found
  whichever way it is addressed.

- 0f51b62: Let a platform admin put a demoted enterprise trial back on from the
  organizations list. The endpoint has been there since the demotion sweeper
  shipped, and nothing in the admin app called it, so a demotion was one-way from
  the admin app and undoing one meant a hand-written API call.

  Re-arm trial sits beside Disable and Extend trial, in the row menu and in the
  peek panel footer, and it is offered on a demoted trial and on nothing else. A
  trial that has converted or is still running is refused by the server, an
  expired one has not been demoted yet, and Extend trial covers the trials that
  are running: the two actions are never offered on the same record.

  The confirmation says what the action does rather than what its day count
  suggests. Re-arming restores the organization's account type, brings its model
  provider keys back, takes it out from behind the book-a-demo gate, and starts a
  fresh trial of the given length counted from now, not from the date the old one
  ended on. That is a different action from extending, which only adds days to an
  end date.

  The day count starts at fourteen days, the trial length the rest of the system
  assumes, and a count the server would refuse is refused in the browser instead
  of being sent. A request the server rejects leaves the dialog open with the day
  count intact, so the operator adjusts the attempt rather than retyping it. The
  row repaints from the answer, so an organization does not move out from under
  the operator who just acted on it.

## 0.2.0

### Minor Changes

- 29c9b8b: Peek at an organization beside the list. Every row in the admin organizations
  list carries a peek control that docks a 400px panel next to the table instead
  of leaving the page, so the search term, the filters and the scroll position
  all survive the look-up. The panel shows account type, trial end, member count,
  creation date and both ids, with a copy control beside each id that confirms
  with a check. The table narrows to Name, Slug and Type while the panel is open,
  and gives the operator's own column choices straight back when it closes.

  The control takes the mouse and the keyboard alike. It reports whether its own
  panel is open, and it closes the panel it opened. Arrow Down and Arrow Up walk
  the panel through the rows on screen and scroll each one into view, and Escape
  closes it. Every close puts the keyboard back on the control that opened the
  panel. A screen reader is told which organization the panel is showing, each
  time the panel opens, moves to another row, or closes.

### Patch Changes

- e0baacf: Let a platform admin set one account type on many organizations at once. Ticking
  rows in the organizations list turns the strip above the table into a bulk
  control: it counts what is ticked, offers the account types the server accepts,
  and reads the count and the target type back in a confirmation before anything is
  written. Nothing is written until that confirmation is accepted.

  The ids come from the ticked rows and from nowhere else. There is no field that
  takes an id, because the write matches an id case-sensitively while the list
  search matches case-insensitively: an id pasted in the wrong case would come back
  as missing while the row it names sat on screen.

  An id the server matched no organization to is named on screen after the write,
  by the organization the operator ticked, rather than being counted silently as
  done. The selection is dropped whenever the operator pages, sorts or filters, so
  an account type cannot be set on a record that scrolled out of view.

  Row selection is opt-in for the shared admin table. A page that does not ask for
  it renders exactly as before. The checkbox column is pinned to the left edge, the
  way the actions column is pinned to the right, because the list is wider than the
  window and the control that picks rows would otherwise scroll out of reach.

- e38546e: The admin organizations list now filters on sets rather than one value at a time: `account_types` takes several account types, `trial_states` takes any of running, ending soon, expired, demoted, converted or none, and `disabled_states` takes active, disabled or both. An empty set means no filter, except `disabled_states`, where an empty set still means active only. A value outside the known list matches nothing rather than failing the request, so an operator pasting a colleague's URL gets an empty table instead of an error. The total reported alongside the page counts the same filtered set, trial state included.

  This is an expand step. The single-valued `account_type` and the `include_disabled` boolean both still work: `account_type` joins `account_types` as one more member of the same set, and `disabled_states` overrides `include_disabled` when supplied. Keeping them lets the server ship before the dashboard changes. Retiring them is a separate contraction step.

- 83ca4ea: Gather the organizations list's two row controls into one Actions column, pinned
  to the right edge of the table. Peek and the row menu used to sit in two
  separate columns ahead of the name, which put two controls between the operator
  and the thing they came to read, and left the row's first column carrying
  nothing they could name. They are now one cell at the end of the row, peek
  first, sized to its contents so it does not read as an empty gutter.

  Pinned rather than merely last, because the list is wider than most windows. A
  trailing column that scrolled with the rest would start life off the right edge:
  in a 760 pixel wide window it sat 227 pixels past the edge of the viewport, so
  neither control could be seen or clicked until the operator scrolled the table
  sideways. Pinned, both are on screen from the start and hold their position
  while the columns scroll underneath them.

  The pinned cell takes its colour from the row it belongs to rather than a flat
  one of its own, so it stays invisible as a cell: it matches the highlight on the
  row being peeked at, and it matches the hover. To give it something opaque to
  inherit, rows in the organizations list now carry a background of their own, and
  their hover and open-menu highlights became fully opaque. A translucent
  highlight was painted twice on the pinned cell, once by the row and once by the
  cell stacked above it, which made it visibly darker than the rest of its row and
  let the scrolled columns show through it. Only this list changes; every other
  admin table keeps the highlights it had.

  Alt+click a row to peek at it, which the peek panel's first release had and then
  lost. Plain click still opens the organization, and the peek button in the
  Actions column still peeks, so the gesture is a shortcut rather than the only
  way in. It works on any part of the row including the organization's name, whose
  link would otherwise treat Alt+click as "save link as" and start a download;
  verified in Chromium that the download does not happen. Holding Alt together
  with Ctrl, Cmd or Shift does not peek, but it still cancels that download rather
  than leaving the operator with an HTML file they did not ask for.

  Ctrl, Cmd and Shift without Alt still belong to the link, so opening an
  organization in a new tab or window works as it did. Used anywhere else in the
  row they now do nothing. Until now they fell through and navigated the list away
  in the current tab, which took the list out from under the tab the operator was
  opening.

  The Actions column cannot be hidden from the Columns menu, which would otherwise
  put peek and every write out of reach of the whole list.

- b5c1aa8: Let a platform admin narrow the organizations list by account type, trial state
  and status, picking as many values in each as they need.

  The toolbar carries one control per group. Any of them opens the same sheet, on
  the group that was pressed, and each group is a multi-select: Type takes any of
  free, pro and enterprise, Trial takes any of the six states the rows show, and
  Status takes active, disabled or both. An empty group is a default rather than
  an empty result, and each control says its own: all types, all trial states,
  active only.

  The trial options read from the same map the trial badge on the row renders, so
  the filter and the rows it returns cannot say different words for one state.

  Nothing reaches the table until the operator applies. Picking three values
  otherwise costs three requests and shows two lists nobody asked for on the way
  to the one they did. Escape closes the sheet, discards the edit and puts the
  keyboard back on the control it opened from. Clear all resets every filter and
  leaves the search term alone: the term is not a filter this sheet holds.

  The chosen filters live in the URL, so an operator can paste the view they are
  looking at and get the same rows back. Applying returns to the first page and
  keeps the sort. Values are ordered as the pickers offer them and deduplicated,
  so two operators who chose the same filter in a different order send one request
  and share one cache entry. An account type from outside the offered list is kept
  rather than dropped, because dropping it would widen a link's view while the
  control still read "all types".

  The request now sends `account_types`, `trial_states` and `disabled_states`, each
  repeated once per chosen value, in place of the single `account_type` and the
  `include_disabled` flag.

- 4bc2406: Fix several ways the organizations list left a keyboard operator without an
  answer while the peek panel was open.

  The panel is now a tab stop with a visible focus ring. Arrow Up and Arrow Down
  on it walk the peek from record to record, and before this the only way to reach
  it was the focus it took when it opened: one Tab to the close button and record
  navigation was gone until the operator went back to the row's peek control.

  The panel's arrow keys now reach only the panel itself and the peek control of
  the row it is showing. Every other control under the list keeps its own keys
  back: Arrow Down on the pager, or on a button inside the panel body, scrolls the
  way it does anywhere else instead of walking the panel to a record the operator
  is not looking at and swallowing the scroll on the way.

  Escape is scoped wider, because it has no scrolling of its own to lose. It
  closes the panel from anywhere inside it, the buttons in its body included, and
  puts the keyboard back on the peek control of the row that was open. It still
  does nothing on the pager, and a tooltip or menu opened from inside the panel
  still takes the first Escape for itself.

  The Columns control also works again while the panel is open. The panel
  overrides six columns: it hides five, and it forces Name on. A menu click on
  any of those used to do nothing an operator could see, because the panel's
  override outranked it. Checking one the panel hides snapped the checkbox back
  with no column and nothing said, and unchecking Name was worse: that one looked
  like nothing had happened, then took the Name column away later, when the panel
  closed.

  Toggling any of the six now closes the panel and applies the change in the
  same moment, and says which way it went, because toggling a column is asking
  for that column. A column the panel does not override leaves the panel where it
  is, and the keyboard stays in the Columns menu either way.

- 657bbd6: Show a platform admin how much work the organizations list is holding, in a
  strip of three figures above the table.

  Organizations counts every organization on the platform and says how many were
  created in the last seven days. Trials ending in 7 days counts the trials in the
  `ending_soon` state. Disabled counts the switched-off organizations and says how
  many were switched off in the last seven days.

  Each figure is a control. Pressing one filters the table to the rows behind it,
  replacing whatever was filtered rather than adding to it, and clearing the
  search term the figure never counted.

  The figures describe the whole platform, so they do not change when you filter
  the table. Disabling an organization, re-enabling one or extending a trial moves
  a figure, so each of those refreshes the strip.

  Applying a filter set, from a figure or from the filter sheet, now returns to
  the first page even where the set applied is the one already on.

  The Status control now names the view it is showing, "Active and disabled",
  where it used to count it as "2 selected".

- afb8e53: Let a platform admin disable an organization, re-enable it, and extend its
  enterprise trial without leaving the organizations list.

  Every row carries a menu with those actions, and the peek panel repeats them as
  buttons in its footer, so the operator acts on the record they are already
  reading. The menu offers Disable or Re-enable, never both: which one it shows
  follows the row, so a record disabled a moment ago offers the way back
  immediately. Extend trial is offered only for a trial that is running or ending
  soon, and never for a disabled organization, whose trial runs on while every
  member is locked out.

  Disabling asks for a confirmation first, because it takes Gram away from every
  member of the organization until it is re-enabled. Re-enabling does not: it
  gives access back, and a second press costs a request and nothing else.

  Extending asks for a day count, starting at 14. The days are added to the date
  the trial ends on now rather than to today, so extending it early does not
  shorten it. A count outside 1 to 365 is refused in the dialog, with the reason,
  before a request the server would reject leaves the browser.

  The list repaints from what the write answered, with no second request behind
  it, so the row and the panel show the new state as soon as it lands. A read
  already in flight when the operator acts is dropped rather than left to finish,
  because it answers with the row as it was and would put that back over the
  write.

  A write that failed says why. A write made from a dialog reports inside that
  dialog. Re-enable has no dialog, so its failure raises a message above the list
  that stays until it is dismissed or the next write succeeds. Every outcome also
  goes through the polite region the list already announces the peek with, and the
  keyboard returns to the control that opened the dialog rather than to the top of
  the page.

- 6d39432: A pasted organization id or WorkOS id in the admin organizations search now finds a disabled organization, because investigating a suspended organization is a leading reason to paste an id. Both id matches also ignore casing, so an id that arrived lowercased from a log pipeline still lands on the right organization. The search term is trimmed of surrounding whitespace, and % and \_ inside it match literally instead of acting as wildcards. An id match still respects the account type, trial state and cursor filters.
- d53ce10: The admin organizations search now matches a pasted organization id or WorkOS id, so an engineer who pastes an id lands on that organization instead of an empty list. Both id matches are exact; name and slug keep matching as a case-insensitive substring.
- cff9f27: Scroll a long list in the admin app and the table moves, not the page. The
  search box, the filters and the pagination row hold their places while the rows
  travel under them, and the sidebar and the page header stay on screen
  throughout.

  Until now a list longer than the window grew the whole page instead. Reaching
  the fiftieth organization carried the search controls off the top of the screen
  and left the pagination row somewhere below the fold, so an operator who wanted
  the next page, or wanted to change a filter after reading the rows, had to
  scroll the document back up to find the controls again.

  Every page in the admin app gains the behaviour. Anything taller than the window
  now scrolls within the layout, and the shell around it stays put.

- af17932: The admin organizations endpoint can now sort and page. A caller names a column, a direction and a 1-based page number, and gets that page back together with the number of organizations the filters matched. Sortable columns are name, slug, account type, member count, created date, disabled date and trial end date; an unknown column or direction falls back to the default order rather than failing a shared link. Cursor paging is untouched and still runs the deployed dashboard, so both walks work until the client half lands.
- 5cb678d: Show an organization's real trial state in the admin dashboard. The
  organizations list, the peek panel and the organization detail page all read
  `trial_state` and `trial_ends_at` instead of `free_trial_ends_at`, which was
  defaulted for every organization and so reported a trial that never happened.

  The list column is now headed `Trial` rather than `Trial ends`, and reads as a
  state: a badge carrying `Running`, `Ending soon`, `Expired`, `Demoted` or
  `Converted`, with `ends` and the date beside it only while the trial is still
  live. An organization that never trialled reads as a dash.

  The three surfaces render one shared component, so the same organization cannot
  read one way in the list and another way on its own page. The old fields stay
  on the API for now; a follow-up takes them off the wire.

## 0.1.2

### Patch Changes

- 08414c0: Add the URL contract that opens the customer-facing dashboard already scoped to a chosen organization, replacing the set-a-cookie-and-log-in-again routine. The slug rides in the `redirect` parameter of the login URL, which the server reads back as the first path segment of the destination; both sides are now pinned by tests so the shape cannot drift, because getting it wrong lands the operator on their own organization with no error. The row action that uses this link follows.
- cfc9efd: The admin organizations and organization detail endpoints now report each organization's real trial state and end date, read from the trials table. The state separates a running trial from one that is ending soon, expired, demoted or converted, and from an organization that never trialled. The existing free trial fields default to fourteen days after signup for every organization, so they make every row look like it is trialling. Those fields still ship unchanged, and the admin dashboard starts reading the new ones in a follow-up.

## 0.1.1

### Patch Changes

- 11f6e49: Keep the organizations list's search and filters in the URL, so an operator can paste the view they are looking at to a colleague and get the same rows back. The table gains a persistent toolbar row with a Columns control, and rows return to the standard density.
