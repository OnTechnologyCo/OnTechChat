package server

import (
	"errors"
	"net"
	"net/url"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

const aiProviderDefaultEffort = "provider_default"

var aiIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)
var aiEffortPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,31}$`)

type AIConfig struct {
	Enabled               bool               `json:"enabled"`
	DefaultModelID        string             `json:"default_model_id,omitempty"`
	RequestTimeoutSeconds int                `json:"request_timeout_seconds"`
	Concurrency           int                `json:"concurrency"`
	QueueLimit            int                `json:"queue_limit"`
	PerUserPerMinute      int                `json:"per_user_per_minute"`
	ContextMaxBytes       int                `json:"context_max_bytes"`
	SystemPrompt          string             `json:"system_prompt,omitempty"`
	Providers             []AIProviderConfig `json:"providers,omitempty"`
	Models                []AIModelConfig    `json:"models,omitempty"`
}
type AIProviderConfig struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	Type            string `json:"type"`
	Enabled         bool   `json:"enabled"`
	TimeoutSeconds  int    `json:"timeout_seconds"`
	Concurrency     int    `json:"concurrency"`
	BaseURL         string `json:"base_url,omitempty"`
	APIKeyEncrypted string `json:"api_key_encrypted,omitempty"`
	Executable      string `json:"executable,omitempty"`
	WorkDir         string `json:"work_dir,omitempty"`
	apiKey          string
	analysisOnly    bool
}

type AIModelConfig struct {
	ID                string   `json:"id"`
	ProviderID        string   `json:"provider_id"`
	Name              string   `json:"name"`
	Model             string   `json:"model"`
	Enabled           bool     `json:"enabled"`
	DefaultEffort     string   `json:"default_effort,omitempty"`
	AllowedEfforts    []string `json:"allowed_efforts,omitempty"`
	SupportedEfforts  []string `json:"supported_efforts,omitempty"`
	CapabilitySource  string   `json:"capability_source,omitempty"`
	CapabilityChecked int64    `json:"capability_checked_at,omitempty"`
}
type AISettings struct {
	Enabled               bool                 `json:"enabled"`
	DefaultModelID        string               `json:"default_model_id,omitempty"`
	RequestTimeoutSeconds int                  `json:"request_timeout_seconds"`
	Concurrency           int                  `json:"concurrency"`
	QueueLimit            int                  `json:"queue_limit"`
	PerUserPerMinute      int                  `json:"per_user_per_minute"`
	ContextMaxBytes       int                  `json:"context_max_bytes"`
	SystemPrompt          string               `json:"system_prompt,omitempty"`
	Providers             []AIProviderSettings `json:"providers,omitempty"`
	Models                []AIModelConfig      `json:"models,omitempty"`
}

type AIProviderSettings struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	Type           string `json:"type"`
	Enabled        bool   `json:"enabled"`
	TimeoutSeconds int    `json:"timeout_seconds"`
	Concurrency    int    `json:"concurrency"`
	BaseURL        string `json:"base_url,omitempty"`
	APIKey         string `json:"api_key,omitempty"`
	ClearAPIKey    bool   `json:"clear_api_key,omitempty"`
	HasAPIKey      bool   `json:"has_api_key"`
	Executable     string `json:"executable,omitempty"`
	WorkDir        string `json:"work_dir,omitempty"`
}

func defaultAIConfig() AIConfig {
	return AIConfig{
		RequestTimeoutSeconds: 120,
		Concurrency:           2,
		QueueLimit:            16,
		PerUserPerMinute:      12,
		ContextMaxBytes:       128 << 10,
		Providers: []AIProviderConfig{
			{ID: "openai", Name: "OpenAI 호환 API", Type: "openai_compatible", TimeoutSeconds: 120, Concurrency: 2, BaseURL: "https://api.openai.com/v1"},
			{ID: "claude-cli", Name: "Claude Code CLI", Type: "claude_cli", TimeoutSeconds: 180, Concurrency: 1, Executable: "claude"},
			{ID: "codex-cli", Name: "Codex CLI", Type: "codex_cli", TimeoutSeconds: 180, Concurrency: 1, Executable: "codex"},
		},
	}
}

func validateAIBaseURL(raw string) error {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return errors.New("AI HTTP endpoint must be an origin/path without credentials, query, or fragment")
	}
	if u.Scheme != "https" && u.Scheme != "http" {
		return errors.New("AI HTTP endpoint must use http or https")
	}
	if u.Scheme == "http" {
		host := u.Hostname()
		ip := net.ParseIP(host)
		if !strings.EqualFold(host, "localhost") && (ip == nil || !ip.IsLoopback()) {
			return errors.New("plaintext AI HTTP endpoint is allowed only on loopback")
		}
	}
	return nil
}

func normalizeAIEfforts(values []string) ([]string, error) {
	seen := map[string]bool{}
	out := make([]string, 0, len(values))
	for _, raw := range values {
		v := strings.ToLower(strings.TrimSpace(raw))
		if v == "" || v == aiProviderDefaultEffort {
			continue
		}
		if !aiEffortPattern.MatchString(v) {
			return nil, errors.New("invalid AI reasoning effort")
		}
		if !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	sort.Strings(out)
	return out, nil
}
func effectiveAIEfforts(m AIModelConfig) []string {
	supported := map[string]bool{}
	for _, v := range m.SupportedEfforts {
		supported[strings.ToLower(strings.TrimSpace(v))] = true
	}
	out := []string{aiProviderDefaultEffort}
	for _, v := range m.AllowedEfforts {
		v = strings.ToLower(strings.TrimSpace(v))
		if v != "" && v != aiProviderDefaultEffort && supported[v] {
			out = append(out, v)
		}
	}
	sort.Strings(out[1:])
	return out
}

func findAIProvider(ai AIConfig, id string) (AIProviderConfig, bool) {
	for _, p := range ai.Providers {
		if p.ID == id {
			return p, true
		}
	}
	return AIProviderConfig{}, false
}

func findAIModel(ai AIConfig, id string) (AIModelConfig, bool) {
	for _, m := range ai.Models {
		if m.ID == id {
			return m, true
		}
	}
	return AIModelConfig{}, false
}
func resolveAISelection(ai AIConfig, modelID, effort string) (AIProviderConfig, AIModelConfig, string, error) {
	if modelID == "" {
		modelID = ai.DefaultModelID
	}
	m, ok := findAIModel(ai, modelID)
	if !ok || !m.Enabled {
		return AIProviderConfig{}, AIModelConfig{}, "", bad("사용 가능한 AI 모델을 선택하세요.")
	}
	p, ok := findAIProvider(ai, m.ProviderID)
	if !ok || !p.Enabled {
		return AIProviderConfig{}, AIModelConfig{}, "", apiError{503, "선택한 AI 연결을 사용할 수 없습니다."}
	}
	if effort == "" {
		effort = m.DefaultEffort
	}
	if effort == "" {
		effort = aiProviderDefaultEffort
	}
	effort = strings.ToLower(strings.TrimSpace(effort))
	for _, allowed := range effectiveAIEfforts(m) {
		if effort == allowed {
			return p, m, effort, nil
		}
	}
	return AIProviderConfig{}, AIModelConfig{}, "", bad("선택한 모델에서 지원하지 않는 추론 설정입니다.")
}
func validateAIConfig(ai AIConfig) error {
	if ai.RequestTimeoutSeconds < 5 || ai.RequestTimeoutSeconds > 1800 ||
		ai.Concurrency < 1 || ai.Concurrency > 32 ||
		ai.QueueLimit < 1 || ai.QueueLimit > 200 ||
		ai.PerUserPerMinute < 1 || ai.PerUserPerMinute > 120 ||
		ai.ContextMaxBytes < 4096 || ai.ContextMaxBytes > 1<<20 ||
		len(ai.SystemPrompt) > 16<<10 {
		return errors.New("invalid AI limits")
	}
	providers := map[string]AIProviderConfig{}
	for _, p := range ai.Providers {
		p.ID = strings.TrimSpace(p.ID)
		if !aiIDPattern.MatchString(p.ID) || p.Name == "" || len(p.Name) > 80 ||
			p.TimeoutSeconds < 5 || p.TimeoutSeconds > 1800 || p.Concurrency < 1 || p.Concurrency > 16 {
			return errors.New("invalid AI provider")
		}
		switch p.Type {
		case "openai_compatible":
			if p.BaseURL == "" || validateAIBaseURL(p.BaseURL) != nil {
				return errors.New("invalid AI HTTP provider")
			}
			if p.Enabled && p.APIKeyEncrypted == "" && p.apiKey == "" {
				return errors.New("enabled AI HTTP provider requires API key")
			}
		case "claude_cli", "codex_cli":
			if strings.TrimSpace(p.Executable) == "" || strings.ContainsAny(p.Executable, "\r\n\x00") {
				return errors.New("AI CLI provider requires executable")
			}
			if p.WorkDir != "" && (!filepath.IsAbs(p.WorkDir) || len(p.WorkDir) > 1024 || strings.ContainsAny(p.WorkDir, "\r\n") || strings.IndexByte(p.WorkDir, 0) >= 0) {
				return errors.New("AI CLI work directory must be an absolute path")
			}
		default:
			return errors.New("unknown AI provider type")
		}
		if _, exists := providers[p.ID]; exists {
			return errors.New("duplicate AI provider id")
		}
		providers[p.ID] = p
	}
	models := map[string]bool{}
	for _, m := range ai.Models {
		if !aiIDPattern.MatchString(m.ID) || !aiIDPattern.MatchString(m.ProviderID) ||
			m.Name == "" || len(m.Name) > 80 || strings.TrimSpace(m.Model) == "" || len(m.Model) > 200 {
			return errors.New("invalid AI model")
		}
		if _, ok := providers[m.ProviderID]; !ok {
			return errors.New("AI model references missing provider")
		}
		if models[m.ID] {
			return errors.New("duplicate AI model id")
		}
		models[m.ID] = true
		allowed, err := normalizeAIEfforts(m.AllowedEfforts)
		if err != nil {
			return err
		}
		supported, err := normalizeAIEfforts(m.SupportedEfforts)
		if err != nil {
			return err
		}
		m.AllowedEfforts, m.SupportedEfforts = allowed, supported
		if m.DefaultEffort == "" {
			m.DefaultEffort = aiProviderDefaultEffort
		}
		validDefault := false
		for _, v := range effectiveAIEfforts(m) {
			if v == m.DefaultEffort {
				validDefault = true
				break
			}
		}
		if !validDefault {
			return errors.New("AI model default reasoning effort is unsupported")
		}
	}
	if ai.Enabled {
		if ai.DefaultModelID == "" {
			return errors.New("AI default model required")
		}
		p, m, _, err := resolveAISelection(ai, ai.DefaultModelID, "")
		if err != nil || !p.Enabled || !m.Enabled {
			return errors.New("AI default model is unavailable")
		}
	}
	return nil
}

func aiSettingsView(ai AIConfig) AISettings {
	out := AISettings{
		Enabled: ai.Enabled, DefaultModelID: ai.DefaultModelID,
		RequestTimeoutSeconds: ai.RequestTimeoutSeconds, Concurrency: ai.Concurrency,
		QueueLimit: ai.QueueLimit, PerUserPerMinute: ai.PerUserPerMinute,
		ContextMaxBytes: ai.ContextMaxBytes, SystemPrompt: ai.SystemPrompt,
		Models: append([]AIModelConfig(nil), ai.Models...),
	}
	for _, p := range ai.Providers {
		out.Providers = append(out.Providers, AIProviderSettings{
			ID: p.ID, Name: p.Name, Type: p.Type, Enabled: p.Enabled,
			TimeoutSeconds: p.TimeoutSeconds, Concurrency: p.Concurrency,
			BaseURL: p.BaseURL, Executable: p.Executable, WorkDir: p.WorkDir,
			HasAPIKey: p.APIKeyEncrypted != "",
		})
	}
	return out
}

func (a *App) candidateAISettings(old AIConfig, in AISettings) (AIConfig, error) {
	next := AIConfig{
		Enabled: in.Enabled, DefaultModelID: strings.TrimSpace(in.DefaultModelID),
		RequestTimeoutSeconds: in.RequestTimeoutSeconds, Concurrency: in.Concurrency,
		QueueLimit: in.QueueLimit, PerUserPerMinute: in.PerUserPerMinute,
		ContextMaxBytes: in.ContextMaxBytes, SystemPrompt: in.SystemPrompt,
		Models: append([]AIModelConfig(nil), in.Models...),
	}
	oldProviders := map[string]AIProviderConfig{}
	for _, p := range old.Providers {
		oldProviders[p.ID] = p
	}
	for _, p := range in.Providers {
		p.ID = strings.TrimSpace(p.ID)
		p.Name = strings.TrimSpace(p.Name)
		p.Type = strings.TrimSpace(p.Type)
		if p.ClearAPIKey && p.APIKey != "" {
			return old, bad("AI API 키 삭제와 새 키 입력을 동시에 할 수 없습니다.")
		}
		cfg := AIProviderConfig{
			ID: p.ID, Name: p.Name, Type: p.Type, Enabled: p.Enabled,
			TimeoutSeconds: p.TimeoutSeconds, Concurrency: p.Concurrency,
			BaseURL: strings.TrimSpace(p.BaseURL), Executable: strings.TrimSpace(p.Executable),
			WorkDir: strings.TrimSpace(p.WorkDir),
		}
		if prev, ok := oldProviders[p.ID]; ok {
			if prev.Type != cfg.Type {
				return old, bad("기존 AI 연결의 종류는 같은 ID로 변경할 수 없습니다.")
			}
			cfg.APIKeyEncrypted = prev.APIKeyEncrypted
			cfg.apiKey = prev.apiKey
		}
		if p.ClearAPIKey {
			cfg.APIKeyEncrypted, cfg.apiKey = "", ""
		}
		if p.APIKey != "" {
			if len(p.APIKey) > 8192 || strings.ContainsAny(p.APIKey, "\r\n\x00") {
				return old, bad("AI API 키 형식을 확인하세요.")
			}
			enc, err := sealSetting(a.store, "ai-provider-key/"+cfg.ID, []byte(p.APIKey))
			if err != nil {
				return old, err
			}
			cfg.APIKeyEncrypted, cfg.apiKey = enc, p.APIKey
		}
		next.Providers = append(next.Providers, cfg)
	}
	for i := range next.Models {
		next.Models[i].ID = strings.TrimSpace(next.Models[i].ID)
		next.Models[i].ProviderID = strings.TrimSpace(next.Models[i].ProviderID)
		next.Models[i].Name = strings.TrimSpace(next.Models[i].Name)
		next.Models[i].Model = strings.TrimSpace(next.Models[i].Model)
		next.Models[i].DefaultEffort = strings.ToLower(strings.TrimSpace(next.Models[i].DefaultEffort))
		allowed, err := normalizeAIEfforts(next.Models[i].AllowedEfforts)
		if err != nil {
			return old, bad("AI 모델의 사용자 추론 설정을 확인하세요.")
		}
		supported, err := normalizeAIEfforts(next.Models[i].SupportedEfforts)
		if err != nil {
			return old, bad("AI 모델의 지원 추론 설정을 확인하세요.")
		}
		next.Models[i].AllowedEfforts = allowed
		next.Models[i].SupportedEfforts = supported
		if next.Models[i].DefaultEffort == "" {
			next.Models[i].DefaultEffort = aiProviderDefaultEffort
		}
	}
	if err := validateAIConfig(next); err != nil {
		return old, bad("AI 설정값을 확인하세요: " + err.Error())
	}
	return next, nil
}
