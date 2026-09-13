from gcp.pubsub.v1 import options_pb2 as _options_pb2
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from typing import ClassVar as _ClassVar, Optional as _Optional

DESCRIPTOR: _descriptor.FileDescriptor

class GitleaksAnalysis(_message.Message):
    __slots__ = ("request_id", "chat_message_id", "project_id", "organization_id", "risk_policy_id", "risk_policy_version", "created_at", "reply_urn", "content", "content_part_id", "chat_id", "parent_chat_message_id", "origin_risk_policy_id", "origin_risk_policy_version", "message_link_reason", "execution_path", "tool_call_id", "tool_name", "hook_source", "user_id", "message_type", "policy_link_reason", "external_conversation_id", "finding_surface")
    REQUEST_ID_FIELD_NUMBER: _ClassVar[int]
    CHAT_MESSAGE_ID_FIELD_NUMBER: _ClassVar[int]
    PROJECT_ID_FIELD_NUMBER: _ClassVar[int]
    ORGANIZATION_ID_FIELD_NUMBER: _ClassVar[int]
    RISK_POLICY_ID_FIELD_NUMBER: _ClassVar[int]
    RISK_POLICY_VERSION_FIELD_NUMBER: _ClassVar[int]
    CREATED_AT_FIELD_NUMBER: _ClassVar[int]
    REPLY_URN_FIELD_NUMBER: _ClassVar[int]
    CONTENT_FIELD_NUMBER: _ClassVar[int]
    CONTENT_PART_ID_FIELD_NUMBER: _ClassVar[int]
    CHAT_ID_FIELD_NUMBER: _ClassVar[int]
    PARENT_CHAT_MESSAGE_ID_FIELD_NUMBER: _ClassVar[int]
    ORIGIN_RISK_POLICY_ID_FIELD_NUMBER: _ClassVar[int]
    ORIGIN_RISK_POLICY_VERSION_FIELD_NUMBER: _ClassVar[int]
    MESSAGE_LINK_REASON_FIELD_NUMBER: _ClassVar[int]
    EXECUTION_PATH_FIELD_NUMBER: _ClassVar[int]
    TOOL_CALL_ID_FIELD_NUMBER: _ClassVar[int]
    TOOL_NAME_FIELD_NUMBER: _ClassVar[int]
    HOOK_SOURCE_FIELD_NUMBER: _ClassVar[int]
    USER_ID_FIELD_NUMBER: _ClassVar[int]
    MESSAGE_TYPE_FIELD_NUMBER: _ClassVar[int]
    POLICY_LINK_REASON_FIELD_NUMBER: _ClassVar[int]
    EXTERNAL_CONVERSATION_ID_FIELD_NUMBER: _ClassVar[int]
    FINDING_SURFACE_FIELD_NUMBER: _ClassVar[int]
    request_id: str
    chat_message_id: str
    project_id: str
    organization_id: str
    risk_policy_id: str
    risk_policy_version: int
    created_at: str
    reply_urn: str
    content: str
    content_part_id: str
    chat_id: str
    parent_chat_message_id: str
    origin_risk_policy_id: str
    origin_risk_policy_version: int
    message_link_reason: str
    execution_path: str
    tool_call_id: str
    tool_name: str
    hook_source: str
    user_id: str
    message_type: str
    policy_link_reason: str
    external_conversation_id: str
    finding_surface: str
    def __init__(self, request_id: _Optional[str] = ..., chat_message_id: _Optional[str] = ..., project_id: _Optional[str] = ..., organization_id: _Optional[str] = ..., risk_policy_id: _Optional[str] = ..., risk_policy_version: _Optional[int] = ..., created_at: _Optional[str] = ..., reply_urn: _Optional[str] = ..., content: _Optional[str] = ..., content_part_id: _Optional[str] = ..., chat_id: _Optional[str] = ..., parent_chat_message_id: _Optional[str] = ..., origin_risk_policy_id: _Optional[str] = ..., origin_risk_policy_version: _Optional[int] = ..., message_link_reason: _Optional[str] = ..., execution_path: _Optional[str] = ..., tool_call_id: _Optional[str] = ..., tool_name: _Optional[str] = ..., hook_source: _Optional[str] = ..., user_id: _Optional[str] = ..., message_type: _Optional[str] = ..., policy_link_reason: _Optional[str] = ..., external_conversation_id: _Optional[str] = ..., finding_surface: _Optional[str] = ...) -> None: ...
