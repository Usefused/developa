package application

import "strings"

const (
	cloudTransferLimitation   = "Bounded code evidence and prompts were sent to Ollama Cloud for this result; inference did not run locally."
	cloudRevisionLimitation   = "Cloud model identity records the provider-reported revision, not a reproducible local weights digest."
	cloudUnverifiedLimitation = "The cloud provider revision was not verified; a stable model version cannot be established."
	cloudNoTransferLimitation = "Ollama Cloud was configured, but this result did not invoke a model or transfer code evidence."
	cloudCachedLimitation     = "This execution reused validated results from earlier Ollama Cloud analysis; cached batches did not send code evidence again."
	featureCachedLimitation   = "Validated feature batches were reused for identical bounded evidence and model identity. Changes to page boundaries or prompt inputs can miss the cache."
)

type modelIdentity struct {
	name     string
	backend  string
	revision string
}

func parseModelIdentity(identity string) modelIdentity {
	if name, revision, cloud := strings.Cut(identity, "@cloud:"); cloud {
		return modelIdentity{name: name, backend: "cloud", revision: revision}
	}
	name, revision, _ := strings.Cut(identity, "@sha256:")
	return modelIdentity{name: name, backend: "local", revision: revision}
}

func (identity modelIdentity) hasRevision() bool {
	return identity.revision != "" && identity.revision != "unverified"
}

func modelLimitations(model string, generated bool) []string {
	identity := parseModelIdentity(model)
	if identity.backend != "cloud" {
		return []string{}
	}
	if !generated {
		return []string{cloudNoTransferLimitation}
	}
	if !identity.hasRevision() {
		return []string{cloudTransferLimitation, cloudUnverifiedLimitation}
	}
	return []string{cloudTransferLimitation, cloudRevisionLimitation}
}

func featureModelLimitations(state *featureState) []string {
	limits := modelLimitations(state.run.Model, state.modelCalls > 0)
	if state.cachedBatches == 0 {
		return limits
	}
	if parseModelIdentity(state.run.Model).backend == "cloud" {
		if state.modelCalls == 0 {
			limits = []string{cloudRevisionLimitation}
		}
		limits = append(limits, cloudCachedLimitation)
	}
	return append(limits, featureCachedLimitation)
}
