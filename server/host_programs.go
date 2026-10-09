package server

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type ProgramActionDefinition struct {
	Name       string   `json:"name"`
	Arguments  []string `json:"arguments,omitempty"`
	AllowForce bool     `json:"allow_force,omitempty"`
}

type ProgramDefinition struct {
	ID               string                    `json:"id"`
	Name             string                    `json:"name"`
	Executable       string                    `json:"executable"`
	WorkDir          string                    `json:"work_dir,omitempty"`
	StopFile         string                    `json:"stop_file,omitempty"`
	Actions          []ProgramActionDefinition `json:"actions"`
	ResourceKey      string                    `json:"resource_key,omitempty"`
	Enabled          bool                      `json:"enabled"`
	Revision         uint64                    `json:"revision"`
	ExecutableSHA256 string                    `json:"executable_sha256,omitempty"`
	CreatedAt        int64                     `json:"created_at"`
	UpdatedAt        int64                     `json:"updated_at"`
}

type ProgramInstance struct {
	ID          string `json:"id"`
	ProgramID   string `json:"program_id"`
	OwnerID     string `json:"owner_id"`
	JobID       string `json:"job_id,omitempty"`
	PID         int    `json:"pid"`
	StartedAt   int64  `json:"started_at"`
	FinishedAt  int64  `json:"finished_at,omitempty"`
	Executable  string `json:"executable"`
	ResourceKey string `json:"resource_key,omitempty"`
	State       string `json:"state"`
	ExitCode    int    `json:"exit_code,omitempty"`
}

type hostProgramRuntime struct {
	def      ProgramDefinition
	instance ProgramInstance
	cmd      *exec.Cmd
	done     chan struct{}
}

type hostProgramController struct {
	mu        sync.Mutex
	locks     map[string]bool
	instances map[string]*hostProgramRuntime
}

func newHostProgramController() *hostProgramController {
	return &hostProgramController{
		locks: map[string]bool{}, instances: map[string]*hostProgramRuntime{},
	}
}
func (c *hostProgramController) lock(key string) bool {
	if key == "" {
		return true
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.locks[key] {
		return false
	}
	c.locks[key] = true
	return true
}

func (c *hostProgramController) unlock(key string) {
	if key == "" {
		return
	}
	c.mu.Lock()
	delete(c.locks, key)
	c.mu.Unlock()
}

func validProgramActionName(name string) bool {
	switch name {
	case "start", "status", "stop", "force_stop":
		return true
	default:
		return false
	}
}

func findProgramAction(def ProgramDefinition, name string) (ProgramActionDefinition, bool) {
	for _, action := range def.Actions {
		if action.Name == name {
			return action, true
		}
	}
	return ProgramActionDefinition{}, false
}

func publicProgramDefinition(def ProgramDefinition) map[string]any {
	actions := make([]string, 0, len(def.Actions))
	for _, a := range def.Actions {
		actions = append(actions, a.Name)
	}
	sort.Strings(actions)
	return map[string]any{
		"id": def.ID, "name": def.Name, "enabled": def.Enabled,
		"actions": actions, "revision": def.Revision,
	}
}

func adminProgramDefinition(def ProgramDefinition) map[string]any {
	out := publicProgramDefinition(def)
	out["executable"] = def.Executable
	out["work_dir"] = def.WorkDir
	out["stop_file"] = def.StopFile
	out["resource_key"] = def.ResourceKey
	out["executable_sha256"] = def.ExecutableSHA256
	out["action_definitions"] = def.Actions
	out["created_at"] = def.CreatedAt
	out["updated_at"] = def.UpdatedAt
	return out
}

func hostProgramEnv() []string {
	keys := []string{"SystemRoot", "WINDIR", "SystemDrive", "ComSpec", "PATHEXT", "ProgramFiles", "ProgramFiles(x86)"}
	out := make([]string, 0, len(keys)+1)
	for _, key := range keys {
		if value := os.Getenv(key); value != "" {
			out = append(out, key+"="+value)
		}
	}
	if value := os.Getenv("SystemRoot"); value != "" {
		out = append(out, "PATH="+filepath.Join(value, "System32"))
	}
	return out
}

func pathUnder(root, target string) bool {
	if root == "" || target == "" {
		return false
	}
	rootAbs, e1 := filepath.Abs(root)
	targetAbs, e2 := filepath.Abs(target)
	if e1 != nil || e2 != nil {
		return false
	}
	rel, err := filepath.Rel(rootAbs, targetAbs)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

func trustedProgramPath(a *App, target string) bool {
	if target == "" || !filepath.IsAbs(target) {
		return false
	}
	roots := []string{os.Getenv("ProgramFiles"), os.Getenv("ProgramFiles(x86)"), os.Getenv("SystemRoot"), filepath.Join(a.cfg.DataDir, "registered-programs")}
	for _, root := range roots {
		if root != "" && pathUnder(root, target) {
			return true
		}
	}
	return false
}

func validateProgramDefinition(a *App, def ProgramDefinition) error {
	if !aiIDPattern.MatchString(def.ID) || strings.TrimSpace(def.Name) == "" || len(def.Name) > 80 {
		return bad("프로그램 ID와 이름을 확인하세요.")
	}
	if !filepath.IsAbs(def.Executable) {
		return bad("등록 프로그램 실행 파일은 절대경로여야 합니다.")
	}
	info, err := os.Stat(def.Executable)
	if err != nil || !info.Mode().IsRegular() {
		return bad("등록 프로그램 실행 파일을 찾을 수 없습니다.")
	}
	if pathUnder(a.agentWorkspaceRoot(), def.Executable) {
		return bad("사용자 작업공간의 실행 파일은 등록 프로그램으로 사용할 수 없습니다.")
	}
	if !trustedProgramPath(a, def.Executable) {
		return bad("등록 프로그램은 Program Files, Windows 또는 서버 관리 registered-programs 폴더의 실행 파일만 사용할 수 있습니다.")
	}
	managedRoot := filepath.Join(a.cfg.DataDir, "registered-programs")
	if pathUnder(managedRoot, def.Executable) {
		if err := secureDirectory(managedRoot); err != nil {
			return bad("서버 관리 프로그램 폴더의 ACL을 확인하세요.")
		}
	}
	if def.WorkDir == "" {
		def.WorkDir = filepath.Dir(def.Executable)
	}
	if !filepath.IsAbs(def.WorkDir) {
		return bad("등록 프로그램 작업 폴더는 절대경로여야 합니다.")
	}
	if pathUnder(a.agentWorkspaceRoot(), def.WorkDir) {
		return bad("사용자 작업공간을 등록 프로그램 작업 폴더로 사용할 수 없습니다.")
	}
	if !trustedProgramPath(a, def.WorkDir) {
		return bad("등록 프로그램 작업 폴더도 신뢰된 프로그램 경로 내부여야 합니다.")
	}
	if def.StopFile != "" {
		if !filepath.IsAbs(def.StopFile) || !pathUnder(def.WorkDir, def.StopFile) {
			return bad("정상 종료 신호 파일은 등록 프로그램 작업 폴더 내부여야 합니다.")
		}
	}
	if len(def.Actions) == 0 || len(def.Actions) > 8 {
		return bad("등록 프로그램 동작을 1~8개 지정하세요.")
	}
	seen := map[string]bool{}
	for _, action := range def.Actions {
		if !validProgramActionName(action.Name) || seen[action.Name] {
			return bad("등록 프로그램 동작 이름을 확인하세요.")
		}
		seen[action.Name] = true
		if len(action.Arguments) > 16 {
			return bad("등록 프로그램 고정 인자가 너무 많습니다.")
		}
		for _, arg := range action.Arguments {
			if len(arg) > 512 || strings.ContainsRune(arg, 0) {
				return bad("등록 프로그램 고정 인자를 확인하세요.")
			}
		}
		if action.Name == "force_stop" && !action.AllowForce {
			return bad("강제 종료 동작은 allow_force가 필요합니다.")
		}
	}
	return nil
}

func (a *App) listHostPrograms(w http.ResponseWriter, r *http.Request, u User) {
	out := []map[string]any{}
	err := a.store.view(func(st *State) error {
		for _, def := range st.ProgramDefinitions {
			if !def.Enabled && !isSuperadmin(u) {
				continue
			}
			allowed := isSuperadmin(u)
			if !allowed {
				for _, action := range def.Actions {
					if hasGrant(st, u.ID, "program", def.ID, "program."+action.Name) {
						allowed = true
						break
					}
				}
			}
			if allowed {
				if isSuperadmin(u) {
					out = append(out, adminProgramDefinition(def))
				} else {
					out = append(out, publicProgramDefinition(def))
				}
			}
		}
		return nil
	})
	if err != nil {
		fail(w, err)
		return
	}
	sort.Slice(out, func(i, j int) bool { return out[i]["name"].(string) < out[j]["name"].(string) })
	respond(w, 200, out)
}
func (a *App) registerHostProgram(w http.ResponseWriter, r *http.Request, u User) {
	if !isSuperadmin(u) {
		fail(w, forbidden())
		return
	}
	var in struct {
		ID          string                    `json:"id"`
		Name        string                    `json:"name"`
		Executable  string                    `json:"executable"`
		WorkDir     string                    `json:"work_dir,omitempty"`
		StopFile    string                    `json:"stop_file,omitempty"`
		Actions     []ProgramActionDefinition `json:"actions"`
		ResourceKey string                    `json:"resource_key,omitempty"`
		Enabled     bool                      `json:"enabled"`
	}
	if err := decode(w, r, &in); err != nil {
		fail(w, err)
		return
	}
	def := ProgramDefinition{
		ID: strings.TrimSpace(in.ID), Name: strings.TrimSpace(in.Name),
		Executable: filepath.Clean(strings.TrimSpace(in.Executable)),
		WorkDir:    filepath.Clean(strings.TrimSpace(in.WorkDir)),
		StopFile:   filepath.Clean(strings.TrimSpace(in.StopFile)),
		Actions:    in.Actions, ResourceKey: strings.TrimSpace(in.ResourceKey),
		Enabled: in.Enabled, Revision: 1, CreatedAt: now(), UpdatedAt: now(),
	}
	if in.WorkDir == "" {
		def.WorkDir = filepath.Dir(def.Executable)
	}
	if in.StopFile == "" {
		def.StopFile = ""
	}
	if err := validateProgramDefinition(a, def); err != nil {
		fail(w, err)
		return
	}
	hash, _, err := fileSHA256(def.Executable)
	if err != nil {
		fail(w, err)
		return
	}
	def.ExecutableSHA256 = hash
	err = a.store.update(func(st *State) error {
		if _, exists := st.ProgramDefinitions[def.ID]; exists {
			return apiError{409, "이미 등록된 프로그램 ID입니다."}
		}
		st.ProgramDefinitions[def.ID] = def
		appendAudit(st, AuditEvent{
			ActorID: u.ID, Action: "program.register",
			ResourceType: "program", ResourceID: def.ID, Result: "allowed",
		})
		return nil
	})
	if err != nil {
		fail(w, err)
		return
	}
	respond(w, http.StatusCreated, publicProgramDefinition(def))
}

func (a *App) programActionAllowed(st *State, u User, def ProgramDefinition, action string) bool {
	if isSuperadmin(u) {
		return true
	}
	return hasGrant(st, u.ID, "program", def.ID, "program."+action)
}

func (a *App) hostProgramAction(w http.ResponseWriter, r *http.Request, u User) {
	programID := r.PathValue("id")
	var in struct {
		Action string `json:"action"`
	}
	if err := decode(w, r, &in); err != nil {
		fail(w, err)
		return
	}
	actionName := strings.TrimSpace(in.Action)
	var def ProgramDefinition
	var action ProgramActionDefinition
	err := a.store.view(func(st *State) error {
		var ok bool
		def, ok = st.ProgramDefinitions[programID]
		if !ok || !def.Enabled {
			return notfound()
		}
		action, ok = findProgramAction(def, actionName)
		if !ok || !a.programActionAllowed(st, u, def, actionName) {
			return forbidden()
		}
		return nil
	})
	if err != nil {
		fail(w, err)
		return
	}
	switch actionName {
	case "start":
		out, err := a.startHostProgram(u, def, action)
		if err != nil {
			fail(w, err)
			return
		}
		respond(w, http.StatusAccepted, out)
	case "status":
		respond(w, 200, a.hostProgramStatus(def.ID))
	case "stop":
		out, err := a.stopHostProgram(u, def, false)
		if err != nil {
			fail(w, err)
			return
		}
		respond(w, 200, out)
	case "force_stop":
		if !action.AllowForce {
			fail(w, forbidden())
			return
		}
		out, err := a.stopHostProgram(u, def, true)
		if err != nil {
			fail(w, err)
			return
		}
		respond(w, 200, out)
	}
}
func publicProgramInstance(v ProgramInstance) map[string]any {
	return map[string]any{
		"id": v.ID, "program_id": v.ProgramID, "owner_id": v.OwnerID,
		"job_id": v.JobID, "pid": v.PID, "started_at": v.StartedAt,
		"finished_at": v.FinishedAt, "resource_key": v.ResourceKey,
		"state": v.State, "exit_code": v.ExitCode,
	}
}

func verifyRegisteredExecutable(def ProgramDefinition) error {
	hash, _, err := fileSHA256(def.Executable)
	if err != nil {
		return apiError{503, "등록 프로그램 실행 파일을 확인하지 못했습니다."}
	}
	if !strings.EqualFold(hash, def.ExecutableSHA256) {
		return apiError{409, "등록 이후 실행 파일이 변경되어 시작하지 않았습니다."}
	}
	return nil
}

func (a *App) startHostProgram(u User, def ProgramDefinition, action ProgramActionDefinition) (map[string]any, error) {
	if err := verifyRegisteredExecutable(def); err != nil {
		return nil, err
	}
	if !a.hostPrograms.lock(def.ResourceKey) {
		return nil, apiError{409, "같은 공용 자원을 사용하는 프로그램이 이미 실행 중입니다."}
	}
	unlock := true
	defer func() {
		if unlock {
			a.hostPrograms.unlock(def.ResourceKey)
		}
	}()
	a.hostPrograms.mu.Lock()
	if _, exists := a.hostPrograms.instances[def.ID]; exists {
		a.hostPrograms.mu.Unlock()
		return nil, apiError{409, "프로그램이 이미 실행 중입니다."}
	}
	a.hostPrograms.mu.Unlock()

	if def.StopFile != "" {
		_ = os.Remove(def.StopFile)
	}
	cmd := exec.Command(def.Executable, action.Arguments...)
	cmd.Dir = def.WorkDir
	cmd.Env = hostProgramEnv()
	cmd.Stdin = nil
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		return nil, apiError{503, "등록 프로그램을 시작하지 못했습니다."}
	}
	instance := ProgramInstance{
		ID: randomID(), ProgramID: def.ID, OwnerID: u.ID,
		PID: cmd.Process.Pid, StartedAt: now(), Executable: def.Executable,
		ResourceKey: def.ResourceKey, State: "running",
	}
	runtime := &hostProgramRuntime{def: def, instance: instance, cmd: cmd, done: make(chan struct{})}
	err := a.store.update(func(st *State) error {
		current, ok := st.ProgramDefinitions[def.ID]
		if !ok || !current.Enabled || current.Revision != def.Revision ||
			!strings.EqualFold(current.ExecutableSHA256, def.ExecutableSHA256) {
			return apiError{409, "프로그램 등록 정보가 변경되어 시작을 취소했습니다."}
		}
		st.ProgramInstances[instance.ID] = instance
		appendAudit(st, AuditEvent{
			ActorID: u.ID, Action: "program.start",
			ResourceType: "program", ResourceID: def.ID, Result: "allowed",
		})
		return nil
	})
	if err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return nil, err
	}
	a.hostPrograms.mu.Lock()
	if _, exists := a.hostPrograms.instances[def.ID]; exists {
		a.hostPrograms.mu.Unlock()
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return nil, apiError{409, "프로그램 실행 상태가 충돌했습니다."}
	}
	a.hostPrograms.instances[def.ID] = runtime
	a.hostPrograms.mu.Unlock()
	unlock = false
	go a.waitHostProgram(runtime)
	return publicProgramInstance(instance), nil
}

func (a *App) waitHostProgram(runtime *hostProgramRuntime) {
	err := runtime.cmd.Wait()
	exitCode := 0
	state := "stopped"
	if err != nil {
		state = "exited"
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = -1
		}
	}
	runtime.instance.State = state
	runtime.instance.ExitCode = exitCode
	runtime.instance.FinishedAt = now()
	_ = a.store.update(func(st *State) error {
		current, ok := st.ProgramInstances[runtime.instance.ID]
		if !ok || current.State != "running" {
			return errUnchanged
		}
		current.State = runtime.instance.State
		current.ExitCode = runtime.instance.ExitCode
		current.FinishedAt = runtime.instance.FinishedAt
		st.ProgramInstances[current.ID] = current
		return nil
	})
	a.hostPrograms.mu.Lock()
	if current := a.hostPrograms.instances[runtime.def.ID]; current == runtime {
		delete(a.hostPrograms.instances, runtime.def.ID)
	}
	a.hostPrograms.mu.Unlock()
	a.hostPrograms.unlock(runtime.def.ResourceKey)
	close(runtime.done)
}

func (a *App) hostProgramStatus(programID string) map[string]any {
	a.hostPrograms.mu.Lock()
	runtime := a.hostPrograms.instances[programID]
	if runtime != nil {
		out := publicProgramInstance(runtime.instance)
		a.hostPrograms.mu.Unlock()
		return out
	}
	a.hostPrograms.mu.Unlock()
	var latest ProgramInstance
	found := false
	_ = a.store.view(func(st *State) error {
		for _, v := range st.ProgramInstances {
			if v.ProgramID != programID {
				continue
			}
			if !found || v.StartedAt > latest.StartedAt ||
				(v.StartedAt == latest.StartedAt && v.ID > latest.ID) {
				latest, found = v, true
			}
		}
		return nil
	})
	if !found {
		return map[string]any{"program_id": programID, "state": "not_started"}
	}
	return publicProgramInstance(latest)
}

func (a *App) stopHostProgram(u User, def ProgramDefinition, force bool) (map[string]any, error) {
	a.hostPrograms.mu.Lock()
	runtime := a.hostPrograms.instances[def.ID]
	a.hostPrograms.mu.Unlock()
	if runtime == nil {
		return a.hostProgramStatus(def.ID), nil
	}
	if runtime.instance.Executable != def.Executable ||
		runtime.instance.PID != runtime.cmd.Process.Pid ||
		runtime.instance.StartedAt == 0 {
		return nil, apiError{409, "추적 중인 프로그램 인스턴스 정보가 일치하지 않습니다."}
	}
	action := "program.stop"
	if force {
		action = "program.force_stop"
		if err := runtime.cmd.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
			return nil, apiError{503, "추적 중인 프로그램을 강제 종료하지 못했습니다."}
		}
	} else {
		if def.StopFile == "" {
			return nil, apiError{409, "이 프로그램에는 정상 종료 동작이 등록되지 않았습니다."}
		}
		f, err := os.OpenFile(def.StopFile, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil && !errors.Is(err, os.ErrExist) {
			return nil, apiError{503, "정상 종료 신호를 전달하지 못했습니다."}
		}
		if err == nil {
			_, _ = f.WriteString("stop\n")
			_ = f.Sync()
			_ = f.Close()
		}
	}
	_ = a.store.update(func(st *State) error {
		appendAudit(st, AuditEvent{
			ActorID: u.ID, Action: action,
			ResourceType: "program", ResourceID: def.ID, Result: "allowed",
		})
		return nil
	})
	wait := 5 * time.Second
	if force {
		wait = 3 * time.Second
	}
	select {
	case <-runtime.done:
		return a.hostProgramStatus(def.ID), nil
	case <-time.After(wait):
		if force {
			return nil, apiError{503, "강제 종료 완료를 확인하지 못했습니다."}
		}
		return nil, apiError{409, "정상 종료 응답 시간이 지났습니다. 강제 종료 권한이 있으면 별도 동작을 사용하세요."}
	}
}
func (c *hostProgramController) closeAll() {
	c.mu.Lock()
	runtimes := make([]*hostProgramRuntime, 0, len(c.instances))
	for _, runtime := range c.instances {
		runtimes = append(runtimes, runtime)
	}
	c.mu.Unlock()
	for _, runtime := range runtimes {
		if runtime.cmd != nil && runtime.cmd.Process != nil {
			_ = runtime.cmd.Process.Kill()
		}
		select {
		case <-runtime.done:
		case <-time.After(3 * time.Second):
		}
	}
}

func interruptProgramInstances(s *Store) error {
	return s.update(func(st *State) error {
		changed := false
		for id, v := range st.ProgramInstances {
			if v.State != "running" {
				continue
			}
			v.State = "interrupted"
			v.FinishedAt = now()
			st.ProgramInstances[id] = v
			changed = true
		}
		if !changed {
			return errUnchanged
		}
		return nil
	})
}

func (a *App) diagnoseAgentRuntime(w http.ResponseWriter, r *http.Request, u User) {
	if !isSuperadmin(u) {
		fail(w, forbidden())
		return
	}
	goRoot := a.agentGoRoot()
	out := map[string]any{
		"enabled":                   true,
		"isolation":                 a.cfg.Agent.WindowsIsolation,
		"network_policy":            a.cfg.Agent.NetworkPolicy,
		"go_root_configured":        goRoot != "",
		"workspace_root_configured": true,
		"ready":                     false,
	}
	if a.cfg.Agent.WindowsIsolation != "appcontainer" ||
		a.cfg.Agent.NetworkPolicy != "blocked" ||
		a.agentRunner == nil || a.agentPlanner == nil {
		out["reason"] = "runtime_unavailable"
		respond(w, 200, out)
		return
	}
	goexe := filepath.Join(goRoot, "bin", "go.exe")
	if info, err := os.Stat(goexe); err != nil || info.IsDir() {
		out["reason"] = "go_toolchain_unavailable"
		respond(w, 200, out)
		return
	}
	claudeReady := false
	for _, p := range a.cfg.AI.Providers {
		if p.Enabled && p.Type == "claude_cli" {
			if ready, _ := aiProviderRuntimeState(p); ready {
				claudeReady = true
				break
			}
		}
	}
	if !claudeReady {
		out["reason"] = "planner_unavailable"
		respond(w, 200, out)
		return
	}
	out["ready"] = true
	out["reason"] = "configured"
	out["note"] = "실제 AppContainer self-test 통과 여부는 공식 Windows 통합 테스트 결과와 별도로 확인하세요."
	respond(w, 200, out)
}

var _ = context.Canceled

func agentProgramCatalog(st *State, u User) []AgentProgramToolInfo {
	out := []AgentProgramToolInfo{}
	for _, def := range st.ProgramDefinitions {
		if !def.Enabled {
			continue
		}
		actions := []string{}
		for _, action := range def.Actions {
			if isSuperadmin(u) || hasGrant(st, u.ID, "program", def.ID, "program."+action.Name) {
				actions = append(actions, action.Name)
			}
		}
		if len(actions) == 0 {
			continue
		}
		sort.Strings(actions)
		out = append(out, AgentProgramToolInfo{ID: def.ID, Name: def.Name, Actions: actions})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func (a *App) executeAgentHostProgram(job AgentJob, call AgentToolCall) (AgentToolResult, error) {
	res := AgentToolResult{ID: call.ID, Name: call.Name}
	if strings.TrimSpace(call.ProgramID) == "" || strings.TrimSpace(call.Action) == "" ||
		call.Path != "" || call.Content != "" || call.Output != "" || len(call.Args) != 0 {
		return res, bad("등록 프로그램 도구 인자가 올바르지 않습니다.")
	}
	var def ProgramDefinition
	var action ProgramActionDefinition
	var user User
	err := a.store.view(func(st *State) error {
		current, ok := st.AgentJobs[job.ID]
		if !ok || agentTerminal(current.Status) {
			return notfound()
		}
		user, ok = st.Users[current.OwnerID]
		if !ok || user.Disabled {
			return forbidden()
		}
		def, ok = st.ProgramDefinitions[call.ProgramID]
		if !ok || !def.Enabled {
			return notfound()
		}
		action, ok = findProgramAction(def, call.Action)
		if !ok || !a.programActionAllowed(st, user, def, call.Action) {
			return forbidden()
		}
		return nil
	})
	if err != nil {
		return res, err
	}
	switch call.Action {
	case "start":
		out, e := a.startHostProgram(user, def, action)
		if e != nil {
			return res, e
		}
		res.OK = true
		res.Summary = "등록 프로그램 시작"
		if state, ok := out["state"].(string); ok {
			res.Output = "state=" + state
		}
	case "status":
		out := a.hostProgramStatus(def.ID)
		res.OK = true
		res.Summary = "등록 프로그램 상태 확인"
		if state, ok := out["state"].(string); ok {
			res.Output = "state=" + state
		}
	case "stop":
		out, e := a.stopHostProgram(user, def, false)
		if e != nil {
			return res, e
		}
		res.OK = true
		res.Summary = "등록 프로그램 정상 종료"
		if state, ok := out["state"].(string); ok {
			res.Output = "state=" + state
		}
	case "force_stop":
		if !action.AllowForce {
			return res, forbidden()
		}
		out, e := a.stopHostProgram(user, def, true)
		if e != nil {
			return res, e
		}
		res.OK = true
		res.Summary = "등록 프로그램 강제 종료"
		if state, ok := out["state"].(string); ok {
			res.Output = "state=" + state
		}
	default:
		return res, bad("허용되지 않은 등록 프로그램 동작입니다.")
	}
	return res, nil
}

func (a *App) updateHostProgram(w http.ResponseWriter, r *http.Request, u User) {
	if !isSuperadmin(u) {
		fail(w, forbidden())
		return
	}
	id := r.PathValue("id")
	var in struct {
		Revision    uint64                    `json:"revision"`
		Name        string                    `json:"name"`
		Executable  string                    `json:"executable"`
		WorkDir     string                    `json:"work_dir,omitempty"`
		StopFile    string                    `json:"stop_file,omitempty"`
		Actions     []ProgramActionDefinition `json:"actions"`
		ResourceKey string                    `json:"resource_key,omitempty"`
		Enabled     bool                      `json:"enabled"`
	}
	if err := decode(w, r, &in); err != nil {
		fail(w, err)
		return
	}
	a.hostPrograms.mu.Lock()
	_, running := a.hostPrograms.instances[id]
	a.hostPrograms.mu.Unlock()
	if running {
		fail(w, apiError{409, "실행 중인 프로그램 정의는 변경할 수 없습니다."})
		return
	}
	var old ProgramDefinition
	err := a.store.view(func(st *State) error {
		var ok bool
		old, ok = st.ProgramDefinitions[id]
		if !ok {
			return notfound()
		}
		return nil
	})
	if err != nil {
		fail(w, err)
		return
	}
	def := old
	def.Name = strings.TrimSpace(in.Name)
	def.Executable = filepath.Clean(strings.TrimSpace(in.Executable))
	def.WorkDir = filepath.Clean(strings.TrimSpace(in.WorkDir))
	def.StopFile = filepath.Clean(strings.TrimSpace(in.StopFile))
	def.Actions = in.Actions
	def.ResourceKey = strings.TrimSpace(in.ResourceKey)
	def.Enabled = in.Enabled
	if in.WorkDir == "" {
		def.WorkDir = filepath.Dir(def.Executable)
	}
	if in.StopFile == "" {
		def.StopFile = ""
	}
	if err = validateProgramDefinition(a, def); err != nil {
		fail(w, err)
		return
	}
	hash, _, err := fileSHA256(def.Executable)
	if err != nil {
		fail(w, err)
		return
	}
	def.ExecutableSHA256 = hash
	def.UpdatedAt = now()
	def.Revision = old.Revision + 1
	err = a.store.update(func(st *State) error {
		current, ok := st.ProgramDefinitions[id]
		if !ok {
			return notfound()
		}
		if current.Revision != in.Revision {
			return apiError{409, "프로그램 정의가 다른 작업에서 변경되었습니다. 다시 읽어 주세요."}
		}
		st.ProgramDefinitions[id] = def
		appendAudit(st, AuditEvent{
			ActorID: u.ID, Action: "program.update",
			ResourceType: "program", ResourceID: id, Result: "allowed",
		})
		return nil
	})
	if err != nil {
		fail(w, err)
		return
	}
	respond(w, 200, adminProgramDefinition(def))
}
