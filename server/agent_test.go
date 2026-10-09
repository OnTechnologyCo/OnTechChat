package server

import (
	"net/http"
	"testing"
)

func TestAgentRoleMigrationIdempotent(t *testing.T) {
	store, err := openStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err = store.update(func(st *State) error {
		addUser(st, User{ID: "legacy-admin", Username: "legacy", Role: "admin", Hash: "x"})
		addUser(st, User{ID: "member", Username: "member", Role: roleUser, Hash: "x"})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err = migrateAgentState(store); err != nil {
		t.Fatal(err)
	}
	if err = migrateAgentState(store); err != nil {
		t.Fatal(err)
	}
	_ = store.view(func(st *State) error {
		if st.Users["legacy-admin"].Role != roleSuperadmin {
			t.Fatalf("legacy admin role=%q", st.Users["legacy-admin"].Role)
		}
		if st.Users["member"].Role != roleUser || st.AgentMigrationVersion != agentStateMigrationVersion {
			t.Fatalf("migration state=%+v version=%d", st.Users["member"], st.AgentMigrationVersion)
		}
		if _, ok := st.AIProfiles[defaultAIProfileID]; !ok {
			t.Fatal("default AI profile missing")
		}
		return nil
	})
}

func TestAgentAIFriendIdempotentAndUserIsolation(t *testing.T) {
	a, server, _, bob, eve := testApp(t)
	a.cfg.AI = aiTestConfig()
	bobToken := loginTest(t, server, bob.Username)
	eveToken := loginTest(t, server, eve.Username)
	open := func(token string) string {
		status, body, _ := request(t, server, token, http.MethodPost, "/api/ai/friends/"+defaultAIProfileID+"/open", map[string]any{})
		expectStatus(t, status, http.StatusCreated, body)
		return decodeMap(t, body)["id"].(string)
	}
	bobRoom := open(bobToken)
	status, body, _ := request(t, server, bobToken, http.MethodPost, "/api/ai/friends/"+defaultAIProfileID+"/open", map[string]any{})
	expectStatus(t, status, http.StatusOK, body)
	if decodeMap(t, body)["id"].(string) != bobRoom {
		t.Fatal("repeated AI friend open created another room")
	}
	eveRoom := open(eveToken)
	if eveRoom == bobRoom {
		t.Fatal("two users share one AI room")
	}
	_ = a.store.view(func(st *State) error {
		if st.Rooms[bobRoom].OwnerID != bob.ID || st.Rooms[eveRoom].OwnerID != eve.ID {
			t.Fatal("AI room ownership mismatch")
		}
		return nil
	})
}

func TestAgentProjectGrantBoundary(t *testing.T) {
	a, server, admin, bob, eve := testApp(t)
	a.cfg.Agent.Enabled = true
	_ = a.store.update(func(st *State) error {
		u := st.Users[bob.ID]
		u.AgentEnabled = true
		st.Users[bob.ID] = u
		return nil
	})
	adminToken := loginTest(t, server, admin.Username)
	bobToken := loginTest(t, server, bob.Username)
	eveToken := loginTest(t, server, eve.Username)
	status, body, _ := request(t, server, bobToken, http.MethodPost, "/api/agent/projects", map[string]any{"name": "개인 프로젝트"})
	expectStatus(t, status, http.StatusCreated, body)
	projectID := decodeMap(t, body)["id"].(string)
	status, body, _ = request(t, server, eveToken, http.MethodGet, "/api/agent/projects/"+projectID, nil)
	expectStatus(t, status, http.StatusNotFound, body)

	status, body, _ = request(t, server, adminToken, http.MethodPost, "/api/grants", map[string]any{
		"subject_id": eve.ID, "resource_type": "project", "resource_id": projectID, "actions": []string{"project.read"},
	})
	expectStatus(t, status, http.StatusCreated, body)
	status, body, _ = request(t, server, eveToken, http.MethodGet, "/api/agent/projects/"+projectID, nil)
	expectStatus(t, status, http.StatusOK, body)
	project := decodeMap(t, body)
	if project["can_write"].(bool) {
		t.Fatal("read grant unexpectedly allowed write")
	}
	status, body, _ = request(t, server, eveToken, http.MethodPatch, "/api/agent/projects/"+projectID, map[string]any{
		"name": "침범", "revision": project["revision"],
	})
	expectStatus(t, status, http.StatusNotFound, body)
}

func TestAgentDelegatedAdminCannotEscalate(t *testing.T) {
	a, server, super, bob, eve := testApp(t)
	hash, _ := passwordHash(testPassword)
	delegated := User{ID: randomID(), Username: "delegated", Role: roleAdmin, Hash: hash}
	if err := a.store.update(func(st *State) error {
		addUser(st, delegated)
		st.AccessGrants["manage-bob"] = AccessGrant{
			ID: "manage-bob", SubjectID: delegated.ID, ResourceType: "user",
			ResourceID: bob.ID, Actions: []string{"user.manage"}, IssuerID: super.ID, CreatedAt: now(), UpdatedAt: now(),
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	token := loginTest(t, server, delegated.Username)
	status, body, _ := request(t, server, token, http.MethodPatch, "/api/users/"+bob.ID, map[string]any{"disabled": true})
	expectStatus(t, status, http.StatusOK, body)
	status, body, _ = request(t, server, token, http.MethodPatch, "/api/users/"+eve.ID, map[string]any{"disabled": true})
	expectStatus(t, status, http.StatusForbidden, body)
	status, body, _ = request(t, server, token, http.MethodPatch, "/api/users/"+super.ID, map[string]any{"disabled": true})
	expectStatus(t, status, http.StatusForbidden, body)
	status, body, _ = request(t, server, token, http.MethodPatch, "/api/users/"+eve.ID+"/role", map[string]any{
		"role": roleSuperadmin, "current_password": testPassword,
	})
	expectStatus(t, status, http.StatusForbidden, body)
}
