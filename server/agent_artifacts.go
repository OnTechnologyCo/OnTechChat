package server

import (
	"fmt"
	"net/http"
	"os"
	"strings"
)

func (a *App) downloadAgentArtifact(w http.ResponseWriter, r *http.Request, u User) {
	id := r.PathValue("id")
	var artifact AgentArtifact
	var project AgentProject
	err := a.store.view(func(st *State) error {
		var ok bool
		artifact, ok = st.AgentArtifacts[id]
		if !ok {
			return notfound()
		}
		project, ok = st.AgentProjects[artifact.ProjectID]
		if !ok || !canAccessProject(st, u, project, "project.read") {
			return notfound()
		}
		if artifact.OwnerID != u.ID &&
			!hasGrant(st, u.ID, "project", artifact.ProjectID, "project.read") &&
			!hasGrant(st, u.ID, "project", artifact.ProjectID, "project.manage") {
			return notfound()
		}
		return nil
	})
	if err != nil {
		fail(w, err)
		return
	}
	root, err := a.projectRoot(project)
	if err != nil {
		fail(w, err)
		return
	}
	path, err := resolveAgentPath(root, artifact.RelativePath, false)
	if err != nil {
		fail(w, notfound())
		return
	}
	hash, size, err := fileSHA256(path)
	if err != nil || hash != artifact.SHA256 || size != artifact.Size {
		fail(w, apiError{409, "산출물 파일이 등록 이후 변경되어 다운로드할 수 없습니다."})
		return
	}
	f, err := os.Open(path)
	if err != nil {
		fail(w, notfound())
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		fail(w, notfound())
		return
	}
	name := strings.ReplaceAll(artifact.Name, string(rune(34)), "")
	name = strings.ReplaceAll(name, "\r", "")
	name = strings.ReplaceAll(name, "\n", "")
	w.Header().Set("Content-Type", artifact.MIME)
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", name))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeContent(w, r, name, info.ModTime(), f)
}
