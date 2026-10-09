package server

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAgentAutoWorkspaceRolePolicyAndIdempotency(t *testing.T) {
	a, _, _, bob, eve := testApp(t)
	a.cfg.Agent.Enabled = false // Legacy switch is intentionally ignored by automatic AI workspace.
	if err := a.store.update(func(st *State) error {
		v := st.Users[eve.ID]
		v.Role = roleAdmin
		st.Users[eve.ID] = v
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	var adminUser User
	_ = a.store.view(func(st *State) error {
		adminUser = st.Users[eve.ID]
		return nil
	})

	userProject, err := a.ensureDefaultAgentProject(bob)
	if err != nil {
		t.Fatal(err)
	}
	again, err := a.ensureDefaultAgentProject(bob)
	if err != nil {
		t.Fatal(err)
	}
	if userProject.ID != again.ID || !again.Default {
		t.Fatalf("default workspace is not idempotent: %#v %#v", userProject, again)
	}

	adminProject, err := a.ensureDefaultAgentProject(adminUser)
	if err != nil {
		t.Fatal(err)
	}
	userRoot, err := a.projectRoot(userProject)
	if err != nil {
		t.Fatal(err)
	}
	adminRoot, err := a.projectRoot(adminProject)
	if err != nil {
		t.Fatal(err)
	}
	if strings.EqualFold(userRoot, adminRoot) {
		t.Fatal("different users received the same workspace")
	}
	for _, tc := range []struct {
		root     string
		username string
		role     string
	}{
		{userRoot, bob.Username, roleUser},
		{adminRoot, adminUser.Username, roleAdmin},
	} {
		for _, name := range []string{"AGENT.md", "AGENTS.md", "CLAUDE.md"} {
			raw, err := os.ReadFile(filepath.Join(tc.root, name))
			if err != nil {
				t.Fatalf("%s missing: %v", name, err)
			}
			text := string(raw)
			if !strings.Contains(text, "username: "+tc.username) ||
				!strings.Contains(text, "role: "+tc.role) {
				t.Fatalf("%s has wrong role policy: %s", name, text)
			}
		}
	}

	if err = os.WriteFile(filepath.Join(userRoot, "AGENT.md"), []byte("# changed"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = a.ensureDefaultAgentProject(bob); err != nil {
		t.Fatal(err)
	}
	repaired, err := os.ReadFile(filepath.Join(userRoot, "AGENT.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(repaired), "username: "+bob.Username) ||
		strings.Contains(string(repaired), "# changed") {
		t.Fatalf("managed policy was not repaired: %s", repaired)
	}

	for _, name := range []string{"AGENT.md", "agents.md", "CLAUDE.md"} {
		if err := validateAgentToolCall(AgentToolCall{
			ID: "policy-write", Name: "workspace.write", Path: name, Content: "override",
		}); err == nil {
			t.Fatalf("managed policy write accepted: %s", name)
		}
	}
	if err := validateAgentToolCall(AgentToolCall{
		ID: "normal-write", Name: "workspace.write", Path: "notes.md", Content: "ok",
	}); err != nil {
		t.Fatalf("normal workspace write rejected: %v", err)
	}
}

func TestAutomaticClaudeChatCreatesWorkspaceWithoutAgentToggle(t *testing.T) {
	a, _, _, bob, _ := testApp(t)
	cfg := claudeAgentTestConfig()
	cfg.Providers[0].Executable = filepath.Join(os.Getenv("SystemRoot"), "System32", "cmd.exe")
	a.cfg.AI = cfg
	a.cfg.Agent.Enabled = false

	room := Room{
		ID: randomID(), Name: "auto-agent", OwnerID: bob.ID, Kind: "ai", Created: now(),
		Members: map[string]int64{bob.ID: 0}, AIProfileID: defaultAIProfileID,
		AIModelID: cfg.Models[0].ID, AIEffort: cfg.Models[0].DefaultEffort,
	}
	if err := a.store.update(func(st *State) error {
		st.Rooms[room.ID] = room
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	message, job, created, err := a.enqueueAutomaticAgentJob(
		room, bob, "간단한 프로그램을 만들어 줘", "autoclient001",
	)
	if err != nil {
		t.Fatal(err)
	}
	if !created || job.Mode != "auto" || job.ProjectID == "" || message.ID == 0 {
		t.Fatalf("automatic job not created: created=%v job=%+v message=%+v", created, job, message)
	}

	var project AgentProject
	var storedUser User
	if err = a.store.view(func(st *State) error {
		project = st.AgentProjects[job.ProjectID]
		storedUser = st.Users[bob.ID]
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if storedUser.AgentEnabled {
		t.Fatal("test requires legacy per-user agent toggle to remain off")
	}
	if !project.Default || project.OwnerID != bob.ID {
		t.Fatalf("wrong automatic workspace: %+v", project)
	}
	root, err := a.projectRoot(project)
	if err != nil {
		t.Fatal(err)
	}
	policy, err := os.ReadFile(filepath.Join(root, "AGENT.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(policy), "role: user") {
		t.Fatalf("automatic policy missing user role: %s", policy)
	}

	duplicateMessage, duplicateJob, duplicateCreated, err := a.enqueueAutomaticAgentJob(
		room, bob, "간단한 프로그램을 만들어 줘", "autoclient001",
	)
	if err != nil {
		t.Fatal(err)
	}
	if duplicateCreated || duplicateJob.ID != job.ID || duplicateMessage.ID != message.ID {
		t.Fatal("automatic AI request idempotency failed")
	}
}

func TestAgentAutoWorkspaceDoesNotReuseLegacyProject(t *testing.T) {
	a, _, _, bob, _ := testApp(t)
	legacy := AgentProject{
		ID: randomID(), OwnerID: bob.ID, Name: "Legacy Project",
		StorageKey: randomID(), Enabled: true, Default: false, Revision: 1,
		QuotaBytes: a.cfg.Agent.ProjectQuotaBytes, CreatedAt: now() - 100, UpdatedAt: now() - 100,
	}
	if err := a.store.update(func(st *State) error {
		st.AgentProjects[legacy.ID] = legacy
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	auto, err := a.ensureDefaultAgentProject(bob)
	if err != nil {
		t.Fatal(err)
	}
	if auto.ID == legacy.ID || !auto.Default || auto.OwnerID != bob.ID {
		t.Fatalf("automatic workspace reused legacy project: legacy=%+v auto=%+v", legacy, auto)
	}
	if _, err = os.Stat(filepath.Join(mustProjectRoot(t, a, auto), "AGENT.md")); err != nil {
		t.Fatalf("automatic policy missing: %v", err)
	}
}

func mustProjectRoot(t *testing.T, a *App, p AgentProject) string {
	t.Helper()
	root, err := a.projectRoot(p)
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func TestAgentLiveAutomaticClaudeWorkspace(t *testing.T) {
	if os.Getenv("ONTECH_AGENT_LIVE") != "1" {
		t.Skip("set ONTECH_AGENT_LIVE=1 for actual automatic Claude workspace verification")
	}
	a, server, _, bob, _ := testApp(t)
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	goRoot := filepath.Join(root, "tools", "go")
	if _, err = os.Stat(filepath.Join(goRoot, "bin", "go.exe")); err != nil {
		t.Skipf("project Go toolchain unavailable: %v", err)
	}
	a.cfg.AI = claudeAgentTestConfig()
	a.cfg.Agent.GoRoot = goRoot
	a.cfg.Agent.TimeoutSeconds = 180
	a.agentPlanner = newDefaultAgentPlanner()
	a.wg.Add(1)
	go func() {
		defer a.wg.Done()
		a.agentJobs.run(a.ctx)
	}()
	token := loginTest(t, server, bob.Username)
	status, body, _ := request(t, server, token, http.MethodPost,
		"/api/ai/friends/"+defaultAIProfileID+"/open", map[string]any{})
	if status != http.StatusCreated && status != http.StatusOK {
		t.Fatalf("open AI friend: %d %s", status, body)
	}
	roomID := decodeMap(t, body)["id"].(string)
	status, body, _ = request(t, server, token, http.MethodPost,
		"/api/rooms/"+roomID+"/messages", map[string]any{
			"text":      "AGENT.md에서 현재 사용자 역할을 확인한 뒤 현재 작업공간에 auto-proof.txt를 만들고 정확히 ONTECH_AUTO_POLICY_OK 한 줄만 저장해 주세요. 파일을 다시 읽어 확인하고 다른 경로·네트워크·프로그램 실행은 사용하지 마세요.",
			"client_id": "liveautoclaude01",
		})
	expectStatus(t, status, http.StatusAccepted, body)
	jobData := decodeMap(t, body)["agent_job"]
	if jobData == nil {
		t.Fatalf("automatic agent job missing: %s", body)
	}
	jobID := jobData.(map[string]any)["id"].(string)
	deadline := time.Now().Add(120 * time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(500 * time.Millisecond)
		status, body, _ = request(t, server, token, http.MethodGet,
			"/api/agent/jobs/"+jobID, nil)
		expectStatus(t, status, http.StatusOK, body)
		if decodeMap(t, body)["status"] == agentCompleted {
			break
		}
	}
	if decodeMap(t, body)["status"] != agentCompleted {
		t.Fatalf("automatic Claude job not completed: %s", body)
	}
	project, err := a.ensureDefaultAgentProject(bob)
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := a.projectRoot(project)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(workspace, "auto-proof.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(raw)) != "ONTECH_AUTO_POLICY_OK" {
		t.Fatalf("unexpected proof file: %q", raw)
	}
}
