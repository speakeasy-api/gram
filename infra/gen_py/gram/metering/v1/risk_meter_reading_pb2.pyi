from gcp.pubsub.v1 import options_pb2 as _options_pb2
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from typing import ClassVar as _ClassVar, Optional as _Optional

DESCRIPTOR: _descriptor.FileDescriptor

class RiskMeterReading(_message.Message):
    __slots__ = ("reading",)
    READING_FIELD_NUMBER: _ClassVar[int]
    reading: bytes
    def __init__(self, reading: _Optional[bytes] = ...) -> None: ...
