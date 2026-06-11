package services

import (
	"kickrss/config"
)

func GetSettings() map[string]interface{} {
	cfg := config.GlobalConfig
	aiCfg := cfg.AI
	defaultAI := aiCfg.Default
	chatCfg := aiCfg.Tasks.Chat
	fulltextCfg := cfg.Fulltext
	classifyCfg := cfg.Classify

	chatMaxTokens := 1200
	if chatCfg.MaxTokens != nil {
		chatMaxTokens = *chatCfg.MaxTokens
	}

	return map[string]interface{}{
		"fetch_interval_minutes": cfg.FetchIntervalMinutes,
		"min_text_chars":         fulltextCfg.MinTextChars,
		"promote_threshold":      classifyCfg.PromoteThreshold,

		"ai_base_url": defaultAI.BaseURL,
		"ai_api_key":  defaultAI.APIKey,
		"ai_model":    defaultAI.Model,

		"ai_pregenerate":           aiCfg.Pregenerate,
		"ai_stream":                aiCfg.Stream,
		"ai_auto_summary":          aiCfg.AutoSummary,
		"ai_summary_length":        aiCfg.SummaryLength,
		"ai_summary_lang":          aiCfg.SummaryLanguage,
		"system_lang":              cfg.SystemLanguage,
		"interest_profile_enabled": cfg.InterestProfileEnabled,

		"chat_base_url":   chatCfg.BaseURL,
		"chat_api_key":    chatCfg.APIKey,
		"chat_model":      chatCfg.Model,
		"chat_max_tokens": chatMaxTokens,
	}
}

func UpdateSettings(
	fetchIntervalMinutes *int,
	minTextChars *int,
	promoteThreshold *int,
	aiBaseURL *string,
	aiAPIKey *string,
	aiModel *string,
	aiPregenerate *bool,
	aiStream *bool,
	aiAutoSummary *bool,
	aiSummaryLength *string,
	aiSummaryLang *string,
	systemLang *string,
	chatBaseURL *string,
	chatAPIKey *string,
	chatModel *string,
	chatMaxTokens *int,
	interestProfileEnabled *bool,
) {
	cfg := config.GlobalConfig

	if fetchIntervalMinutes != nil {
		cfg.FetchIntervalMinutes = *fetchIntervalMinutes
	}
	if minTextChars != nil {
		cfg.Fulltext.MinTextChars = *minTextChars
	}
	if promoteThreshold != nil {
		cfg.Classify.PromoteThreshold = *promoteThreshold
	}
	if aiBaseURL != nil {
		cfg.AI.Default.BaseURL = *aiBaseURL
	}
	if aiAPIKey != nil {
		cfg.AI.Default.APIKey = *aiAPIKey
	}
	if aiModel != nil {
		cfg.AI.Default.Model = *aiModel
	}
	if aiPregenerate != nil {
		cfg.AI.Pregenerate = *aiPregenerate
	}
	if aiStream != nil {
		cfg.AI.Stream = *aiStream
	}
	if aiAutoSummary != nil {
		cfg.AI.AutoSummary = *aiAutoSummary
	}
	if aiSummaryLength != nil {
		cfg.AI.SummaryLength = *aiSummaryLength
	}
	if aiSummaryLang != nil {
		cfg.AI.SummaryLanguage = *aiSummaryLang
	}
	if systemLang != nil {
		cfg.SystemLanguage = *systemLang
	}
	if chatBaseURL != nil {
		cfg.AI.Tasks.Chat.BaseURL = *chatBaseURL
	}
	if chatAPIKey != nil {
		cfg.AI.Tasks.Chat.APIKey = *chatAPIKey
	}
	if chatModel != nil {
		cfg.AI.Tasks.Chat.Model = *chatModel
	}
	if chatMaxTokens != nil {
		cfg.AI.Tasks.Chat.MaxTokens = chatMaxTokens
	}
	if interestProfileEnabled != nil {
		cfg.InterestProfileEnabled = *interestProfileEnabled
	}

	_ = config.SaveConfig()
}
