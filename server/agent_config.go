package server

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type AgentConfig struct {
	Enabled            bool   `json:"enabled"`
	WorkspaceRoot      string `json:"workspace_root,omitempty"`
	GoRoot             string `json:"go_root,omitempty"`
	Concurrency        int    `json:"concurrency"`
	QueueLimit         int    `json:"queue_limit"`
	PerUserConcurrency int    `json:"per_user_concurrency"`
	ProcessLimit       int    `json:"process_limit"`
	MemoryMB           int    `json:"memory_mb"`
	TimeoutSeconds     int    `json:"timeout_seconds"`
	OutputMaxBytes     int    `json:"output_max_bytes"`
	ProjectQuotaBytes  int64  `json:"project_quota_bytes"`
	NetworkPolicy      string `json:"network_policy"`
	WindowsIsolation   string `json:"windows_isolation"`
}

type AgentSettings struct {
	Enabled            bool   `json:"enabled"`
	WorkspaceRoot      string `json:"workspace_root,omitempty"`
	GoRoot             string `json:"go_root,omitempty"`
	Concurrency        int    `json:"concurrency"`
	QueueLimit         int    `json:"queue_limit"`
	PerUserConcurrency int    `json:"per_user_concurrency"`
	ProcessLimit       int    `json:"process_limit"`
	MemoryMB           int    `json:"memory_mb"`
	TimeoutSeconds     int    `json:"timeout_seconds"`
	OutputMaxBytes     int    `json:"output_max_bytes"`
	ProjectQuotaBytes  int64  `json:"project_quota_bytes"`
	NetworkPolicy      string `json:"network_policy"`
	WindowsIsolation   string `json:"windows_isolation"`
}

func defaultAgentConfig() AgentConfig {
	return AgentConfig{
		Enabled: true, Concurrency: 2, QueueLimit: 16, PerUserConcurrency: 1,
		ProcessLimit: 8, MemoryMB: 512, TimeoutSeconds: 300,
		OutputMaxBytes: 1 << 20, ProjectQuotaBytes: 512 << 20,
		NetworkPolicy: "blocked", WindowsIsolation: "appcontainer",
	}
}

func validateAgentConfig(c AgentConfig) error {
	if c.Concurrency < 1 || c.Concurrency > 16 ||
		c.QueueLimit < 1 || c.QueueLimit > 200 ||
		c.PerUserConcurrency < 1 || c.PerUserConcurrency > 4 ||
		c.ProcessLimit < 1 || c.ProcessLimit > 32 ||
		c.MemoryMB < 64 || c.MemoryMB > 8192 ||
		c.TimeoutSeconds < 5 || c.TimeoutSeconds > 3600 ||
		c.OutputMaxBytes < 4096 || c.OutputMaxBytes > 16<<20 ||
		c.ProjectQuotaBytes < 1<<20 || c.ProjectQuotaBytes > 50<<30 {
		return errors.New("invalid agent limits")
	}
	if c.NetworkPolicy != "blocked" {
		return errors.New("agent network policy must be blocked")
	}
	if c.WindowsIsolation != "appcontainer" {
		return errors.New("unsupported agent isolation")
	}
	if c.WorkspaceRoot != "" {
		if !filepath.IsAbs(c.WorkspaceRoot) || len(c.WorkspaceRoot) > 1024 || strings.ContainsAny(c.WorkspaceRoot, "\r\n\x00") {
			return errors.New("agent workspace_root must be an absolute path")
		}
	}
	if c.GoRoot != "" {
		if !filepath.IsAbs(c.GoRoot) || len(c.GoRoot) > 1024 || strings.ContainsAny(c.GoRoot, "\r\n\x00") {
			return errors.New("agent go_root must be an absolute path")
		}
	}
	return nil
}

func agentSettingsView(c AgentConfig) AgentSettings {
	return AgentSettings{
		Enabled: c.Enabled, WorkspaceRoot: c.WorkspaceRoot, GoRoot: c.GoRoot, Concurrency: c.Concurrency,
		QueueLimit: c.QueueLimit, PerUserConcurrency: c.PerUserConcurrency,
		ProcessLimit: c.ProcessLimit, MemoryMB: c.MemoryMB, TimeoutSeconds: c.TimeoutSeconds,
		OutputMaxBytes: c.OutputMaxBytes, ProjectQuotaBytes: c.ProjectQuotaBytes,
		NetworkPolicy: c.NetworkPolicy, WindowsIsolation: c.WindowsIsolation,
	}
}

func candidateAgentSettings(old AgentConfig, in AgentSettings) (AgentConfig, error) {
	c := old
	c.Enabled = in.Enabled
	c.WorkspaceRoot = strings.TrimSpace(in.WorkspaceRoot)
	c.GoRoot = strings.TrimSpace(in.GoRoot)
	c.Concurrency = in.Concurrency
	c.QueueLimit = in.QueueLimit
	c.PerUserConcurrency = in.PerUserConcurrency
	c.ProcessLimit = in.ProcessLimit
	c.MemoryMB = in.MemoryMB
	c.TimeoutSeconds = in.TimeoutSeconds
	c.OutputMaxBytes = in.OutputMaxBytes
	c.ProjectQuotaBytes = in.ProjectQuotaBytes
	c.NetworkPolicy = strings.TrimSpace(in.NetworkPolicy)
	c.WindowsIsolation = strings.TrimSpace(in.WindowsIsolation)
	if err := validateAgentConfig(c); err != nil {
		return old, bad("에이전트 설정값을 확인하세요: " + err.Error())
	}
	return c, nil
}

func (a *App) agentWorkspaceRoot() string {
	return filepath.Join(a.cfg.DataDir, "workspaces")
}

func (a *App) agentGoRoot() string {
	if configured := strings.TrimSpace(a.cfg.Agent.GoRoot); configured != "" {
		if info, err := os.Stat(filepath.Join(configured, "bin", "go.exe")); err == nil && !info.IsDir() {
			return configured
		}
	}
	for _, name := range []string{"go.exe", "go"} {
		if found, err := exec.LookPath(name); err == nil {
			root := filepath.Dir(filepath.Dir(found))
			if info, e := os.Stat(filepath.Join(root, "bin", "go.exe")); e == nil && !info.IsDir() {
				return root
			}
		}
	}
	for _, root := range []string{filepath.Join(os.Getenv("ProgramFiles"), "Go"), filepath.Join(os.Getenv("ProgramFiles(x86)"), "Go")} {
		if info, err := os.Stat(filepath.Join(root, "bin", "go.exe")); err == nil && !info.IsDir() {
			return root
		}
	}
	return ""
}
