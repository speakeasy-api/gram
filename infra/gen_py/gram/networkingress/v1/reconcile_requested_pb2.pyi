from gcp.pubsub.v1 import options_pb2 as _options_pb2
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from typing import ClassVar as _ClassVar, Optional as _Optional

DESCRIPTOR: _descriptor.FileDescriptor

class ReconcileRequested(_message.Message):
    __slots__ = ("ingress_id", "temporal_task_queue", "organization_id")
    INGRESS_ID_FIELD_NUMBER: _ClassVar[int]
    TEMPORAL_TASK_QUEUE_FIELD_NUMBER: _ClassVar[int]
    ORGANIZATION_ID_FIELD_NUMBER: _ClassVar[int]
    ingress_id: str
    temporal_task_queue: str
    organization_id: str
    def __init__(self, ingress_id: _Optional[str] = ..., temporal_task_queue: _Optional[str] = ..., organization_id: _Optional[str] = ...) -> None: ...
