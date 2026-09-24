from gcp.pubsub.v1 import options_pb2 as _options_pb2
from google.protobuf.internal import containers as _containers
from google.protobuf.internal import enum_type_wrapper as _enum_type_wrapper
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from collections.abc import Iterable as _Iterable, Mapping as _Mapping
from typing import ClassVar as _ClassVar, Optional as _Optional, Union as _Union

DESCRIPTOR: _descriptor.FileDescriptor

class Message(_message.Message):
    __slots__ = ("id", "organization_id", "project_id", "conversation_id", "role", "created_at", "produced_at", "provenance", "tool_call_id", "finish_reason", "body", "body_reference", "correlation_id", "conversation_context")
    class Role(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
        __slots__ = ()
        ROLE_UNSPECIFIED: _ClassVar[Message.Role]
        ROLE_SYSTEM: _ClassVar[Message.Role]
        ROLE_DEVELOPER: _ClassVar[Message.Role]
        ROLE_USER: _ClassVar[Message.Role]
        ROLE_ASSISTANT: _ClassVar[Message.Role]
        ROLE_TOOL: _ClassVar[Message.Role]
    ROLE_UNSPECIFIED: Message.Role
    ROLE_SYSTEM: Message.Role
    ROLE_DEVELOPER: Message.Role
    ROLE_USER: Message.Role
    ROLE_ASSISTANT: Message.Role
    ROLE_TOOL: Message.Role
    class Provenance(_message.Message):
        __slots__ = ("source", "external_message_id", "user_id", "external_user_id", "assistant_id", "provider", "model", "replayed", "adapter", "hook_source", "hostname", "user_agent", "user_email", "account")
        SOURCE_FIELD_NUMBER: _ClassVar[int]
        EXTERNAL_MESSAGE_ID_FIELD_NUMBER: _ClassVar[int]
        USER_ID_FIELD_NUMBER: _ClassVar[int]
        EXTERNAL_USER_ID_FIELD_NUMBER: _ClassVar[int]
        ASSISTANT_ID_FIELD_NUMBER: _ClassVar[int]
        PROVIDER_FIELD_NUMBER: _ClassVar[int]
        MODEL_FIELD_NUMBER: _ClassVar[int]
        REPLAYED_FIELD_NUMBER: _ClassVar[int]
        ADAPTER_FIELD_NUMBER: _ClassVar[int]
        HOOK_SOURCE_FIELD_NUMBER: _ClassVar[int]
        HOSTNAME_FIELD_NUMBER: _ClassVar[int]
        USER_AGENT_FIELD_NUMBER: _ClassVar[int]
        USER_EMAIL_FIELD_NUMBER: _ClassVar[int]
        ACCOUNT_FIELD_NUMBER: _ClassVar[int]
        source: str
        external_message_id: str
        user_id: str
        external_user_id: str
        assistant_id: str
        provider: str
        model: str
        replayed: bool
        adapter: str
        hook_source: str
        hostname: str
        user_agent: str
        user_email: str
        account: Message.Account
        def __init__(self, source: _Optional[str] = ..., external_message_id: _Optional[str] = ..., user_id: _Optional[str] = ..., external_user_id: _Optional[str] = ..., assistant_id: _Optional[str] = ..., provider: _Optional[str] = ..., model: _Optional[str] = ..., replayed: _Optional[bool] = ..., adapter: _Optional[str] = ..., hook_source: _Optional[str] = ..., hostname: _Optional[str] = ..., user_agent: _Optional[str] = ..., user_email: _Optional[str] = ..., account: _Optional[_Union[Message.Account, _Mapping]] = ...) -> None: ...
    class Account(_message.Message):
        __slots__ = ("user_account_id", "account_type", "billing_mode")
        USER_ACCOUNT_ID_FIELD_NUMBER: _ClassVar[int]
        ACCOUNT_TYPE_FIELD_NUMBER: _ClassVar[int]
        BILLING_MODE_FIELD_NUMBER: _ClassVar[int]
        user_account_id: str
        account_type: str
        billing_mode: str
        def __init__(self, user_account_id: _Optional[str] = ..., account_type: _Optional[str] = ..., billing_mode: _Optional[str] = ...) -> None: ...
    class ConversationContext(_message.Message):
        __slots__ = ("external_conversation_id", "working_directory")
        EXTERNAL_CONVERSATION_ID_FIELD_NUMBER: _ClassVar[int]
        WORKING_DIRECTORY_FIELD_NUMBER: _ClassVar[int]
        external_conversation_id: str
        working_directory: str
        def __init__(self, external_conversation_id: _Optional[str] = ..., working_directory: _Optional[str] = ...) -> None: ...
    class Body(_message.Message):
        __slots__ = ("parts", "source_content", "source_content_json")
        PARTS_FIELD_NUMBER: _ClassVar[int]
        SOURCE_CONTENT_FIELD_NUMBER: _ClassVar[int]
        SOURCE_CONTENT_JSON_FIELD_NUMBER: _ClassVar[int]
        parts: _containers.RepeatedCompositeFieldContainer[Message.Part]
        source_content: Message.ContentReference
        source_content_json: bytes
        def __init__(self, parts: _Optional[_Iterable[_Union[Message.Part, _Mapping]]] = ..., source_content: _Optional[_Union[Message.ContentReference, _Mapping]] = ..., source_content_json: _Optional[bytes] = ...) -> None: ...
    class Part(_message.Message):
        __slots__ = ("text", "tool_call", "content_reference")
        TEXT_FIELD_NUMBER: _ClassVar[int]
        TOOL_CALL_FIELD_NUMBER: _ClassVar[int]
        CONTENT_REFERENCE_FIELD_NUMBER: _ClassVar[int]
        text: str
        tool_call: Message.ToolCall
        content_reference: Message.ContentReference
        def __init__(self, text: _Optional[str] = ..., tool_call: _Optional[_Union[Message.ToolCall, _Mapping]] = ..., content_reference: _Optional[_Union[Message.ContentReference, _Mapping]] = ...) -> None: ...
    class ToolCall(_message.Message):
        __slots__ = ("id", "name", "arguments_json")
        ID_FIELD_NUMBER: _ClassVar[int]
        NAME_FIELD_NUMBER: _ClassVar[int]
        ARGUMENTS_JSON_FIELD_NUMBER: _ClassVar[int]
        id: str
        name: str
        arguments_json: str
        def __init__(self, id: _Optional[str] = ..., name: _Optional[str] = ..., arguments_json: _Optional[str] = ...) -> None: ...
    class ContentReference(_message.Message):
        __slots__ = ("uri", "media_type", "size_bytes", "sha256")
        URI_FIELD_NUMBER: _ClassVar[int]
        MEDIA_TYPE_FIELD_NUMBER: _ClassVar[int]
        SIZE_BYTES_FIELD_NUMBER: _ClassVar[int]
        SHA256_FIELD_NUMBER: _ClassVar[int]
        uri: str
        media_type: str
        size_bytes: int
        sha256: bytes
        def __init__(self, uri: _Optional[str] = ..., media_type: _Optional[str] = ..., size_bytes: _Optional[int] = ..., sha256: _Optional[bytes] = ...) -> None: ...
    ID_FIELD_NUMBER: _ClassVar[int]
    ORGANIZATION_ID_FIELD_NUMBER: _ClassVar[int]
    PROJECT_ID_FIELD_NUMBER: _ClassVar[int]
    CONVERSATION_ID_FIELD_NUMBER: _ClassVar[int]
    ROLE_FIELD_NUMBER: _ClassVar[int]
    CREATED_AT_FIELD_NUMBER: _ClassVar[int]
    PRODUCED_AT_FIELD_NUMBER: _ClassVar[int]
    PROVENANCE_FIELD_NUMBER: _ClassVar[int]
    TOOL_CALL_ID_FIELD_NUMBER: _ClassVar[int]
    FINISH_REASON_FIELD_NUMBER: _ClassVar[int]
    BODY_FIELD_NUMBER: _ClassVar[int]
    BODY_REFERENCE_FIELD_NUMBER: _ClassVar[int]
    CORRELATION_ID_FIELD_NUMBER: _ClassVar[int]
    CONVERSATION_CONTEXT_FIELD_NUMBER: _ClassVar[int]
    id: str
    organization_id: str
    project_id: str
    conversation_id: str
    role: Message.Role
    created_at: str
    produced_at: str
    provenance: Message.Provenance
    tool_call_id: str
    finish_reason: str
    body: Message.Body
    body_reference: Message.ContentReference
    correlation_id: str
    conversation_context: Message.ConversationContext
    def __init__(self, id: _Optional[str] = ..., organization_id: _Optional[str] = ..., project_id: _Optional[str] = ..., conversation_id: _Optional[str] = ..., role: _Optional[_Union[Message.Role, str]] = ..., created_at: _Optional[str] = ..., produced_at: _Optional[str] = ..., provenance: _Optional[_Union[Message.Provenance, _Mapping]] = ..., tool_call_id: _Optional[str] = ..., finish_reason: _Optional[str] = ..., body: _Optional[_Union[Message.Body, _Mapping]] = ..., body_reference: _Optional[_Union[Message.ContentReference, _Mapping]] = ..., correlation_id: _Optional[str] = ..., conversation_context: _Optional[_Union[Message.ConversationContext, _Mapping]] = ...) -> None: ...
