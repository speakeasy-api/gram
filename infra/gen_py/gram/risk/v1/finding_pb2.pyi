from gcp.pubsub.v1 import options_pb2 as _options_pb2
from google.protobuf.internal import containers as _containers
from google.protobuf.internal import enum_type_wrapper as _enum_type_wrapper
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from collections.abc import Iterable as _Iterable, Mapping as _Mapping
from typing import ClassVar as _ClassVar, Optional as _Optional, Union as _Union

DESCRIPTOR: _descriptor.FileDescriptor

class Finding(_message.Message):
    __slots__ = ("id", "request_id", "chat_message_id", "project_id", "organization_id", "risk_policy_id", "risk_policy_version", "created_at", "rule_id", "description", "match", "start_pos", "end_pos", "tags", "source", "confidence", "dead_letter_reason", "content_part_id", "false_positive_at", "surface", "field", "path", "tool_call_id", "excluded_at", "excluded_reason", "excluded_detail", "event_kind", "shadow", "attribution", "execution", "enforcement_outcome")
    class EnforcementOutcome(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
        __slots__ = ()
        ENFORCEMENT_OUTCOME_UNSPECIFIED: _ClassVar[Finding.EnforcementOutcome]
        ENFORCEMENT_OUTCOME_LOGGED: _ClassVar[Finding.EnforcementOutcome]
        ENFORCEMENT_OUTCOME_DENIED: _ClassVar[Finding.EnforcementOutcome]
        ENFORCEMENT_OUTCOME_WITHHELD: _ClassVar[Finding.EnforcementOutcome]
        ENFORCEMENT_OUTCOME_WARNED_PENDING: _ClassVar[Finding.EnforcementOutcome]
        ENFORCEMENT_OUTCOME_WARNED_ACKNOWLEDGED: _ClassVar[Finding.EnforcementOutcome]
        ENFORCEMENT_OUTCOME_WARNED_ABANDONED: _ClassVar[Finding.EnforcementOutcome]
        ENFORCEMENT_OUTCOME_QUARANTINED: _ClassVar[Finding.EnforcementOutcome]
    ENFORCEMENT_OUTCOME_UNSPECIFIED: Finding.EnforcementOutcome
    ENFORCEMENT_OUTCOME_LOGGED: Finding.EnforcementOutcome
    ENFORCEMENT_OUTCOME_DENIED: Finding.EnforcementOutcome
    ENFORCEMENT_OUTCOME_WITHHELD: Finding.EnforcementOutcome
    ENFORCEMENT_OUTCOME_WARNED_PENDING: Finding.EnforcementOutcome
    ENFORCEMENT_OUTCOME_WARNED_ACKNOWLEDGED: Finding.EnforcementOutcome
    ENFORCEMENT_OUTCOME_WARNED_ABANDONED: Finding.EnforcementOutcome
    ENFORCEMENT_OUTCOME_QUARANTINED: Finding.EnforcementOutcome
    class Attribution(_message.Message):
        __slots__ = ("chat_id", "user_id", "external_user_id", "assistant_id", "message_created_at", "chat_source", "team", "user_email")
        CHAT_ID_FIELD_NUMBER: _ClassVar[int]
        USER_ID_FIELD_NUMBER: _ClassVar[int]
        EXTERNAL_USER_ID_FIELD_NUMBER: _ClassVar[int]
        ASSISTANT_ID_FIELD_NUMBER: _ClassVar[int]
        MESSAGE_CREATED_AT_FIELD_NUMBER: _ClassVar[int]
        CHAT_SOURCE_FIELD_NUMBER: _ClassVar[int]
        TEAM_FIELD_NUMBER: _ClassVar[int]
        USER_EMAIL_FIELD_NUMBER: _ClassVar[int]
        chat_id: str
        user_id: str
        external_user_id: str
        assistant_id: str
        message_created_at: str
        chat_source: str
        team: str
        user_email: str
        def __init__(self, chat_id: _Optional[str] = ..., user_id: _Optional[str] = ..., external_user_id: _Optional[str] = ..., assistant_id: _Optional[str] = ..., message_created_at: _Optional[str] = ..., chat_source: _Optional[str] = ..., team: _Optional[str] = ..., user_email: _Optional[str] = ...) -> None: ...
    class Execution(_message.Message):
        __slots__ = ("execution_id", "mcp_server_id", "meta_mcp_server_id", "toolset_id", "tool_name", "phase", "mediation_surface", "method", "principal_kind", "identity_stamped")
        EXECUTION_ID_FIELD_NUMBER: _ClassVar[int]
        MCP_SERVER_ID_FIELD_NUMBER: _ClassVar[int]
        META_MCP_SERVER_ID_FIELD_NUMBER: _ClassVar[int]
        TOOLSET_ID_FIELD_NUMBER: _ClassVar[int]
        TOOL_NAME_FIELD_NUMBER: _ClassVar[int]
        PHASE_FIELD_NUMBER: _ClassVar[int]
        MEDIATION_SURFACE_FIELD_NUMBER: _ClassVar[int]
        METHOD_FIELD_NUMBER: _ClassVar[int]
        PRINCIPAL_KIND_FIELD_NUMBER: _ClassVar[int]
        IDENTITY_STAMPED_FIELD_NUMBER: _ClassVar[int]
        execution_id: str
        mcp_server_id: str
        meta_mcp_server_id: str
        toolset_id: str
        tool_name: str
        phase: str
        mediation_surface: str
        method: str
        principal_kind: str
        identity_stamped: bool
        def __init__(self, execution_id: _Optional[str] = ..., mcp_server_id: _Optional[str] = ..., meta_mcp_server_id: _Optional[str] = ..., toolset_id: _Optional[str] = ..., tool_name: _Optional[str] = ..., phase: _Optional[str] = ..., mediation_surface: _Optional[str] = ..., method: _Optional[str] = ..., principal_kind: _Optional[str] = ..., identity_stamped: _Optional[bool] = ...) -> None: ...
    ID_FIELD_NUMBER: _ClassVar[int]
    REQUEST_ID_FIELD_NUMBER: _ClassVar[int]
    CHAT_MESSAGE_ID_FIELD_NUMBER: _ClassVar[int]
    PROJECT_ID_FIELD_NUMBER: _ClassVar[int]
    ORGANIZATION_ID_FIELD_NUMBER: _ClassVar[int]
    RISK_POLICY_ID_FIELD_NUMBER: _ClassVar[int]
    RISK_POLICY_VERSION_FIELD_NUMBER: _ClassVar[int]
    CREATED_AT_FIELD_NUMBER: _ClassVar[int]
    RULE_ID_FIELD_NUMBER: _ClassVar[int]
    DESCRIPTION_FIELD_NUMBER: _ClassVar[int]
    MATCH_FIELD_NUMBER: _ClassVar[int]
    START_POS_FIELD_NUMBER: _ClassVar[int]
    END_POS_FIELD_NUMBER: _ClassVar[int]
    TAGS_FIELD_NUMBER: _ClassVar[int]
    SOURCE_FIELD_NUMBER: _ClassVar[int]
    CONFIDENCE_FIELD_NUMBER: _ClassVar[int]
    DEAD_LETTER_REASON_FIELD_NUMBER: _ClassVar[int]
    CONTENT_PART_ID_FIELD_NUMBER: _ClassVar[int]
    FALSE_POSITIVE_AT_FIELD_NUMBER: _ClassVar[int]
    SURFACE_FIELD_NUMBER: _ClassVar[int]
    FIELD_FIELD_NUMBER: _ClassVar[int]
    PATH_FIELD_NUMBER: _ClassVar[int]
    TOOL_CALL_ID_FIELD_NUMBER: _ClassVar[int]
    EXCLUDED_AT_FIELD_NUMBER: _ClassVar[int]
    EXCLUDED_REASON_FIELD_NUMBER: _ClassVar[int]
    EXCLUDED_DETAIL_FIELD_NUMBER: _ClassVar[int]
    EVENT_KIND_FIELD_NUMBER: _ClassVar[int]
    SHADOW_FIELD_NUMBER: _ClassVar[int]
    ATTRIBUTION_FIELD_NUMBER: _ClassVar[int]
    EXECUTION_FIELD_NUMBER: _ClassVar[int]
    ENFORCEMENT_OUTCOME_FIELD_NUMBER: _ClassVar[int]
    id: str
    request_id: str
    chat_message_id: str
    project_id: str
    organization_id: str
    risk_policy_id: str
    risk_policy_version: int
    created_at: str
    rule_id: str
    description: str
    match: str
    start_pos: int
    end_pos: int
    tags: _containers.RepeatedScalarFieldContainer[str]
    source: str
    confidence: float
    dead_letter_reason: str
    content_part_id: str
    false_positive_at: str
    surface: str
    field: str
    path: str
    tool_call_id: str
    excluded_at: str
    excluded_reason: str
    excluded_detail: str
    event_kind: str
    shadow: bool
    attribution: Finding.Attribution
    execution: Finding.Execution
    enforcement_outcome: Finding.EnforcementOutcome
    def __init__(self, id: _Optional[str] = ..., request_id: _Optional[str] = ..., chat_message_id: _Optional[str] = ..., project_id: _Optional[str] = ..., organization_id: _Optional[str] = ..., risk_policy_id: _Optional[str] = ..., risk_policy_version: _Optional[int] = ..., created_at: _Optional[str] = ..., rule_id: _Optional[str] = ..., description: _Optional[str] = ..., match: _Optional[str] = ..., start_pos: _Optional[int] = ..., end_pos: _Optional[int] = ..., tags: _Optional[_Iterable[str]] = ..., source: _Optional[str] = ..., confidence: _Optional[float] = ..., dead_letter_reason: _Optional[str] = ..., content_part_id: _Optional[str] = ..., false_positive_at: _Optional[str] = ..., surface: _Optional[str] = ..., field: _Optional[str] = ..., path: _Optional[str] = ..., tool_call_id: _Optional[str] = ..., excluded_at: _Optional[str] = ..., excluded_reason: _Optional[str] = ..., excluded_detail: _Optional[str] = ..., event_kind: _Optional[str] = ..., shadow: _Optional[bool] = ..., attribution: _Optional[_Union[Finding.Attribution, _Mapping]] = ..., execution: _Optional[_Union[Finding.Execution, _Mapping]] = ..., enforcement_outcome: _Optional[_Union[Finding.EnforcementOutcome, str]] = ...) -> None: ...
