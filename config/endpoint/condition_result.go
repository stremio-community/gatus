package endpoint

// ConditionResult result of a Condition
type ConditionResult struct {
	// Condition that was evaluated
	Condition string `json:"condition"`

	// Success whether the condition was met (successful) or not (failed)
	Success bool `json:"success"`

	// Details are optional label/value pairs explaining how the condition was evaluated.
	// Only external endpoints can set them, by pushing them alongside the condition.
	Details []*ConditionResultDetail `json:"details,omitempty"`
}

// ConditionResultDetail is a single label/value pair of a ConditionResult's Details
type ConditionResultDetail struct {
	Label string `json:"label"`
	Value string `json:"value"`
}
