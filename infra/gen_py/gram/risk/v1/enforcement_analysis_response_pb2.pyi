from gcp.pubsub.v1 import options_pb2 as _options_pb2
from google.protobuf.internal import containers as _containers
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from collections.abc import Iterable as _Iterable, Mapping as _Mapping
from typing import ClassVar as _ClassVar, Optional as _Optional, Union as _Union

DESCRIPTOR: _descriptor.FileDescriptor

class EnforcementAnalysisResponse(_message.Message):
    __slots__ = ("correlation_id", "created_at", "error", "scan_duration_ms", "analyzer", "presidio", "gitleaks", "prompt_injection", "prompt_policy", "custom_rules")
    class Finding(_message.Message):
        __slots__ = ("rule_id", "entity", "description", "match", "start_pos", "end_pos", "confidence", "tags")
        RULE_ID_FIELD_NUMBER: _ClassVar[int]
        ENTITY_FIELD_NUMBER: _ClassVar[int]
        DESCRIPTION_FIELD_NUMBER: _ClassVar[int]
        MATCH_FIELD_NUMBER: _ClassVar[int]
        START_POS_FIELD_NUMBER: _ClassVar[int]
        END_POS_FIELD_NUMBER: _ClassVar[int]
        CONFIDENCE_FIELD_NUMBER: _ClassVar[int]
        TAGS_FIELD_NUMBER: _ClassVar[int]
        rule_id: str
        entity: str
        description: str
        match: str
        start_pos: int
        end_pos: int
        confidence: float
        tags: _containers.RepeatedScalarFieldContainer[str]
        def __init__(self, rule_id: _Optional[str] = ..., entity: _Optional[str] = ..., description: _Optional[str] = ..., match: _Optional[str] = ..., start_pos: _Optional[int] = ..., end_pos: _Optional[int] = ..., confidence: _Optional[float] = ..., tags: _Optional[_Iterable[str]] = ...) -> None: ...
    class PresidioResult(_message.Message):
        __slots__ = ("findings",)
        FINDINGS_FIELD_NUMBER: _ClassVar[int]
        findings: _containers.RepeatedCompositeFieldContainer[EnforcementAnalysisResponse.Finding]
        def __init__(self, findings: _Optional[_Iterable[_Union[EnforcementAnalysisResponse.Finding, _Mapping]]] = ...) -> None: ...
    class GitleaksResult(_message.Message):
        __slots__ = ("findings",)
        FINDINGS_FIELD_NUMBER: _ClassVar[int]
        findings: _containers.RepeatedCompositeFieldContainer[EnforcementAnalysisResponse.Finding]
        def __init__(self, findings: _Optional[_Iterable[_Union[EnforcementAnalysisResponse.Finding, _Mapping]]] = ...) -> None: ...
    class PromptInjectionResult(_message.Message):
        __slots__ = ("findings",)
        FINDINGS_FIELD_NUMBER: _ClassVar[int]
        findings: _containers.RepeatedCompositeFieldContainer[EnforcementAnalysisResponse.Finding]
        def __init__(self, findings: _Optional[_Iterable[_Union[EnforcementAnalysisResponse.Finding, _Mapping]]] = ...) -> None: ...
    class PromptPolicyResult(_message.Message):
        __slots__ = ("findings",)
        FINDINGS_FIELD_NUMBER: _ClassVar[int]
        findings: _containers.RepeatedCompositeFieldContainer[EnforcementAnalysisResponse.Finding]
        def __init__(self, findings: _Optional[_Iterable[_Union[EnforcementAnalysisResponse.Finding, _Mapping]]] = ...) -> None: ...
    class CustomRulesResult(_message.Message):
        __slots__ = ("findings",)
        FINDINGS_FIELD_NUMBER: _ClassVar[int]
        findings: _containers.RepeatedCompositeFieldContainer[EnforcementAnalysisResponse.Finding]
        def __init__(self, findings: _Optional[_Iterable[_Union[EnforcementAnalysisResponse.Finding, _Mapping]]] = ...) -> None: ...
    CORRELATION_ID_FIELD_NUMBER: _ClassVar[int]
    CREATED_AT_FIELD_NUMBER: _ClassVar[int]
    ERROR_FIELD_NUMBER: _ClassVar[int]
    SCAN_DURATION_MS_FIELD_NUMBER: _ClassVar[int]
    ANALYZER_FIELD_NUMBER: _ClassVar[int]
    PRESIDIO_FIELD_NUMBER: _ClassVar[int]
    GITLEAKS_FIELD_NUMBER: _ClassVar[int]
    PROMPT_INJECTION_FIELD_NUMBER: _ClassVar[int]
    PROMPT_POLICY_FIELD_NUMBER: _ClassVar[int]
    CUSTOM_RULES_FIELD_NUMBER: _ClassVar[int]
    correlation_id: str
    created_at: str
    error: str
    scan_duration_ms: float
    analyzer: str
    presidio: EnforcementAnalysisResponse.PresidioResult
    gitleaks: EnforcementAnalysisResponse.GitleaksResult
    prompt_injection: EnforcementAnalysisResponse.PromptInjectionResult
    prompt_policy: EnforcementAnalysisResponse.PromptPolicyResult
    custom_rules: EnforcementAnalysisResponse.CustomRulesResult
    def __init__(self, correlation_id: _Optional[str] = ..., created_at: _Optional[str] = ..., error: _Optional[str] = ..., scan_duration_ms: _Optional[float] = ..., analyzer: _Optional[str] = ..., presidio: _Optional[_Union[EnforcementAnalysisResponse.PresidioResult, _Mapping]] = ..., gitleaks: _Optional[_Union[EnforcementAnalysisResponse.GitleaksResult, _Mapping]] = ..., prompt_injection: _Optional[_Union[EnforcementAnalysisResponse.PromptInjectionResult, _Mapping]] = ..., prompt_policy: _Optional[_Union[EnforcementAnalysisResponse.PromptPolicyResult, _Mapping]] = ..., custom_rules: _Optional[_Union[EnforcementAnalysisResponse.CustomRulesResult, _Mapping]] = ...) -> None: ...
