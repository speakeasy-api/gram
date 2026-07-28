from gcp.pubsub.v1 import options_pb2 as _options_pb2
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from typing import ClassVar as _ClassVar, Optional as _Optional

DESCRIPTOR: _descriptor.FileDescriptor

class LogRecord(_message.Message):
    __slots__ = ("id", "time_unix_nano", "observed_time_unix_nano", "severity_text", "body", "trace_id", "span_id", "attributes_json", "resource_attributes_json", "gram_project_id", "gram_deployment_id", "gram_function_id", "gram_urn", "service_name", "service_version", "gram_chat_id")
    ID_FIELD_NUMBER: _ClassVar[int]
    TIME_UNIX_NANO_FIELD_NUMBER: _ClassVar[int]
    OBSERVED_TIME_UNIX_NANO_FIELD_NUMBER: _ClassVar[int]
    SEVERITY_TEXT_FIELD_NUMBER: _ClassVar[int]
    BODY_FIELD_NUMBER: _ClassVar[int]
    TRACE_ID_FIELD_NUMBER: _ClassVar[int]
    SPAN_ID_FIELD_NUMBER: _ClassVar[int]
    ATTRIBUTES_JSON_FIELD_NUMBER: _ClassVar[int]
    RESOURCE_ATTRIBUTES_JSON_FIELD_NUMBER: _ClassVar[int]
    GRAM_PROJECT_ID_FIELD_NUMBER: _ClassVar[int]
    GRAM_DEPLOYMENT_ID_FIELD_NUMBER: _ClassVar[int]
    GRAM_FUNCTION_ID_FIELD_NUMBER: _ClassVar[int]
    GRAM_URN_FIELD_NUMBER: _ClassVar[int]
    SERVICE_NAME_FIELD_NUMBER: _ClassVar[int]
    SERVICE_VERSION_FIELD_NUMBER: _ClassVar[int]
    GRAM_CHAT_ID_FIELD_NUMBER: _ClassVar[int]
    id: str
    time_unix_nano: int
    observed_time_unix_nano: int
    severity_text: str
    body: str
    trace_id: str
    span_id: str
    attributes_json: str
    resource_attributes_json: str
    gram_project_id: str
    gram_deployment_id: str
    gram_function_id: str
    gram_urn: str
    service_name: str
    service_version: str
    gram_chat_id: str
    def __init__(self, id: _Optional[str] = ..., time_unix_nano: _Optional[int] = ..., observed_time_unix_nano: _Optional[int] = ..., severity_text: _Optional[str] = ..., body: _Optional[str] = ..., trace_id: _Optional[str] = ..., span_id: _Optional[str] = ..., attributes_json: _Optional[str] = ..., resource_attributes_json: _Optional[str] = ..., gram_project_id: _Optional[str] = ..., gram_deployment_id: _Optional[str] = ..., gram_function_id: _Optional[str] = ..., gram_urn: _Optional[str] = ..., service_name: _Optional[str] = ..., service_version: _Optional[str] = ..., gram_chat_id: _Optional[str] = ...) -> None: ...
