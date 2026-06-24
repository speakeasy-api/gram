// Configuration tab body for the Policy Detail shell (AGE-2704).
//
// Renders the create/edit form as a single scrollable, sectioned page: the kind
// chooser (create mode, prompt policies enabled), the risk/prompt form bodies
// (every section stacked, with a sticky section-nav rail), and a single sticky
// bottom-right Create / Save changes action.

import { Badge, Button } from "@speakeasy-api/moonshine";
import { Loader2 } from "lucide-react";
import { useDetectionRulesStore } from "../detection-rules-data";
import { PolicyKindChoice } from "./policy-kind-choice";
import { PolicySheetBody } from "./risk-policy-body";
import { PromptPolicySheetBody } from "./prompt-policy-body";
import type { PolicyKind } from "./payload";
import type { usePolicyForm } from "./use-policy-form";

export function PolicyConfigurationTab({
  form,
  mode,
  nlEnabled,
}: {
  form: ReturnType<typeof usePolicyForm>;
  mode: "create" | "edit";
  nlEnabled: boolean;
}): JSX.Element {
  const { state, setters, derived, kindParam, setKindParam, handlers } = form;
  const { customRules } = useDetectionRulesStore();
  const editingPolicy = derived.editingPolicy;

  // The kind chooser only appears in create mode with prompt policies enabled,
  // before a kind is committed to `?kind=`.
  const isChoosingPolicyKind =
    mode === "create" && nlEnabled && kindParam == null;

  if (isChoosingPolicyKind) {
    return (
      <PolicyKindChoice
        onSelect={(kind: PolicyKind) => void setKindParam(kind)}
      />
    );
  }

  const formPolicyKind = state.formPolicyKind;
  const mutationPending = derived.mutationPending;

  return (
    <div className="space-y-6">
      {formPolicyKind === "risk" ? (
        <PolicySheetBody
          key={editingPolicy?.id ?? "new-risk-policy"}
          formName={state.formName}
          setFormName={setters.setFormName}
          formEnabled={state.formEnabled}
          setFormEnabled={setters.setFormEnabled}
          selectedCategories={state.selectedCategories}
          setSelectedCategories={setters.setSelectedCategories}
          disabledRules={state.disabledRules}
          setDisabledRules={setters.setDisabledRules}
          customRules={customRules}
          selectedCustomRuleIds={state.selectedCustomRuleIds}
          setSelectedCustomRuleIds={setters.setSelectedCustomRuleIds}
          scopeInclude={state.scopeInclude}
          setScopeInclude={setters.setScopeInclude}
          scopeExempt={state.scopeExempt}
          setScopeExempt={setters.setScopeExempt}
          scopeMode={state.scopeMode}
          setScopeMode={setters.setScopeMode}
          selectedMessageTypes={state.selectedMessageTypes}
          setSelectedMessageTypes={setters.setSelectedMessageTypes}
          formAction={state.formAction}
          setFormAction={setters.setFormAction}
          formAutoName={state.formAutoName}
          setFormAutoName={setters.setFormAutoName}
          formUserMessage={state.formUserMessage}
          setFormUserMessage={setters.setFormUserMessage}
          formAudienceType={state.formAudienceType}
          setFormAudienceType={setters.setFormAudienceType}
          selectedAudiencePrincipalUrns={state.selectedAudiencePrincipalUrns}
          setSelectedAudiencePrincipalUrns={
            setters.setSelectedAudiencePrincipalUrns
          }
        />
      ) : (
        <PromptPolicySheetBody
          key={editingPolicy?.id ?? "new-prompt-policy"}
          isEditing={!!editingPolicy}
          formName={state.formName}
          setFormName={setters.setFormName}
          formPromptInstruction={state.formPromptInstruction}
          setFormPromptInstruction={setters.setFormPromptInstruction}
          formAction={state.formAction}
          setFormAction={setters.setFormAction}
          formAutoName={state.formAutoName}
          setFormAutoName={setters.setFormAutoName}
          formEnabled={state.formEnabled}
          setFormEnabled={setters.setFormEnabled}
          formModel={state.formModel}
          setFormModel={setters.setFormModel}
          formTemperature={state.formTemperature}
          setFormTemperature={setters.setFormTemperature}
          formFailOpen={state.formFailOpen}
          setFormFailOpen={setters.setFormFailOpen}
          selectedMessageTypes={state.selectedMessageTypes}
          setSelectedMessageTypes={setters.setSelectedMessageTypes}
        />
      )}

      <div className="border-border bg-background sticky bottom-0 flex items-center justify-end gap-3 border-t py-4">
        {editingPolicy && derived.isDirty && (
          <Badge variant="warning">
            <Badge.Text>Unsaved changes</Badge.Text>
          </Badge>
        )}
        <Button onClick={handlers.handleSave} disabled={derived.saveDisabled}>
          {mutationPending && (
            <Button.LeftIcon>
              <Loader2 className="h-4 w-4 animate-spin" />
            </Button.LeftIcon>
          )}
          <Button.Text>
            {mutationPending
              ? "Saving..."
              : editingPolicy
                ? "Save changes"
                : "Create"}
          </Button.Text>
        </Button>
      </div>
    </div>
  );
}
