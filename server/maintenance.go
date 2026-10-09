package server

import (
	"os"
	"path/filepath"
	"strings"
	"time"
)

func (a *App) maintenanceLoop() {
	a.cleanup()
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-a.ctx.Done():
			return
		case <-ticker.C:
			a.cleanup()
		}
	}
}
func (a *App) cleanup() {
	needs := false
	_ = a.store.view(func(st *State) error {
		for _, m := range st.Messages {
			if m.Expires > 0 && m.Expires <= now() {
				needs = true
			}
		}
		for owner, points := range st.LocationHistory {
			for _, p := range points {
				_, ok := st.Users[owner]
				if p.Captured <= now()-historyRetention || !ok {
					needs = true
					break
				}
			}
		}
		for _, q := range st.LocationRequests {
			if q.Created+86400 <= now() {
				needs = true
			}
		}
		for _, s := range st.Sessions {
			if !s.active() {
				needs = true
			}
		}
		for _, p := range st.Outbox {
			if p.Expires+7*86400 <= now() {
				needs = true
			}
		}
		for _, q := range st.AIRequests {
			if aiTerminal(q.Status) && q.FinishedAt > 0 && q.FinishedAt+30*86400 <= now() {
				needs = true
			}
		}
		return nil
	})
	rooms := map[string]bool{}
	historyRemoved := false
	if needs {
		e := a.store.update(func(st *State) error {
			historyRemoved = pruneLocationHistory(st)
			kept := st.Messages[:0]
			removed := map[int64]bool{}
			for _, m := range st.Messages {
				if m.Expires > 0 && m.Expires <= now() {
					rooms[m.RoomID] = true
					removed[m.ID] = true
					continue
				}
				kept = append(kept, m)
			}
			st.Messages = kept
			for id, q := range st.LocationRequests {
				if q.Created+86400 <= now() {
					delete(st.LocationRequests, id)
				}
			}
			for k, s := range st.Sessions {
				if !s.active() {
					delete(st.Sessions, k)
				}
			}
			for k, p := range st.Outbox {
				if removed[p.MessageID] || p.Expires+7*86400 <= now() {
					delete(st.Outbox, k)
				}
			}
			for id, q := range st.AIRequests {
				if aiTerminal(q.Status) && q.FinishedAt > 0 && q.FinishedAt+30*86400 <= now() {
					delete(st.AIRequests, id)
				}
			}
			return nil
		})
		if e == nil {
			if len(rooms) > 0 || historyRemoved {
				a.store.discardOldBackup()
			}
			for id := range rooms {
				a.changed(id)
			}
		}
	}
	a.uploadMu.Lock()
	defer a.uploadMu.Unlock()
	referenced := map[string]bool{}
	_ = a.store.view(func(st *State) error {
		for id := range st.Files {
			referenced[id] = true
		}
		return nil
	})
	files, _ := os.ReadDir(filepath.Join(a.cfg.DataDir, "files"))
	for _, f := range files {
		if f.IsDir() || referenced[f.Name()] || (!fileIDPattern.MatchString(f.Name()) && !strings.HasPrefix(f.Name(), ".upload-")) {
			continue
		}
		info, e := f.Info()
		if e == nil && time.Since(info.ModTime()) > time.Hour {
			_ = os.Remove(filepath.Join(a.cfg.DataDir, "files", f.Name()))
		}
	}
}
