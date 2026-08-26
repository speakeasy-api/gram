from gcp.pubsub.v1 import options_pb2 as _options_pb2
from google.protobuf.internal import containers as _containers
from google.protobuf.internal import enum_type_wrapper as _enum_type_wrapper
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from collections.abc import Iterable as _Iterable, Mapping as _Mapping
from typing import ClassVar as _ClassVar, Optional as _Optional, Union as _Union

DESCRIPTOR: _descriptor.FileDescriptor

class InboundMetric(_message.Message):
    __slots__ = ("name", "description", "unit", "gauge", "sum", "histogram", "exponential_histogram", "summary", "metadata", "resource", "resource_schema_url", "scope", "scope_schema_url", "provenance")
    class AggregationTemporality(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
        __slots__ = ()
        AGGREGATION_TEMPORALITY_UNSPECIFIED: _ClassVar[InboundMetric.AggregationTemporality]
        AGGREGATION_TEMPORALITY_DELTA: _ClassVar[InboundMetric.AggregationTemporality]
        AGGREGATION_TEMPORALITY_CUMULATIVE: _ClassVar[InboundMetric.AggregationTemporality]
    AGGREGATION_TEMPORALITY_UNSPECIFIED: InboundMetric.AggregationTemporality
    AGGREGATION_TEMPORALITY_DELTA: InboundMetric.AggregationTemporality
    AGGREGATION_TEMPORALITY_CUMULATIVE: InboundMetric.AggregationTemporality
    class DataPointFlags(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
        __slots__ = ()
        DATA_POINT_FLAGS_DO_NOT_USE: _ClassVar[InboundMetric.DataPointFlags]
        DATA_POINT_FLAGS_NO_RECORDED_VALUE_MASK: _ClassVar[InboundMetric.DataPointFlags]
    DATA_POINT_FLAGS_DO_NOT_USE: InboundMetric.DataPointFlags
    DATA_POINT_FLAGS_NO_RECORDED_VALUE_MASK: InboundMetric.DataPointFlags
    class Provenance(_message.Message):
        __slots__ = ("source", "organization_id", "project_id")
        SOURCE_FIELD_NUMBER: _ClassVar[int]
        ORGANIZATION_ID_FIELD_NUMBER: _ClassVar[int]
        PROJECT_ID_FIELD_NUMBER: _ClassVar[int]
        source: str
        organization_id: str
        project_id: str
        def __init__(self, source: _Optional[str] = ..., organization_id: _Optional[str] = ..., project_id: _Optional[str] = ...) -> None: ...
    class Resource(_message.Message):
        __slots__ = ("attributes", "dropped_attributes_count")
        ATTRIBUTES_FIELD_NUMBER: _ClassVar[int]
        DROPPED_ATTRIBUTES_COUNT_FIELD_NUMBER: _ClassVar[int]
        attributes: _containers.RepeatedCompositeFieldContainer[InboundMetric.KeyValue]
        dropped_attributes_count: int
        def __init__(self, attributes: _Optional[_Iterable[_Union[InboundMetric.KeyValue, _Mapping]]] = ..., dropped_attributes_count: _Optional[int] = ...) -> None: ...
    class InstrumentationScope(_message.Message):
        __slots__ = ("name", "version", "attributes", "dropped_attributes_count")
        NAME_FIELD_NUMBER: _ClassVar[int]
        VERSION_FIELD_NUMBER: _ClassVar[int]
        ATTRIBUTES_FIELD_NUMBER: _ClassVar[int]
        DROPPED_ATTRIBUTES_COUNT_FIELD_NUMBER: _ClassVar[int]
        name: str
        version: str
        attributes: _containers.RepeatedCompositeFieldContainer[InboundMetric.KeyValue]
        dropped_attributes_count: int
        def __init__(self, name: _Optional[str] = ..., version: _Optional[str] = ..., attributes: _Optional[_Iterable[_Union[InboundMetric.KeyValue, _Mapping]]] = ..., dropped_attributes_count: _Optional[int] = ...) -> None: ...
    class Gauge(_message.Message):
        __slots__ = ("data_points",)
        DATA_POINTS_FIELD_NUMBER: _ClassVar[int]
        data_points: _containers.RepeatedCompositeFieldContainer[InboundMetric.NumberDataPoint]
        def __init__(self, data_points: _Optional[_Iterable[_Union[InboundMetric.NumberDataPoint, _Mapping]]] = ...) -> None: ...
    class Sum(_message.Message):
        __slots__ = ("data_points", "aggregation_temporality", "is_monotonic")
        DATA_POINTS_FIELD_NUMBER: _ClassVar[int]
        AGGREGATION_TEMPORALITY_FIELD_NUMBER: _ClassVar[int]
        IS_MONOTONIC_FIELD_NUMBER: _ClassVar[int]
        data_points: _containers.RepeatedCompositeFieldContainer[InboundMetric.NumberDataPoint]
        aggregation_temporality: InboundMetric.AggregationTemporality
        is_monotonic: bool
        def __init__(self, data_points: _Optional[_Iterable[_Union[InboundMetric.NumberDataPoint, _Mapping]]] = ..., aggregation_temporality: _Optional[_Union[InboundMetric.AggregationTemporality, str]] = ..., is_monotonic: _Optional[bool] = ...) -> None: ...
    class Histogram(_message.Message):
        __slots__ = ("data_points", "aggregation_temporality")
        DATA_POINTS_FIELD_NUMBER: _ClassVar[int]
        AGGREGATION_TEMPORALITY_FIELD_NUMBER: _ClassVar[int]
        data_points: _containers.RepeatedCompositeFieldContainer[InboundMetric.HistogramDataPoint]
        aggregation_temporality: InboundMetric.AggregationTemporality
        def __init__(self, data_points: _Optional[_Iterable[_Union[InboundMetric.HistogramDataPoint, _Mapping]]] = ..., aggregation_temporality: _Optional[_Union[InboundMetric.AggregationTemporality, str]] = ...) -> None: ...
    class ExponentialHistogram(_message.Message):
        __slots__ = ("data_points", "aggregation_temporality")
        DATA_POINTS_FIELD_NUMBER: _ClassVar[int]
        AGGREGATION_TEMPORALITY_FIELD_NUMBER: _ClassVar[int]
        data_points: _containers.RepeatedCompositeFieldContainer[InboundMetric.ExponentialHistogramDataPoint]
        aggregation_temporality: InboundMetric.AggregationTemporality
        def __init__(self, data_points: _Optional[_Iterable[_Union[InboundMetric.ExponentialHistogramDataPoint, _Mapping]]] = ..., aggregation_temporality: _Optional[_Union[InboundMetric.AggregationTemporality, str]] = ...) -> None: ...
    class Summary(_message.Message):
        __slots__ = ("data_points",)
        DATA_POINTS_FIELD_NUMBER: _ClassVar[int]
        data_points: _containers.RepeatedCompositeFieldContainer[InboundMetric.SummaryDataPoint]
        def __init__(self, data_points: _Optional[_Iterable[_Union[InboundMetric.SummaryDataPoint, _Mapping]]] = ...) -> None: ...
    class NumberDataPoint(_message.Message):
        __slots__ = ("attributes", "start_time_unix_nano", "time_unix_nano", "as_double", "as_int", "exemplars", "flags")
        ATTRIBUTES_FIELD_NUMBER: _ClassVar[int]
        START_TIME_UNIX_NANO_FIELD_NUMBER: _ClassVar[int]
        TIME_UNIX_NANO_FIELD_NUMBER: _ClassVar[int]
        AS_DOUBLE_FIELD_NUMBER: _ClassVar[int]
        AS_INT_FIELD_NUMBER: _ClassVar[int]
        EXEMPLARS_FIELD_NUMBER: _ClassVar[int]
        FLAGS_FIELD_NUMBER: _ClassVar[int]
        attributes: _containers.RepeatedCompositeFieldContainer[InboundMetric.KeyValue]
        start_time_unix_nano: int
        time_unix_nano: int
        as_double: float
        as_int: int
        exemplars: _containers.RepeatedCompositeFieldContainer[InboundMetric.Exemplar]
        flags: int
        def __init__(self, attributes: _Optional[_Iterable[_Union[InboundMetric.KeyValue, _Mapping]]] = ..., start_time_unix_nano: _Optional[int] = ..., time_unix_nano: _Optional[int] = ..., as_double: _Optional[float] = ..., as_int: _Optional[int] = ..., exemplars: _Optional[_Iterable[_Union[InboundMetric.Exemplar, _Mapping]]] = ..., flags: _Optional[int] = ...) -> None: ...
    class HistogramDataPoint(_message.Message):
        __slots__ = ("attributes", "start_time_unix_nano", "time_unix_nano", "count", "sum", "bucket_counts", "explicit_bounds", "exemplars", "flags", "min", "max")
        ATTRIBUTES_FIELD_NUMBER: _ClassVar[int]
        START_TIME_UNIX_NANO_FIELD_NUMBER: _ClassVar[int]
        TIME_UNIX_NANO_FIELD_NUMBER: _ClassVar[int]
        COUNT_FIELD_NUMBER: _ClassVar[int]
        SUM_FIELD_NUMBER: _ClassVar[int]
        BUCKET_COUNTS_FIELD_NUMBER: _ClassVar[int]
        EXPLICIT_BOUNDS_FIELD_NUMBER: _ClassVar[int]
        EXEMPLARS_FIELD_NUMBER: _ClassVar[int]
        FLAGS_FIELD_NUMBER: _ClassVar[int]
        MIN_FIELD_NUMBER: _ClassVar[int]
        MAX_FIELD_NUMBER: _ClassVar[int]
        attributes: _containers.RepeatedCompositeFieldContainer[InboundMetric.KeyValue]
        start_time_unix_nano: int
        time_unix_nano: int
        count: int
        sum: float
        bucket_counts: _containers.RepeatedScalarFieldContainer[int]
        explicit_bounds: _containers.RepeatedScalarFieldContainer[float]
        exemplars: _containers.RepeatedCompositeFieldContainer[InboundMetric.Exemplar]
        flags: int
        min: float
        max: float
        def __init__(self, attributes: _Optional[_Iterable[_Union[InboundMetric.KeyValue, _Mapping]]] = ..., start_time_unix_nano: _Optional[int] = ..., time_unix_nano: _Optional[int] = ..., count: _Optional[int] = ..., sum: _Optional[float] = ..., bucket_counts: _Optional[_Iterable[int]] = ..., explicit_bounds: _Optional[_Iterable[float]] = ..., exemplars: _Optional[_Iterable[_Union[InboundMetric.Exemplar, _Mapping]]] = ..., flags: _Optional[int] = ..., min: _Optional[float] = ..., max: _Optional[float] = ...) -> None: ...
    class ExponentialHistogramDataPoint(_message.Message):
        __slots__ = ("attributes", "start_time_unix_nano", "time_unix_nano", "count", "sum", "scale", "zero_count", "positive", "negative", "flags", "exemplars", "min", "max", "zero_threshold")
        class Buckets(_message.Message):
            __slots__ = ("offset", "bucket_counts")
            OFFSET_FIELD_NUMBER: _ClassVar[int]
            BUCKET_COUNTS_FIELD_NUMBER: _ClassVar[int]
            offset: int
            bucket_counts: _containers.RepeatedScalarFieldContainer[int]
            def __init__(self, offset: _Optional[int] = ..., bucket_counts: _Optional[_Iterable[int]] = ...) -> None: ...
        ATTRIBUTES_FIELD_NUMBER: _ClassVar[int]
        START_TIME_UNIX_NANO_FIELD_NUMBER: _ClassVar[int]
        TIME_UNIX_NANO_FIELD_NUMBER: _ClassVar[int]
        COUNT_FIELD_NUMBER: _ClassVar[int]
        SUM_FIELD_NUMBER: _ClassVar[int]
        SCALE_FIELD_NUMBER: _ClassVar[int]
        ZERO_COUNT_FIELD_NUMBER: _ClassVar[int]
        POSITIVE_FIELD_NUMBER: _ClassVar[int]
        NEGATIVE_FIELD_NUMBER: _ClassVar[int]
        FLAGS_FIELD_NUMBER: _ClassVar[int]
        EXEMPLARS_FIELD_NUMBER: _ClassVar[int]
        MIN_FIELD_NUMBER: _ClassVar[int]
        MAX_FIELD_NUMBER: _ClassVar[int]
        ZERO_THRESHOLD_FIELD_NUMBER: _ClassVar[int]
        attributes: _containers.RepeatedCompositeFieldContainer[InboundMetric.KeyValue]
        start_time_unix_nano: int
        time_unix_nano: int
        count: int
        sum: float
        scale: int
        zero_count: int
        positive: InboundMetric.ExponentialHistogramDataPoint.Buckets
        negative: InboundMetric.ExponentialHistogramDataPoint.Buckets
        flags: int
        exemplars: _containers.RepeatedCompositeFieldContainer[InboundMetric.Exemplar]
        min: float
        max: float
        zero_threshold: float
        def __init__(self, attributes: _Optional[_Iterable[_Union[InboundMetric.KeyValue, _Mapping]]] = ..., start_time_unix_nano: _Optional[int] = ..., time_unix_nano: _Optional[int] = ..., count: _Optional[int] = ..., sum: _Optional[float] = ..., scale: _Optional[int] = ..., zero_count: _Optional[int] = ..., positive: _Optional[_Union[InboundMetric.ExponentialHistogramDataPoint.Buckets, _Mapping]] = ..., negative: _Optional[_Union[InboundMetric.ExponentialHistogramDataPoint.Buckets, _Mapping]] = ..., flags: _Optional[int] = ..., exemplars: _Optional[_Iterable[_Union[InboundMetric.Exemplar, _Mapping]]] = ..., min: _Optional[float] = ..., max: _Optional[float] = ..., zero_threshold: _Optional[float] = ...) -> None: ...
    class SummaryDataPoint(_message.Message):
        __slots__ = ("attributes", "start_time_unix_nano", "time_unix_nano", "count", "sum", "quantile_values", "flags")
        class ValueAtQuantile(_message.Message):
            __slots__ = ("quantile", "value")
            QUANTILE_FIELD_NUMBER: _ClassVar[int]
            VALUE_FIELD_NUMBER: _ClassVar[int]
            quantile: float
            value: float
            def __init__(self, quantile: _Optional[float] = ..., value: _Optional[float] = ...) -> None: ...
        ATTRIBUTES_FIELD_NUMBER: _ClassVar[int]
        START_TIME_UNIX_NANO_FIELD_NUMBER: _ClassVar[int]
        TIME_UNIX_NANO_FIELD_NUMBER: _ClassVar[int]
        COUNT_FIELD_NUMBER: _ClassVar[int]
        SUM_FIELD_NUMBER: _ClassVar[int]
        QUANTILE_VALUES_FIELD_NUMBER: _ClassVar[int]
        FLAGS_FIELD_NUMBER: _ClassVar[int]
        attributes: _containers.RepeatedCompositeFieldContainer[InboundMetric.KeyValue]
        start_time_unix_nano: int
        time_unix_nano: int
        count: int
        sum: float
        quantile_values: _containers.RepeatedCompositeFieldContainer[InboundMetric.SummaryDataPoint.ValueAtQuantile]
        flags: int
        def __init__(self, attributes: _Optional[_Iterable[_Union[InboundMetric.KeyValue, _Mapping]]] = ..., start_time_unix_nano: _Optional[int] = ..., time_unix_nano: _Optional[int] = ..., count: _Optional[int] = ..., sum: _Optional[float] = ..., quantile_values: _Optional[_Iterable[_Union[InboundMetric.SummaryDataPoint.ValueAtQuantile, _Mapping]]] = ..., flags: _Optional[int] = ...) -> None: ...
    class Exemplar(_message.Message):
        __slots__ = ("filtered_attributes", "time_unix_nano", "as_double", "as_int", "span_id", "trace_id")
        FILTERED_ATTRIBUTES_FIELD_NUMBER: _ClassVar[int]
        TIME_UNIX_NANO_FIELD_NUMBER: _ClassVar[int]
        AS_DOUBLE_FIELD_NUMBER: _ClassVar[int]
        AS_INT_FIELD_NUMBER: _ClassVar[int]
        SPAN_ID_FIELD_NUMBER: _ClassVar[int]
        TRACE_ID_FIELD_NUMBER: _ClassVar[int]
        filtered_attributes: _containers.RepeatedCompositeFieldContainer[InboundMetric.KeyValue]
        time_unix_nano: int
        as_double: float
        as_int: int
        span_id: bytes
        trace_id: bytes
        def __init__(self, filtered_attributes: _Optional[_Iterable[_Union[InboundMetric.KeyValue, _Mapping]]] = ..., time_unix_nano: _Optional[int] = ..., as_double: _Optional[float] = ..., as_int: _Optional[int] = ..., span_id: _Optional[bytes] = ..., trace_id: _Optional[bytes] = ...) -> None: ...
    class AnyValue(_message.Message):
        __slots__ = ("string_value", "bool_value", "int_value", "double_value", "array_value", "kvlist_value", "bytes_value")
        STRING_VALUE_FIELD_NUMBER: _ClassVar[int]
        BOOL_VALUE_FIELD_NUMBER: _ClassVar[int]
        INT_VALUE_FIELD_NUMBER: _ClassVar[int]
        DOUBLE_VALUE_FIELD_NUMBER: _ClassVar[int]
        ARRAY_VALUE_FIELD_NUMBER: _ClassVar[int]
        KVLIST_VALUE_FIELD_NUMBER: _ClassVar[int]
        BYTES_VALUE_FIELD_NUMBER: _ClassVar[int]
        string_value: str
        bool_value: bool
        int_value: int
        double_value: float
        array_value: InboundMetric.ArrayValue
        kvlist_value: InboundMetric.KeyValueList
        bytes_value: bytes
        def __init__(self, string_value: _Optional[str] = ..., bool_value: _Optional[bool] = ..., int_value: _Optional[int] = ..., double_value: _Optional[float] = ..., array_value: _Optional[_Union[InboundMetric.ArrayValue, _Mapping]] = ..., kvlist_value: _Optional[_Union[InboundMetric.KeyValueList, _Mapping]] = ..., bytes_value: _Optional[bytes] = ...) -> None: ...
    class ArrayValue(_message.Message):
        __slots__ = ("values",)
        VALUES_FIELD_NUMBER: _ClassVar[int]
        values: _containers.RepeatedCompositeFieldContainer[InboundMetric.AnyValue]
        def __init__(self, values: _Optional[_Iterable[_Union[InboundMetric.AnyValue, _Mapping]]] = ...) -> None: ...
    class KeyValueList(_message.Message):
        __slots__ = ("values",)
        VALUES_FIELD_NUMBER: _ClassVar[int]
        values: _containers.RepeatedCompositeFieldContainer[InboundMetric.KeyValue]
        def __init__(self, values: _Optional[_Iterable[_Union[InboundMetric.KeyValue, _Mapping]]] = ...) -> None: ...
    class KeyValue(_message.Message):
        __slots__ = ("key", "value")
        KEY_FIELD_NUMBER: _ClassVar[int]
        VALUE_FIELD_NUMBER: _ClassVar[int]
        key: str
        value: InboundMetric.AnyValue
        def __init__(self, key: _Optional[str] = ..., value: _Optional[_Union[InboundMetric.AnyValue, _Mapping]] = ...) -> None: ...
    NAME_FIELD_NUMBER: _ClassVar[int]
    DESCRIPTION_FIELD_NUMBER: _ClassVar[int]
    UNIT_FIELD_NUMBER: _ClassVar[int]
    GAUGE_FIELD_NUMBER: _ClassVar[int]
    SUM_FIELD_NUMBER: _ClassVar[int]
    HISTOGRAM_FIELD_NUMBER: _ClassVar[int]
    EXPONENTIAL_HISTOGRAM_FIELD_NUMBER: _ClassVar[int]
    SUMMARY_FIELD_NUMBER: _ClassVar[int]
    METADATA_FIELD_NUMBER: _ClassVar[int]
    RESOURCE_FIELD_NUMBER: _ClassVar[int]
    RESOURCE_SCHEMA_URL_FIELD_NUMBER: _ClassVar[int]
    SCOPE_FIELD_NUMBER: _ClassVar[int]
    SCOPE_SCHEMA_URL_FIELD_NUMBER: _ClassVar[int]
    PROVENANCE_FIELD_NUMBER: _ClassVar[int]
    name: str
    description: str
    unit: str
    gauge: InboundMetric.Gauge
    sum: InboundMetric.Sum
    histogram: InboundMetric.Histogram
    exponential_histogram: InboundMetric.ExponentialHistogram
    summary: InboundMetric.Summary
    metadata: _containers.RepeatedCompositeFieldContainer[InboundMetric.KeyValue]
    resource: InboundMetric.Resource
    resource_schema_url: str
    scope: InboundMetric.InstrumentationScope
    scope_schema_url: str
    provenance: InboundMetric.Provenance
    def __init__(self, name: _Optional[str] = ..., description: _Optional[str] = ..., unit: _Optional[str] = ..., gauge: _Optional[_Union[InboundMetric.Gauge, _Mapping]] = ..., sum: _Optional[_Union[InboundMetric.Sum, _Mapping]] = ..., histogram: _Optional[_Union[InboundMetric.Histogram, _Mapping]] = ..., exponential_histogram: _Optional[_Union[InboundMetric.ExponentialHistogram, _Mapping]] = ..., summary: _Optional[_Union[InboundMetric.Summary, _Mapping]] = ..., metadata: _Optional[_Iterable[_Union[InboundMetric.KeyValue, _Mapping]]] = ..., resource: _Optional[_Union[InboundMetric.Resource, _Mapping]] = ..., resource_schema_url: _Optional[str] = ..., scope: _Optional[_Union[InboundMetric.InstrumentationScope, _Mapping]] = ..., scope_schema_url: _Optional[str] = ..., provenance: _Optional[_Union[InboundMetric.Provenance, _Mapping]] = ...) -> None: ...
