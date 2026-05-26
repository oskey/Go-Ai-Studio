package api

import (
	"kt-ai-studio/internal/models"

	"github.com/sashabaranov/go-openai"
)

func applyProviderAdvancedRequestParams(provider models.LLMProvider, req openai.ChatCompletionRequest) openai.ChatCompletionRequest {
	if !provider.EnableAdvancedRequestParams {
		return req
	}
	if provider.RequestMaxTokens > 0 {
		req.MaxTokens = provider.RequestMaxTokens
	}
	if provider.RequestTemperature > 0 {
		req.Temperature = provider.RequestTemperature
	}
	return req
}
