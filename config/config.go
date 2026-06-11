package config

import (
	"os"
	"path/filepath"
	"sync"
	"gopkg.in/yaml.v3"
)

type AIConfigDetail struct {
	BaseURL        string `yaml:"base_url"`
	APIKey         string `yaml:"api_key"`
	Model          string `yaml:"model"`
	BatchSize      int    `yaml:"batch_size"`
	MaxConcurrency int    `yaml:"max_concurrency"`
	MaxTokens      *int   `yaml:"max_tokens"`
}

type AITasks struct {
	Classify AIConfigDetail `yaml:"classify"`
	Seed     AIConfigDetail `yaml:"seed"`
	Summary  AIConfigDetail `yaml:"summary"`
	Chat     AIConfigDetail `yaml:"chat"`
	Profile  AIConfigDetail `yaml:"profile"`
}

type AIConfig struct {
	Default         AIConfigDetail `yaml:"default"`
	Tasks           AITasks        `yaml:"tasks"`
	Pregenerate     bool           `yaml:"pregenerate"`
	Stream          bool           `yaml:"stream"`
	AutoSummary     bool           `yaml:"auto_summary"`
	SummaryLength   string         `yaml:"summary_length"`
	SummaryLanguage string         `yaml:"summary_language"`
}

type FulltextConfig struct {
	Fetcher             string `yaml:"fetcher"`
	RenderingServiceURL string `yaml:"rendering_service_url"`
	MinTextChars        int    `yaml:"min_text_chars"`
}

type ClassifyConfig struct {
	PromoteThreshold int `yaml:"promote_threshold"`
}

type AppConfig struct {
	DBPath                 string         `yaml:"db_path"`
	Port                   int            `yaml:"port"`
	FetchIntervalMinutes   int            `yaml:"fetch_interval_minutes"`
	AI                     AIConfig       `yaml:"ai"`
	Fulltext               FulltextConfig `yaml:"fulltext"`
	Classify               ClassifyConfig `yaml:"classify"`
	SystemLanguage         string         `yaml:"system_language"`
	InterestProfileEnabled bool           `yaml:"interest_profile_enabled"`
}

var (
	GlobalConfig *AppConfig
	configMutex  sync.RWMutex
	ConfigPath   string
)

func LoadConfig(path string) error {
	configMutex.Lock()
	defer configMutex.Unlock()

	ConfigPath = path
	file, err := os.Open(path)
	if err != nil {
		// Fallback defaults
		GlobalConfig = &AppConfig{
			DBPath:               "myrss.db",
			Port:                 8888,
			FetchIntervalMinutes: 15,
			Fulltext: FulltextConfig{
				MinTextChars: 200,
				Fetcher:      "trafilatura",
			},
		}
		return nil
	}
	defer file.Close()

	decoder := yaml.NewDecoder(file)
	cfg := &AppConfig{}
	if err := decoder.Decode(cfg); err != nil {
		return err
	}

	if cfg.DBPath == "" {
		cfg.DBPath = "myrss.db"
	}
	if cfg.Port == 0 {
		cfg.Port = 8888
	}
	if cfg.FetchIntervalMinutes == 0 {
		cfg.FetchIntervalMinutes = 15
	}
	if cfg.Fulltext.MinTextChars == 0 {
		cfg.Fulltext.MinTextChars = 200
	}
	if cfg.Classify.PromoteThreshold == 0 {
		cfg.Classify.PromoteThreshold = 5
	}

	GlobalConfig = cfg
	return nil
}

func SaveConfig() error {
	configMutex.Lock()
	defer configMutex.Unlock()

	if ConfigPath == "" {
		ConfigPath = "config.yaml"
	}

	dir := filepath.Dir(ConfigPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	file, err := os.Create(ConfigPath)
	if err != nil {
		return err
	}
	defer file.Close()

	encoder := yaml.NewEncoder(file)
	encoder.SetIndent(2)
	return encoder.Encode(GlobalConfig)
}

func GetDBPath() string {
	envDBPath := os.Getenv("DB_PATH")
	if envDBPath != "" {
		return envDBPath
	}
	configMutex.RLock()
	defer configMutex.RUnlock()
	return GlobalConfig.DBPath
}

func GetAIConfig(taskName string) AIConfigDetail {
	configMutex.RLock()
	defer configMutex.RUnlock()

	var taskCfg AIConfigDetail
	switch taskName {
	case "classify":
		taskCfg = GlobalConfig.AI.Tasks.Classify
	case "seed":
		taskCfg = GlobalConfig.AI.Tasks.Seed
	case "summary":
		taskCfg = GlobalConfig.AI.Tasks.Summary
	case "chat":
		taskCfg = GlobalConfig.AI.Tasks.Chat
	case "profile":
		taskCfg = GlobalConfig.AI.Tasks.Profile
	}

	def := GlobalConfig.AI.Default
	res := AIConfigDetail{
		BaseURL:        taskCfg.BaseURL,
		APIKey:         taskCfg.APIKey,
		Model:          taskCfg.Model,
		BatchSize:      taskCfg.BatchSize,
		MaxConcurrency: taskCfg.MaxConcurrency,
		MaxTokens:      taskCfg.MaxTokens,
	}

	if res.BaseURL == "" {
		res.BaseURL = def.BaseURL
	}
	if res.BaseURL == "" {
		res.BaseURL = "http://localhost:9999/v1"
	}
	if res.APIKey == "" {
		res.APIKey = def.APIKey
	}
	if res.Model == "" {
		res.Model = def.Model
	}
	if res.Model == "" {
		res.Model = "qwen-local"
	}
	if res.BatchSize == 0 {
		res.BatchSize = 25
	}
	if res.MaxConcurrency == 0 {
		res.MaxConcurrency = 2
	}

	return res
}
