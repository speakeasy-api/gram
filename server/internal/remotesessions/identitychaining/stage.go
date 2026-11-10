package identitychaining

// Stage names where an identity chaining attempt stopped.
type Stage string

const (
	StageSelection     Stage = "selection"
	StageAuthorization Stage = "authorization"
	StageDelegation    Stage = "delegation"
	StageExchange      Stage = "exchange"
	StageValidation    Stage = "validation"
	StageRedemption    Stage = "redemption"
	StagePersistence   Stage = "persistence"
	StageComplete      Stage = "complete"
)
