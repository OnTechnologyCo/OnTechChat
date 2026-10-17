package server

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"
)

func initialize(c Config, reset bool) error {
	s, e := openStore(c.DataDir)
	if e != nil {
		return e
	}
	defer s.Close()
	var admin User
	hasAdmin := false
	_ = s.view(func(st *State) error {
		for _, u := range st.Users {
			if u.Role == roleSuperadmin || u.Role == roleAdmin {
				admin = u
				hasAdmin = true
				break
			}
		}
		return nil
	})
	if hasAdmin && !reset {
		fmt.Println("Already initialized. Existing accounts unchanged.")
		return nil
	}
	password := randomToken()
	hash, e := passwordHash(password)
	if e != nil {
		return e
	}
	if !hasAdmin {
		admin = User{ID: randomID(), Username: "admin", Role: roleSuperadmin, Created: now()}
	}
	admin.Role = roleSuperadmin
	admin.Hash = hash
	admin.Disabled = false
	name := "INITIAL-LOGIN.txt"
	if reset {
		name = "RESET-LOGIN-" + time.Now().Format("20060102-150405") + ".txt"
	}
	path := filepath.Join(c.DataDir, name)
	contents := fmt.Sprintf("OnTech Chat administrator login\r\nUsername: %s\r\nPassword: %s\r\n\r\nChange the password after first login. Delete this file after saving it securely.\r\n", admin.Username, password)
	if e = writeExclusive(path, []byte(contents), 0600); e != nil {
		return fmt.Errorf("write initial login file (use --reset-admin for recovery): %w", e)
	}
	e = s.update(func(st *State) error {
		if hasAdmin {
			st.Users[admin.ID] = admin
		} else {
			addUser(st, admin)
		}
		for key, se := range st.Sessions {
			if se.UserID == admin.ID {
				delete(st.Sessions, key)
			}
		}
		return nil
	})
	if e != nil {
		_ = os.Remove(path)
		return e
	}
	fmt.Println("Initialized. Login details saved locally to:", path)
	return nil
}
func run() error {
	exe, _ := os.Executable()
	cfgPath := flag.String("config", filepath.Join(filepath.Dir(exe), "config.json"), "configuration path")
	initFlag := flag.Bool("init", false, "initialize configuration and admin account")
	reset := flag.Bool("reset-admin", false, "reset admin password; server must be stopped")
	check := flag.Bool("check-config", false, "validate configuration without network access")
	serviceFlag := flag.Bool("service", false, "run under Windows Service Control Manager")
	migrate := flag.Bool("migrate-config", false, "one-time migration of legacy INI/environment/key references into encrypted JSON; no network")
	publish := flag.String("publish-apk", "", "publish verified APK bundle directory; no user data or server restart")
	showVersion := flag.Bool("version", false, "print version")
	flag.Parse()
	if *showVersion {
		fmt.Println("OnTechChat Server", version)
		return nil
	}
	abs, e := filepath.Abs(*cfgPath)
	if e != nil {
		return e
	}
	if *initFlag {
		if _, e = os.Stat(abs); errors.Is(e, os.ErrNotExist) {
			if e = os.MkdirAll(filepath.Dir(abs), 0755); e != nil {
				return e
			}
			b, _ := json.MarshalIndent(defaults(), "", "  ")
			if e = writeExclusive(abs, append(b, '\n'), 0600); e != nil {
				return e
			}
		}
	}
	c, e := loadConfig(abs)
	if e != nil {
		return e
	}
	if *publish != "" {
		return publishAPK(c, *publish)
	}
	if *migrate {
		st, err := openStore(c.DataDir)
		if err != nil {
			return err
		}
		defer st.Close()
		_, err = migrateConfiguration(c, st)
		if err != nil {
			return err
		}
		fmt.Println("JSON migration complete. No external requests made.")
		return nil
	}
	if *check {
		fmt.Printf("Configuration valid. Mail enabled=%t, FCM enabled=%t. No external requests made.\n", c.Mail.Enabled, c.FCM.Enabled)
		return nil
	}
	if *initFlag || *reset {
		return initialize(c, *reset)
	}
	if *serviceFlag {
		return runWindowsService(c)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return serve(ctx, c, func() {})
}

func serve(ctx context.Context, c Config, ready func()) error {
	for {
		next, reload, e := serveGeneration(ctx, c, ready)
		if e != nil {
			return e
		}
		if !reload || ctx.Err() != nil {
			return nil
		}
		c = next
	}
}
func serveGeneration(ctx context.Context, c Config, ready func()) (Config, bool, error) {
	a, e := newApp(c)
	if e != nil {
		return Config{}, false, e
	}
	defer a.Close()
	c = a.cfg
	userCount := 0
	_ = a.store.view(func(st *State) error { userCount = len(st.Users); return nil })
	if userCount == 0 {
		return Config{}, false, errors.New("run --init before starting the server")
	}
	ln, e := net.Listen("tcp", c.Listen)
	if e != nil {
		return Config{}, false, e
	}
	defer ln.Close()
	server := &http.Server{Handler: a.Handler(), ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 90 * time.Second, IdleTimeout: 75 * time.Second, MaxHeaderBytes: 32 << 10, TLSConfig: &tls.Config{MinVersion: tls.VersionTLS12}}
	stopHTTPS := a.startAutoHTTPS()
	defer stopHTTPS()
	a.startWorkers()
	done := make(chan struct{})
	shutdownDone := make(chan struct{})
	var next Config
	var reload bool
	go func() {
		defer close(shutdownDone)
		select {
		case <-ctx.Done():
		case next = <-a.reload:
			reload = true
			// 브라우저에 저장 응답을 전달한 후 장기 연결과 작업자를 종료합니다.
			timer := time.NewTimer(500 * time.Millisecond)
			select {
			case <-timer.C:
			case <-ctx.Done():
				timer.Stop()
				reload = false
			}
		case <-done:
			return
		}
		a.cancel()
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if e := server.Shutdown(shutdown); e != nil {
			_ = server.Close()
		}
	}()
	defer func() { close(done); <-shutdownDone }()
	scheme := "http"
	if c.TLSCert != "" {
		scheme = "https"
	}
	log.Printf("OnTechChat Server %s listening at %s://%s (mail=%t FCM=%t)", version, scheme, c.Listen, c.Mail.Enabled, c.FCM.Enabled)
	if a.store.recovered {
		log.Print("Recovered last committed backup after interrupted snapshot replacement")
	}
	ready()
	if c.TLSCert != "" {
		e = server.ServeTLS(ln, c.TLSCert, c.TLSKey)
	} else {
		e = server.Serve(ln)
	}
	if errors.Is(e, http.ErrServerClosed) {
		<-shutdownDone
		return next, reload, nil
	}
	return Config{}, false, e
}
func Main() {
	if e := run(); e != nil {
		log.Print("Server stopped: ", e)
		os.Exit(1)
	}
}
