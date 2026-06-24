// usePolicyForm — the single source of truth for the policy create/edit form
// (AGE-2704). Holds all form state, derived validation flags, the save payload
// builders, and the draft-eval `evalSource`. Lifted out of PolicyCenter.tsx so
// the Configuration tab and the Evals tab (which evals the on-screen config)
// share one form instance.

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useQueryState } from "nuqs";
import { useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "react-router";
import {
  invalidateAllRiskListPolicies,
  useRiskCreatePolicyMutation,
  useRiskPoliciesUpdateMutation,
} from "@gram/client/react-query/index.js";
import { invalidateAllRiskPoliciesStatus } from "@gram/client/react-query/riskPoliciesStatus.js";
import type { RiskPolicy } from "@gram/client/models/components/riskpolicy.js";
import type { PolicyEvalCandidateConfig } from "@gram/client/models/components/policyevalcandidateconfig.js";
import {
  type PolicyAction,
  type PolicyMessageType,
  type RuleCategory,
  DETECTION_RULES,
} from "../policy-data";
import {
  ALL_POLICY_MESSAGE_TYPES,
  policyMessageTypesForForm,
  policyToCategories,
} from "../policy-display";
import { useCelStatus } from "../use-cel-status";
import { useRoutes } from "@/routes";
import {
  DEFAULT_JUDGE_TEMPERATURE,
  buildCandidatePayload,
  candidateFromPolicy,
  candidatesEqual,
  categoriesToPayload,
  isPromptPolicy,
  pinnedHiddenRuleIds,
  policyMessageTypesForPayload,
  promptPolicyName,
  type PolicyAudienceType,
  type PolicyKind,
  type SerializableConfigInput,
} from "./payload";

export type PolicyFormMode = "create" | "edit";

/** The eval source the Evals tab should run: a saved policy_id when the form is
 * clean, otherwise an inline candidate (create mode, or a saved policy with
 * unsaved edits). */
export type EvalSource =
  | { policyId: string }
  | { candidate: PolicyEvalCandidateConfig };

// Exported wrapper carries an explicit return type (the codebase idiom for
// hooks: an inner impl with an inferred shape + a thin annotated wrapper).
export function usePolicyForm(args: {
  mode: PolicyFormMode;
  initialPolicy: RiskPolicy | null;
  nlEnabled: boolean;
}): ReturnType<typeof usePolicyFormImpl> {
  return usePolicyFormImpl(args);
}

function usePolicyFormImpl({
  mode,
  initialPolicy,
  nlEnabled,
}: {
  mode: PolicyFormMode;
  initialPolicy: RiskPolicy | null;
  nlEnabled: boolean;
}) {
  const queryClient = useQueryClient();
  const routes = useRoutes();
  const navigate = useNavigate();

  // Create-mode policy kind lives in `?kind=`. In edit mode the kind is derived
  // from the loaded policy. Flag off => always risk.
  const [kindParam, setKindParam] = useQueryState("kind");

  const editingPolicy = mode === "edit" ? initialPolicy : null;

  // --- form state (ported from PolicyCenter useStates) ----------------------
  const [formPolicyKind, setFormPolicyKind] = useState<PolicyKind>("risk");
  const [formName, setFormName] = useState("");
  const [formEnabled, setFormEnabled] = useState(true);
  const [formPromptInstruction, setFormPromptInstruction] = useState("");
  const [selectedCategories, setSelectedCategories] = useState<
    Set<RuleCategory>
  >(new Set<RuleCategory>());
  const [disabledRules, setDisabledRules] = useState<Set<string>>(new Set());
  const [selectedCustomRuleIds, setSelectedCustomRuleIds] = useState<
    Set<string>
  >(new Set<string>());
  const [scopeInclude, setScopeInclude] = useState("");
  const [scopeExempt, setScopeExempt] = useState("");
  const [scopeMode, setScopeMode] = useState<"messageTypes" | "cel">(
    "messageTypes",
  );
  const [selectedMessageTypes, setSelectedMessageTypes] = useState<
    Set<PolicyMessageType>
  >(new Set(ALL_POLICY_MESSAGE_TYPES));
  const [formAction, setFormAction] = useState<PolicyAction>("flag");
  const [formAutoName, setFormAutoName] = useState(true);
  const [formUserMessage, setFormUserMessage] = useState("");
  const [formModel, setFormModel] = useState("");
  const [formTemperature, setFormTemperature] = useState(
    DEFAULT_JUDGE_TEMPERATURE,
  );
  const [formFailOpen, setFormFailOpen] = useState(true);
  const [formAudienceType, setFormAudienceType] =
    useState<PolicyAudienceType>("everyone");
  const [selectedAudiencePrincipalUrns, setSelectedAudiencePrincipalUrns] =
    useState<Set<string>>(new Set<string>());

  // --- reset (= handleCreate body) ------------------------------------------
  // Initialize the form for create mode at the given kind.
  const reset = useCallback((kind: PolicyKind) => {
    setFormPolicyKind(kind);
    setFormName("");
    setFormEnabled(true);
    setFormPromptInstruction("");
    setSelectedCategories(new Set<RuleCategory>());
    setDisabledRules(new Set());
    setSelectedCustomRuleIds(new Set<string>());
    setScopeInclude("");
    setScopeExempt("");
    setScopeMode("messageTypes");
    setSelectedMessageTypes(new Set(ALL_POLICY_MESSAGE_TYPES));
    setFormAction("flag");
    setFormAutoName(true);
    setFormUserMessage("");
    setFormModel("");
    setFormTemperature(DEFAULT_JUDGE_TEMPERATURE);
    setFormFailOpen(true);
    setFormAudienceType("everyone");
    setSelectedAudiencePrincipalUrns(new Set<string>());
  }, []);

  // --- hydrateFrom (= handleEdit body) --------------------------------------
  const hydrateFrom = useCallback((policy: RiskPolicy) => {
    const isPrompt = isPromptPolicy(policy);
    const kind: PolicyKind = isPrompt ? "prompt" : "risk";
    setFormPolicyKind(kind);
    setFormName(policy.name);
    setFormEnabled(policy.enabled);
    // Scope CEL applies to both kinds; load it before the kind branch.
    const loadedInclude = policy.scopeInclude ?? "";
    setScopeInclude(loadedInclude);
    setScopeExempt(policy.scopeExempt ?? "");
    setScopeMode(loadedInclude.trim() !== "" ? "cel" : "messageTypes");
    if (isPrompt) {
      setFormPromptInstruction(policy.prompt ?? "");
      setSelectedMessageTypes(policyMessageTypesForForm(policy.messageTypes));
      setFormAction((policy.action as PolicyAction) ?? "flag");
      setFormAutoName(policy.autoName ?? true);
      setFormUserMessage(policy.userMessage ?? "");
      setFormModel(policy.modelConfig?.model ?? "");
      setFormTemperature(
        policy.modelConfig?.temperature ?? DEFAULT_JUDGE_TEMPERATURE,
      );
      setFormFailOpen(policy.modelConfig?.failOpen ?? true);
      setFormAudienceType("everyone");
      setSelectedAudiencePrincipalUrns(new Set<string>());
      return;
    }
    setFormPromptInstruction("");
    const customRuleIds = policy.customRuleIds ?? [];
    const categories = policyToCategories(
      policy.sources,
      policy.presidioEntities,
    );
    if (customRuleIds.length > 0) {
      categories.add("custom");
    }
    setSelectedCategories(categories);
    setDisabledRules(new Set(policy.disabledRules ?? []));
    setSelectedCustomRuleIds(new Set<string>(customRuleIds));
    setSelectedMessageTypes(policyMessageTypesForForm(policy.messageTypes));
    setFormAction((policy.action as PolicyAction) ?? "flag");
    setFormAutoName(policy.autoName ?? true);
    setFormUserMessage(policy.userMessage ?? "");
    const audienceType = policy.audienceType ?? "everyone";
    setFormAudienceType(audienceType);
    setSelectedAudiencePrincipalUrns(
      audienceType === "targeted"
        ? new Set<string>(policy.audiencePrincipalUrns ?? [])
        : new Set<string>(),
    );
  }, []);

  // Hydrate from the loaded policy in edit mode, once per policy id+version.
  const hydratedKeyRef = useRef<string | null>(null);
  useEffect(() => {
    if (mode !== "edit" || !initialPolicy) return;
    const key = `${initialPolicy.id}:${initialPolicy.version}`;
    if (hydratedKeyRef.current === key) return;
    hydratedKeyRef.current = key;
    hydrateFrom(initialPolicy);
  }, [mode, initialPolicy, hydrateFrom]);

  // Initialize create-mode form when the kind resolves. Re-run when the chosen
  // kind changes (e.g. via the type chooser).
  const createKind: PolicyKind =
    mode === "create"
      ? kindParam === "prompt" && nlEnabled
        ? "prompt"
        : "risk"
      : "risk";
  const createInitRef = useRef<string | null>(null);
  useEffect(() => {
    if (mode !== "create") return;
    // Only initialize once a kind is committed (chooser dismissed / flag off).
    if (nlEnabled && kindParam == null) return;
    const key = createKind;
    if (createInitRef.current === key) return;
    createInitRef.current = key;
    reset(createKind);
  }, [mode, nlEnabled, kindParam, createKind, reset]);

  // --- mutations ------------------------------------------------------------
  const invalidate = useCallback(() => {
    void invalidateAllRiskListPolicies(queryClient);
    void invalidateAllRiskPoliciesStatus(queryClient);
  }, [queryClient]);

  const createMutation = useRiskCreatePolicyMutation({
    onSuccess: (result) => {
      invalidate();
      // Land on the new policy's detail view.
      void navigate(routes.policyDetail.href(result.id), { replace: true });
    },
  });

  const updateMutation = useRiskPoliciesUpdateMutation({
    onSuccess: () => {
      invalidate();
    },
  });

  // --- save (= handleSave body, verbatim) -----------------------------------
  const handleSave = useCallback(() => {
    const includeCel = scopeMode === "cel" ? scopeInclude.trim() : "";
    const exemptCel = scopeExempt.trim();
    const applicationUpdate = {
      scopeInclude: includeCel,
      scopeExempt: exemptCel,
    };
    const applicationCreate = {
      ...(includeCel ? { scopeInclude: includeCel } : {}),
      ...(exemptCel ? { scopeExempt: exemptCel } : {}),
    };
    if (formPolicyKind === "prompt") {
      const prompt = formPromptInstruction.trim();
      const name = formAutoName ? promptPolicyName(prompt) : formName;
      const temperatureIsCustom = formTemperature !== DEFAULT_JUDGE_TEMPERATURE;
      const hasModelConfig =
        !!editingPolicy?.modelConfig ||
        !!formModel ||
        temperatureIsCustom ||
        !formFailOpen;
      const modelConfig = hasModelConfig
        ? {
            ...(formModel ? { model: formModel } : {}),
            ...(temperatureIsCustom ? { temperature: formTemperature } : {}),
            failOpen: formFailOpen,
          }
        : undefined;
      const userMessagePayload = formUserMessage.trim()
        ? { userMessage: formUserMessage }
        : {};
      const promptMessageTypes =
        policyMessageTypesForPayload(selectedMessageTypes);
      if (editingPolicy) {
        updateMutation.mutate({
          request: {
            updateRiskPolicyRequestBody: {
              id: editingPolicy.id,
              name,
              enabled: formEnabled,
              prompt,
              messageTypes: promptMessageTypes,
              action: formAction,
              autoName: formAutoName,
              ...applicationUpdate,
              ...(modelConfig ? { modelConfig } : {}),
              ...userMessagePayload,
            },
          },
        });
      } else {
        createMutation.mutate({
          request: {
            createRiskPolicyRequestBody: {
              name,
              policyType: "prompt_based",
              enabled: formEnabled,
              prompt,
              messageTypes: promptMessageTypes,
              action: formAction,
              autoName: formAutoName,
              ...applicationCreate,
              ...(modelConfig ? { modelConfig } : {}),
              ...userMessagePayload,
            },
          },
        });
      }
      return;
    }

    const messageTypes = policyMessageTypesForPayload(selectedMessageTypes);
    const {
      sources,
      presidioEntities,
      promptInjectionRules,
      disabledRules: payloadDisabled,
    } = categoriesToPayload(
      selectedCategories,
      disabledRules,
      pinnedHiddenRuleIds(
        editingPolicy ? editingPolicy.presidioEntities : undefined,
      ),
    );
    const action =
      sources.includes("destructive_tool") && formAction === "block"
        ? "flag"
        : formAction;
    const audiencePrincipalUrns =
      formAudienceType === "targeted" ? [...selectedAudiencePrincipalUrns] : [];
    if (editingPolicy) {
      updateMutation.mutate({
        request: {
          updateRiskPolicyRequestBody: {
            id: editingPolicy.id,
            name: formName,
            enabled: formEnabled,
            sources,
            presidioEntities,
            promptInjectionRules,
            disabledRules: payloadDisabled,
            customRuleIds: [...selectedCustomRuleIds],
            messageTypes,
            ...applicationUpdate,
            action,
            audienceType: formAudienceType,
            audiencePrincipalUrns,
            autoName: formAutoName,
            userMessage: formUserMessage,
          },
        },
      });
    } else {
      createMutation.mutate({
        request: {
          createRiskPolicyRequestBody: {
            ...(formAutoName ? {} : { name: formName }),
            enabled: formEnabled,
            sources,
            presidioEntities,
            promptInjectionRules,
            disabledRules: payloadDisabled,
            customRuleIds: [...selectedCustomRuleIds],
            messageTypes,
            ...applicationCreate,
            action,
            audienceType: formAudienceType,
            audiencePrincipalUrns,
            autoName: formAutoName,
            ...(formUserMessage.trim() ? { userMessage: formUserMessage } : {}),
          },
        },
      });
    }
  }, [
    scopeMode,
    scopeInclude,
    scopeExempt,
    formPolicyKind,
    formPromptInstruction,
    formAutoName,
    formName,
    formTemperature,
    editingPolicy,
    formModel,
    formFailOpen,
    formUserMessage,
    selectedMessageTypes,
    formEnabled,
    formAction,
    selectedCategories,
    disabledRules,
    formAudienceType,
    selectedAudiencePrincipalUrns,
    selectedCustomRuleIds,
    updateMutation,
    createMutation,
  ]);

  // --- derived validation ---------------------------------------------------
  const mutationPending = createMutation.isPending || updateMutation.isPending;
  const includeCelStatus = useCelStatus(
    scopeMode === "cel" ? scopeInclude : "",
  );
  const exemptCelStatus = useCelStatus(scopeExempt);
  const scopeMissing =
    scopeMode === "messageTypes"
      ? selectedMessageTypes.size === 0
      : scopeInclude.trim() === "";
  const hasEnabledDetector =
    selectedCustomRuleIds.size > 0 ||
    [...selectedCategories].some((c) =>
      DETECTION_RULES[c]?.some((r) => !r.hidden && !disabledRules.has(r.id)),
    );
  // True when the current config has something to evaluate: at least one enabled
  // detector (risk) or a non-empty prompt (prompt). Gates eval runs.
  const hasDetection =
    formPolicyKind === "prompt"
      ? formPromptInstruction.trim() !== ""
      : hasEnabledDetector;
  const isRiskWizard = formPolicyKind === "risk";
  const applicationInvalid =
    (scopeMode === "cel" && includeCelStatus.kind === "error") ||
    exemptCelStatus.kind === "error";
  const saveDisabled =
    (formPolicyKind === "prompt" && !formPromptInstruction.trim()) ||
    (isRiskWizard && !hasEnabledDetector) ||
    (!formAutoName && !formName.trim()) ||
    scopeMissing ||
    applicationInvalid ||
    (formPolicyKind === "risk" &&
      formAudienceType === "targeted" &&
      selectedAudiencePrincipalUrns.size === 0) ||
    mutationPending;

  // --- eval source ----------------------------------------------------------
  // Build the candidate from the current form. Dirty = differs from the loaded
  // policy's normalized candidate; clean saved policies eval by policy_id.
  const serializableState: SerializableConfigInput = {
    formPolicyKind,
    formPromptInstruction,
    selectedCategories,
    disabledRules,
    selectedCustomRuleIds,
    selectedMessageTypes,
    scopeMode,
    scopeInclude,
    scopeExempt,
    formModel,
    formTemperature,
    formFailOpen,
    editingPolicy,
  };

  const candidate = useMemo(
    () => buildCandidatePayload(serializableState),
    // eslint-disable-next-line react-hooks/exhaustive-deps -- serializableState is rebuilt each render from these primitives/sets
    [
      formPolicyKind,
      formPromptInstruction,
      selectedCategories,
      disabledRules,
      selectedCustomRuleIds,
      selectedMessageTypes,
      scopeMode,
      scopeInclude,
      scopeExempt,
      formModel,
      formTemperature,
      formFailOpen,
      editingPolicy,
    ],
  );

  const savedCandidate = useMemo(
    () => (editingPolicy ? candidateFromPolicy(editingPolicy) : null),
    [editingPolicy],
  );

  const isDirty = useMemo(() => {
    if (!savedCandidate) return true; // create mode is always "dirty".
    return !candidatesEqual(candidate, savedCandidate);
  }, [candidate, savedCandidate]);

  const evalSource: EvalSource = useMemo(() => {
    if (editingPolicy && !isDirty) {
      return { policyId: editingPolicy.id };
    }
    return { candidate };
  }, [editingPolicy, isDirty, candidate]);

  return {
    // raw state + setters
    state: {
      formPolicyKind,
      formName,
      formEnabled,
      formPromptInstruction,
      selectedCategories,
      disabledRules,
      selectedCustomRuleIds,
      scopeInclude,
      scopeExempt,
      scopeMode,
      selectedMessageTypes,
      formAction,
      formAutoName,
      formUserMessage,
      formModel,
      formTemperature,
      formFailOpen,
      formAudienceType,
      selectedAudiencePrincipalUrns,
    },
    setters: {
      setFormPolicyKind,
      setFormName,
      setFormEnabled,
      setFormPromptInstruction,
      setSelectedCategories,
      setDisabledRules,
      setSelectedCustomRuleIds,
      setScopeInclude,
      setScopeExempt,
      setScopeMode,
      setSelectedMessageTypes,
      setFormAction,
      setFormAutoName,
      setFormUserMessage,
      setFormModel,
      setFormTemperature,
      setFormFailOpen,
      setFormAudienceType,
      setSelectedAudiencePrincipalUrns,
    },
    derived: {
      editingPolicy,
      saveDisabled,
      scopeMissing,
      hasEnabledDetector,
      hasDetection,
      mutationPending,
      isDirty,
    },
    // create-flow query state
    kindParam,
    setKindParam,
    handlers: { handleSave, reset, hydrateFrom },
    evalSource,
  };
}
