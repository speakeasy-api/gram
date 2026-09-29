from gcp.pubsub.v1 import options_pb2 as _options_pb2
from google.protobuf.internal import containers as _containers
from google.protobuf.internal import enum_type_wrapper as _enum_type_wrapper
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from collections.abc import Iterable as _Iterable, Mapping as _Mapping
from typing import ClassVar as _ClassVar, Optional as _Optional, Union as _Union

DESCRIPTOR: _descriptor.FileDescriptor

class Reading(_message.Message):
    __slots__ = ("id", "evaluation_attempt_id", "organization_id", "project_id", "conversation_id", "message_id", "message_role", "sensor_id", "message_created_at", "evaluated_at", "definition_hash", "configured_model", "models", "compiler_version", "multi_label", "choice", "score")
    class MessageRole(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
        __slots__ = ()
        MESSAGE_ROLE_UNSPECIFIED: _ClassVar[Reading.MessageRole]
        MESSAGE_ROLE_USER: _ClassVar[Reading.MessageRole]
        MESSAGE_ROLE_ASSISTANT: _ClassVar[Reading.MessageRole]
    MESSAGE_ROLE_UNSPECIFIED: Reading.MessageRole
    MESSAGE_ROLE_USER: Reading.MessageRole
    MESSAGE_ROLE_ASSISTANT: Reading.MessageRole
    class Probability(_message.Message):
        __slots__ = ("signal_id", "probability")
        SIGNAL_ID_FIELD_NUMBER: _ClassVar[int]
        PROBABILITY_FIELD_NUMBER: _ClassVar[int]
        signal_id: str
        probability: float
        def __init__(self, signal_id: _Optional[str] = ..., probability: _Optional[float] = ...) -> None: ...
    class MultiLabel(_message.Message):
        __slots__ = ("signals",)
        SIGNALS_FIELD_NUMBER: _ClassVar[int]
        signals: _containers.RepeatedCompositeFieldContainer[Reading.Probability]
        def __init__(self, signals: _Optional[_Iterable[_Union[Reading.Probability, _Mapping]]] = ...) -> None: ...
    class Choice(_message.Message):
        __slots__ = ("selected_signal_id", "distribution", "confidence")
        SELECTED_SIGNAL_ID_FIELD_NUMBER: _ClassVar[int]
        DISTRIBUTION_FIELD_NUMBER: _ClassVar[int]
        CONFIDENCE_FIELD_NUMBER: _ClassVar[int]
        selected_signal_id: str
        distribution: _containers.RepeatedCompositeFieldContainer[Reading.Probability]
        confidence: float
        def __init__(self, selected_signal_id: _Optional[str] = ..., distribution: _Optional[_Iterable[_Union[Reading.Probability, _Mapping]]] = ..., confidence: _Optional[float] = ...) -> None: ...
    class Score(_message.Message):
        __slots__ = ("expected_index", "distribution", "confidence")
        EXPECTED_INDEX_FIELD_NUMBER: _ClassVar[int]
        DISTRIBUTION_FIELD_NUMBER: _ClassVar[int]
        CONFIDENCE_FIELD_NUMBER: _ClassVar[int]
        expected_index: float
        distribution: _containers.RepeatedCompositeFieldContainer[Reading.Probability]
        confidence: float
        def __init__(self, expected_index: _Optional[float] = ..., distribution: _Optional[_Iterable[_Union[Reading.Probability, _Mapping]]] = ..., confidence: _Optional[float] = ...) -> None: ...
    ID_FIELD_NUMBER: _ClassVar[int]
    EVALUATION_ATTEMPT_ID_FIELD_NUMBER: _ClassVar[int]
    ORGANIZATION_ID_FIELD_NUMBER: _ClassVar[int]
    PROJECT_ID_FIELD_NUMBER: _ClassVar[int]
    CONVERSATION_ID_FIELD_NUMBER: _ClassVar[int]
    MESSAGE_ID_FIELD_NUMBER: _ClassVar[int]
    MESSAGE_ROLE_FIELD_NUMBER: _ClassVar[int]
    SENSOR_ID_FIELD_NUMBER: _ClassVar[int]
    MESSAGE_CREATED_AT_FIELD_NUMBER: _ClassVar[int]
    EVALUATED_AT_FIELD_NUMBER: _ClassVar[int]
    DEFINITION_HASH_FIELD_NUMBER: _ClassVar[int]
    CONFIGURED_MODEL_FIELD_NUMBER: _ClassVar[int]
    MODELS_FIELD_NUMBER: _ClassVar[int]
    COMPILER_VERSION_FIELD_NUMBER: _ClassVar[int]
    MULTI_LABEL_FIELD_NUMBER: _ClassVar[int]
    CHOICE_FIELD_NUMBER: _ClassVar[int]
    SCORE_FIELD_NUMBER: _ClassVar[int]
    id: str
    evaluation_attempt_id: str
    organization_id: str
    project_id: str
    conversation_id: str
    message_id: str
    message_role: Reading.MessageRole
    sensor_id: str
    message_created_at: str
    evaluated_at: str
    definition_hash: str
    configured_model: str
    models: _containers.RepeatedScalarFieldContainer[str]
    compiler_version: str
    multi_label: Reading.MultiLabel
    choice: Reading.Choice
    score: Reading.Score
    def __init__(self, id: _Optional[str] = ..., evaluation_attempt_id: _Optional[str] = ..., organization_id: _Optional[str] = ..., project_id: _Optional[str] = ..., conversation_id: _Optional[str] = ..., message_id: _Optional[str] = ..., message_role: _Optional[_Union[Reading.MessageRole, str]] = ..., sensor_id: _Optional[str] = ..., message_created_at: _Optional[str] = ..., evaluated_at: _Optional[str] = ..., definition_hash: _Optional[str] = ..., configured_model: _Optional[str] = ..., models: _Optional[_Iterable[str]] = ..., compiler_version: _Optional[str] = ..., multi_label: _Optional[_Union[Reading.MultiLabel, _Mapping]] = ..., choice: _Optional[_Union[Reading.Choice, _Mapping]] = ..., score: _Optional[_Union[Reading.Score, _Mapping]] = ...) -> None: ...
