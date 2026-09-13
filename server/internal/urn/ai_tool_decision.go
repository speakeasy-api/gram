package urn

import (
	"database/sql/driver"
)

const deviceAgentAiTargetDecisionPrefix = "ai_tool_decision"

// AIToolDecision identifies an organization's access decision
// for one Shadow AI target. The table is keyed by (organization_id,
// target_id), so the URN id is the two values joined with a slash (e.g.
// "org_123/cursor").
type AIToolDecision struct {
	ID string
}

func NewAIToolDecision(organizationID string, targetID string) AIToolDecision {
	return AIToolDecision{ID: organizationID + "/" + targetID}
}

func ParseAIToolDecision(value string) (AIToolDecision, error) {
	id, err := settingsURNParse(deviceAgentAiTargetDecisionPrefix, value)
	if err != nil {
		return AIToolDecision{}, err
	}
	return AIToolDecision{ID: id}, nil
}

func (u AIToolDecision) IsZero() bool {
	return u.ID == ""
}

func (u AIToolDecision) String() string {
	return settingsURNString(deviceAgentAiTargetDecisionPrefix, u.ID)
}

func (u AIToolDecision) MarshalJSON() ([]byte, error) {
	return settingsURNMarshalJSON(deviceAgentAiTargetDecisionPrefix, u.ID)
}

func (u *AIToolDecision) UnmarshalJSON(data []byte) error {
	id, err := settingsURNUnmarshalJSON(deviceAgentAiTargetDecisionPrefix, data)
	if err != nil {
		return err
	}
	u.ID = id
	return nil
}

func (u *AIToolDecision) Scan(value any) error {
	if value == nil {
		return nil
	}
	id, err := settingsURNScan(deviceAgentAiTargetDecisionPrefix, "AIToolDecision", value)
	if err != nil {
		return err
	}
	u.ID = id
	return nil
}

func (u AIToolDecision) Value() (driver.Value, error) {
	return settingsURNValue(deviceAgentAiTargetDecisionPrefix, u.ID)
}

func (u AIToolDecision) MarshalText() ([]byte, error) {
	return settingsURNMarshalText(deviceAgentAiTargetDecisionPrefix, u.ID)
}

func (u *AIToolDecision) UnmarshalText(text []byte) error {
	id, err := settingsURNUnmarshalText(deviceAgentAiTargetDecisionPrefix, text)
	if err != nil {
		return err
	}
	u.ID = id
	return nil
}
