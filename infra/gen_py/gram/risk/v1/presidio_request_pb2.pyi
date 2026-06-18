import datetime

from gcp.pubsub.v1 import options_pb2 as _options_pb2
from google.protobuf import timestamp_pb2 as _timestamp_pb2
from google.protobuf.internal import containers as _containers
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from collections.abc import Iterable as _Iterable, Mapping as _Mapping
from typing import ClassVar as _ClassVar, Optional as _Optional, Union as _Union

DESCRIPTOR: _descriptor.FileDescriptor

class PresidioRequest(_message.Message):
    __slots__ = ("request_id", "created_at", "reply_urn", "contents", "entities")
    REQUEST_ID_FIELD_NUMBER: _ClassVar[int]
    CREATED_AT_FIELD_NUMBER: _ClassVar[int]
    REPLY_URN_FIELD_NUMBER: _ClassVar[int]
    CONTENTS_FIELD_NUMBER: _ClassVar[int]
    ENTITIES_FIELD_NUMBER: _ClassVar[int]
    request_id: str
    created_at: _timestamp_pb2.Timestamp
    reply_urn: str
    contents: _containers.RepeatedScalarFieldContainer[str]
    entities: _containers.RepeatedScalarFieldContainer[str]
    def __init__(self, request_id: _Optional[str] = ..., created_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ..., reply_urn: _Optional[str] = ..., contents: _Optional[_Iterable[str]] = ..., entities: _Optional[_Iterable[str]] = ...) -> None: ...
