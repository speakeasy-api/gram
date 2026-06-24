// Standard (detection-rule) policy wizard body + scope/action/audience
// subcomponents (AGE-2704). Moved verbatim from PolicyCenter.tsx.

import { Avatar, AvatarFallback, AvatarImage } from "@/components/ui/avatar";
import { Checkbox } from "@/components/ui/checkbox";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { RadioGroup, RadioGroupItem } from "@/components/ui/radio-group";
import { SearchBar } from "@/components/ui/search-bar";
import { Switch } from "@/components/ui/switch";
import { TextArea } from "@/components/ui/textarea";
import { cn } from "@/lib/utils";
import { Badge } from "@speakeasy-api/moonshine";
import { ChevronRight } from "lucide-react";
import { useMemo, useState, type ReactNode } from "react";
import { useMembers, useRoles } from "@gram/client/react-query/index.js";
import type { AccessMember } from "@gram/client/models/components/accessmember.js";
import type { Role } from "@gram/client/models/components/role.js";
import {
  DETECTION_RULES,
  POLICY_MESSAGE_TYPE_META,
  type PolicyAction,
  type PolicyMessageType,
  type RuleCategory,
} from "../policy-data";
import { ActionBadge } from "../policy-summary";
import { ALL_POLICY_MESSAGE_TYPES } from "../policy-display";
import { CelExpressionField } from "../cel-field";
import { useDetectionRulesStore } from "../detection-rules-data";
import { CustomizeRulesSheet, DetectorCard } from "./detector-card";
import { FormLayout, FormSection } from "./wizard-chrome";
import { POLICY_FORM_SECTIONS } from "./wizard-steps";
import {
  ALL_CATEGORIES,
  FLAG_ONLY_CATEGORIES,
  SCOPE_EXEMPT_CEL_EXAMPLES,
  SCOPE_INCLUDE_CEL_EXAMPLES,
  filterAudiencePrincipalsForChoice,
  policyAudienceChoiceForSelection,
  type PolicyAudienceChoice,
  type PolicyAudienceType,
} from "./payload";

const USER_SEARCH_RESULT_LIMIT = 10;

const ACTION_OPTIONS: {
  value: PolicyAction;
  title: string;
  description: string;
}[] = [
  {
    value: "flag",
    title: "Log for review",
    description: "Log findings for review without interrupting the session",
  },
  {
    value: "block",
    title: "Deny the request",
    description: "Deny prompts and tool calls that match detection rules",
  },
];

function memberDisplayName(member: AccessMember): string {
  return member.name || member.email;
}

function memberInitials(member: Pick<AccessMember, "email" | "name">): string {
  const source = member.name.trim() || member.email.trim();
  const initials = source
    .split(/\s+/)
    .map((part) => part[0])
    .join("")
    .toUpperCase()
    .slice(0, 2);

  return initials || "?";
}

function memberMatchesSearch(member: AccessMember, search: string): boolean {
  const normalizedSearch = search.trim().toLowerCase();
  if (!normalizedSearch) {
    return false;
  }

  return (
    member.name.toLowerCase().includes(normalizedSearch) ||
    member.email.toLowerCase().includes(normalizedSearch)
  );
}

function compareMembersByName(a: AccessMember, b: AccessMember): number {
  return memberDisplayName(a).localeCompare(memberDisplayName(b));
}

function compareRolesByName(a: Role, b: Role): number {
  return a.name.localeCompare(b.name);
}

export function PolicySheetBody({
  formName,
  setFormName,
  formEnabled,
  setFormEnabled,
  selectedCategories,
  setSelectedCategories,
  disabledRules,
  setDisabledRules,
  customRules,
  selectedCustomRuleIds,
  setSelectedCustomRuleIds,
  scopeInclude,
  setScopeInclude,
  scopeExempt,
  setScopeExempt,
  scopeMode,
  setScopeMode,
  selectedMessageTypes,
  setSelectedMessageTypes,
  formAction,
  setFormAction,
  formAutoName,
  setFormAutoName,
  formUserMessage,
  setFormUserMessage,
  formAudienceType,
  setFormAudienceType,
  selectedAudiencePrincipalUrns,
  setSelectedAudiencePrincipalUrns,
}: {
  formName: string;
  setFormName: (v: string) => void;
  formEnabled: boolean;
  setFormEnabled: (v: boolean) => void;
  selectedCategories: Set<RuleCategory>;
  setSelectedCategories: (v: Set<RuleCategory>) => void;
  disabledRules: Set<string>;
  setDisabledRules: (v: Set<string>) => void;
  customRules: ReturnType<typeof useDetectionRulesStore>["customRules"];
  selectedCustomRuleIds: Set<string>;
  setSelectedCustomRuleIds: (v: Set<string>) => void;
  scopeInclude: string;
  setScopeInclude: (v: string) => void;
  scopeExempt: string;
  setScopeExempt: (v: string) => void;
  scopeMode: "messageTypes" | "cel";
  setScopeMode: (v: "messageTypes" | "cel") => void;
  selectedMessageTypes: Set<PolicyMessageType>;
  setSelectedMessageTypes: (v: Set<PolicyMessageType>) => void;
  formAction: PolicyAction;
  setFormAction: (v: PolicyAction) => void;
  formAutoName: boolean;
  setFormAutoName: (v: boolean) => void;
  formUserMessage: string;
  setFormUserMessage: (v: string) => void;
  formAudienceType: PolicyAudienceType;
  setFormAudienceType: (v: PolicyAudienceType) => void;
  selectedAudiencePrincipalUrns: Set<string>;
  setSelectedAudiencePrincipalUrns: (v: Set<string>) => void;
}): JSX.Element {
  // The org's custom rules collapse into their own section; the Customize sheet
  // opens for one detector category at a time.
  const [detectionExpanded, setDetectionExpanded] = useState(true);
  const [customizeCategory, setCustomizeCategory] =
    useState<RuleCategory | null>(null);
  const selectedBuiltinCount = ALL_CATEGORIES.filter((c) =>
    selectedCategories.has(c),
  ).length;

  // Toggle a whole built-in detector category on/off (clears any per-rule
  // disables for it). Flag-only categories force the policy action to flag.
  const toggleCategory = (cat: RuleCategory, checked: boolean) => {
    const rules = DETECTION_RULES[cat].filter((r) => !r.hidden);
    const nextCats = new Set(selectedCategories);
    const nextDisabled = new Set(disabledRules);
    if (checked) {
      nextCats.add(cat);
    } else {
      nextCats.delete(cat);
    }
    for (const rule of rules) nextDisabled.delete(rule.id);
    setSelectedCategories(nextCats);
    setDisabledRules(nextDisabled);
    if (checked && FLAG_ONLY_CATEGORIES.has(cat) && formAction === "block") {
      setFormAction("flag");
    }
  };
  const flagOnlySelected = [...FLAG_ONLY_CATEGORIES].some((c) =>
    selectedCategories.has(c),
  );

  // Custom rules attach as detectors only; a match records a finding. Message
  // exemptions are expressed via the policy's scope_exempt, not by rule id.
  const toggleDetector = (ruleId: string, checked: boolean) => {
    const next = new Set(selectedCustomRuleIds);
    if (checked) {
      next.add(ruleId);
    } else {
      next.delete(ruleId);
    }
    setSelectedCustomRuleIds(next);
  };

  return (
    <>
      <FormLayout sections={POLICY_FORM_SECTIONS}>
        <FormSection
          id="detection"
          title="What should this policy detect?"
          description="Turn on detector categories and attach your organization's custom rules."
        >
          <div className="space-y-6">
            {/* Built-in rules */}
            <div className="space-y-3">
              <div className="flex items-center justify-between">
                <Label className="text-sm font-medium">Built-in rules</Label>
                <span className="text-muted-foreground text-xs">
                  {selectedBuiltinCount} on
                </span>
              </div>
              <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-3">
                {ALL_CATEGORIES.map((cat) => (
                  <DetectorCard
                    key={cat}
                    category={cat}
                    selected={selectedCategories.has(cat)}
                    disabledRules={disabledRules}
                    onToggle={(checked) => toggleCategory(cat, checked)}
                    onCustomize={() => setCustomizeCategory(cat)}
                  />
                ))}
              </div>
            </div>

            {customRules.length > 0 && (
              <RuleSelectList
                title="Custom Rules"
                description={
                  <>
                    Attach your organization's custom rules as{" "}
                    <span className="text-foreground font-medium">
                      detectors
                    </span>{" "}
                    — a match records a finding.
                  </>
                }
                idPrefix="detector"
                customRules={customRules}
                selectedRuleIds={selectedCustomRuleIds}
                onToggleRule={toggleDetector}
                expanded={detectionExpanded}
                onToggle={() => setDetectionExpanded((v) => !v)}
              />
            )}
          </div>
        </FormSection>

        <FormSection
          id="scope"
          title="Where should it evaluate?"
          description="Apply everywhere, or narrow the scope to reduce noise and cost."
        >
          <div className="space-y-6">
            {/* Scope is a mutex: message-type cards (coarse) XOR a CEL include
                predicate (fine). The segmented control conveys that. */}
            <div className="space-y-3">
              <div className="border-border inline-flex rounded-md border p-0.5">
                {(
                  [
                    { key: "messageTypes", label: "Message types" },
                    { key: "cel", label: "CEL expression" },
                  ] as const
                ).map((opt) => (
                  <button
                    key={opt.key}
                    type="button"
                    onClick={() => setScopeMode(opt.key)}
                    className={cn(
                      "rounded px-3 py-1 text-xs font-medium transition-colors",
                      scopeMode === opt.key
                        ? "bg-foreground text-background"
                        : "text-muted-foreground hover:text-foreground",
                    )}
                  >
                    {opt.label}
                  </button>
                ))}
              </div>
              <p className="text-muted-foreground text-xs">
                {scopeMode === "messageTypes"
                  ? "Apply to whole session parts. Switch to a CEL expression to match on tool or content attributes instead."
                  : "Apply only to messages matching the expression below — this replaces the message-type selection."}
              </p>
            </div>

            {scopeMode === "messageTypes" ? (
              <>
                <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
                  {ALL_POLICY_MESSAGE_TYPES.map((type) => (
                    <ScopeCard
                      key={type}
                      type={type as PolicyMessageType}
                      checked={selectedMessageTypes.has(
                        type as PolicyMessageType,
                      )}
                      onToggle={(checked) => {
                        const updated = new Set(selectedMessageTypes);
                        if (checked) {
                          updated.add(type as PolicyMessageType);
                        } else {
                          updated.delete(type as PolicyMessageType);
                        }
                        setSelectedMessageTypes(updated);
                      }}
                    />
                  ))}
                </div>
                {selectedMessageTypes.size === 0 && (
                  <p className="text-destructive text-xs">
                    Select at least one session part.
                  </p>
                )}
              </>
            ) : (
              <div className="space-y-2">
                <Label className="text-sm font-medium">
                  Evaluate messages matching
                </Label>
                <p className="text-muted-foreground text-xs">
                  The policy evaluates a message only when this expression is
                  true.
                </p>
                <CelExpressionField
                  value={scopeInclude}
                  onChange={setScopeInclude}
                  examples={SCOPE_INCLUDE_CEL_EXAMPLES}
                />
              </div>
            )}

            {/* Exemptions — always available and additive (not part of the
                scope mutex). A match here skips the whole policy. */}
            <div className="border-border space-y-4 border-t pt-6">
              <div>
                <Label className="text-sm font-medium">Exemptions</Label>
                <p className="text-muted-foreground text-xs">
                  Skip the whole policy for any message matching this expression
                  — an allowlist, regardless of the scope above.
                </p>
              </div>
              <CelExpressionField
                value={scopeExempt}
                onChange={setScopeExempt}
                examples={SCOPE_EXEMPT_CEL_EXAMPLES}
              />
            </div>
          </div>
        </FormSection>

        <FormSection
          id="action"
          title="What happens on a match?"
          description="Choose how the policy responds when its detection rules fire."
        >
          <div className="space-y-6">
            <ActionPicker
              formAction={formAction}
              setFormAction={setFormAction}
              flagOnlySelected={flagOnlySelected}
            />

            {/* Who the policy applies to (audience). */}
            <PolicyAudiencePicker
              formAudienceType={formAudienceType}
              setFormAudienceType={setFormAudienceType}
              selectedAudiencePrincipalUrns={selectedAudiencePrincipalUrns}
              setSelectedAudiencePrincipalUrns={
                setSelectedAudiencePrincipalUrns
              }
            />

            {/* Custom message — only relevant for block-action policies that
          surface a user-facing reason at deny time. Flag-action policies
          record findings silently, so no message is needed. */}
            {formAction === "block" && (
              <div className="space-y-2">
                <Label className="text-sm font-medium">Custom Message</Label>
                <p className="text-muted-foreground text-xs">
                  Shown to the user when this policy blocks a tool call or
                  prompt. Leave blank to use the default message.
                </p>
                <TextArea
                  value={formUserMessage}
                  onChange={setFormUserMessage}
                  placeholder="e.g. This action was blocked by your organization's security policy. Contact your admin for help."
                  rows={3}
                />
              </div>
            )}
          </div>
        </FormSection>

        <FormSection
          id="details"
          title="Name & enable"
          description="Name the policy and choose whether it scans messages."
        >
          <div className="space-y-6">
            {/* Policy Name */}
            <div className="space-y-2">
              <div className="flex items-center justify-between">
                <Label className="text-sm font-medium">Policy Name</Label>
                <div className="flex items-center gap-2">
                  <span className="text-muted-foreground text-xs">Auto</span>
                  <Switch
                    checked={formAutoName}
                    onCheckedChange={setFormAutoName}
                  />
                </div>
              </div>
              {formAutoName ? (
                <p className="text-muted-foreground text-xs">
                  Name will be generated automatically based on detection rules
                  and action.
                </p>
              ) : (
                <>
                  <Input
                    value={formName}
                    onChange={(value) => setFormName(value)}
                    placeholder="e.g. Secret Detection"
                  />
                  {!formName.trim() && (
                    <p className="text-destructive text-xs">
                      Name is required.
                    </p>
                  )}
                </>
              )}
            </div>

            {/* Enabled toggle */}
            <div className="flex items-center justify-between">
              <div>
                <Label className="text-sm font-medium">Enabled</Label>
                <p className="text-muted-foreground text-xs">
                  Enable this policy to begin scanning messages.
                </p>
              </div>
              <Switch checked={formEnabled} onCheckedChange={setFormEnabled} />
            </div>
          </div>
        </FormSection>
      </FormLayout>
      {customizeCategory && (
        <CustomizeRulesSheet
          category={customizeCategory}
          selectedCategories={selectedCategories}
          setSelectedCategories={setSelectedCategories}
          disabledRules={disabledRules}
          setDisabledRules={setDisabledRules}
          onClose={() => setCustomizeCategory(null)}
        />
      )}
    </>
  );
}

export function PolicyAudiencePicker({
  formAudienceType,
  setFormAudienceType,
  selectedAudiencePrincipalUrns,
  setSelectedAudiencePrincipalUrns,
}: {
  formAudienceType: PolicyAudienceType;
  setFormAudienceType: (v: PolicyAudienceType) => void;
  selectedAudiencePrincipalUrns: Set<string>;
  setSelectedAudiencePrincipalUrns: (v: Set<string>) => void;
}): JSX.Element {
  const { data: rolesData } = useRoles();
  const { data: membersData } = useMembers();
  const roles = useMemo(
    () => [...(rolesData?.roles ?? [])].sort(compareRolesByName),
    [rolesData?.roles],
  );
  const members = useMemo(
    () => [...(membersData?.members ?? [])].sort(compareMembersByName),
    [membersData?.members],
  );
  const [audienceChoice, setAudienceChoice] = useState<PolicyAudienceChoice>(
    () =>
      policyAudienceChoiceForSelection(
        formAudienceType,
        selectedAudiencePrincipalUrns,
      ),
  );
  const [userSearch, setUserSearch] = useState("");

  const togglePrincipal = (principalUrn: string, checked: boolean) => {
    const next = new Set(selectedAudiencePrincipalUrns);
    if (checked) {
      next.add(principalUrn);
    } else {
      next.delete(principalUrn);
    }
    setSelectedAudiencePrincipalUrns(next);
  };

  const selectAudienceChoice = (choice: PolicyAudienceChoice) => {
    setAudienceChoice(choice);
    setFormAudienceType(choice === "everyone" ? "everyone" : "targeted");
    setSelectedAudiencePrincipalUrns(
      filterAudiencePrincipalsForChoice(selectedAudiencePrincipalUrns, choice),
    );
    if (choice !== "users") {
      setUserSearch("");
    }
  };

  return (
    <div className="space-y-3">
      <div className="space-y-1">
        <Label className="text-sm font-medium">Audience</Label>
        <p className="text-muted-foreground text-xs">
          Choose which users this policy evaluates.
        </p>
      </div>
      <RadioGroup
        value={audienceChoice}
        onValueChange={(value) =>
          selectAudienceChoice(value as PolicyAudienceChoice)
        }
      >
        <div className="border-border divide-border divide-y rounded-lg border">
          <PolicyAudienceChoiceRow
            id="policy-audience-everyone"
            value="everyone"
            title="Everyone"
            description="Evaluate this policy for every user in the organization."
          />
          <PolicyAudienceChoiceRow
            id="policy-audience-users"
            value="users"
            title="Specific users"
            description="Search and select individual organization members."
          />
          <PolicyAudienceChoiceRow
            id="policy-audience-roles"
            value="roles"
            title="Specific roles"
            description="Evaluate this policy for every member of selected roles."
          />
        </div>
      </RadioGroup>

      {audienceChoice === "users" && (
        <SpecificUsersAudienceSection
          members={members}
          userSearch={userSearch}
          setUserSearch={setUserSearch}
          selectedAudiencePrincipalUrns={selectedAudiencePrincipalUrns}
          onTogglePrincipal={togglePrincipal}
        />
      )}

      {audienceChoice === "roles" && (
        <div className="border-border rounded-lg border">
          <AudiencePrincipalSection title="Roles">
            {roles.length === 0 ? (
              <p className="text-muted-foreground px-4 py-3 text-sm">
                No roles available.
              </p>
            ) : (
              roles.map((role) => {
                const principalUrn = role.principalUrn;
                return (
                  <AudiencePrincipalRow
                    key={principalUrn}
                    id={`audience-${principalUrn}`}
                    checked={selectedAudiencePrincipalUrns.has(principalUrn)}
                    title={role.name}
                    subtitle={`${role.memberCount} members`}
                    onCheckedChange={(checked) =>
                      togglePrincipal(principalUrn, checked)
                    }
                  />
                );
              })
            )}
          </AudiencePrincipalSection>
          {selectedAudiencePrincipalUrns.size === 0 && (
            <p className="text-muted-foreground border-border border-t px-4 py-3 text-xs">
              Select at least one role to save a targeted policy.
            </p>
          )}
        </div>
      )}
    </div>
  );
}

function SpecificUsersAudienceSection({
  members,
  userSearch,
  setUserSearch,
  selectedAudiencePrincipalUrns,
  onTogglePrincipal,
}: {
  members: AccessMember[];
  userSearch: string;
  setUserSearch: (value: string) => void;
  selectedAudiencePrincipalUrns: Set<string>;
  onTogglePrincipal: (principalUrn: string, checked: boolean) => void;
}) {
  const memberByPrincipalUrn = useMemo(
    () => new Map(members.map((member) => [member.principalUrn, member])),
    [members],
  );
  const selectedUserPrincipalUrns = useMemo(
    () =>
      [...selectedAudiencePrincipalUrns]
        .filter((principalUrn) => principalUrn.startsWith("user:"))
        .sort((a, b) => {
          const aMember = memberByPrincipalUrn.get(a);
          const bMember = memberByPrincipalUrn.get(b);
          const aLabel = aMember ? memberDisplayName(aMember) : a;
          const bLabel = bMember ? memberDisplayName(bMember) : b;
          return aLabel.localeCompare(bLabel);
        }),
    [memberByPrincipalUrn, selectedAudiencePrincipalUrns],
  );
  const selectedUserOptions = selectedUserPrincipalUrns.map((principalUrn) => {
    const member = memberByPrincipalUrn.get(principalUrn);
    return {
      member,
      principalUrn,
      title: member ? memberDisplayName(member) : principalUrn,
      subtitle: member?.email ?? "Unknown user",
    };
  });
  const matchingMembers = useMemo(
    () => members.filter((member) => memberMatchesSearch(member, userSearch)),
    [members, userSearch],
  );
  const unselectedMatchingMembers = matchingMembers.filter(
    (member) => !selectedAudiencePrincipalUrns.has(member.principalUrn),
  );
  const visibleSearchResults = unselectedMatchingMembers.slice(
    0,
    USER_SEARCH_RESULT_LIMIT,
  );
  const hiddenResultCount = Math.max(
    unselectedMatchingMembers.length - visibleSearchResults.length,
    0,
  );
  const hasSearch = userSearch.trim().length > 0;

  return (
    <div className="border-border rounded-lg border">
      <div className="space-y-4 p-4">
        <SearchBar
          value={userSearch}
          onChange={setUserSearch}
          placeholder="Search users by name or email"
          className="w-full"
        />

        {selectedUserOptions.length > 0 && (
          <div className="space-y-2">
            <div className="text-muted-foreground text-xs font-medium">
              Selected users
            </div>
            <div className="border-border divide-border divide-y overflow-hidden rounded-md border">
              {selectedUserOptions.map((option) => (
                <AudiencePrincipalRow
                  key={option.principalUrn}
                  id={`audience-selected-${option.principalUrn}`}
                  checked
                  title={option.title}
                  subtitle={option.subtitle}
                  leading={
                    <AudienceMemberAvatar
                      name={option.member?.name ?? option.title}
                      email={option.member?.email ?? option.subtitle}
                      photoUrl={option.member?.photoUrl}
                    />
                  }
                  onCheckedChange={(checked) =>
                    onTogglePrincipal(option.principalUrn, checked)
                  }
                />
              ))}
            </div>
          </div>
        )}

        <UserSearchResults
          hasSearch={hasSearch}
          hiddenResultCount={hiddenResultCount}
          results={visibleSearchResults}
          selectedAudiencePrincipalUrns={selectedAudiencePrincipalUrns}
          onTogglePrincipal={onTogglePrincipal}
        />
      </div>

      {selectedUserPrincipalUrns.length === 0 && (
        <p className="text-muted-foreground border-border border-t px-4 py-3 text-xs">
          Select at least one user to save a targeted policy.
        </p>
      )}
    </div>
  );
}

function UserSearchResults({
  hasSearch,
  hiddenResultCount,
  results,
  selectedAudiencePrincipalUrns,
  onTogglePrincipal,
}: {
  hasSearch: boolean;
  hiddenResultCount: number;
  results: AccessMember[];
  selectedAudiencePrincipalUrns: Set<string>;
  onTogglePrincipal: (principalUrn: string, checked: boolean) => void;
}) {
  if (!hasSearch) {
    return (
      <p className="text-muted-foreground text-sm">
        Search users by name or email to add them.
      </p>
    );
  }

  if (results.length === 0) {
    return (
      <p className="text-muted-foreground text-sm">No matching users to add.</p>
    );
  }

  return (
    <div className="space-y-2">
      <div className="text-muted-foreground text-xs font-medium">
        Search results
      </div>
      <div className="border-border divide-border divide-y overflow-hidden rounded-md border">
        {results.map((member) => {
          const principalUrn = member.principalUrn;
          return (
            <AudiencePrincipalRow
              key={principalUrn}
              id={`audience-result-${principalUrn}`}
              checked={selectedAudiencePrincipalUrns.has(principalUrn)}
              title={memberDisplayName(member)}
              subtitle={member.email}
              leading={
                <AudienceMemberAvatar
                  name={member.name}
                  email={member.email}
                  photoUrl={member.photoUrl}
                />
              }
              onCheckedChange={(checked) =>
                onTogglePrincipal(principalUrn, checked)
              }
            />
          );
        })}
      </div>
      {hiddenResultCount > 0 && (
        <p className="text-muted-foreground text-xs">
          Showing first {USER_SEARCH_RESULT_LIMIT} matches. Refine the search to
          narrow results.
        </p>
      )}
    </div>
  );
}

function AudienceMemberAvatar({
  name,
  email,
  photoUrl,
}: {
  name: string;
  email: string;
  photoUrl?: string;
}) {
  return (
    <Avatar className="h-7 w-7">
      {photoUrl && <AvatarImage src={photoUrl} alt={name || email} />}
      <AvatarFallback className="text-xs">
        {memberInitials({ name, email })}
      </AvatarFallback>
    </Avatar>
  );
}

function PolicyAudienceChoiceRow({
  id,
  value,
  title,
  description,
}: {
  id: string;
  value: PolicyAudienceChoice;
  title: string;
  description: string;
}) {
  return (
    <label
      htmlFor={id}
      className="hover:bg-muted/40 flex cursor-pointer gap-3 px-4 py-3"
    >
      <RadioGroupItem id={id} value={value} className="mt-0.5" />
      <span className="min-w-0">
        <span className="block text-sm font-medium">{title}</span>
        <span className="text-muted-foreground block text-xs">
          {description}
        </span>
      </span>
    </label>
  );
}

function AudiencePrincipalSection({
  title,
  children,
}: {
  title: string;
  children: ReactNode;
}) {
  return (
    <div className="border-border border-b last:border-b-0">
      <div className="bg-muted/30 border-border border-b px-4 py-2 text-xs font-medium">
        {title}
      </div>
      <div className="divide-border divide-y">{children}</div>
    </div>
  );
}

function AudiencePrincipalRow({
  id,
  checked,
  title,
  subtitle,
  leading,
  onCheckedChange,
}: {
  id: string;
  checked: boolean;
  title: string;
  subtitle: string;
  leading?: ReactNode;
  onCheckedChange: (checked: boolean) => void;
}) {
  return (
    <label
      htmlFor={id}
      className="hover:bg-muted/40 flex cursor-pointer items-start gap-3 px-4 py-3"
    >
      <Checkbox
        id={id}
        checked={checked}
        onCheckedChange={(value) => onCheckedChange(!!value)}
        className="mt-0.5"
      />
      {leading}
      <span className="min-w-0 flex-1">
        <span className="block truncate text-sm font-medium">{title}</span>
        <span className="text-muted-foreground block truncate text-xs">
          {subtitle}
        </span>
      </span>
    </label>
  );
}

/** One session-part as a selectable card (Scope step). */
export function ScopeCard({
  type,
  checked,
  onToggle,
}: {
  type: PolicyMessageType;
  checked: boolean;
  onToggle: (checked: boolean) => void;
}): JSX.Element {
  const meta = POLICY_MESSAGE_TYPE_META[type];
  return (
    <label
      className={cn(
        "flex cursor-pointer items-start gap-3 rounded-lg border p-3 transition-colors",
        checked
          ? "border-foreground bg-muted/40"
          : "border-border hover:bg-muted/30",
      )}
    >
      <Checkbox
        checked={checked}
        onCheckedChange={(next) => onToggle(!!next)}
        className="mt-0.5"
      />
      <div className="min-w-0">
        <div className="text-sm font-medium">{meta.label}</div>
        <div className="text-muted-foreground text-xs">{meta.description}</div>
      </div>
    </label>
  );
}

export function ActionPicker({
  formAction,
  setFormAction,
  flagOnlySelected = false,
}: {
  formAction: PolicyAction;
  setFormAction: (v: PolicyAction) => void;
  flagOnlySelected?: boolean;
}): JSX.Element {
  const actionValue =
    flagOnlySelected && formAction === "block" ? "flag" : formAction;

  return (
    <RadioGroup
      value={actionValue}
      onValueChange={(v) => {
        if (flagOnlySelected && v === "block") {
          return;
        }
        setFormAction(v as PolicyAction);
      }}
      className="space-y-2.5"
    >
      {ACTION_OPTIONS.map((opt) => {
        const disabled = flagOnlySelected && opt.value === "block";
        const selected = actionValue === opt.value;

        return (
          <label
            key={opt.value}
            htmlFor={`action-${opt.value}`}
            className={cn(
              "flex items-start gap-3 rounded-lg border p-3.5 transition-colors",
              disabled
                ? "border-border cursor-not-allowed opacity-60"
                : selected
                  ? "border-foreground bg-muted/40 cursor-pointer"
                  : "border-border hover:bg-muted/30 cursor-pointer",
            )}
          >
            <RadioGroupItem
              value={opt.value}
              id={`action-${opt.value}`}
              className="mt-0.5"
              disabled={disabled}
            />
            <div className="flex-1">
              <div className="flex items-center gap-2">
                <ActionBadge action={opt.value} />
                <span className="text-sm font-medium">{opt.title}</span>
              </div>
              <div className="text-muted-foreground mt-1.5 text-xs">
                {opt.description}
              </div>
              {disabled && (
                <div className="text-destructive mt-1 text-xs font-medium">
                  Destructive Tools and Destructive CLI Commands support
                  flagging only.
                </div>
              )}
            </div>
          </label>
        );
      })}
    </RadioGroup>
  );
}

/** A collapsible checkbox list of the org's custom rules. Used in the
 *  standard-policy wizard's Detect step to attach rules as detectors. */
export function RuleSelectList({
  title,
  description,
  idPrefix,
  customRules,
  selectedRuleIds,
  onToggleRule,
  expanded,
  onToggle,
}: {
  title: string;
  description: ReactNode;
  idPrefix: string;
  customRules: ReturnType<typeof useDetectionRulesStore>["customRules"];
  selectedRuleIds: Set<string>;
  onToggleRule: (ruleId: string, checked: boolean) => void;
  expanded: boolean;
  onToggle: () => void;
}): JSX.Element {
  const selectedCount = customRules.filter((r) =>
    selectedRuleIds.has(r.id),
  ).length;
  return (
    <div className="space-y-3">
      <button
        type="button"
        onClick={onToggle}
        className="flex w-full items-center gap-2"
      >
        <ChevronRight
          className={cn(
            "text-muted-foreground h-4 w-4 shrink-0 transition-transform",
            expanded && "rotate-90",
          )}
        />
        <Label className="cursor-pointer text-sm font-medium">{title}</Label>
        {selectedCount > 0 && (
          <Badge variant="neutral">
            <Badge.Text>{selectedCount} selected</Badge.Text>
          </Badge>
        )}
      </button>
      {expanded && (
        <div className="border-border divide-border divide-y rounded-lg border">
          <p className="text-muted-foreground px-4 py-3 text-xs">
            {description}
          </p>
          <div className="space-y-2 px-4 py-3">
            {customRules.map((rule) => (
              <div key={rule.id} className="flex items-center gap-3 py-1">
                <Checkbox
                  id={`${idPrefix}-${rule.id}`}
                  checked={selectedRuleIds.has(rule.id)}
                  onCheckedChange={(next) => onToggleRule(rule.id, !!next)}
                />
                <label
                  htmlFor={`${idPrefix}-${rule.id}`}
                  className="min-w-0 flex-1 cursor-pointer truncate text-xs"
                >
                  <span className="text-foreground">
                    {rule.title || rule.id}
                  </span>
                  <span className="text-muted-foreground ml-2 font-mono text-[10px]">
                    {rule.id}
                  </span>
                </label>
              </div>
            ))}
          </div>
        </div>
      )}
    </div>
  );
}
