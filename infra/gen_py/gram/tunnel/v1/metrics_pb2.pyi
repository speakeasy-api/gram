from gcp.pubsub.v1 import options_pb2 as _options_pb2
from google.protobuf.internal import containers as _containers
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from collections.abc import Iterable as _Iterable
from typing import ClassVar as _ClassVar, Optional as _Optional

DESCRIPTOR: _descriptor.FileDescriptor

class MetricsSnapshot(_message.Message):
    __slots__ = ("source_id", "producer_id", "bucket_unix", "revision", "kind", "server_id", "method", "client_family", "attempts", "successes", "errors", "canceled", "incomplete", "latency_bins", "connections", "consumers", "substreams", "connections_opened")
    SOURCE_ID_FIELD_NUMBER: _ClassVar[int]
    PRODUCER_ID_FIELD_NUMBER: _ClassVar[int]
    BUCKET_UNIX_FIELD_NUMBER: _ClassVar[int]
    REVISION_FIELD_NUMBER: _ClassVar[int]
    KIND_FIELD_NUMBER: _ClassVar[int]
    SERVER_ID_FIELD_NUMBER: _ClassVar[int]
    METHOD_FIELD_NUMBER: _ClassVar[int]
    CLIENT_FAMILY_FIELD_NUMBER: _ClassVar[int]
    ATTEMPTS_FIELD_NUMBER: _ClassVar[int]
    SUCCESSES_FIELD_NUMBER: _ClassVar[int]
    ERRORS_FIELD_NUMBER: _ClassVar[int]
    CANCELED_FIELD_NUMBER: _ClassVar[int]
    INCOMPLETE_FIELD_NUMBER: _ClassVar[int]
    LATENCY_BINS_FIELD_NUMBER: _ClassVar[int]
    CONNECTIONS_FIELD_NUMBER: _ClassVar[int]
    CONSUMERS_FIELD_NUMBER: _ClassVar[int]
    SUBSTREAMS_FIELD_NUMBER: _ClassVar[int]
    CONNECTIONS_OPENED_FIELD_NUMBER: _ClassVar[int]
    source_id: str
    producer_id: str
    bucket_unix: int
    revision: int
    kind: str
    server_id: str
    method: str
    client_family: str
    attempts: int
    successes: int
    errors: int
    canceled: int
    incomplete: int
    latency_bins: _containers.RepeatedScalarFieldContainer[int]
    connections: int
    consumers: int
    substreams: int
    connections_opened: int
    def __init__(self, source_id: _Optional[str] = ..., producer_id: _Optional[str] = ..., bucket_unix: _Optional[int] = ..., revision: _Optional[int] = ..., kind: _Optional[str] = ..., server_id: _Optional[str] = ..., method: _Optional[str] = ..., client_family: _Optional[str] = ..., attempts: _Optional[int] = ..., successes: _Optional[int] = ..., errors: _Optional[int] = ..., canceled: _Optional[int] = ..., incomplete: _Optional[int] = ..., latency_bins: _Optional[_Iterable[int]] = ..., connections: _Optional[int] = ..., consumers: _Optional[int] = ..., substreams: _Optional[int] = ..., connections_opened: _Optional[int] = ...) -> None: ...
