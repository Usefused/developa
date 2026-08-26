package domain

import "regexp"

var flowIdentifier = regexp.MustCompile(`^[a-f0-9]{64}$`)

func NormalizeFlowOptions(options FlowOptions) (FlowOptions, error) {
	if options.Depth == 0 {
		options.Depth = 6
	}
	if options.Limit == 0 {
		options.Limit = 80
	}
	if options.Depth < 1 || options.Depth > 12 || options.Limit < 1 || options.Limit > 100 {
		return options, ErrInvalidInput
	}
	if !validFlowSelection(options) {
		return options, ErrInvalidInput
	}
	return options, nil
}

func validFlowSelection(options FlowOptions) bool {
	if options.SymbolID != "" && options.FeatureID != "" {
		return false
	}
	return validOptionalFlowID(options.SymbolID) && validOptionalFlowID(options.FeatureID)
}

func validOptionalFlowID(value string) bool { return value == "" || flowIdentifier.MatchString(value) }
