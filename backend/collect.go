package main

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"orbitarelay/internal/eximlog"
	"orbitarelay/internal/push"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	maxCollectEvents = 5000
	maxCollectBody   = 8 << 20
)

type pushStatus struct {
	mu      sync.Mutex
	lastOK  time.Time
	lastErr string
	sent    int64
}

func (p *pushStatus) ok(n int) {
	p.mu.Lock()
	p.lastOK, p.lastErr = time.Now(), ""
	p.sent += int64(n)
	p.mu.Unlock()
}

func (p *pushStatus) fail(err error) (changed bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	msg := err.Error()
	changed = p.lastErr != msg
	p.lastErr = msg
	return
}

func (p *pushStatus) idleFor() time.Duration {
	p.mu.Lock()
	defer p.mu.Unlock()
	return time.Since(p.lastOK)
}

func (a *App) centralClient() (*push.Client, error) {
	host, port := a.sStr("collect_host"), a.sInt("collect_port")
	if host == "" || port == 0 {
		return nil, errors.New("servidor central não configurado")
	}
	return &push.Client{Scheme: a.sStr("collect_scheme"), Host: host, Port: port, BasePath: a.sStr("collect_path"),
		Token: a.sStr("collect_token"), SkipVerify: a.sBool("collect_skip_verify"), UserAgent: "NavalRelay/" + version}, nil
}

func (a *App) pushBatch(ctx context.Context, evs []logEvent) error {
	if len(evs) == 0 {
		return nil
	}
	c, err := a.centralClient()
	if err != nil {
		return err
	}
	if err := c.Send(ctx, evs); err != nil {
		return err
	}
	a.push.ok(len(evs))
	return nil
}

func (a *App) pingCentral(ctx context.Context) (push.Info, error) {
	c, err := a.centralClient()
	if err != nil {
		return push.Info{}, err
	}
	return c.Ping(ctx)
}

func (a *App) runPush(ctx context.Context) {
	for {
		wait := 10 * time.Second
		if a.ready() {
			if n := a.sInt("collect_interval"); n >= 2 {
				wait = time.Duration(n) * time.Second
			}
			if a.sStr("deploy_mode") == "client" {
				err := a.ingestOnce(ctx, "push", a.pushBatch)
				if err == nil && a.push.idleFor() > time.Minute {
					_, err = a.pingCentral(ctx)
					if err == nil {
						a.push.ok(0)
					}
				}
				if err != nil && ctx.Err() == nil {
					if a.push.fail(err) {
						log.Printf("envio ao servidor central: %v", err)
					}
				}
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
	}
}

func (a *App) handleCollectTest(w http.ResponseWriter, r *http.Request) {
	out, err := a.pingCentral(r.Context())
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "node": out.Node, "version": out.Version})
}

func (a *App) handleCollectStatus(w http.ResponseWriter, r *http.Request) {
	a.push.mu.Lock()
	out := map[string]any{"last_error": a.push.lastErr, "sent": a.push.sent}
	if !a.push.lastOK.IsZero() {
		out["last_ok"] = a.push.lastOK
	}
	a.push.mu.Unlock()
	pending := int64(0)
	if fi, err := os.Stat(filepath.Join(a.logDir, "main.log")); err == nil {
		var ino, pos int64
		if a.row(r.Context(), `SELECT inode,pos FROM ingest_state WHERE file='push'`).Scan(&ino, &pos) != nil || ino == eximlog.InodeOf(fi) {
			pending = max(fi.Size()-pos, 0)
		} else {
			pending = fi.Size()
		}
	}
	out["pending_bytes"] = pending
	writeJSON(w, 200, out)
}

type nodeCtx struct {
	ID   int64
	Name string
}

func (a *App) nodeAuth(w http.ResponseWriter, r *http.Request) (nodeCtx, bool) {
	var n nodeCtx
	if a.sStr("deploy_mode") != "server" {
		writeErr(w, 404, "não encontrado")
		return n, false
	}
	ip := clientIP(r)
	if a.collectRL.over("collect:" + ip) {
		w.Header().Set("Retry-After", "300")
		writeErr(w, 429, "muitas tentativas")
		return n, false
	}
	tok, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if ok && tok != "" {
		err := a.row(r.Context(), `SELECT id,name FROM nodes WHERE token_hash=? AND disabled=0 AND kind='relay'`, hashToken(strings.TrimSpace(tok))).Scan(&n.ID, &n.Name)
		if err == nil {
			_, _ = a.exec(r.Context(), `UPDATE nodes SET last_seen=UTC_TIMESTAMP(6), last_ip=?, version=? WHERE id=?`,
				trunc(ip, 64), trunc(r.Header.Get("User-Agent"), 32), n.ID)
			return n, true
		}
	}
	a.collectRL.allow("collect:" + ip)
	writeErr(w, 401, "token inválido")
	return n, false
}

func (a *App) handleCollectPing(w http.ResponseWriter, r *http.Request) {
	n, ok := a.nodeAuth(w, r)
	if !ok {
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "node": n.Name, "version": version})
}

func (a *App) handleCollect(w http.ResponseWriter, r *http.Request) {
	n, ok := a.nodeAuth(w, r)
	if !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxCollectBody)
	var rd io.Reader = r.Body
	if strings.EqualFold(r.Header.Get("Content-Encoding"), "gzip") {
		gz, err := gzip.NewReader(r.Body)
		if err != nil {
			writeErr(w, 400, "gzip inválido")
			return
		}
		defer gz.Close()
		rd = io.LimitReader(gz, 4*maxCollectBody)
	}
	var in struct {
		Events []push.WireEvent `json:"events"`
	}
	if err := json.NewDecoder(rd).Decode(&in); err != nil {
		writeErr(w, 400, "corpo inválido")
		return
	}
	if len(in.Events) > maxCollectEvents {
		writeErr(w, 413, "lote grande demais")
		return
	}
	evs := make([]logEvent, 0, len(in.Events))
	limit := time.Now().Add(48 * time.Hour)
	for _, e := range in.Events {
		if !eximlog.ReID.MatchString(e.ID) || e.At.IsZero() || e.At.After(limit) || e.At.Year() < 2000 {
			continue
		}
		ev := logEvent{At: e.At.UTC(), ID: e.ID, Kind: e.Kind, Size: max(e.Size, 0)}
		switch e.Kind {
		case "<=":
			ev.Sender, ev.Host, ev.IP = strings.ToLower(e.Sender), e.Host, e.IP
			ev.Subject = trunc(clean(e.Subject), 500)
		case "=>", "->", "**", "==":
			if e.Rcpt == "" {
				continue
			}
			ev.Rcpt, ev.RHost, ev.RIP = strings.ToLower(e.Rcpt), e.RHost, e.RIP
			ev.Detail = trunc(clean(e.Detail), 1000)
		case "Completed":
		default:
			continue
		}
		evs = append(evs, ev)
	}
	if err := a.flush(r.Context(), n.ID, evs); err != nil {
		log.Printf("coleta de %s: %v", n.Name, err)
		writeErr(w, 500, "erro de banco")
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "accepted": len(evs), "rejected": len(in.Events) - len(evs)})
}
