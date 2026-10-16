package analytics

import (
	"github.com/Masterminds/squirrel"

	"github.com/speakeasy-api/gram/server/internal/productfeatures"
)

// The field literals below state only what a field has; an omitted field is
// its zero value by design, and the file is excluded from exhaustruct for it.

// Lookup names the sensor datasets read through.
const (
	// SensorNamesLookup maps a sensor id to its current name, deleted sensors
	// included, so past readings show the name the configuration page shows.
	SensorNamesLookup = "sigint_sensor_names"

	// SignalNamesLookup maps a signal id to its current name, deleted signals
	// included.
	SignalNamesLookup = "sigint_signal_names"

	// SensorMinConfidenceLookup maps a sensor id to its minimum classifier
	// confidence, formatted as a decimal string. A sensor without one has no
	// entry.
	SensorMinConfidenceLookup = "sigint_sensor_min_confidence"
)

// sensorLookups are the lookups the sensor datasets declare.
var sensorLookups = []*Lookup{
	{
		Name:        SensorNamesLookup,
		Description: "The project's sensors, by id, under their current names. A reading of a sensor that no longer has a name shows its id.",
	},
	{
		Name:        SignalNamesLookup,
		Description: "The project's signals, by id, under their current names. An answer for a signal that no longer has a name shows its id.",
	},
	{
		Name:        SensorMinConfidenceLookup,
		Description: "Each sensor's current minimum classifier confidence. answer_status compares every answer, past ones included, with it.",
	},
}

// The answered, not_applicable, draft and failed outcomes of an evaluation,
// as stored in sigint_readings.status.
const sensorStatusAnswered = "answered"

// sensorReadingColumns are what both sensor datasets read from one reading.
// Every alias differs from the stored column it reads, so a predicate on the
// stored column never resolves to the alias.
var sensorReadingColumns = []string{
	"id",
	"toString(sensor_id) AS sensor_key",
	"event_id AS message_key",
	"ifNull(toString(conversation_id), '') AS conversation_key",
	"actor_user_email AS user_email",
	"conversation_role AS message_role",
	"source AS message_source",
	"toUnixTimestamp64Nano(event_occurred_at) AS occurred_at",
}

// sensorEvaluationsSource is one row per reading id at its latest attempt:
// an answer or the outcome that replaced one.
var sensorEvaluationsSource = Source{
	Table: sigintReadings,
	Key:   "id",
	Columns: append([]string{
		"status AS evaluation_status",
		"failure_reason AS evaluation_failure_reason",
	}, sensorReadingColumns...),
}

// sensorAnswers fans one answered reading out into its signal answers. Each
// element is (signal id, confidence, matched):
//   - multi_label: every configured signal, confidence |2p - 1|, matched when
//     p is at least 0.5;
//   - choice: the selected signal with the classifier's confidence;
//   - score: the most likely level (the first of equally likely ones, so
//     the lowest), with the classifier's confidence.
//
// A reading whose result does not fit its variant yields no answer rather
// than an invented one.
const sensorAnswers = `ARRAY JOIN multiIf(
	result_type = 'multi_label',
	arrayMap(p -> (toString(tupleElement(p, 'signal_id')), toNullable(abs(2 * tupleElement(p, 'probability') - 1)), if(tupleElement(p, 'probability') >= 0.5, 'true', 'false')), probabilities),
	result_type = 'choice' AND selected_signal_id IS NOT NULL,
	[(toString(assumeNotNull(selected_signal_id)), confidence, 'true')],
	result_type = 'score' AND notEmpty(probabilities),
	[(toString(tupleElement(probabilities[arrayFirstIndex(p -> tupleElement(p, 'probability') = arrayMax(arrayMap(q -> tupleElement(q, 'probability'), probabilities)), probabilities)], 'signal_id')), confidence, 'true')],
	CAST([], 'Array(Tuple(String, Nullable(Float64), String))')
) AS answer`

// confidenceBucket names an answer's 0.1-wide confidence band, such as
// 0.6-0.7, the top band closed at 1. An unrated answer has none.
const confidenceBucket = `if(answer_confidence IS NULL, '', multiIf(
	toUInt8(floor(least(assumeNotNull(answer_confidence), 0.999999) * 10)) = 9, '0.9-1.0',
	concat('0.', toString(toUInt8(floor(least(assumeNotNull(answer_confidence), 0.999999) * 10))), '-0.', toString(toUInt8(floor(least(assumeNotNull(answer_confidence), 0.999999) * 10)) + 1))
)) AS answer_confidence_bucket`

// expandSensorAnswers adds the answer columns and its status against the
// sensor's current minimum confidence.
func expandSensorAnswers(qc QueryContext, rows squirrel.SelectBuilder) squirrel.SelectBuilder {
	rows = rows.JoinClause(sensorAnswers).
		Column("tupleElement(answer, 1) AS signal_key").
		Column("tupleElement(answer, 2) AS answer_confidence").
		Column("tupleElement(answer, 3) AS answer_matched").
		Column(confidenceBucket)

	threshold := "CAST(NULL, 'Nullable(Float64)')"
	var args []any
	if raws, targets := lookupPairs(qc.Lookups[SensorMinConfidenceLookup]); len(raws) > 0 {
		threshold = "toFloat64OrNull(transform(toString(sensor_id), ?, ?, ''))"
		args = []any{raws, targets}
	}
	return rows.Column(`multiIf(
		answer_confidence IS NULL, 'unrated',
		`+threshold+` IS NULL OR assumeNotNull(answer_confidence) >= `+threshold+`, 'confident',
		'uncertain') AS answer_status`, append(args, args...)...)
}

// sensorSignalsSource is one row per signal answer of a reading whose latest
// attempt answered.
var sensorSignalsSource = Source{
	Table:   sigintReadings,
	Key:     "id",
	Where:   []squirrel.Sqlizer{squirrel.Eq{"status": sensorStatusAnswered}},
	Columns: sensorReadingColumns,
	Expand:  expandSensorAnswers,
	Lookups: []string{SensorMinConfidenceLookup},
}

// The fields both sensor datasets share.
var (
	sensorField = Field{
		Name:         "sensor",
		Type:         TypeString,
		Role:         RoleDimension,
		Default:      true,
		Operators:    equalsIn,
		Aggregations: countDistinct,
		Expr:         "sensor_key",
		Typeahead:    "sensor_key",
		Lookup:       SensorNamesLookup,
		Description:  "The sensor, under its current name. A filter may name it or its id.",
	}
	sensorMessageField = Field{
		Name:         "message",
		Type:         TypeString,
		Role:         RoleDimension,
		Operators:    equalsIn,
		Aggregations: countDistinct,
		Expr:         "message_key",
		Typeahead:    "message_key",
		Description:  "The evaluated message. count_distinct over it counts messages.",
	}
	sensorConversationField = Field{
		Name:         "conversation",
		Type:         TypeString,
		Role:         RoleDimension,
		Operators:    equalsIn,
		Aggregations: countDistinct,
		Expr:         "conversation_key",
		Typeahead:    "conversation_key",
		Description:  "The conversation the message belongs to. count_distinct over it counts conversations with evidence, not their purpose.",
	}
	sensorUserField = Field{
		Name:         "user",
		Type:         TypeString,
		Role:         RoleDimension,
		Operators:    equalsIn,
		Aggregations: countDistinct,
		Expr:         "user_email",
		Typeahead:    "user_email",
		Description:  "The email ingestion observed for the message's author. Empty when the producer reported none, so count_distinct counts identified people only.",
	}
	sensorRoleField = Field{
		Name:        "role",
		Type:        TypeString,
		Role:        RoleDimension,
		Operators:   equalsIn,
		Expr:        "message_role",
		Typeahead:   "message_role",
		Description: "The evaluated message's role: user or assistant.",
	}
	sensorSourceField = Field{
		Name:        "source",
		Type:        TypeString,
		Role:        RoleDimension,
		Operators:   equalsIn,
		Expr:        "message_source",
		Typeahead:   "message_source",
		Description: "The surface the message was ingested from.",
	}
)

// sensorCoverageCaveat is what every sensor dataset says about coverage.
const sensorCoverageCaveat = "Counts cover only messages the evaluator handled: a message it never processed has no row, so coverage is among handled messages, not all messages."

// SensorEvaluations is one row per reading: a message a sensor evaluated, at
// its latest attempt, in every status.
var SensorEvaluations = &Dataset{
	Name:          "sensor_evaluations",
	Kind:          KindEvent,
	Grain:         "sensor evaluation",
	Description:   "One row per sensor evaluation of a message, at its latest attempt, so a retry never adds a row. status is answered, not_applicable (the sensor's match expression excluded the message), draft (the sensor's configuration was incomplete) or failed (failure_reason says why). count counts evaluations; count_distinct over message, conversation and user counts what they cover. Group by status for coverage. " + sensorCoverageCaveat,
	TimeExpr:      "occurred_at",
	RetentionDays: MaxSensorReadingTimeRangeDays,
	Feature:       productfeatures.FeatureSignalsIntelligence,
	Fields: []Field{
		sensorField,
		{
			Name:        "status",
			Type:        TypeString,
			Role:        RoleDimension,
			Default:     true,
			Operators:   equalsIn,
			Expr:        "evaluation_status",
			Typeahead:   "evaluation_status",
			Description: "answered, not_applicable, draft or failed.",
		},
		{
			Name:        "failure_reason",
			Type:        TypeString,
			Role:        RoleDimension,
			Operators:   equalsIn,
			Expr:        "evaluation_failure_reason",
			Typeahead:   "evaluation_failure_reason",
			Description: "Why a failed evaluation failed, as a short code. Empty for every other status.",
		},
		sensorMessageField,
		sensorConversationField,
		sensorUserField,
		sensorRoleField,
		sensorSourceField,
	},
	Source: sensorEvaluationsSource,
}

// SensorSignals is one row per signal answer of an answered evaluation.
var SensorSignals = &Dataset{
	Name:          "sensor_signals",
	Kind:          KindEvent,
	Grain:         "signal answer",
	Description:   "One row per signal answer of an answered sensor evaluation, at its latest attempt: every configured signal of a multi-label sensor, the selected signal of an exclusive sensor, and the most likely level of an ordered-score sensor. One message can produce several rows, across signals and sensors, so count rows as answers and use count_distinct over message to count messages. matched is true when a multi-label signal's probability is at least 0.5, and always true for exclusive and ordered-score answers. confidence is how decided the classifier's own distribution was (|2p - 1| for a multi-label signal, the classifier's reported confidence otherwise), not measured accuracy. answer_status is confident when the sensor has no minimum classifier confidence or the answer reaches it, uncertain below it, and unrated when no confidence was reported. The minimum is the sensor's current setting, so changing it reinterprets past answers. " + sensorCoverageCaveat,
	TimeExpr:      "occurred_at",
	RetentionDays: MaxSensorReadingTimeRangeDays,
	Feature:       productfeatures.FeatureSignalsIntelligence,
	Fields: []Field{
		sensorField,
		{
			Name:         "signal",
			Type:         TypeString,
			Role:         RoleDimension,
			Default:      true,
			Operators:    equalsIn,
			Aggregations: countDistinct,
			Expr:         "signal_key",
			Typeahead:    "signal_key",
			Lookup:       SignalNamesLookup,
			Description:  "The answered signal, or ordered-score level, under its current name. A filter may name it or its id.",
		},
		{
			Name:        "matched",
			Type:        TypeString,
			Role:        RoleDimension,
			Operators:   equalsIn,
			Expr:        "answer_matched",
			Typeahead:   "answer_matched",
			Description: "true or false. Only a multi-label signal can be false: its probability was below 0.5.",
		},
		{
			Name:        "answer_status",
			Type:        TypeString,
			Role:        RoleDimension,
			Operators:   equalsIn,
			Expr:        "answer_status",
			Typeahead:   "answer_status",
			Description: "confident, uncertain (below the sensor's minimum classifier confidence) or unrated (no confidence reported).",
		},
		{
			Name:        "confidence_bucket",
			Type:        TypeString,
			Role:        RoleDimension,
			Operators:   equalsIn,
			Expr:        "answer_confidence_bucket",
			Typeahead:   "answer_confidence_bucket",
			Description: "The answer's confidence in 0.1-wide bands, 0.0-0.1 to 0.9-1.0. Empty when unrated. Confidence is the classifier's distribution spread, not accuracy.",
		},
		sensorMessageField,
		sensorConversationField,
		sensorUserField,
		sensorRoleField,
		sensorSourceField,
	},
	Source: sensorSignalsSource,
}
