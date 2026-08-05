from gcp.pubsub.v1 import options_pb2 as _options_pb2
from google.protobuf.internal import containers as _containers
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from collections.abc import Iterable as _Iterable, Mapping as _Mapping
from typing import ClassVar as _ClassVar, Optional as _Optional, Union as _Union

DESCRIPTOR: _descriptor.FileDescriptor

class EnforcementAnalysisRequest(_message.Message):
    __slots__ = ("correlation_id", "project_id", "organization_id", "created_at", "deadline", "analyzer", "risk_policy_id", "risk_policy_version", "presidio", "gitleaks", "prompt_injection", "prompt_policy", "custom_rules")
    class ToolCall(_message.Message):
        __slots__ = ("name", "arguments")
        NAME_FIELD_NUMBER: _ClassVar[int]
        ARGUMENTS_FIELD_NUMBER: _ClassVar[int]
        name: str
        arguments: str
        def __init__(self, name: _Optional[str] = ..., arguments: _Optional[str] = ...) -> None: ...
    class PresidioPayload(_message.Message):
        __slots__ = ("content", "entities", "score_threshold")
        CONTENT_FIELD_NUMBER: _ClassVar[int]
        ENTITIES_FIELD_NUMBER: _ClassVar[int]
        SCORE_THRESHOLD_FIELD_NUMBER: _ClassVar[int]
        content: str
        entities: _containers.RepeatedScalarFieldContainer[str]
        score_threshold: float
        def __init__(self, content: _Optional[str] = ..., entities: _Optional[_Iterable[str]] = ..., score_threshold: _Optional[float] = ...) -> None: ...
    class GitleaksPayload(_message.Message):
        __slots__ = ("content",)
        CONTENT_FIELD_NUMBER: _ClassVar[int]
        content: str
        def __init__(self, content: _Optional[str] = ...) -> None: ...
    class PromptInjectionPayload(_message.Message):
        __slots__ = ("content", "user_id", "l1_enabled", "message_type", "body", "tool_name", "tool_calls")
        CONTENT_FIELD_NUMBER: _ClassVar[int]
        USER_ID_FIELD_NUMBER: _ClassVar[int]
        L1_ENABLED_FIELD_NUMBER: _ClassVar[int]
        MESSAGE_TYPE_FIELD_NUMBER: _ClassVar[int]
        BODY_FIELD_NUMBER: _ClassVar[int]
        TOOL_NAME_FIELD_NUMBER: _ClassVar[int]
        TOOL_CALLS_FIELD_NUMBER: _ClassVar[int]
        content: str
        user_id: str
        l1_enabled: bool
        message_type: str
        body: str
        tool_name: str
        tool_calls: _containers.RepeatedCompositeFieldContainer[EnforcementAnalysisRequest.ToolCall]
        def __init__(self, content: _Optional[str] = ..., user_id: _Optional[str] = ..., l1_enabled: _Optional[bool] = ..., message_type: _Optional[str] = ..., body: _Optional[str] = ..., tool_name: _Optional[str] = ..., tool_calls: _Optional[_Iterable[_Union[EnforcementAnalysisRequest.ToolCall, _Mapping]]] = ...) -> None: ...
    class PromptPolicyPayload(_message.Message):
        __slots__ = ("content", "user_id", "prompt", "model_config", "message_type", "body", "tool_name", "tool_calls")
        CONTENT_FIELD_NUMBER: _ClassVar[int]
        USER_ID_FIELD_NUMBER: _ClassVar[int]
        PROMPT_FIELD_NUMBER: _ClassVar[int]
        MODEL_CONFIG_FIELD_NUMBER: _ClassVar[int]
        MESSAGE_TYPE_FIELD_NUMBER: _ClassVar[int]
        BODY_FIELD_NUMBER: _ClassVar[int]
        TOOL_NAME_FIELD_NUMBER: _ClassVar[int]
        TOOL_CALLS_FIELD_NUMBER: _ClassVar[int]
        content: str
        user_id: str
        prompt: str
        model_config: bytes
        message_type: str
        body: str
        tool_name: str
        tool_calls: _containers.RepeatedCompositeFieldContainer[EnforcementAnalysisRequest.ToolCall]
        def __init__(self, content: _Optional[str] = ..., user_id: _Optional[str] = ..., prompt: _Optional[str] = ..., model_config: _Optional[bytes] = ..., message_type: _Optional[str] = ..., body: _Optional[str] = ..., tool_name: _Optional[str] = ..., tool_calls: _Optional[_Iterable[_Union[EnforcementAnalysisRequest.ToolCall, _Mapping]]] = ...) -> None: ...
    class CustomRulesPayload(_message.Message):
        __slots__ = ("content", "kind", "tool_calls", "custom_rule_ids")
        CONTENT_FIELD_NUMBER: _ClassVar[int]
        KIND_FIELD_NUMBER: _ClassVar[int]
        TOOL_CALLS_FIELD_NUMBER: _ClassVar[int]
        CUSTOM_RULE_IDS_FIELD_NUMBER: _ClassVar[int]
        content: str
        kind: str
        tool_calls: _containers.RepeatedCompositeFieldContainer[EnforcementAnalysisRequest.ToolCall]
        custom_rule_ids: _containers.RepeatedScalarFieldContainer[str]
        def __init__(self, content: _Optional[str] = ..., kind: _Optional[str] = ..., tool_calls: _Optional[_Iterable[_Union[EnforcementAnalysisRequest.ToolCall, _Mapping]]] = ..., custom_rule_ids: _Optional[_Iterable[str]] = ...) -> None: ...
    CORRELATION_ID_FIELD_NUMBER: _ClassVar[int]
    PROJECT_ID_FIELD_NUMBER: _ClassVar[int]
    ORGANIZATION_ID_FIELD_NUMBER: _ClassVar[int]
    CREATED_AT_FIELD_NUMBER: _ClassVar[int]
    DEADLINE_FIELD_NUMBER: _ClassVar[int]
    ANALYZER_FIELD_NUMBER: _ClassVar[int]
    RISK_POLICY_ID_FIELD_NUMBER: _ClassVar[int]
    RISK_POLICY_VERSION_FIELD_NUMBER: _ClassVar[int]
    PRESIDIO_FIELD_NUMBER: _ClassVar[int]
    GITLEAKS_FIELD_NUMBER: _ClassVar[int]
    PROMPT_INJECTION_FIELD_NUMBER: _ClassVar[int]
    PROMPT_POLICY_FIELD_NUMBER: _ClassVar[int]
    CUSTOM_RULES_FIELD_NUMBER: _ClassVar[int]
    correlation_id: str
    project_id: str
    organization_id: str
    created_at: str
    deadline: str
    analyzer: str
    risk_policy_id: str
    risk_policy_version: int
    presidio: EnforcementAnalysisRequest.PresidioPayload
    gitleaks: EnforcementAnalysisRequest.GitleaksPayload
    prompt_injection: EnforcementAnalysisRequest.PromptInjectionPayload
    prompt_policy: EnforcementAnalysisRequest.PromptPolicyPayload
    custom_rules: EnforcementAnalysisRequest.CustomRulesPayload
    def __init__(self, correlation_id: _Optional[str] = ..., project_id: _Optional[str] = ..., organization_id: _Optional[str] = ..., created_at: _Optional[str] = ..., deadline: _Optional[str] = ..., analyzer: _Optional[str] = ..., risk_policy_id: _Optional[str] = ..., risk_policy_version: _Optional[int] = ..., presidio: _Optional[_Union[EnforcementAnalysisRequest.PresidioPayload, _Mapping]] = ..., gitleaks: _Optional[_Union[EnforcementAnalysisRequest.GitleaksPayload, _Mapping]] = ..., prompt_injection: _Optional[_Union[EnforcementAnalysisRequest.PromptInjectionPayload, _Mapping]] = ..., prompt_policy: _Optional[_Union[EnforcementAnalysisRequest.PromptPolicyPayload, _Mapping]] = ..., custom_rules: _Optional[_Union[EnforcementAnalysisRequest.CustomRulesPayload, _Mapping]] = ...) -> None: ...
