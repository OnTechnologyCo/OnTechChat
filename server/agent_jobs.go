package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	agentQueued      = "queued"
	agentPlanning    = "planning"
	agentRunning     = "running"
	agentCompleted   = "completed"
	agentFailed      = "failed"
	agentCancelled   = "cancelled"
	agentInterrupted = "interrupted"
)

type AgentJob struct {
	ID             string                     `json:"id"`
	OwnerID        string                     `json:"owner_id"`
	RoomID         string                     `json:"room_id"`
	ProjectID      string                     `json:"project_id"`
	ClientID       string                     `json:"client_id"`
	RetryOf        string                     `json:"retry_of,omitempty"`
	SourceMessage  int64                      `json:"source_message_id,omitempty"`
	AnswerMessage  int64                      `json:"answer_message_id,omitempty"`
	Mode           string                     `json:"mode"`
	Status         string                     `json:"status"`
	ProviderID     string                     `json:"provider_id"`
	ModelID        string                     `json:"model_id"`
	Model          string                     `json:"model"`
	Effort         string                     `json:"effort"`
	SentEffort     string                     `json:"sent_effort,omitempty"`
	ObservedModel  string                     `json:"observed_model,omitempty"`
	RequestText    string                     `json:"request_text,omitempty"`
	PolicyRevision uint64                     `json:"policy_revision"`
	ErrorCode      string                     `json:"error_code,omitempty"`
	Error          string                     `json:"error,omitempty"`
	CreatedAt      int64                      `json:"created_at"`
	StartedAt      int64                      `json:"started_at,omitempty"`
	FinishedAt     int64                      `json:"finished_at,omitempty"`
	Turns          int                        `json:"turns,omitempty"`
	ToolCount      int                        `json:"tool_count,omitempty"`
	ArtifactIDs    []string                   `json:"artifact_ids,omitempty"`
	ToolResults    map[string]AgentToolResult `json:"tool_results,omitempty"`
	Log            []string                   `json:"log,omitempty"`
}
type AgentArtifact struct {
	ID           string `json:"id"`
	OwnerID      string `json:"owner_id"`
	ProjectID    string `json:"project_id"`
	JobID        string `json:"job_id"`
	RelativePath string `json:"relative_path"`
	Name         string `json:"name"`
	MIME         string `json:"mime"`
	Size         int64  `json:"size"`
	SHA256       string `json:"sha256"`
	CreatedAt    int64  `json:"created_at"`
}

type agentJobManager struct {
	app            *App
	wakeCh         chan struct{}
	mu             sync.Mutex
	running        int
	runningUser    map[string]int
	runningProject map[string]bool
	cancels        map[string]context.CancelFunc
	jobs           sync.WaitGroup
}

func newAgentJobManager(a *App) *agentJobManager {
	return &agentJobManager{
		app: a, wakeCh: make(chan struct{}, 1),
		runningUser: map[string]int{}, runningProject: map[string]bool{}, cancels: map[string]context.CancelFunc{},
	}
}
func (m *agentJobManager) wake() {
	select {
	case m.wakeCh <- struct{}{}:
	default:
	}
}

func (m *agentJobManager) run(ctx context.Context) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	defer m.cancelAll()
	m.wake()
	for {
		select {
		case <-ctx.Done():
			return
		case <-m.wakeCh:
			m.dispatch()
		case <-ticker.C:
			m.dispatch()
		}
	}
}

func (m *agentJobManager) wait() { m.jobs.Wait() }

func (m *agentJobManager) cancelAll() {
	m.mu.Lock()
	cancels := make([]context.CancelFunc, 0, len(m.cancels))
	for _, cancel := range m.cancels {
		cancels = append(cancels, cancel)
	}
	m.mu.Unlock()
	for _, cancel := range cancels {
		cancel()
	}
}
func (m *agentJobManager) queued() []AgentJob {
	out := []AgentJob{}
	_ = m.app.store.view(func(st *State) error {
		for _, q := range st.AgentJobs {
			if q.Status == agentQueued {
				out = append(out, q)
			}
		}
		return nil
	})
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt == out[j].CreatedAt {
			return out[i].ID < out[j].ID
		}
		return out[i].CreatedAt < out[j].CreatedAt
	})
	return out
}

func (m *agentJobManager) capacity(owner string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.running < m.app.cfg.Agent.Concurrency &&
		m.runningUser[owner] < m.app.cfg.Agent.PerUserConcurrency
}

func (m *agentJobManager) dispatch() {
	if m.app.agentPlanner == nil || m.app.agentRunner == nil {
		return
	}
	for {
		list := m.queued()
		if len(list) == 0 {
			return
		}
		progress := false
		for _, q := range list {
			if !m.capacity(q.OwnerID) {
				continue
			}
			if m.start(q) {
				progress = true
				break
			}
		}
		if !progress {
			return
		}
	}
}

func (m *agentJobManager) start(q AgentJob) bool {
	started := false
	err := m.app.store.update(func(st *State) error {
		current, ok := st.AgentJobs[q.ID]
		if !ok || current.Status != agentQueued {
			return errUnchanged
		}
		u, uok := st.Users[current.OwnerID]
		p, pok := st.AgentProjects[current.ProjectID]
		if !uok || !pok || !canUseAgentConfig(m.app, u) ||
			!canAccessProject(st, u, p, "project.run") {
			current.Status = agentFailed
			current.ErrorCode = "AGENT_PERMISSION_REVOKED"
			current.Error = "작업 권한이 없어 실행하지 않았습니다."
			current.FinishedAt = now()
			st.AgentJobs[current.ID] = current
			return nil
		}
		current.Status = agentPlanning
		current.StartedAt = now()
		st.AgentJobs[current.ID] = current
		q = current
		started = true
		return nil
	})
	if err != nil || !started {
		return false
	}
	ctx, cancel := context.WithTimeout(m.app.ctx, time.Duration(m.app.cfg.Agent.TimeoutSeconds)*time.Second)
	m.mu.Lock()
	m.running++
	m.runningUser[q.OwnerID]++
	m.cancels[q.ID] = cancel
	m.mu.Unlock()
	m.jobs.Add(1)
	go m.execute(ctx, q)
	m.app.changed(q.RoomID)
	return true
}

func (m *agentJobManager) release(q AgentJob) {
	m.mu.Lock()
	if cancel := m.cancels[q.ID]; cancel != nil {
		cancel()
		delete(m.cancels, q.ID)
	}
	if m.running > 0 {
		m.running--
	}
	if m.runningUser[q.OwnerID] > 0 {
		m.runningUser[q.OwnerID]--
	}
	m.mu.Unlock()
	m.jobs.Done()
	m.wake()
}

func (m *agentJobManager) cancelProject(projectID string) {
	m.cancelMatching(func(q AgentJob) bool { return q.ProjectID == projectID })
}
func (m *agentJobManager) cancelUnauthorized() {
	m.cancelMatching(func(q AgentJob) bool {
		var allowed bool
		_ = m.app.store.view(func(st *State) error {
			u, uok := st.Users[q.OwnerID]
			p, pok := st.AgentProjects[q.ProjectID]
			allowed = uok && pok && canUseAgentConfig(m.app, u) &&
				canAccessProject(st, u, p, "project.run")
			return nil
		})
		return !allowed
	})
}

func (m *agentJobManager) cancelMatching(match func(AgentJob) bool) {
	var ids []string
	_ = m.app.store.view(func(st *State) error {
		for id, q := range st.AgentJobs {
			if (q.Status == agentQueued || q.Status == agentPlanning || q.Status == agentRunning) && match(q) {
				ids = append(ids, id)
			}
		}
		return nil
	})
	for _, id := range ids {
		m.cancel(id)
	}
}

func (m *agentJobManager) cancel(id string) {
	m.mu.Lock()
	cancel := m.cancels[id]
	m.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	var roomID, ownerID string
	_ = m.app.store.update(func(st *State) error {
		q, ok := st.AgentJobs[id]
		if !ok || (q.Status != agentQueued && q.Status != agentPlanning && q.Status != agentRunning) {
			return errUnchanged
		}
		q.Status = agentCancelled
		q.ErrorCode = "AGENT_CANCELLED"
		q.Error = "작업을 중지했습니다."
		q.FinishedAt = now()
		st.AgentJobs[id] = q
		roomID, ownerID = q.RoomID, q.OwnerID
		appendAudit(st, AuditEvent{
			ActorID: q.OwnerID, Action: "agent.cancel",
			ResourceType: "project", ResourceID: q.ProjectID,
			JobID: q.ID, Result: "allowed",
		})
		return nil
	})
	if roomID != "" {
		m.app.changed(roomID, ownerID)
	}
}

func interruptAgentJobs(s *Store) error {
	return s.update(func(st *State) error {
		changed := false
		for id, q := range st.AgentJobs {
			if q.Status != agentQueued && q.Status != agentPlanning && q.Status != agentRunning {
				continue
			}
			q.Status = agentInterrupted
			q.ErrorCode = "AGENT_SERVER_RESTARTED"
			q.Error = "서버가 다시 시작되어 작업이 중단되었습니다."
			q.FinishedAt = now()
			st.AgentJobs[id] = q
			changed = true
		}
		if !changed {
			return errUnchanged
		}
		return nil
	})
}

func publicAgentJob(q AgentJob) map[string]any {
	return map[string]any{
		"id": q.ID, "room_id": q.RoomID, "project_id": q.ProjectID,
		"status": q.Status, "mode": q.Mode, "model_id": q.ModelID,
		"effort": q.Effort, "error_code": q.ErrorCode, "error": q.Error,
		"created_at": q.CreatedAt, "started_at": q.StartedAt, "finished_at": q.FinishedAt,
		"turns": q.Turns, "tool_count": q.ToolCount,
		"artifact_ids": q.ArtifactIDs, "log": q.Log,
	}
}

func agentTerminal(status string) bool {
	switch status {
	case agentCompleted, agentFailed, agentCancelled, agentInterrupted:
		return true
	default:
		return false
	}
}
func buildAgentConversation(st *State, q AgentJob, maxBytes int) []AIWireMessage {
	if maxBytes < 4096 {
		maxBytes = 4096
	}
	items := []AIWireMessage{}
	used := 0
	for i := len(st.Messages) - 1; i >= 0; i-- {
		msg := st.Messages[i]
		if msg.RoomID != q.RoomID || msg.ID >= q.SourceMessage || msg.Kind != "text" {
			continue
		}
		role := ""
		switch {
		case msg.UserID == q.OwnerID:
			role = "user"
		case msg.UserID == "":
			role = "assistant"
		default:
			continue
		}
		if msg.Text == "" {
			continue
		}
		size := len(msg.Text) + 32
		if used+size > maxBytes {
			break
		}
		items = append(items, AIWireMessage{Role: role, Content: msg.Text})
		used += size
		if len(items) >= 24 {
			break
		}
	}
	for i, j := 0, len(items)-1; i < j; i, j = i+1, j-1 {
		items[i], items[j] = items[j], items[i]
	}
	return items
}

func (m *agentJobManager) execute(ctx context.Context, q AgentJob) {
	defer m.release(q)
	var project AgentProject
	var provider AIProviderConfig
	var model AIModelConfig
	var currentUser User
	var conversation []AIWireMessage
	err := m.app.store.view(func(st *State) error {
		current, ok := st.AgentJobs[q.ID]
		if !ok || current.Status != agentPlanning {
			return errUnchanged
		}
		u, uok := st.Users[current.OwnerID]
		p, pok := st.AgentProjects[current.ProjectID]
		if !uok || !pok || !canUseAgentConfig(m.app, u) ||
			!canAccessProject(st, u, p, "project.run") {
			return AIExecutionError{Code: "AGENT_PERMISSION_REVOKED", Public: "작업 권한이 회수되었습니다."}
		}
		currentUser = u
		project = p
		conversation = buildAgentConversation(st, current, m.app.cfg.AI.ContextMaxBytes/2)
		var okp, okm bool
		provider, okp = findAIProvider(m.app.cfg.AI, current.ProviderID)
		model, okm = findAIModel(m.app.cfg.AI, current.ModelID)
		if !okp || !okm || model.Model != current.Model || (provider.Type != "claude_cli" && provider.Type != "codex_cli") {
			return AIExecutionError{Code: "AGENT_CONFIGURATION_CHANGED", Public: "에이전트 AI 설정이 변경되었습니다."}
		}
		return nil
	})
	if err != nil {
		if err != errUnchanged {
			m.finishError(q, err)
		}
		return
	}
	media, err := m.app.prepareAIMedia(q.OwnerID, q.RoomID, q.SourceMessage)
	if err != nil {
		m.finishError(q, err)
		return
	}
	defer os.RemoveAll(media.Dir)
	ctx = withAIMedia(ctx, media)
	q.RequestText += media.Text
	root, err := m.app.projectRoot(project)
	if err != nil {
		m.finishError(q, err)
		return
	}
	if err = m.app.ensureAgentPolicyFiles(currentUser, project); err != nil {
		m.finishError(q, AIExecutionError{Code: "AGENT_POLICY_WRITE_FAILED", Public: "사용자 Agent 정책 파일을 준비하지 못했습니다."})
		return
	}
	policy, err := readWorkspaceFile(root, "AGENT.md", 64<<10)
	if err != nil {
		m.finishError(q, AIExecutionError{Code: "AGENT_POLICY_READ_FAILED", Public: "사용자 Agent 정책 파일을 읽지 못했습니다."})
		return
	}
	files, err := listWorkspace(root)
	if err != nil {
		files = nil
	}
	results := []AgentToolResult{}
	for turnNo := 1; turnNo <= 6; turnNo++ {
		if err = m.checkPermission(q); err != nil {
			m.finishError(q, err)
			return
		}
		m.updateProgress(q.ID, agentPlanning, turnNo, len(results), "AI가 다음 작업을 계획하고 있습니다.")
		programs := []AgentProgramToolInfo{}
		_ = m.app.store.view(func(st *State) error {
			if currentUser, ok := st.Users[q.OwnerID]; ok {
				programs = agentProgramCatalog(st, currentUser)
			}
			return nil
		})
		turn, planErr := m.app.agentPlanner.Next(ctx, provider, model, q.Effort, AgentPlannerInput{
			Request: q.RequestText, ProjectName: project.Name, Policy: policy,
			Conversation: conversation, Files: files, Programs: programs,
			RemoteControl: m.remoteControlAvailable(q.OwnerID),
			Results:       results, Turn: turnNo, WorkDir: root,
		})
		if planErr != nil {
			m.finishError(q, planErr)
			return
		}
		if len(turn.ToolCalls) == 0 {
			final := strings.TrimSpace(turn.Final)
			if final == "" {
				m.finishError(q, AIExecutionError{Code: "AGENT_EMPTY_FINAL", Public: "AI가 작업 결과를 반환하지 않았습니다."})
				return
			}
			if err = m.finishSuccess(q, final, media); err != nil {
				m.finishError(q, AIExecutionError{Code: "AGENT_STORAGE_FAILED", Public: "작업 결과를 저장하지 못했습니다."})
			}
			return
		}
		if len(results)+len(turn.ToolCalls) > 12 {
			m.finishError(q, AIExecutionError{Code: "AGENT_TOOL_LIMIT", Public: "작업 도구 호출 한도를 초과했습니다."})
			return
		}
		m.updateProgress(q.ID, agentRunning, turnNo, len(results), "허용된 도구를 실행하고 있습니다.")
		for _, call := range turn.ToolCalls {
			if err = m.checkPermission(q); err != nil {
				m.finishError(q, err)
				return
			}
			if prior, ok := m.savedToolResult(q.ID, call.ID); ok {
				results = append(results, prior)
				continue
			}
			result := m.executeTool(ctx, q, project, call)
			results = append(results, result)
			if err = m.saveToolResult(q.ID, call, result); err != nil {
				m.finishError(q, AIExecutionError{Code: "AGENT_STORAGE_FAILED", Public: "도구 실행 결과를 저장하지 못했습니다."})
				return
			}
			if ctx.Err() != nil {
				m.finishError(q, ctx.Err())
				return
			}
		}
		files, _ = listWorkspace(root)
	}
	m.finishError(q, AIExecutionError{Code: "AGENT_TURN_LIMIT", Public: "AI 작업 단계 한도를 초과했습니다."})
}

func (m *agentJobManager) checkPermission(q AgentJob) error {
	return m.app.store.view(func(st *State) error {
		current, ok := st.AgentJobs[q.ID]
		if !ok || (current.Status != agentPlanning && current.Status != agentRunning) {
			return context.Canceled
		}
		u, uok := st.Users[q.OwnerID]
		p, pok := st.AgentProjects[q.ProjectID]
		if !uok || !pok || !canUseAgentConfig(m.app, u) ||
			!canAccessProject(st, u, p, "project.run") {
			return AIExecutionError{Code: "AGENT_PERMISSION_REVOKED", Public: "작업 권한이 회수되었습니다."}
		}
		return nil
	})
}
func (m *agentJobManager) updateProgress(id, status string, turns, tools int, note string) {
	_ = m.app.store.update(func(st *State) error {
		q, ok := st.AgentJobs[id]
		if !ok || agentTerminal(q.Status) {
			return errUnchanged
		}
		q.Status = status
		q.Turns = turns
		q.ToolCount = tools
		if note != "" {
			q.Log = appendBoundedAgentLog(q.Log, note)
		}
		st.AgentJobs[id] = q
		return nil
	})
}

func appendBoundedAgentLog(log []string, line string) []string {
	line = strings.TrimSpace(line)
	if line == "" {
		return log
	}
	runes := []rune(line)
	if len(runes) > 400 {
		line = string(runes[:400]) + "…"
	}
	log = append(log, line)
	if len(log) > 40 {
		log = append([]string(nil), log[len(log)-40:]...)
	}
	return log
}

func (m *agentJobManager) savedToolResult(jobID, toolID string) (AgentToolResult, bool) {
	var result AgentToolResult
	var ok bool
	_ = m.app.store.view(func(st *State) error {
		if q, exists := st.AgentJobs[jobID]; exists && q.ToolResults != nil {
			result, ok = q.ToolResults[toolID]
		}
		return nil
	})
	return result, ok
}
func (m *agentJobManager) saveToolResult(jobID string, call AgentToolCall, result AgentToolResult) error {
	return m.app.store.update(func(st *State) error {
		q, ok := st.AgentJobs[jobID]
		if !ok || agentTerminal(q.Status) {
			return errUnchanged
		}
		if q.ToolResults == nil {
			q.ToolResults = map[string]AgentToolResult{}
		}
		if previous, exists := q.ToolResults[call.ID]; exists {
			if previous.Name != call.Name {
				return apiError{409, "같은 tool_call_id를 다른 도구에 재사용할 수 없습니다."}
			}
			return errUnchanged
		}
		q.ToolResults[call.ID] = result
		q.ToolCount = len(q.ToolResults)
		summary := call.Name + ": " + result.Summary
		q.Log = appendBoundedAgentLog(q.Log, summary)
		st.AgentJobs[jobID] = q
		appendAudit(st, AuditEvent{
			ActorID: q.OwnerID, Action: "agent.tool." + call.Name,
			ResourceType: "project", ResourceID: q.ProjectID,
			JobID: q.ID, Result: func() string {
				if result.OK {
					return "allowed"
				}
				return "failed"
			}(),
		})
		return nil
	})
}

func validateAgentToolCall(call AgentToolCall) error {
	if strings.HasPrefix(call.Name, "remotecontrol.") {
		return validateRemoteControlCall(call)
	}
	switch call.Name {
	case "workspace.list":
		if call.Path != "" || call.Content != "" || len(call.Args) != 0 || call.Output != "" || call.ProgramID != "" || call.Action != "" {
			return bad("workspace.list 인자가 올바르지 않습니다.")
		}
	case "workspace.read":
		if call.Path == "" || call.Content != "" || len(call.Args) != 0 || call.Output != "" || call.ProgramID != "" || call.Action != "" {
			return bad("workspace.read 인자가 올바르지 않습니다.")
		}
	case "workspace.write":
		if call.Path == "" || len(call.Args) != 0 || call.Output != "" || call.ProgramID != "" || call.Action != "" {
			return bad("workspace.write 인자가 올바르지 않습니다.")
		}
		if managedAgentPolicyPath(call.Path) {
			return forbidden()
		}
	case "build.run":
		if call.Path != "" || call.Content != "" || len(call.Args) != 0 || call.ProgramID != "" || call.Action != "" {
			return bad("build.run 인자가 올바르지 않습니다.")
		}
	case "program.run":
		if call.Path == "" || call.Content != "" || call.Output != "" || call.ProgramID != "" || call.Action != "" || !validProgramArgs(call.Args) {
			return bad("program.run 인자가 올바르지 않습니다.")
		}
	case "host_program.action":
		if call.ProgramID == "" || call.Action == "" || call.Path != "" || call.Content != "" || call.Output != "" || len(call.Args) != 0 {
			return bad("host_program.action 인자가 올바르지 않습니다.")
		}
	default:
		return bad("허용되지 않은 도구입니다.")
	}
	return nil
}

func (m *agentJobManager) executeTool(ctx context.Context, q AgentJob, project AgentProject, call AgentToolCall) AgentToolResult {
	result := AgentToolResult{ID: call.ID, Name: call.Name}
	failResult := func(summary string) AgentToolResult {
		result.OK = false
		result.Summary = summary
		return result
	}
	if err := validateAgentToolCall(call); err != nil {
		return failResult(publicAgentError(err))
	}
	if strings.HasPrefix(call.Name, "remotecontrol.") {
		return m.executeRemoteControl(ctx, q, call)
	}
	root, err := m.app.projectRoot(project)
	if err != nil {
		return failResult("프로젝트 경로를 확인하지 못했습니다.")
	}
	switch call.Name {
	case "workspace.list":
		files, e := listWorkspace(root)
		if e != nil {
			return failResult("프로젝트 파일 목록을 읽지 못했습니다.")
		}
		result.OK = true
		result.Summary = fmt.Sprintf("파일 %d개", len(files))
		result.Output = strings.Join(files, "\n")
		return result
	case "workspace.read":
		text, e := readWorkspaceFile(root, call.Path, 256<<10)
		if e != nil {
			return failResult("요청한 파일을 읽지 못했습니다.")
		}
		result.OK = true
		result.Summary = "파일 읽기 완료"
		result.Output = text
		return result
	case "workspace.write":
		if call.Path == "" {
			return failResult("파일 경로가 없습니다.")
		}
		if e := ensureProjectWriteQuota(root, project, call.Path, int64(len(call.Content))); e != nil {
			return failResult(e.Error())
		}
		if e := writeWorkspaceFile(root, call.Path, call.Content, 256<<10); e != nil {
			return failResult("파일 작성에 실패했습니다: " + publicAgentError(e))
		}
		result.OK = true
		result.Summary = "파일 작성 완료: " + filepath.ToSlash(call.Path)
		return result
	case "build.run":
		return m.runGoBuild(ctx, q, project, call)
	case "program.run":
		return m.runProjectProgram(ctx, q, project, call)
	case "host_program.action":
		res, e := m.app.executeAgentHostProgram(q, call)
		if e != nil {
			return failResult(publicAgentError(e))
		}
		return res
	default:
		return failResult("허용되지 않은 도구입니다.")
	}
}
func ensureProjectWriteQuota(root string, project AgentProject, rel string, newBytes int64) error {
	current, err := workspaceSize(root, project.QuotaBytes)
	if err != nil {
		return err
	}
	var old int64
	if path, e := resolveAgentPath(root, rel, true); e == nil {
		if info, e := os.Stat(path); e == nil && info.Mode().IsRegular() {
			old = info.Size()
		}
	}
	if current-old+newBytes > project.QuotaBytes {
		return bad("프로젝트 저장 용량 한도를 초과합니다.")
	}
	return nil
}

func publicAgentError(err error) string {
	var ae apiError
	if errors.As(err, &ae) {
		return ae.message
	}
	var ex AIExecutionError
	if errors.As(err, &ex) {
		return ex.Public
	}
	return "격리 실행 오류"
}

func (m *agentJobManager) runGoBuild(ctx context.Context, q AgentJob, project AgentProject, call AgentToolCall) AgentToolResult {
	result := AgentToolResult{ID: call.ID, Name: call.Name}
	root, err := m.app.projectRoot(project)
	if err != nil {
		result.Summary = "프로젝트 경로 오류"
		return result
	}
	goroot := m.app.agentGoRoot()
	if goroot == "" {
		result.Summary = "서버에서 Go toolchain을 자동으로 찾지 못했습니다."
		return result
	}
	goexe := filepath.Join(goroot, "bin", "go.exe")
	if info, e := os.Stat(goexe); e != nil || info.IsDir() {
		result.Summary = "자동 감지한 Go toolchain을 사용할 수 없습니다."
		return result
	}
	output := call.Output
	if strings.TrimSpace(output) == "" {
		output = "bin\\app.exe"
	}
	outputPath, err := resolveAgentPath(root, output, true)
	if err != nil || strings.ToLower(filepath.Ext(outputPath)) != ".exe" {
		result.Summary = "빌드 출력 경로가 올바르지 않습니다."
		return result
	}
	if err = os.MkdirAll(filepath.Dir(outputPath), 0700); err != nil {
		result.Summary = "빌드 출력 폴더를 만들지 못했습니다."
		return result
	}
	relOut, _ := filepath.Rel(root, outputPath)
	env, err := agentEnvironment(root, goroot)
	if err != nil {
		result.Summary = "격리 환경을 구성하지 못했습니다."
		return result
	}
	env["GOPROXY"] = "off"
	env["GOSUMDB"] = "off"
	run, err := m.app.agentRunner.Run(ctx, agentRunRequest{
		OwnerID: q.OwnerID, ProjectID: q.ProjectID, Executable: goexe,
		Args: []string{"build", "-trimpath", "-o", relOut, "."},
		Dir:  root, Env: env, ReadOnlyPaths: []string{goroot},
	})
	if err != nil || run.ExitCode != 0 || !run.IsAppContainer {
		result.Summary = "Go 빌드 실패"
		result.Output = limitAgentOutput(run.Stderr+"\n"+run.Stdout, 16<<10)
		return result
	}
	if _, err = workspaceSize(root, project.QuotaBytes); err != nil {
		_ = os.Remove(outputPath)
		result.Summary = "빌드 결과가 프로젝트 용량 한도를 초과했습니다."
		return result
	}
	artifact, err := m.registerArtifact(q, project, filepath.ToSlash(relOut))
	if err != nil {
		result.Summary = "빌드는 성공했지만 산출물 등록에 실패했습니다."
		return result
	}
	result.OK = true
	result.Summary = "AppContainer Go 빌드 성공"
	result.Output = "path=" + filepath.ToSlash(relOut) + "; artifact_id=" + artifact.ID
	return result
}

func validProgramArgs(args []string) bool {
	if len(args) > 8 {
		return false
	}
	for _, arg := range args {
		if len(arg) > 512 || strings.ContainsRune(arg, 0) {
			return false
		}
	}
	return true
}

func (m *agentJobManager) runProjectProgram(ctx context.Context, q AgentJob, project AgentProject, call AgentToolCall) AgentToolResult {
	result := AgentToolResult{ID: call.ID, Name: call.Name}
	if !validProgramArgs(call.Args) {
		result.Summary = "프로그램 인자 형식이 올바르지 않습니다."
		return result
	}
	root, err := m.app.projectRoot(project)
	if err != nil {
		result.Summary = "프로젝트 경로 오류"
		return result
	}
	exe, err := resolveAgentPath(root, call.Path, false)
	if err != nil || strings.ToLower(filepath.Ext(exe)) != ".exe" {
		result.Summary = "프로젝트 내부 실행 파일만 실행할 수 있습니다."
		return result
	}
	info, err := os.Stat(exe)
	if err != nil || !info.Mode().IsRegular() {
		result.Summary = "실행 파일을 찾을 수 없습니다."
		return result
	}
	goroot := m.app.agentGoRoot()
	env, err := agentEnvironment(root, goroot)
	if err != nil {
		result.Summary = "격리 환경을 구성하지 못했습니다."
		return result
	}
	run, err := m.app.agentRunner.Run(ctx, agentRunRequest{
		OwnerID: q.OwnerID, ProjectID: q.ProjectID, Executable: exe,
		Args: append([]string(nil), call.Args...), Dir: root, Env: env,
	})
	if err != nil || run.ExitCode != 0 || !run.IsAppContainer {
		result.Summary = "프로그램 실행 실패"
		result.Output = limitAgentOutput(run.Stderr+"\n"+run.Stdout, 32<<10)
		return result
	}
	result.OK = true
	result.Summary = "AppContainer 프로그램 실행 성공"
	result.Output = limitAgentOutput(run.Stdout, 32<<10)
	return result
}

func limitAgentOutput(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "\n…출력 생략…"
}
func (m *agentJobManager) registerArtifact(q AgentJob, project AgentProject, relative string) (AgentArtifact, error) {
	var artifact AgentArtifact
	root, err := m.app.projectRoot(project)
	if err != nil {
		return artifact, err
	}
	path, err := resolveAgentPath(root, relative, false)
	if err != nil {
		return artifact, err
	}
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return artifact, bad("산출물 파일을 찾을 수 없습니다.")
	}
	hash, size, err := fileSHA256(path)
	if err != nil {
		return artifact, err
	}
	artifact = AgentArtifact{
		ID: randomID(), OwnerID: q.OwnerID, ProjectID: q.ProjectID, JobID: q.ID,
		RelativePath: filepath.ToSlash(relative), Name: filepath.Base(path),
		MIME: artifactMIME(path), Size: size, SHA256: hash, CreatedAt: now(),
	}
	err = m.app.store.update(func(st *State) error {
		current, ok := st.AgentJobs[q.ID]
		if !ok || agentTerminal(current.Status) {
			return errUnchanged
		}
		u, uok := st.Users[q.OwnerID]
		p, pok := st.AgentProjects[q.ProjectID]
		if !uok || !pok || !canAccessProject(st, u, p, "project.read") {
			return forbidden()
		}
		st.AgentArtifacts[artifact.ID] = artifact
		current.ArtifactIDs = append(current.ArtifactIDs, artifact.ID)
		st.AgentJobs[q.ID] = current
		appendAudit(st, AuditEvent{
			ActorID: q.OwnerID, Action: "artifact.register",
			ResourceType: "project", ResourceID: q.ProjectID,
			JobID: q.ID, Result: "allowed",
		})
		return nil
	})
	return artifact, err
}

func (m *agentJobManager) finishSuccess(q AgentJob, final string, inputs ...*aiMedia) error {
	media := firstAIMedia(inputs)
	return m.app.commitAIMedia(q.OwnerID, q.RoomID, media, func(st *State, created *[]string) error {
		current, ok := st.AgentJobs[q.ID]
		if !ok || (current.Status != agentPlanning && current.Status != agentRunning) {
			return errUnchanged
		}
		u, uok := st.Users[q.OwnerID]
		p, pok := st.AgentProjects[q.ProjectID]
		room, rok := st.Rooms[q.RoomID]
		if !uok || !pok || !rok || room.Kind != "ai" || room.OwnerID != q.OwnerID ||
			!canUseAgentConfig(m.app, u) || !canAccessProject(st, u, p, "project.run") {
			current.Status = agentFailed
			current.ErrorCode = "AGENT_PERMISSION_REVOKED"
			current.Error = "완료 시점에 작업 권한이 없어 결과를 저장하지 않았습니다."
			current.FinishedAt = now()
			st.AgentJobs[q.ID] = current
			return nil
		}
		msg, err := addMessage(st, Message{
			RoomID: q.RoomID, UserID: "", Username: "온테크 AI", Kind: "text",
			Text: final, ClientID: "agent-" + q.ID, AgentJobID: q.ID,
			AIModelID: q.ModelID, AIEffort: q.Effort,
		}, m.app.cfg.MaxMessages, m.app.cfg.FCM.Enabled)
		if err != nil {
			return err
		}
		if media != nil {
			if e := m.app.saveAIImages(st, q.OwnerID, q.RoomID, q.ID, "", media.Generated, created); e != nil {
				return e
			}
		}
		current.Status = agentCompleted
		current.AnswerMessage = msg.ID
		current.ErrorCode, current.Error = "", ""
		current.FinishedAt = now()
		current.Log = appendBoundedAgentLog(current.Log, "작업을 완료했습니다.")
		st.AgentJobs[q.ID] = current
		appendAudit(st, AuditEvent{
			ActorID: q.OwnerID, Action: "agent.complete",
			ResourceType: "project", ResourceID: q.ProjectID,
			JobID: q.ID, Result: "allowed",
		})
		return nil
	})
}

func (m *agentJobManager) finishError(q AgentJob, runErr error) {
	status := agentFailed
	code, message := "AGENT_FAILED", "에이전트 작업에 실패했습니다."
	if errors.Is(runErr, context.Canceled) {
		status, code, message = agentCancelled, "AGENT_CANCELLED", "작업을 중지했습니다."
	} else if errors.Is(runErr, context.DeadlineExceeded) {
		code, message = "AGENT_TIMEOUT", "에이전트 작업 시간이 초과되었습니다."
	} else {
		var ae AIExecutionError
		if errors.As(runErr, &ae) {
			code, message = ae.Code, ae.Public
		}
		var api apiError
		if errors.As(runErr, &api) {
			message = api.message
		}
	}
	changed := false
	_ = m.app.store.update(func(st *State) error {
		current, ok := st.AgentJobs[q.ID]
		if !ok || agentTerminal(current.Status) {
			return errUnchanged
		}
		current.Status = status
		current.ErrorCode = code
		current.Error = message
		current.FinishedAt = now()
		current.Log = appendBoundedAgentLog(current.Log, message)
		st.AgentJobs[q.ID] = current
		appendAudit(st, AuditEvent{
			ActorID: q.OwnerID, Action: "agent.finish",
			ResourceType: "project", ResourceID: q.ProjectID,
			JobID: q.ID, Result: "failed", Code: code,
		})
		changed = true
		return nil
	})
	if changed {
		m.app.changed(q.RoomID)
	}
}
func findAgentJobByClient(st *State, ownerID, projectID, clientID string) (AgentJob, bool) {
	for _, q := range st.AgentJobs {
		if q.OwnerID == ownerID && q.ProjectID == projectID && q.ClientID == clientID {
			return q, true
		}
	}
	return AgentJob{}, false
}

func activeAgentJobForProject(st *State, projectID string) (AgentJob, bool) {
	for _, q := range st.AgentJobs {
		if q.ProjectID == projectID &&
			(q.Status == agentQueued || q.Status == agentPlanning || q.Status == agentRunning) {
			return q, true
		}
	}
	return AgentJob{}, false
}

func activeAgentJobForRoom(st *State, roomID string) (AgentJob, bool) {
	for _, q := range st.AgentJobs {
		if q.RoomID == roomID &&
			(q.Status == agentQueued || q.Status == agentPlanning || q.Status == agentRunning) {
			return q, true
		}
	}
	return AgentJob{}, false
}

func (a *App) enqueueAutomaticAgentJob(room Room, u User, text, clientID string) (Message, AgentJob, bool, error) {
	var message Message
	var q AgentJob
	if !canUseAgentConfig(a, u) {
		return message, q, false, forbidden()
	}
	project, err := a.ensureDefaultAgentProject(u)
	if err != nil {
		return message, q, false, err
	}
	provider, model, resolved, err := a.resolveAIRuntime(room.AIModelID, room.AIEffort)
	if err != nil {
		return message, q, false, err
	}
	if provider.Type != "claude_cli" && provider.Type != "codex_cli" {
		return message, q, false, AIExecutionError{Code: "AGENT_PROVIDER_UNSUPPORTED", Public: "선택한 모델은 자동 작업 Agent를 지원하지 않습니다."}
	}
	if !a.limiter.allow("ai:"+u.ID, a.cfg.AI.PerUserPerMinute, time.Minute) {
		return message, q, false, apiError{429, "AI 요청 횟수가 너무 많습니다. 잠시 후 다시 시도하세요."}
	}
	q = AgentJob{
		ID: randomID(), OwnerID: u.ID, RoomID: room.ID, ProjectID: project.ID,
		ClientID: clientID, Mode: "auto", Status: agentQueued,
		ProviderID: provider.ID, ModelID: model.ID, Model: model.Model,
		Effort: resolved, RequestText: text, CreatedAt: now(),
		ToolResults: map[string]AgentToolResult{},
	}
	created := false
	err = a.store.update(func(st *State) error {
		currentUser, ok := st.Users[u.ID]
		if !ok || !canUseAgentConfig(a, currentUser) {
			return forbidden()
		}
		currentRoom, ok := member(st, room.ID, currentUser.ID)
		if !ok || currentRoom.Kind != "ai" || currentRoom.OwnerID != currentUser.ID || len(currentRoom.Members) != 1 {
			return forbidden()
		}
		p, ok := st.AgentProjects[project.ID]
		if !ok || !canAccessProject(st, currentUser, p, "project.run") {
			return notfound()
		}
		if old, ok := findAgentJobByClient(st, currentUser.ID, p.ID, clientID); ok {
			if old.RoomID != room.ID || old.RequestText != text {
				return apiError{409, "같은 client_id로 다른 AI 요청을 보낼 수 없습니다."}
			}
			q = old
			if m, found := findMessageByID(st, old.SourceMessage); found {
				message = m
			}
			return errUnchanged
		}
		if aiReq, active := activeAIRequest(st, room.ID); active {
			return codedAPIError{status: 409, code: "AI_REQUEST_ACTIVE", message: "이 AI 대화방에서 이미 응답을 생성하고 있습니다.", data: map[string]any{"request": publicAIRequest(aiReq)}}
		}
		if active, ok := activeAgentJobForRoom(st, room.ID); ok {
			return codedAPIError{status: 409, code: "AGENT_REQUEST_ACTIVE", message: "이 AI 대화방에서 이미 응답을 생성하고 있습니다.", data: map[string]any{"agent_job": publicAgentJob(active)}}
		}
		queued := 0
		for _, old := range st.AgentJobs {
			if old.Status == agentQueued {
				queued++
			}
		}
		if queued >= a.cfg.Agent.QueueLimit {
			return apiError{503, "AI 작업 대기열이 가득 찼습니다."}
		}
		if !aiMessageCapacityAvailable(st, a.cfg.MaxMessages, 2) {
			return apiError{503, "AI 질문과 답변을 저장할 공간이 부족합니다."}
		}
		q.PolicyRevision = st.Revision + 1
		message, err = addMessage(st, Message{
			RoomID: room.ID, UserID: currentUser.ID, Username: currentUser.Username,
			Kind: "text", Text: text, ClientID: "agent-user-" + clientID, AgentJobID: q.ID,
		}, a.cfg.MaxMessages, false)
		if err != nil {
			return err
		}
		q.SourceMessage = message.ID
		st.AgentJobs[q.ID] = q
		appendAudit(st, AuditEvent{ActorID: currentUser.ID, Action: "agent.auto_create", ResourceType: "project", ResourceID: p.ID, JobID: q.ID, Result: "allowed"})
		created = true
		return nil
	})
	return message, q, created, err
}

func (a *App) sendAutomaticAgentMessage(w http.ResponseWriter, r *http.Request, u User, room Room, text, clientID string) {
	message, q, created, err := a.enqueueAutomaticAgentJob(room, u, text, clientID)
	if err != nil {
		fail(w, err)
		return
	}
	a.changed(room.ID)
	if created && a.agentJobs != nil {
		a.agentJobs.wake()
	}
	respond(w, http.StatusAccepted, map[string]any{"message": message, "agent_job": publicAgentJob(q)})
}

func (a *App) createAgentJob(w http.ResponseWriter, r *http.Request, u User) {
	if !canUseAgentConfig(a, u) {
		fail(w, apiError{403, "에이전트 작업 사용 권한이 없습니다."})
		return
	}
	var in struct {
		RoomID    string `json:"room_id"`
		ProjectID string `json:"project_id"`
		ClientID  string `json:"client_id"`
		Request   string `json:"request"`
		ModelID   string `json:"model_id,omitempty"`
		Effort    string `json:"effort,omitempty"`
	}
	if err := decode(w, r, &in); err != nil {
		fail(w, err)
		return
	}
	in.Request = strings.TrimSpace(in.Request)
	if !clientIDPattern.MatchString(in.ClientID) || in.Request == "" || len(in.Request) > 16000 {
		fail(w, bad("작업 요청과 client_id를 확인하세요."))
		return
	}
	var room Room
	var project AgentProject
	err := a.store.view(func(st *State) error {
		var ok bool
		room, ok = member(st, in.RoomID, u.ID)
		if !ok || room.Kind != "ai" || room.OwnerID != u.ID || len(room.Members) != 1 {
			return forbidden()
		}
		project, ok = st.AgentProjects[in.ProjectID]
		if !ok || !canAccessProject(st, u, project, "project.run") {
			return notfound()
		}
		if aiReq, active := activeAIRequest(st, room.ID); active {
			return codedAPIError{status: 409, code: "AI_REQUEST_ACTIVE", message: "AI 답변 생성 중에는 작업 모드를 시작할 수 없습니다.", data: map[string]any{"request": publicAIRequest(aiReq)}}
		}
		if old, ok := findAgentJobByClient(st, u.ID, project.ID, in.ClientID); ok {
			if old.RequestText != in.Request {
				return apiError{409, "같은 client_id로 다른 작업을 요청할 수 없습니다."}
			}
			return codedAPIError{
				status: http.StatusAccepted, code: "AGENT_DUPLICATE",
				message: "이미 접수된 작업입니다.",
				data:    map[string]any{"request": publicAgentJob(old)},
			}
		}
		if active, ok := activeAgentJobForRoom(st, room.ID); ok {
			return codedAPIError{
				status: 409, code: "AGENT_REQUEST_ACTIVE",
				message: "이 AI 대화방에서 이미 응답을 생성하고 있습니다.",
				data:    map[string]any{"request": publicAgentJob(active)},
			}
		}
		return nil
	})
	if err != nil {
		var ce codedAPIError
		if errors.As(err, &ce) && ce.code == "AGENT_DUPLICATE" {
			respond(w, http.StatusAccepted, ce.data)
			return
		}
		fail(w, err)
		return
	}
	modelID := strings.TrimSpace(in.ModelID)
	if modelID == "" {
		modelID = room.AIModelID
	}
	effort := strings.TrimSpace(in.Effort)
	if effort == "" {
		effort = room.AIEffort
	}
	provider, model, resolved, err := a.resolveAIRuntime(modelID, effort)
	if err != nil {
		fail(w, err)
		return
	}
	if provider.Type != "claude_cli" && provider.Type != "codex_cli" {
		fail(w, apiError{503, "자동 작업은 Claude 또는 Codex CLI 모델을 선택하세요."})
		return
	}
	q := AgentJob{
		ID: randomID(), OwnerID: u.ID, RoomID: room.ID, ProjectID: project.ID,
		ClientID: in.ClientID, Mode: "work", Status: agentQueued,
		ProviderID: provider.ID, ModelID: model.ID, Model: model.Model,
		Effort: resolved, RequestText: in.Request, CreatedAt: now(),
		ToolResults: map[string]AgentToolResult{},
	}
	var message Message
	created := false
	err = a.store.update(func(st *State) error {
		currentUser, ok := st.Users[u.ID]
		if !ok || !canUseAgentConfig(a, currentUser) {
			return forbidden()
		}
		p, ok := st.AgentProjects[project.ID]
		if !ok || !canAccessProject(st, currentUser, p, "project.run") {
			return notfound()
		}
		if aiReq, active := activeAIRequest(st, room.ID); active {
			return codedAPIError{status: 409, code: "AI_REQUEST_ACTIVE", message: "AI 답변 생성 중에는 작업 모드를 시작할 수 없습니다.", data: map[string]any{"request": publicAIRequest(aiReq)}}
		}
		if old, ok := findAgentJobByClient(st, u.ID, p.ID, in.ClientID); ok {
			if old.RequestText != in.Request {
				return apiError{409, "같은 client_id로 다른 작업을 요청할 수 없습니다."}
			}
			q = old
			return errUnchanged
		}
		if active, ok := activeAgentJobForRoom(st, room.ID); ok {
			return codedAPIError{
				status: 409, code: "AGENT_REQUEST_ACTIVE",
				message: "이 AI 대화방에서 이미 응답을 생성하고 있습니다.",
				data:    map[string]any{"request": publicAgentJob(active)},
			}
		}
		if active, ok := activeAgentJobForProject(st, p.ID); ok {
			return codedAPIError{
				status: 409, code: "AGENT_PROJECT_BUSY",
				message: "이 프로젝트에서 이미 작업을 실행하고 있습니다.",
				data:    map[string]any{"request": publicAgentJob(active)},
			}
		}
		queued := 0
		for _, old := range st.AgentJobs {
			if old.Status == agentQueued {
				queued++
			}
		}
		if queued >= a.cfg.Agent.QueueLimit {
			return apiError{503, "에이전트 작업 대기열이 가득 찼습니다."}
		}
		q.PolicyRevision = st.Revision + 1
		message, err = addMessage(st, Message{
			RoomID: room.ID, UserID: u.ID, Username: u.Username, Kind: "text",
			Text: in.Request, ClientID: "agent-user-" + in.ClientID, AgentJobID: q.ID,
		}, a.cfg.MaxMessages, false)
		if err != nil {
			return err
		}
		q.SourceMessage = message.ID
		st.AgentJobs[q.ID] = q
		appendAudit(st, AuditEvent{
			ActorID: u.ID, Action: "agent.create",
			ResourceType: "project", ResourceID: p.ID,
			JobID: q.ID, Result: "allowed",
		})
		created = true
		return nil
	})
	if err != nil {
		fail(w, err)
		return
	}
	a.changed(room.ID)
	if created && a.agentJobs != nil {
		a.agentJobs.wake()
	}
	respond(w, http.StatusAccepted, map[string]any{
		"message": message, "request": publicAgentJob(q),
	})
}

func (a *App) getAgentJob(w http.ResponseWriter, r *http.Request, u User) {
	id := r.PathValue("id")
	var q AgentJob
	err := a.store.view(func(st *State) error {
		var ok bool
		q, ok = st.AgentJobs[id]
		if !ok {
			return notfound()
		}
		project, pok := st.AgentProjects[q.ProjectID]
		if !pok || !canAccessProject(st, u, project, "project.read") {
			return notfound()
		}
		if q.OwnerID != u.ID && !hasGrant(st, u.ID, "project", q.ProjectID, "project.manage") {
			return notfound()
		}
		return nil
	})
	if err != nil {
		fail(w, err)
		return
	}
	respond(w, 200, publicAgentJob(q))
}

func (a *App) cancelAgentJob(w http.ResponseWriter, r *http.Request, u User) {
	id := r.PathValue("id")
	allowed := false
	err := a.store.view(func(st *State) error {
		q, ok := st.AgentJobs[id]
		if !ok {
			return notfound()
		}
		project, pok := st.AgentProjects[q.ProjectID]
		if !pok || !canAccessProject(st, u, project, "project.run") {
			return notfound()
		}
		allowed = q.OwnerID == u.ID || hasGrant(st, u.ID, "project", q.ProjectID, "project.manage")
		if !allowed {
			return forbidden()
		}
		return nil
	})
	if err != nil {
		fail(w, err)
		return
	}
	if a.agentJobs == nil {
		fail(w, apiError{503, "에이전트 작업자가 준비되지 않았습니다."})
		return
	}
	a.agentJobs.cancel(id)
	var q AgentJob
	_ = a.store.view(func(st *State) error { q = st.AgentJobs[id]; return nil })
	respond(w, 200, publicAgentJob(q))
}

func (a *App) retryAgentJob(w http.ResponseWriter, r *http.Request, u User) {
	id := r.PathValue("id")
	var in struct {
		ClientID string `json:"client_id"`
	}
	if err := decode(w, r, &in); err != nil {
		fail(w, err)
		return
	}
	if !clientIDPattern.MatchString(in.ClientID) {
		fail(w, bad("재시도에는 새 client_id가 필요합니다."))
		return
	}
	var original AgentJob
	err := a.store.view(func(st *State) error {
		var ok bool
		original, ok = st.AgentJobs[id]
		if !ok || original.OwnerID != u.ID || !agentTerminal(original.Status) || original.Status == agentCompleted {
			return notfound()
		}
		project, pok := st.AgentProjects[original.ProjectID]
		if !pok || !canAccessProject(st, u, project, "project.run") {
			return notfound()
		}
		return nil
	})
	if err != nil {
		fail(w, err)
		return
	}
	var q AgentJob
	created := false
	err = a.store.update(func(st *State) error {
		if old, ok := findAgentJobByClient(st, u.ID, original.ProjectID, in.ClientID); ok {
			if old.RetryOf == original.ID {
				q = old
				return errUnchanged
			}
			return apiError{409, "이미 사용한 client_id입니다."}
		}
		if active, ok := activeAgentJobForProject(st, original.ProjectID); ok {
			return codedAPIError{
				status: 409, code: "AGENT_PROJECT_BUSY",
				message: "프로젝트에서 이미 작업 중입니다.",
				data:    map[string]any{"request": publicAgentJob(active)},
			}
		}
		q = original
		q.ID = randomID()
		q.ClientID = in.ClientID
		q.RetryOf = original.ID
		q.SourceMessage = original.SourceMessage
		q.AnswerMessage = 0
		q.Status = agentQueued
		q.ErrorCode, q.Error = "", ""
		q.CreatedAt, q.StartedAt, q.FinishedAt = now(), 0, 0
		q.Turns, q.ToolCount = 0, 0
		q.ArtifactIDs = nil
		q.ToolResults = map[string]AgentToolResult{}
		q.Log = nil
		q.PolicyRevision = st.Revision + 1
		st.AgentJobs[q.ID] = q
		appendAudit(st, AuditEvent{
			ActorID: u.ID, Action: "agent.retry",
			ResourceType: "project", ResourceID: q.ProjectID,
			JobID: q.ID, Result: "allowed",
		})
		created = true
		return nil
	})
	if err != nil {
		fail(w, err)
		return
	}
	if created && a.agentJobs != nil {
		a.agentJobs.wake()
	}
	a.changed(q.RoomID)
	respond(w, http.StatusAccepted, publicAgentJob(q))
}
