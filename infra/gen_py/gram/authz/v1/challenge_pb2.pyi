from gcp.pubsub.v1 import options_pb2 as _options_pb2
from google.protobuf.internal import containers as _containers
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from collections.abc import Iterable as _Iterable, Mapping as _Mapping
from typing import ClassVar as _ClassVar, Optional as _Optional, Union as _Union

DESCRIPTOR: _descriptor.FileDescriptor

class Challenge(_message.Message):
    __slots__ = ("id", "timestamp", "organization_id", "project_id", "trace_id", "span_id", "request_id", "principal_urn", "principal_type", "user_id", "user_external_id", "user_email", "api_key_id", "session_id", "role_slugs", "operation", "outcome", "reason", "scope", "resource_kind", "resource_id", "selector", "expanded_scopes", "requested_checks", "matched_grants", "evaluated_grant_count", "filter_candidate_count", "filter_allowed_count")
    class RequestedCheck(_message.Message):
        __slots__ = ("scope", "resource_kind", "resource_id", "selector")
        SCOPE_FIELD_NUMBER: _ClassVar[int]
        RESOURCE_KIND_FIELD_NUMBER: _ClassVar[int]
        RESOURCE_ID_FIELD_NUMBER: _ClassVar[int]
        SELECTOR_FIELD_NUMBER: _ClassVar[int]
        scope: str
        resource_kind: str
        resource_id: str
        selector: str
        def __init__(self, scope: _Optional[str] = ..., resource_kind: _Optional[str] = ..., resource_id: _Optional[str] = ..., selector: _Optional[str] = ...) -> None: ...
    class MatchedGrant(_message.Message):
        __slots__ = ("principal_urn", "scope", "selector", "matched_via_check_scope")
        PRINCIPAL_URN_FIELD_NUMBER: _ClassVar[int]
        SCOPE_FIELD_NUMBER: _ClassVar[int]
        SELECTOR_FIELD_NUMBER: _ClassVar[int]
        MATCHED_VIA_CHECK_SCOPE_FIELD_NUMBER: _ClassVar[int]
        principal_urn: str
        scope: str
        selector: str
        matched_via_check_scope: str
        def __init__(self, principal_urn: _Optional[str] = ..., scope: _Optional[str] = ..., selector: _Optional[str] = ..., matched_via_check_scope: _Optional[str] = ...) -> None: ...
    ID_FIELD_NUMBER: _ClassVar[int]
    TIMESTAMP_FIELD_NUMBER: _ClassVar[int]
    ORGANIZATION_ID_FIELD_NUMBER: _ClassVar[int]
    PROJECT_ID_FIELD_NUMBER: _ClassVar[int]
    TRACE_ID_FIELD_NUMBER: _ClassVar[int]
    SPAN_ID_FIELD_NUMBER: _ClassVar[int]
    REQUEST_ID_FIELD_NUMBER: _ClassVar[int]
    PRINCIPAL_URN_FIELD_NUMBER: _ClassVar[int]
    PRINCIPAL_TYPE_FIELD_NUMBER: _ClassVar[int]
    USER_ID_FIELD_NUMBER: _ClassVar[int]
    USER_EXTERNAL_ID_FIELD_NUMBER: _ClassVar[int]
    USER_EMAIL_FIELD_NUMBER: _ClassVar[int]
    API_KEY_ID_FIELD_NUMBER: _ClassVar[int]
    SESSION_ID_FIELD_NUMBER: _ClassVar[int]
    ROLE_SLUGS_FIELD_NUMBER: _ClassVar[int]
    OPERATION_FIELD_NUMBER: _ClassVar[int]
    OUTCOME_FIELD_NUMBER: _ClassVar[int]
    REASON_FIELD_NUMBER: _ClassVar[int]
    SCOPE_FIELD_NUMBER: _ClassVar[int]
    RESOURCE_KIND_FIELD_NUMBER: _ClassVar[int]
    RESOURCE_ID_FIELD_NUMBER: _ClassVar[int]
    SELECTOR_FIELD_NUMBER: _ClassVar[int]
    EXPANDED_SCOPES_FIELD_NUMBER: _ClassVar[int]
    REQUESTED_CHECKS_FIELD_NUMBER: _ClassVar[int]
    MATCHED_GRANTS_FIELD_NUMBER: _ClassVar[int]
    EVALUATED_GRANT_COUNT_FIELD_NUMBER: _ClassVar[int]
    FILTER_CANDIDATE_COUNT_FIELD_NUMBER: _ClassVar[int]
    FILTER_ALLOWED_COUNT_FIELD_NUMBER: _ClassVar[int]
    id: str
    timestamp: str
    organization_id: str
    project_id: str
    trace_id: str
    span_id: str
    request_id: str
    principal_urn: str
    principal_type: str
    user_id: str
    user_external_id: str
    user_email: str
    api_key_id: str
    session_id: str
    role_slugs: _containers.RepeatedScalarFieldContainer[str]
    operation: str
    outcome: str
    reason: str
    scope: str
    resource_kind: str
    resource_id: str
    selector: str
    expanded_scopes: _containers.RepeatedScalarFieldContainer[str]
    requested_checks: _containers.RepeatedCompositeFieldContainer[Challenge.RequestedCheck]
    matched_grants: _containers.RepeatedCompositeFieldContainer[Challenge.MatchedGrant]
    evaluated_grant_count: int
    filter_candidate_count: int
    filter_allowed_count: int
    def __init__(self, id: _Optional[str] = ..., timestamp: _Optional[str] = ..., organization_id: _Optional[str] = ..., project_id: _Optional[str] = ..., trace_id: _Optional[str] = ..., span_id: _Optional[str] = ..., request_id: _Optional[str] = ..., principal_urn: _Optional[str] = ..., principal_type: _Optional[str] = ..., user_id: _Optional[str] = ..., user_external_id: _Optional[str] = ..., user_email: _Optional[str] = ..., api_key_id: _Optional[str] = ..., session_id: _Optional[str] = ..., role_slugs: _Optional[_Iterable[str]] = ..., operation: _Optional[str] = ..., outcome: _Optional[str] = ..., reason: _Optional[str] = ..., scope: _Optional[str] = ..., resource_kind: _Optional[str] = ..., resource_id: _Optional[str] = ..., selector: _Optional[str] = ..., expanded_scopes: _Optional[_Iterable[str]] = ..., requested_checks: _Optional[_Iterable[_Union[Challenge.RequestedCheck, _Mapping]]] = ..., matched_grants: _Optional[_Iterable[_Union[Challenge.MatchedGrant, _Mapping]]] = ..., evaluated_grant_count: _Optional[int] = ..., filter_candidate_count: _Optional[int] = ..., filter_allowed_count: _Optional[int] = ...) -> None: ...
