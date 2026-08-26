package application

const answerCachedLimitation = "This answer reused validated output for identical question, bounded evidence, and model identity."
const cloudAnswerCachedLimitation = "This answer reused validated output from an earlier Ollama Cloud request; this request did not send code evidence to a model."

func answerModelLimitations(model string, cached bool) []string {
	if !cached {
		return modelLimitations(model, true)
	}
	if parseModelIdentity(model).backend == "cloud" {
		return []string{answerCachedLimitation, cloudAnswerCachedLimitation, cloudRevisionLimitation}
	}
	return []string{answerCachedLimitation}
}
