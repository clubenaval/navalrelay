package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	_ "github.com/go-sql-driver/mysql"
)

var version = "0.0.4"

type App struct {
	dataDir   string
	configDir string
	logDir    string
	webDir    string
	key       []byte

	mu       sync.RWMutex
	db       *sql.DB
	settings map[string]any

	dbFromEnv  bool
	setupToken string
	limiter    *rateLimiter
	collectRL  *rateLimiter
	resetRL    *rateLimiter
	push       pushStatus
	mc         mcState
}

func env(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

func readJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		writeErr(w, 400, "corpo inválido")
		return false
	}
	return true
}

func (a *App) ready() bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.db != nil
}

func (a *App) activate(ctx context.Context, pool *sql.DB) error {
	a.mu.Lock()
	a.db = pool
	a.settings = map[string]any{}
	a.mu.Unlock()
	if err := a.migrate(ctx); err != nil {
		return err
	}
	if err := a.loadSettings(ctx); err != nil {
		return err
	}
	return a.renderMTA(ctx)
}

func main() {
	a := &App{
		dataDir:   env("ORBITA_DATA_DIR", "/data"),
		configDir: env("ORBITA_CONFIG_DIR", "/config"),
		logDir:    env("ORBITA_LOG_DIR", "/var/log/exim"),
		webDir:    env("ORBITA_WEB_DIR", "/app/web"),
		settings:  map[string]any{},
		limiter:   newRateLimiter(10, 5*time.Minute),
		collectRL: newRateLimiter(10, 5*time.Minute),
		resetRL:   newRateLimiter(6, 15*time.Minute),
	}
	if err := os.MkdirAll(a.dataDir, 0o700); err != nil {
		log.Fatal(err)
	}
	key, err := loadOrCreateMasterKey(a.dataDir)
	if err != nil {
		log.Fatal(err)
	}
	a.key = key

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if c, ok := dbConfigFromEnv(); ok {
		a.dbFromEnv = true
		var pool *sql.DB
		var err error
		for i := 0; i < 30; i++ {
			if pool, err = connectDB(ctx, c.dsn()); err == nil || ctx.Err() != nil {
				break
			}
			log.Printf("aguardando o banco %s:%d (%v)", c.Host, c.Port, err)
			time.Sleep(2 * time.Second)
		}
		if err != nil {
			log.Fatalf("não foi possível conectar ao banco configurado no docker-compose: %v", err)
		}
		if err := a.activate(ctx, pool); err != nil {
			log.Fatalf("falha ao inicializar: %v", err)
		}
	} else if c, err := a.loadBootstrap(); err == nil {
		if pool, err := connectDB(ctx, c.dsn()); err != nil {
			log.Printf("banco indisponível: %v (entrando em modo de configuração)", err)
		} else if err := a.activate(ctx, pool); err != nil {
			log.Fatalf("falha ao inicializar: %v", err)
		}
	}
	if !a.ready() {
		_ = a.renderDefaultMTA()
	}
	a.setupToken = a.ensureSetupToken()

	go a.runIngest(ctx)
	go a.runPush(ctx)
	go a.runMailcow(ctx)
	go a.runMaintenance(ctx)

	srv := &http.Server{
		Addr:              env("ORBITA_LISTEN", ":8080"),
		Handler:           a.routes(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       60 * time.Second,
		WriteTimeout:      5 * time.Minute,
		IdleTimeout:       120 * time.Second,
	}
	go func() {
		<-ctx.Done()
		sc, c := context.WithTimeout(context.Background(), 10*time.Second)
		defer c()
		_ = srv.Shutdown(sc)
	}()
	log.Printf("NavalRelay v%s ouvindo em %s", version, srv.Addr)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}

func (a *App) handleBranding(w http.ResponseWriter, r *http.Request) {
	out := map[string]any{"version": version, "setup_required": !a.setupDone(), "db_ready": a.ready(), "app_name": "NavalRelay", "default_theme": "dark"}
	if a.ready() {
		out["app_name"] = a.sStr("app_name")
		out["timezone"] = a.sStr("timezone")
		out["default_theme"] = a.sStr("default_theme")
		out["relay_mode"] = a.sStr("relay_mode")
		out["deploy_mode"] = a.sStr("deploy_mode")
		out["auth_mode"] = a.sStr("auth_mode")
		var mc int
		_ = a.row(r.Context(), `SELECT count(*) FROM nodes WHERE kind='mailcow'`).Scan(&mc)
		out["has_mailcow"] = mc > 0
		out["can_reset"] = a.canReset()
		var hd []string
		if a.sBool("hide_unknown_rcpt") {
			hd = append(hd, "unknown")
		}
		if a.sBool("hide_rspamd_rejects") {
			hd = append(hd, "rspamd")
		}
		out["hide_default"] = strings.Join(hd, ",")
		out["wizard_done"] = a.sBool("wizard_done")
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, 200, out)
}

func dbConfigFromEnv() (dbConfig, bool) {
	host := os.Getenv("ORBITA_DB_HOST")
	if host == "" {
		return dbConfig{}, false
	}
	port, err := strconv.Atoi(env("ORBITA_DB_PORT", "3306"))
	if err != nil || port < 1 || port > 65535 {
		port = 3306
	}
	return dbConfig{
		Host: host, Port: port, User: env("ORBITA_DB_USER", "orbita"), Password: os.Getenv("ORBITA_DB_PASSWORD"),
		Name: env("ORBITA_DB_NAME", "orbita"), SSLMode: env("ORBITA_DB_SSL", "preferred"),
	}, true
}
