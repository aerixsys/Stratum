package service

// ModelPolicy describes model block-list checks used by services.
type ModelPolicy interface {
	IsBlocked(modelID string) bool
}
