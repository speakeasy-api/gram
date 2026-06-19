import datetime

from google.protobuf import descriptor_pb2 as _descriptor_pb2
from google.protobuf import duration_pb2 as _duration_pb2
from google.protobuf.internal import containers as _containers
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from collections.abc import Mapping as _Mapping
from typing import ClassVar as _ClassVar, Optional as _Optional, Union as _Union

DESCRIPTOR: _descriptor.FileDescriptor
TOPIC_FIELD_NUMBER: _ClassVar[int]
topic: _descriptor.FieldDescriptor
SUBSCRIPTION_FIELD_NUMBER: _ClassVar[int]
subscription: _descriptor.FieldDescriptor

class TopicOptions(_message.Message):
    __slots__ = ("name", "retention_hint", "labels")
    class LabelsEntry(_message.Message):
        __slots__ = ("key", "value")
        KEY_FIELD_NUMBER: _ClassVar[int]
        VALUE_FIELD_NUMBER: _ClassVar[int]
        key: str
        value: str
        def __init__(self, key: _Optional[str] = ..., value: _Optional[str] = ...) -> None: ...
    NAME_FIELD_NUMBER: _ClassVar[int]
    RETENTION_HINT_FIELD_NUMBER: _ClassVar[int]
    LABELS_FIELD_NUMBER: _ClassVar[int]
    name: str
    retention_hint: _duration_pb2.Duration
    labels: _containers.ScalarMap[str, str]
    def __init__(self, name: _Optional[str] = ..., retention_hint: _Optional[_Union[datetime.timedelta, _duration_pb2.Duration, _Mapping]] = ..., labels: _Optional[_Mapping[str, str]] = ...) -> None: ...

class SubscriptionOptions(_message.Message):
    __slots__ = ("name", "retention", "retain_acked_messages", "ack_deadline", "expiration_ttl", "retry_policy", "labels", "filter", "dead_letter", "topic")
    class LabelsEntry(_message.Message):
        __slots__ = ("key", "value")
        KEY_FIELD_NUMBER: _ClassVar[int]
        VALUE_FIELD_NUMBER: _ClassVar[int]
        key: str
        value: str
        def __init__(self, key: _Optional[str] = ..., value: _Optional[str] = ...) -> None: ...
    NAME_FIELD_NUMBER: _ClassVar[int]
    RETENTION_FIELD_NUMBER: _ClassVar[int]
    RETAIN_ACKED_MESSAGES_FIELD_NUMBER: _ClassVar[int]
    ACK_DEADLINE_FIELD_NUMBER: _ClassVar[int]
    EXPIRATION_TTL_FIELD_NUMBER: _ClassVar[int]
    RETRY_POLICY_FIELD_NUMBER: _ClassVar[int]
    LABELS_FIELD_NUMBER: _ClassVar[int]
    FILTER_FIELD_NUMBER: _ClassVar[int]
    DEAD_LETTER_FIELD_NUMBER: _ClassVar[int]
    TOPIC_FIELD_NUMBER: _ClassVar[int]
    name: str
    retention: _duration_pb2.Duration
    retain_acked_messages: bool
    ack_deadline: _duration_pb2.Duration
    expiration_ttl: _duration_pb2.Duration
    retry_policy: RetryPolicy
    labels: _containers.ScalarMap[str, str]
    filter: str
    dead_letter: DeadLetterPolicy
    topic: str
    def __init__(self, name: _Optional[str] = ..., retention: _Optional[_Union[datetime.timedelta, _duration_pb2.Duration, _Mapping]] = ..., retain_acked_messages: _Optional[bool] = ..., ack_deadline: _Optional[_Union[datetime.timedelta, _duration_pb2.Duration, _Mapping]] = ..., expiration_ttl: _Optional[_Union[datetime.timedelta, _duration_pb2.Duration, _Mapping]] = ..., retry_policy: _Optional[_Union[RetryPolicy, _Mapping]] = ..., labels: _Optional[_Mapping[str, str]] = ..., filter: _Optional[str] = ..., dead_letter: _Optional[_Union[DeadLetterPolicy, _Mapping]] = ..., topic: _Optional[str] = ...) -> None: ...

class RetryPolicy(_message.Message):
    __slots__ = ("minimum_backoff", "maximum_backoff")
    MINIMUM_BACKOFF_FIELD_NUMBER: _ClassVar[int]
    MAXIMUM_BACKOFF_FIELD_NUMBER: _ClassVar[int]
    minimum_backoff: _duration_pb2.Duration
    maximum_backoff: _duration_pb2.Duration
    def __init__(self, minimum_backoff: _Optional[_Union[datetime.timedelta, _duration_pb2.Duration, _Mapping]] = ..., maximum_backoff: _Optional[_Union[datetime.timedelta, _duration_pb2.Duration, _Mapping]] = ...) -> None: ...

class DeadLetterPolicy(_message.Message):
    __slots__ = ("name", "max_delivery_attempts")
    NAME_FIELD_NUMBER: _ClassVar[int]
    MAX_DELIVERY_ATTEMPTS_FIELD_NUMBER: _ClassVar[int]
    name: str
    max_delivery_attempts: int
    def __init__(self, name: _Optional[str] = ..., max_delivery_attempts: _Optional[int] = ...) -> None: ...
