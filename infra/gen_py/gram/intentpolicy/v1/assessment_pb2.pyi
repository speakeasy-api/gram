from gcp.pubsub.v1 import options_pb2 as _options_pb2
from google.protobuf.internal import containers as _containers
from google.protobuf.internal import enum_type_wrapper as _enum_type_wrapper
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from collections.abc import Iterable as _Iterable, Mapping as _Mapping
from typing import ClassVar as _ClassVar, Optional as _Optional, Union as _Union

DESCRIPTOR: _descriptor.FileDescriptor

class Assessment(_message.Message):
    __slots__ = ("id", "analysis_id", "organization_id", "project_id", "actor", "tool_call", "guardrail", "evaluator", "decision", "decided_by", "contracts", "labels", "evaluator_error", "usage", "trigger_kind", "analysis_requested_at", "evaluated_at")
    class Actor(_message.Message):
        __slots__ = ("user_id", "user_email")
        USER_ID_FIELD_NUMBER: _ClassVar[int]
        USER_EMAIL_FIELD_NUMBER: _ClassVar[int]
        user_id: str
        user_email: str
        def __init__(self, user_id: _Optional[str] = ..., user_email: _Optional[str] = ...) -> None: ...
    class ToolCall(_message.Message):
        __slots__ = ("chat_id", "generation", "message_id", "sequence", "tool_call_id", "tool_name", "mcp_server", "mcp_function", "arguments_bytes", "occurred_at")
        CHAT_ID_FIELD_NUMBER: _ClassVar[int]
        GENERATION_FIELD_NUMBER: _ClassVar[int]
        MESSAGE_ID_FIELD_NUMBER: _ClassVar[int]
        SEQUENCE_FIELD_NUMBER: _ClassVar[int]
        TOOL_CALL_ID_FIELD_NUMBER: _ClassVar[int]
        TOOL_NAME_FIELD_NUMBER: _ClassVar[int]
        MCP_SERVER_FIELD_NUMBER: _ClassVar[int]
        MCP_FUNCTION_FIELD_NUMBER: _ClassVar[int]
        ARGUMENTS_BYTES_FIELD_NUMBER: _ClassVar[int]
        OCCURRED_AT_FIELD_NUMBER: _ClassVar[int]
        chat_id: str
        generation: int
        message_id: str
        sequence: int
        tool_call_id: str
        tool_name: str
        mcp_server: str
        mcp_function: str
        arguments_bytes: int
        occurred_at: str
        def __init__(self, chat_id: _Optional[str] = ..., generation: _Optional[int] = ..., message_id: _Optional[str] = ..., sequence: _Optional[int] = ..., tool_call_id: _Optional[str] = ..., tool_name: _Optional[str] = ..., mcp_server: _Optional[str] = ..., mcp_function: _Optional[str] = ..., arguments_bytes: _Optional[int] = ..., occurred_at: _Optional[str] = ...) -> None: ...
    class Evaluator(_message.Message):
        __slots__ = ("kind", "provider", "model", "prompt_version")
        KIND_FIELD_NUMBER: _ClassVar[int]
        PROVIDER_FIELD_NUMBER: _ClassVar[int]
        MODEL_FIELD_NUMBER: _ClassVar[int]
        PROMPT_VERSION_FIELD_NUMBER: _ClassVar[int]
        kind: str
        provider: str
        model: str
        prompt_version: str
        def __init__(self, kind: _Optional[str] = ..., provider: _Optional[str] = ..., model: _Optional[str] = ..., prompt_version: _Optional[str] = ...) -> None: ...
    class Guardrail(_message.Message):
        __slots__ = ("id", "version")
        ID_FIELD_NUMBER: _ClassVar[int]
        VERSION_FIELD_NUMBER: _ClassVar[int]
        id: str
        version: str
        def __init__(self, id: _Optional[str] = ..., version: _Optional[str] = ...) -> None: ...
    class Usage(_message.Message):
        __slots__ = ("input_tokens", "output_tokens", "cost_usd", "latency_ms")
        INPUT_TOKENS_FIELD_NUMBER: _ClassVar[int]
        OUTPUT_TOKENS_FIELD_NUMBER: _ClassVar[int]
        COST_USD_FIELD_NUMBER: _ClassVar[int]
        LATENCY_MS_FIELD_NUMBER: _ClassVar[int]
        input_tokens: int
        output_tokens: int
        cost_usd: float
        latency_ms: int
        def __init__(self, input_tokens: _Optional[int] = ..., output_tokens: _Optional[int] = ..., cost_usd: _Optional[float] = ..., latency_ms: _Optional[int] = ...) -> None: ...
    class Label(_message.Message):
        __slots__ = ("key", "kind", "bool_value", "choice_value", "members", "rank", "probability", "distribution", "rationale")
        class Kind(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
            __slots__ = ()
            KIND_UNSPECIFIED: _ClassVar[Assessment.Label.Kind]
            KIND_NOUL: _ClassVar[Assessment.Label.Kind]
            KIND_CHOICE: _ClassVar[Assessment.Label.Kind]
            KIND_MULTI_NOUL: _ClassVar[Assessment.Label.Kind]
        KIND_UNSPECIFIED: Assessment.Label.Kind
        KIND_NOUL: Assessment.Label.Kind
        KIND_CHOICE: Assessment.Label.Kind
        KIND_MULTI_NOUL: Assessment.Label.Kind
        class DistributionEntry(_message.Message):
            __slots__ = ("key", "value")
            KEY_FIELD_NUMBER: _ClassVar[int]
            VALUE_FIELD_NUMBER: _ClassVar[int]
            key: str
            value: float
            def __init__(self, key: _Optional[str] = ..., value: _Optional[float] = ...) -> None: ...
        KEY_FIELD_NUMBER: _ClassVar[int]
        KIND_FIELD_NUMBER: _ClassVar[int]
        BOOL_VALUE_FIELD_NUMBER: _ClassVar[int]
        CHOICE_VALUE_FIELD_NUMBER: _ClassVar[int]
        MEMBERS_FIELD_NUMBER: _ClassVar[int]
        RANK_FIELD_NUMBER: _ClassVar[int]
        PROBABILITY_FIELD_NUMBER: _ClassVar[int]
        DISTRIBUTION_FIELD_NUMBER: _ClassVar[int]
        RATIONALE_FIELD_NUMBER: _ClassVar[int]
        key: str
        kind: Assessment.Label.Kind
        bool_value: bool
        choice_value: str
        members: _containers.RepeatedScalarFieldContainer[str]
        rank: int
        probability: float
        distribution: _containers.ScalarMap[str, float]
        rationale: str
        def __init__(self, key: _Optional[str] = ..., kind: _Optional[_Union[Assessment.Label.Kind, str]] = ..., bool_value: _Optional[bool] = ..., choice_value: _Optional[str] = ..., members: _Optional[_Iterable[str]] = ..., rank: _Optional[int] = ..., probability: _Optional[float] = ..., distribution: _Optional[_Mapping[str, float]] = ..., rationale: _Optional[str] = ...) -> None: ...
    class ContractOutcome(_message.Message):
        __slots__ = ("contract_id", "result", "error", "inputs")
        class InputsEntry(_message.Message):
            __slots__ = ("key", "value")
            KEY_FIELD_NUMBER: _ClassVar[int]
            VALUE_FIELD_NUMBER: _ClassVar[int]
            key: str
            value: str
            def __init__(self, key: _Optional[str] = ..., value: _Optional[str] = ...) -> None: ...
        CONTRACT_ID_FIELD_NUMBER: _ClassVar[int]
        RESULT_FIELD_NUMBER: _ClassVar[int]
        ERROR_FIELD_NUMBER: _ClassVar[int]
        INPUTS_FIELD_NUMBER: _ClassVar[int]
        contract_id: str
        result: str
        error: str
        inputs: _containers.ScalarMap[str, str]
        def __init__(self, contract_id: _Optional[str] = ..., result: _Optional[str] = ..., error: _Optional[str] = ..., inputs: _Optional[_Mapping[str, str]] = ...) -> None: ...
    ID_FIELD_NUMBER: _ClassVar[int]
    ANALYSIS_ID_FIELD_NUMBER: _ClassVar[int]
    ORGANIZATION_ID_FIELD_NUMBER: _ClassVar[int]
    PROJECT_ID_FIELD_NUMBER: _ClassVar[int]
    ACTOR_FIELD_NUMBER: _ClassVar[int]
    TOOL_CALL_FIELD_NUMBER: _ClassVar[int]
    GUARDRAIL_FIELD_NUMBER: _ClassVar[int]
    EVALUATOR_FIELD_NUMBER: _ClassVar[int]
    DECISION_FIELD_NUMBER: _ClassVar[int]
    DECIDED_BY_FIELD_NUMBER: _ClassVar[int]
    CONTRACTS_FIELD_NUMBER: _ClassVar[int]
    LABELS_FIELD_NUMBER: _ClassVar[int]
    EVALUATOR_ERROR_FIELD_NUMBER: _ClassVar[int]
    USAGE_FIELD_NUMBER: _ClassVar[int]
    TRIGGER_KIND_FIELD_NUMBER: _ClassVar[int]
    ANALYSIS_REQUESTED_AT_FIELD_NUMBER: _ClassVar[int]
    EVALUATED_AT_FIELD_NUMBER: _ClassVar[int]
    id: str
    analysis_id: str
    organization_id: str
    project_id: str
    actor: Assessment.Actor
    tool_call: Assessment.ToolCall
    guardrail: Assessment.Guardrail
    evaluator: Assessment.Evaluator
    decision: str
    decided_by: _containers.RepeatedScalarFieldContainer[str]
    contracts: _containers.RepeatedCompositeFieldContainer[Assessment.ContractOutcome]
    labels: _containers.RepeatedCompositeFieldContainer[Assessment.Label]
    evaluator_error: str
    usage: Assessment.Usage
    trigger_kind: str
    analysis_requested_at: str
    evaluated_at: str
    def __init__(self, id: _Optional[str] = ..., analysis_id: _Optional[str] = ..., organization_id: _Optional[str] = ..., project_id: _Optional[str] = ..., actor: _Optional[_Union[Assessment.Actor, _Mapping]] = ..., tool_call: _Optional[_Union[Assessment.ToolCall, _Mapping]] = ..., guardrail: _Optional[_Union[Assessment.Guardrail, _Mapping]] = ..., evaluator: _Optional[_Union[Assessment.Evaluator, _Mapping]] = ..., decision: _Optional[str] = ..., decided_by: _Optional[_Iterable[str]] = ..., contracts: _Optional[_Iterable[_Union[Assessment.ContractOutcome, _Mapping]]] = ..., labels: _Optional[_Iterable[_Union[Assessment.Label, _Mapping]]] = ..., evaluator_error: _Optional[str] = ..., usage: _Optional[_Union[Assessment.Usage, _Mapping]] = ..., trigger_kind: _Optional[str] = ..., analysis_requested_at: _Optional[str] = ..., evaluated_at: _Optional[str] = ...) -> None: ...
