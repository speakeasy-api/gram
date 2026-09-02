from google.protobuf.internal import containers as _containers
from google.protobuf.internal import enum_type_wrapper as _enum_type_wrapper
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from collections.abc import Iterable as _Iterable, Mapping as _Mapping
from typing import ClassVar as _ClassVar, Optional as _Optional, Union as _Union

DESCRIPTOR: _descriptor.FileDescriptor

class EnforcementScanner(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    ENFORCEMENT_SCANNER_UNSPECIFIED: _ClassVar[EnforcementScanner]
    ENFORCEMENT_SCANNER_GITLEAKS: _ClassVar[EnforcementScanner]
    ENFORCEMENT_SCANNER_PRESIDIO: _ClassVar[EnforcementScanner]
    ENFORCEMENT_SCANNER_PROMPT_INJECTION: _ClassVar[EnforcementScanner]
    ENFORCEMENT_SCANNER_JUDGE: _ClassVar[EnforcementScanner]

class EnforcementStatus(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    ENFORCEMENT_STATUS_UNSPECIFIED: _ClassVar[EnforcementStatus]
    ENFORCEMENT_STATUS_OK: _ClassVar[EnforcementStatus]
    ENFORCEMENT_STATUS_ERROR: _ClassVar[EnforcementStatus]
    ENFORCEMENT_STATUS_DEAD_LETTER: _ClassVar[EnforcementStatus]
ENFORCEMENT_SCANNER_UNSPECIFIED: EnforcementScanner
ENFORCEMENT_SCANNER_GITLEAKS: EnforcementScanner
ENFORCEMENT_SCANNER_PRESIDIO: EnforcementScanner
ENFORCEMENT_SCANNER_PROMPT_INJECTION: EnforcementScanner
ENFORCEMENT_SCANNER_JUDGE: EnforcementScanner
ENFORCEMENT_STATUS_UNSPECIFIED: EnforcementStatus
ENFORCEMENT_STATUS_OK: EnforcementStatus
ENFORCEMENT_STATUS_ERROR: EnforcementStatus
ENFORCEMENT_STATUS_DEAD_LETTER: EnforcementStatus

class EnforcementFinding(_message.Message):
    __slots__ = ("rule_id", "category", "score", "start_pos", "end_pos", "surface", "field", "path", "tool_call_id", "masked_preview", "fingerprint")
    RULE_ID_FIELD_NUMBER: _ClassVar[int]
    CATEGORY_FIELD_NUMBER: _ClassVar[int]
    SCORE_FIELD_NUMBER: _ClassVar[int]
    START_POS_FIELD_NUMBER: _ClassVar[int]
    END_POS_FIELD_NUMBER: _ClassVar[int]
    SURFACE_FIELD_NUMBER: _ClassVar[int]
    FIELD_FIELD_NUMBER: _ClassVar[int]
    PATH_FIELD_NUMBER: _ClassVar[int]
    TOOL_CALL_ID_FIELD_NUMBER: _ClassVar[int]
    MASKED_PREVIEW_FIELD_NUMBER: _ClassVar[int]
    FINGERPRINT_FIELD_NUMBER: _ClassVar[int]
    rule_id: str
    category: str
    score: float
    start_pos: int
    end_pos: int
    surface: str
    field: str
    path: str
    tool_call_id: str
    masked_preview: str
    fingerprint: str
    def __init__(self, rule_id: _Optional[str] = ..., category: _Optional[str] = ..., score: _Optional[float] = ..., start_pos: _Optional[int] = ..., end_pos: _Optional[int] = ..., surface: _Optional[str] = ..., field: _Optional[str] = ..., path: _Optional[str] = ..., tool_call_id: _Optional[str] = ..., masked_preview: _Optional[str] = ..., fingerprint: _Optional[str] = ...) -> None: ...

class EnforcementDiagnostics(_message.Message):
    __slots__ = ("scan_duration_ms", "consumer_id", "delivery_attempt")
    SCAN_DURATION_MS_FIELD_NUMBER: _ClassVar[int]
    CONSUMER_ID_FIELD_NUMBER: _ClassVar[int]
    DELIVERY_ATTEMPT_FIELD_NUMBER: _ClassVar[int]
    scan_duration_ms: int
    consumer_id: str
    delivery_attempt: int
    def __init__(self, scan_duration_ms: _Optional[int] = ..., consumer_id: _Optional[str] = ..., delivery_attempt: _Optional[int] = ...) -> None: ...

class EnforcementReply(_message.Message):
    __slots__ = ("correlation_id", "scanner", "status", "reason", "findings", "diagnostics", "policy_id")
    CORRELATION_ID_FIELD_NUMBER: _ClassVar[int]
    SCANNER_FIELD_NUMBER: _ClassVar[int]
    STATUS_FIELD_NUMBER: _ClassVar[int]
    REASON_FIELD_NUMBER: _ClassVar[int]
    FINDINGS_FIELD_NUMBER: _ClassVar[int]
    DIAGNOSTICS_FIELD_NUMBER: _ClassVar[int]
    POLICY_ID_FIELD_NUMBER: _ClassVar[int]
    correlation_id: str
    scanner: EnforcementScanner
    status: EnforcementStatus
    reason: str
    findings: _containers.RepeatedCompositeFieldContainer[EnforcementFinding]
    diagnostics: EnforcementDiagnostics
    policy_id: str
    def __init__(self, correlation_id: _Optional[str] = ..., scanner: _Optional[_Union[EnforcementScanner, str]] = ..., status: _Optional[_Union[EnforcementStatus, str]] = ..., reason: _Optional[str] = ..., findings: _Optional[_Iterable[_Union[EnforcementFinding, _Mapping]]] = ..., diagnostics: _Optional[_Union[EnforcementDiagnostics, _Mapping]] = ..., policy_id: _Optional[str] = ...) -> None: ...
