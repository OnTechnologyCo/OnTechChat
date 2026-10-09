package server

import (
	"net/http"
	"sort"
	"strings"
)

const (
	roleUser       = "user"
	roleAdmin      = "admin"
	roleSuperadmin = "superadmin"

	agentStateMigrationVersion = 1
)

type AccessGrant struct {
	ID           string   `json:"id"`
	SubjectID    string   `json:"subject_id"`
	ResourceType string   `json:"resource_type"`
	ResourceID   string   `json:"resource_id"`
	Actions      []string `json:"actions"`
	IssuerID     string   `json:"issuer_id"`
	ExpiresAt    int64    `json:"expires_at,omitempty"`
	Revoked      bool     `json:"revoked,omitempty"`
	CreatedAt    int64    `json:"created_at"`
	UpdatedAt    int64    `json:"updated_at"`
}

type AuditEvent struct {
	ID             string `json:"id"`
	ActorID        string `json:"actor_id"`
	Action         string `json:"action"`
	ResourceType   string `json:"resource_type,omitempty"`
	ResourceID     string `json:"resource_id,omitempty"`
	JobID          string `json:"job_id,omitempty"`
	Result         string `json:"result"`
	Code           string `json:"code,omitempty"`
	PolicyRevision uint64 `json:"policy_revision"`
	CreatedAt      int64  `json:"created_at"`
}

func validRole(role string) bool {
	switch role {
	case roleUser, roleAdmin, roleSuperadmin:
		return true
	default:
		return false
	}
}

func isSuperadmin(u User) bool { return u.Role == roleSuperadmin }

func grantHasAction(g AccessGrant, action string) bool {
	if g.Revoked || (g.ExpiresAt > 0 && g.ExpiresAt <= now()) {
		return false
	}
	for _, v := range g.Actions {
		if v == action {
			return true
		}
	}
	return false
}

func hasGrant(st *State, subjectID, resourceType, resourceID, action string) bool {
	for _, g := range st.AccessGrants {
		if g.SubjectID == subjectID && g.ResourceType == resourceType && g.ResourceID == resourceID && grantHasAction(g, action) {
			return true
		}
	}
	return false
}

func canManageUser(st *State, actor, target User) bool {
	if actor.Disabled || target.ID == actor.ID {
		return false
	}
	if actor.Role == roleSuperadmin {
		return true
	}
	if actor.Role != roleAdmin || target.Role != roleUser {
		return false
	}
	return hasGrant(st, actor.ID, "user", target.ID, "user.manage")
}

func canAccessProject(st *State, actor User, project AgentProject, action string) bool {
	if actor.Disabled || !project.Enabled {
		return false
	}
	if project.OwnerID == actor.ID {
		return true
	}
	return hasGrant(st, actor.ID, "project", project.ID, action)
}

func appendAudit(st *State, event AuditEvent) {
	if event.ID == "" {
		event.ID = randomID()
	}
	if event.CreatedAt == 0 {
		event.CreatedAt = now()
	}
	event.PolicyRevision = st.Revision + 1
	st.AuditEvents = append(st.AuditEvents, event)
	if len(st.AuditEvents) > 2000 {
		st.AuditEvents = append([]AuditEvent(nil), st.AuditEvents[len(st.AuditEvents)-2000:]...)
	}
}

func countEnabledSuperadmins(st *State, excluding string) int {
	n := 0
	for _, u := range st.Users {
		if u.ID != excluding && u.Role == roleSuperadmin && !u.Disabled {
			n++
		}
	}
	return n
}

func migrateAgentState(s *Store) error {
	return s.update(func(st *State) error {
		changed := ensureAgentStateMaps(st)
		if st.AgentMigrationVersion < agentStateMigrationVersion {
			for id, u := range st.Users {
				if u.Role == "admin" {
					u.Role = roleSuperadmin
					st.Users[id] = u
					changed = true
				}
			}
			st.AgentMigrationVersion = agentStateMigrationVersion
			changed = true
		}
		if ensureDefaultAIProfile(st) {
			changed = true
		}
		if !changed {
			return errUnchanged
		}
		return nil
	})
}

func ensureAgentStateMaps(st *State) bool {
	changed := false
	if st.AIProfiles == nil {
		st.AIProfiles = map[string]AIProfile{}
		changed = true
	}
	if st.AIDefaultRooms == nil {
		st.AIDefaultRooms = map[string]string{}
		changed = true
	}
	if st.AccessGrants == nil {
		st.AccessGrants = map[string]AccessGrant{}
		changed = true
	}
	if st.AgentProjects == nil {
		st.AgentProjects = map[string]AgentProject{}
		changed = true
	}
	if st.AgentJobs == nil {
		st.AgentJobs = map[string]AgentJob{}
		changed = true
	}
	if st.AgentArtifacts == nil {
		st.AgentArtifacts = map[string]AgentArtifact{}
		changed = true
	}
	if st.ProgramDefinitions == nil {
		st.ProgramDefinitions = map[string]ProgramDefinition{}
		changed = true
	}
	if st.ProgramInstances == nil {
		st.ProgramInstances = map[string]ProgramInstance{}
		changed = true
	}
	if st.AuditEvents == nil {
		st.AuditEvents = []AuditEvent{}
		changed = true
	}
	return changed
}

func normalizeGrantActions(actions []string) ([]string, error) {
	if len(actions) == 0 || len(actions) > 16 {
		return nil, bad("권한 동작은 1~16개가 필요합니다.")
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(actions))
	for _, raw := range actions {
		v := strings.TrimSpace(raw)
		if len(v) < 3 || len(v) > 64 || strings.ContainsAny(v, " \t\r\n\x00") || seen[v] {
			return nil, bad("권한 동작 형식을 확인하세요.")
		}
		seen[v] = true
		out = append(out, v)
	}
	sort.Strings(out)
	return out, nil
}

func (a *App) listGrants(w http.ResponseWriter, r *http.Request, u User) {
	if !isSuperadmin(u) {
		fail(w, forbidden())
		return
	}
	out := []AccessGrant{}
	err := a.store.view(func(st *State) error {
		for _, g := range st.AccessGrants {
			out = append(out, g)
		}
		return nil
	})
	if err != nil {
		fail(w, err)
		return
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt > out[j].CreatedAt })
	respond(w, 200, out)
}

func (a *App) createGrant(w http.ResponseWriter, r *http.Request, u User) {
	if !isSuperadmin(u) {
		fail(w, forbidden())
		return
	}
	var in struct {
		SubjectID    string   `json:"subject_id"`
		ResourceType string   `json:"resource_type"`
		ResourceID   string   `json:"resource_id"`
		Actions      []string `json:"actions"`
		ExpiresAt    int64    `json:"expires_at,omitempty"`
	}
	if err := decode(w, r, &in); err != nil {
		fail(w, err)
		return
	}
	actions, err := normalizeGrantActions(in.Actions)
	if err != nil {
		fail(w, err)
		return
	}
	if in.ResourceType != "user" && in.ResourceType != "project" && in.ResourceType != "program" {
		fail(w, bad("지원하지 않는 권한 대상입니다."))
		return
	}
	g := AccessGrant{ID: randomID(), SubjectID: in.SubjectID, ResourceType: in.ResourceType, ResourceID: in.ResourceID, Actions: actions, IssuerID: u.ID, ExpiresAt: in.ExpiresAt, CreatedAt: now(), UpdatedAt: now()}
	err = a.store.update(func(st *State) error {
		subject, ok := st.Users[g.SubjectID]
		if !ok || subject.Disabled || subject.Role == roleSuperadmin {
			return bad("권한을 받을 사용자를 확인하세요.")
		}
		switch g.ResourceType {
		case "user":
			if target, ok := st.Users[g.ResourceID]; !ok || target.Role != roleUser {
				return bad("관리 대상 사용자를 확인하세요.")
			}
		case "project":
			if _, ok := st.AgentProjects[g.ResourceID]; !ok {
				return notfound()
			}
		case "program":
			if _, ok := st.ProgramDefinitions[g.ResourceID]; !ok {
				return notfound()
			}
		}
		st.AccessGrants[g.ID] = g
		appendAudit(st, AuditEvent{ActorID: u.ID, Action: "grant.create", ResourceType: g.ResourceType, ResourceID: g.ResourceID, Result: "allowed"})
		return nil
	})
	if err != nil {
		fail(w, err)
		return
	}
	a.directoryChanged()
	respond(w, http.StatusCreated, g)
}

func (a *App) revokeGrant(w http.ResponseWriter, r *http.Request, u User) {
	if !isSuperadmin(u) {
		fail(w, forbidden())
		return
	}
	id := r.PathValue("id")
	err := a.store.update(func(st *State) error {
		g, ok := st.AccessGrants[id]
		if !ok {
			return notfound()
		}
		if g.Revoked {
			return errUnchanged
		}
		g.Revoked = true
		g.UpdatedAt = now()
		st.AccessGrants[id] = g
		appendAudit(st, AuditEvent{ActorID: u.ID, Action: "grant.revoke", ResourceType: g.ResourceType, ResourceID: g.ResourceID, Result: "allowed"})
		return nil
	})
	if err != nil {
		fail(w, err)
		return
	}
	if a.agentJobs != nil {
		a.agentJobs.cancelUnauthorized()
	}
	a.directoryChanged()
	respond(w, 200, map[string]bool{"ok": true})
}
