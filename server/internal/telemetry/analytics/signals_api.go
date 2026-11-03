package analytics

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	gen "github.com/speakeasy-api/gram/server/gen/analytics"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	signalquery "github.com/speakeasy-api/gram/server/internal/telemetry/analytics/signals"
)

func (s *Service) signalsEnabled(ctx context.Context, organization string) (bool, error) {
	if s.signalFeatures == nil {
		return false, nil
	}
	enabled, err := s.signalFeatures.IsFeatureEnabled(ctx, organization, productfeatures.FeatureSignalsIntelligence)
	if err != nil {
		return false, s.failure(ctx, err, "could not check signals intelligence availability")
	}
	return enabled, nil
}

func (s *Service) requireSignals(ctx context.Context, organization string) error {
	enabled, err := s.signalsEnabled(ctx, organization)
	if err != nil {
		return err
	}
	if !enabled {
		return oops.E(oops.CodeForbidden, nil, "signals intelligence is not enabled for this organization")
	}
	return nil
}

func signalRequestFromPayload(input *gen.AnalyticsSignalQuery) (*signalquery.Request, error) {
	if input == nil {
		return nil, nil
	}
	invalid := func() error {
		return newError(ErrUnsatisfiable, signalquery.Dataset, "signal_query", "", "selection and dimension objects must be present")
	}
	if input.Selection == nil {
		return nil, invalid()
	}
	out := &signalquery.Request{Selection: signalquery.Selection{SensorID: input.Selection.SensorID, SignalID: deref(input.Selection.SignalID), MinimumLevel: input.Selection.MinimumLevel, Threshold: input.Selection.Threshold}, Trigger: input.Trigger, RunID: deref(input.RunID), GroupBy: nil, Filters: nil, Groups: input.Groups, Points: input.Points}
	for _, dimension := range input.GroupBy {
		if dimension == nil {
			return nil, invalid()
		}
		out.GroupBy = append(out.GroupBy, signalquery.Dimension{Field: deref(dimension.Field), Attribute: deref(dimension.Attribute)})
	}
	for _, filter := range input.Filters {
		if filter == nil || filter.Dimension == nil {
			return nil, invalid()
		}
		out.Filters = append(out.Filters, signalquery.Filter{Dimension: signalquery.Dimension{Field: deref(filter.Dimension.Field), Attribute: deref(filter.Dimension.Attribute)}, Operator: filter.Operator, Values: filter.Values})
	}
	return out, nil
}

func signalResultView(result *SignalResult) *gen.AnalyticsSignalResult {
	if result == nil {
		return nil
	}
	m := result.Metadata
	groups := make([]map[string]any, len(result.Groups))
	for i, row := range result.Groups {
		groups[i] = row
	}
	dimensions := make([]*gen.AnalyticsSignalDimension, len(m.GroupBy))
	for i, d := range m.GroupBy {
		dimensions[i] = &gen.AnalyticsSignalDimension{Field: conv.PtrEmpty(d.Field), Attribute: conv.PtrEmpty(d.Attribute)}
	}
	filters := make([]*gen.AnalyticsSignalFilter, len(m.Filters))
	for i, filter := range m.Filters {
		filters[i] = &gen.AnalyticsSignalFilter{Dimension: &gen.AnalyticsSignalDimension{Field: conv.PtrEmpty(filter.Dimension.Field), Attribute: conv.PtrEmpty(filter.Dimension.Attribute)}, Operator: filter.Operator, Values: filter.Values}
	}
	return &gen.AnalyticsSignalResult{Metadata: &gen.AnalyticsSignalMetadata{
		From: m.From, To: m.To, Filters: filters,
		Selection: &gen.AnalyticsSignalSelection{SensorID: m.Selection.SensorID, SignalID: conv.PtrEmpty(m.Selection.SignalID), MinimumLevel: m.Selection.MinimumLevel, Threshold: m.Selection.Threshold},
		Trigger:   m.Trigger, RunID: conv.PtrEmpty(m.RunID), Step: m.Step, PhysicalResolution: m.PhysicalResolution, TimeAxis: m.TimeAxis, Unit: m.Unit, Population: m.Population, Coverage: m.Coverage, QueriedAt: m.QueriedAt, Snapshot: m.Snapshot, Exact: m.Exact, SeriesComplete: m.SeriesComplete, GroupsTruncated: m.GroupsTruncated, GroupBy: dimensions, PointBudget: m.PointBudget,
	}, Summary: result.Summary, Groups: groups}
}

func signalCatalogView(catalog *SignalCatalog) *gen.AnalyticsSignalCatalog {
	out := &gen.AnalyticsSignalCatalog{Dataset: catalog.Dataset, Unit: catalog.Unit, Population: catalog.Population, Steps: catalog.Steps, Operators: catalog.Operators, SystemDimensions: catalog.SystemDimensions, InitialAttributes: catalog.InitialAttributes, MaxSpanDays: catalog.MaxSpanDays, MaxGroups: catalog.MaxGroups, MaxPoints: catalog.MaxPoints, Sensors: make([]*gen.AnalyticsSignalSensor, 0, len(catalog.Sensors)), Signals: make([]*gen.AnalyticsSignalName, 0, len(catalog.Signals))}
	for _, sensor := range catalog.Sensors {
		out.Sensors = append(out.Sensors, &gen.AnalyticsSignalSensor{ID: sensor.ID, Name: sensor.Name, Mode: sensor.Mode, Enabled: sensor.Enabled, SignalIds: sensor.SignalIDs})
	}
	for _, signal := range catalog.Signals {
		out.Signals = append(out.Signals, &gen.AnalyticsSignalName{ID: signal.ID, Name: signal.Name})
	}
	return out
}

// SignalDefinition exposes the same immutable measurement evidence to dashboard users.
func (s *Service) SignalDefinition(ctx context.Context, payload *gen.SignalDefinitionPayload) (*gen.AnalyticsSignalDefinition, error) {
	auth, err := s.authorize(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.requireSignals(ctx, auth.ActiveOrganizationID); err != nil {
		return nil, err
	}
	definition, err := s.engine.ReadSignalDefinition(ctx, Tenant{OrganizationID: auth.ActiveOrganizationID, ProjectID: auth.ProjectID.String()}, payload.SensorID, payload.DefinitionHash)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, oops.C(oops.CodeNotFound)
	}
	if err != nil {
		return nil, s.failure(ctx, err, "could not read measurement definition")
	}
	return &gen.AnalyticsSignalDefinition{SensorID: definition.SensorID, DefinitionHash: definition.DefinitionHash, Definition: definition.Definition, CapturedAt: definition.CapturedAt}, nil
}
