package main

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	mcHold      = 30 * time.Second
	mcOverlap   = 120
	mcFirstRows = 10000
	mcMaxRows   = 20000
)

var (
	rePfMID   = regexp.MustCompile(`^([0-9A-Za-z]{6,20}): message-id=<(.*)>\s*$`)
	rePfFrom  = regexp.MustCompile(`^([0-9A-Za-z]{6,20}): from=<([^>]*)>, size=(\d+)`)
	rePfTo    = regexp.MustCompile(`^([0-9A-Za-z]{6,20}): to=<([^>]*)>, (?:orig_to=<[^>]*>, )?relay=([^\[,\s]*)(?:\[([^\]]*)\])?(?::\d+)?, .*?status=(\w+) ?(?:\((.*)\))?\s*$`)
	reGenMID  = regexp.MustCompile(`^\d{14}\.`)
	rePfDone  = regexp.MustCompile(`^([0-9A-Za-z]{6,20}): removed\s*$`)
	mcNameErr = errors.New("nome inválido (2 a 60 caracteres: letras, números, espaço, ponto, hífen)")
)

type mcCreds struct {
	base string
	key  string
	skip bool
}

type mcState struct {
	mu   sync.Mutex
	mids map[int64]map[string]mcMid
}

type mcMid struct {
	qid string
	at  time.Time
}

func (a *App) mcMap(node int64) map[string]mcMid {
	a.mc.mu.Lock()
	defer a.mc.mu.Unlock()
	if a.mc.mids == nil {
		a.mc.mids = map[int64]map[string]mcMid{}
	}
	m := a.mc.mids[node]
	if m == nil {
		m = map[string]mcMid{}
		a.mc.mids[node] = m
	}
	return m
}

func normalizeMailcowURL(s string) (string, error) {
	s = strings.TrimSpace(s)
	if !strings.Contains(s, "://") {
		s = "https://" + s
	}
	u, err := url.Parse(s)
	if err != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return "", errors.New("endereço inválido: informe algo como https://mail.exemplo.com.br")
	}
	return u.Scheme + "://" + u.Host, nil
}

func (c mcCreds) get(ctx context.Context, path string, out any) error {
	tr := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		TLSClientConfig:       &tls.Config{InsecureSkipVerify: c.skip, MinVersion: tls.VersionTLS12},
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 40 * time.Second,
		DialContext:           (&net.Dialer{Timeout: 10 * time.Second}).DialContext,
	}
	defer tr.CloseIdleConnections()
	cl := &http.Client{Transport: tr, Timeout: 90 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("X-API-Key", c.key)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "NavalRelay/"+version)
	resp, err := cl.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if err != nil {
		return err
	}
	switch {
	case resp.StatusCode >= 300 && resp.StatusCode < 400:
		return fmt.Errorf("redirecionamento (%d): use o endereço final do Mailcow, com o protocolo correto", resp.StatusCode)
	case resp.StatusCode == 401 || resp.StatusCode == 403:
		return errors.New("acesso negado: confira a chave de API e se o IP deste servidor está liberado em Configuração > Acesso > API do Mailcow")
	case resp.StatusCode != 200:
		return fmt.Errorf("o Mailcow respondeu HTTP %d", resp.StatusCode)
	}
	var probe struct {
		Type string `json:"type"`
		Msg  any    `json:"msg"`
	}
	if json.Unmarshal(body, &probe) == nil && probe.Type == "error" {
		return fmt.Errorf("o Mailcow recusou a chamada: %v", probe.Msg)
	}
	if err := json.Unmarshal(body, out); err != nil {
		return errors.New("resposta inesperada: o endereço é mesmo o de um Mailcow? (a API precisa estar habilitada)")
	}
	return nil
}

func (c mcCreds) version(ctx context.Context) (string, error) {
	var v struct {
		Version string `json:"version"`
	}
	if err := c.get(ctx, "/api/v1/get/status/version", &v); err != nil {
		return "", err
	}
	var probe []json.RawMessage
	if err := c.get(ctx, "/api/v1/get/logs/postfix/1", &probe); err != nil {
		return "", err
	}
	return trunc(v.Version, 32), nil
}

type mcLog struct {
	Message string `json:"message"`
	Program string `json:"program"`
	Time    any    `json:"time"`
}

func anyNum(v any) int64 {
	switch t := v.(type) {
	case float64:
		return int64(t)
	case string:
		f, _ := strconv.ParseFloat(strings.TrimSpace(t), 64)
		return int64(f)
	case json.Number:
		f, _ := t.Float64()
		return int64(f)
	}
	return 0
}

func anyStr(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	}
	return ""
}

func anyList(v any) []string {
	switch t := v.(type) {
	case string:
		if t != "" {
			return []string{t}
		}
	case []any:
		var out []string
		for _, x := range t {
			if s := anyStr(x); s != "" {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}

func (c mcCreds) fetch(ctx context.Context, service string, since int64, first bool, timeOf func(json.RawMessage) int64) ([]json.RawMessage, error) {
	count := 1000
	if first {
		count = mcFirstRows
	}
	for {
		var rows []json.RawMessage
		if err := c.get(ctx, fmt.Sprintf("/api/v1/get/logs/%s/%d", service, count), &rows); err != nil {
			return nil, err
		}
		if len(rows) < count || count >= mcMaxRows || since == 0 || timeOf(rows[len(rows)-1]) < since {
			return rows, nil
		}
		count *= 2
		if count > mcMaxRows {
			count = mcMaxRows
		}
	}
}

func decodeLog(raw json.RawMessage) (mcLog, bool) {
	var l mcLog
	return l, json.Unmarshal(raw, &l) == nil
}

func (a *App) mailcowSync(ctx context.Context, id int64, c mcCreds, curRspamd, curPostfix int64) (int64, int64, string, error) {
	ver, err := c.version(ctx)
	if err != nil {
		return curRspamd, curPostfix, "", err
	}
	mids := a.mcMap(id)
	for k, v := range mids {
		if time.Since(v.at) > 15*time.Minute {
			delete(mids, k)
		}
	}
	since := curPostfix - mcOverlap
	if curPostfix == 0 {
		since = 0
	}
	rows, err := c.fetch(ctx, "postfix", since, curPostfix == 0, func(r json.RawMessage) int64 { l, _ := decodeLog(r); return anyNum(l.Time) })
	if err != nil {
		return curRspamd, curPostfix, ver, err
	}
	type pe struct {
		at int64
		l  mcLog
	}
	var pf []pe
	for i := len(rows) - 1; i >= 0; i-- {
		r := rows[i]
		if l, ok := decodeLog(r); ok {
			if t := anyNum(l.Time); t >= since && t > 0 {
				pf = append(pf, pe{t, l})
			}
		}
	}
	sort.SliceStable(pf, func(i, j int) bool { return pf[i].at < pf[j].at })
	newPostfix := curPostfix
	tx, err := a.db.BeginTx(ctx, nil)
	if err != nil {
		return curRspamd, curPostfix, ver, err
	}
	defer tx.Rollback()
	for _, e := range pf {
		if err := mcPostfixLine(ctx, tx, id, e.at, e.l, mids); err != nil {
			return curRspamd, curPostfix, ver, err
		}
		if e.at > newPostfix {
			newPostfix = e.at
		}
	}

	limit := time.Now().Add(-mcHold).Unix()
	rsince := curRspamd
	rrows, err := c.fetch(ctx, "rspamd-history", rsince, curRspamd == 0, func(r json.RawMessage) int64 {
		var m map[string]any
		_ = json.Unmarshal(r, &m)
		return anyNum(m["unix_time"])
	})
	if err != nil {
		return curRspamd, curPostfix, ver, err
	}
	type re struct {
		at int64
		m  map[string]any
	}
	var rs []re
	for i := len(rrows) - 1; i >= 0; i-- {
		r := rrows[i]
		var m map[string]any
		if json.Unmarshal(r, &m) != nil {
			continue
		}
		if t := anyNum(m["unix_time"]); t > 0 && t >= rsince && t <= limit {
			rs = append(rs, re{t, m})
		}
	}
	sort.SliceStable(rs, func(i, j int) bool { return rs[i].at < rs[j].at })
	newRspamd := curRspamd
	for _, e := range rs {
		if err := mcRspamdEntry(ctx, tx, id, e.at, e.m, mids); err != nil {
			return curRspamd, curPostfix, ver, err
		}
		if e.at > newRspamd {
			newRspamd = e.at
		}
	}
	if err := tx.Commit(); err != nil {
		return curRspamd, curPostfix, ver, err
	}
	return newRspamd, newPostfix, ver, nil
}

func mcStub(ctx context.Context, tx *sql.Tx, node int64, qid string, at time.Time) error {
	_, err := tx.ExecContext(ctx, `INSERT IGNORE INTO messages(node_id,exim_id,received_at) VALUES(?,?,?)`, node, qid, at.UTC())
	return err
}

func mcDelivery(ctx context.Context, tx *sql.Tx, node int64, qid string, at time.Time, rcpt, status, rhost, rip, detail string) error {
	rcpt, rhost = trunc(clean(strings.ToLower(rcpt)), 320), trunc(clean(rhost), 255)
	_, err := tx.ExecContext(ctx, `INSERT IGNORE INTO deliveries(message_id,at,recipient,status,remote_host,remote_ip,detail,dk)
		SELECT id,?,?,?,?,?,?,UNHEX(LEFT(SHA2(CONCAT_WS('|',id,?,?,?,?),256),32)) FROM messages WHERE node_id=? AND exim_id=?`,
		at.UTC(), rcpt, status, rhost, trunc(clean(rip), 64), trunc(clean(detail), 1000),
		at.UTC().Format("2006-01-02 15:04:05"), rcpt, status, rhost, node, qid)
	return err
}

func mcPostfixLine(ctx context.Context, tx *sql.Tx, node int64, ts int64, l mcLog, mids map[string]mcMid) error {
	at := time.Unix(ts, 0)
	msg := l.Message
	run := func(q string, args ...any) error { _, err := tx.ExecContext(ctx, q, args...); return err }
	if m := rePfMID.FindStringSubmatch(msg); m != nil {
		mid := strings.ToLower(strings.TrimSpace(m[2]))
		mids[mid] = mcMid{qid: m[1], at: time.Now()}
		if reGenMID.MatchString(mid) && strings.Contains(mid, "."+strings.ToLower(m[1])+"@") {
			if err := mcStub(ctx, tx, node, m[1], at); err != nil {
				return err
			}
			return run(`UPDATE messages SET subject=IF(subject='',?,subject) WHERE node_id=? AND exim_id=?`, "(Notificação gerada pelo Postfix: aviso de atraso ou devolução)", node, m[1])
		}
		return nil
	}
	if m := rePfFrom.FindStringSubmatch(msg); m != nil {
		if err := mcStub(ctx, tx, node, m[1], at); err != nil {
			return err
		}
		size, _ := strconv.Atoi(m[3])
		from := strings.ToLower(m[2])
		return run(`UPDATE messages SET sender=IF(sender='',?,sender), sender_domain=IF(sender_domain='',?,sender_domain), size=IF(size=0,?,size)
			WHERE node_id=? AND exim_id=?`, trunc(clean(from), 320), trunc(clean(domainOf(from)), 255), size, node, m[1])
	}
	if m := rePfTo.FindStringSubmatch(msg); m != nil {
		qid := m[1]
		status := map[string]string{"sent": "delivered", "bounced": "failed", "expired": "failed", "deferred": "deferred"}[m[5]]
		if status == "" {
			return nil
		}
		if err := mcStub(ctx, tx, node, qid, at); err != nil {
			return err
		}
		if err := mcDelivery(ctx, tx, node, qid, at, m[2], status, m[3], m[4], m[6]); err != nil {
			return err
		}
		switch {
		case m[3] == "dovecot" || strings.HasPrefix(l.Program, "postfix/lmtp"):
			_ = run(`UPDATE messages SET direction='in' WHERE node_id=? AND exim_id=? AND direction=''`, node, qid)
		case strings.HasPrefix(l.Program, "postfix/smtp"):
			_ = run(`UPDATE messages SET direction='out' WHERE node_id=? AND exim_id=? AND direction=''`, node, qid)
		}
		switch status {
		case "failed":
			return run(`UPDATE messages SET status='failed' WHERE node_id=? AND exim_id=?`, node, qid)
		case "deferred":
			return run(`UPDATE messages SET status='deferred' WHERE node_id=? AND exim_id=? AND status='queued'`, node, qid)
		}
		return nil
	}
	if m := rePfDone.FindStringSubmatch(msg); m != nil {
		if err := mcStub(ctx, tx, node, m[1], at); err != nil {
			return err
		}
		return run(`UPDATE messages SET completed_at=?, status=CASE WHEN EXISTS
			(SELECT 1 FROM deliveries d WHERE d.message_id=messages.id AND d.status='failed') THEN 'failed'
			WHEN EXISTS (SELECT 1 FROM deliveries d WHERE d.message_id=messages.id) THEN 'delivered' ELSE status END
			WHERE node_id=? AND exim_id=?`, at.UTC(), node, m[1])
	}
	return nil
}

func mcRspamdEntry(ctx context.Context, tx *sql.Tx, node int64, ts int64, m map[string]any, mids map[string]mcMid) error {
	at := time.Unix(ts, 0)
	mid := strings.ToLower(strings.Trim(strings.TrimSpace(anyStr(m["message-id"])), "<>"))
	qid := strings.TrimSpace(anyStr(m["qid"]))
	if qid == "" || qid == "unknown" {
		qid = mids[mid].qid
	}
	matched := qid != ""
	if !matched {
		sum := sha256.Sum256([]byte(fmt.Sprintf("%s|%d|%s", mid, ts, anyStr(m["sender_smtp"]))))
		qid = "r" + hex.EncodeToString(sum[:])[:19]
	}
	sender := strings.ToLower(strings.Trim(strings.TrimSpace(anyStr(m["sender_smtp"])), "<>"))
	rcpts := anyList(m["rcpt_smtp"])
	user := anyStr(m["user"])
	dir := "in"
	if sy, ok := m["symbols"].(map[string]any); (ok && sy["MAILCOW_AUTH"] != nil) || (user != "" && user != "unknown") {
		dir = "out"
	}
	subj := strings.NewReplacer(`\"`, `"`, `\\`, `\`).Replace(anyStr(m["subject"]))
	if d, err := wordDecoder.DecodeHeader(subj); err == nil {
		subj = d
	}
	subj = clean(subj)
	size := int(anyNum(m["size"]))
	ip := strings.TrimPrefix(anyStr(m["ip"]), "::ffff:")
	if ip == "unknown" {
		ip = ""
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO messages(node_id,exim_id,received_at,sender,sender_domain,origin_ip,subject,size,direction)
		VALUES(?,?,?,?,?,?,?,?,?)
		ON DUPLICATE KEY UPDATE received_at=VALUES(received_at), sender=VALUES(sender), sender_domain=VALUES(sender_domain),
		origin_ip=VALUES(origin_ip), subject=VALUES(subject), size=IF(VALUES(size)>0,VALUES(size),size), direction=VALUES(direction)`,
		node, qid, at.UTC(), trunc(clean(sender), 320), trunc(clean(domainOf(sender)), 255), trunc(clean(ip), 64), trunc(subj, 500), size, dir); err != nil {
		return err
	}
	act := anyStr(m["action"])
	detail := fmt.Sprintf("Rspamd: %s (pontuação %s de %s)", act, anyStr(m["score"]), anyStr(m["required_score"]))
	status := ""
	switch act {
	case "reject":
		status = "failed"
	case "soft reject":
		status = "deferred"
	}
	if status == "" {
		if matched {
			return nil
		}
		for _, r := range rcpts {
			if err := mcDelivery(ctx, tx, node, qid, at, r, "accepted", "", ip, "Aceita pelo Rspamd; sem registro de entrega do Postfix nos logs do Mailcow"); err != nil {
				return err
			}
		}
		_, err := tx.ExecContext(ctx, `UPDATE messages SET status='accepted', completed_at=? WHERE node_id=? AND exim_id=? AND status='queued'`, at.UTC(), node, qid)
		return err
	}
	for _, r := range rcpts {
		if err := mcDelivery(ctx, tx, node, qid, at, r, status, "", ip, detail); err != nil {
			return err
		}
	}
	if status == "failed" {
		_, err := tx.ExecContext(ctx, `UPDATE messages SET status='failed', completed_at=? WHERE node_id=? AND exim_id=?`, at.UTC(), node, qid)
		return err
	}
	_, err := tx.ExecContext(ctx, `UPDATE messages SET status='deferred' WHERE node_id=? AND exim_id=? AND status='queued'`, node, qid)
	return err
}

func (a *App) mcCreds(url, enc string, skip bool) (mcCreds, error) {
	key, err := a.decrypt(enc)
	if err != nil {
		return mcCreds{}, errors.New("não foi possível ler a chave de API salva (regrave-a)")
	}
	return mcCreds{base: url, key: key, skip: skip}, nil
}

func (a *App) runMailcow(ctx context.Context) {
	t := time.NewTicker(30 * time.Second)
	defer t.Stop()
	for {
		if a.ready() {
			a.mailcowTick(ctx)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func (a *App) mailcowTick(ctx context.Context) {
	rows, err := a.query(ctx, `SELECT id,name,url,COALESCE(api_key,''),skip_verify,cur_rspamd,cur_postfix,last_error,sync_v FROM nodes WHERE kind='mailcow' AND disabled=0`)
	if err != nil {
		return
	}
	type job struct {
		id        int64
		name, url string
		enc       string
		skip      bool
		cr, cp    int64
		lastErr   string
		sv        int
	}
	var jobs []job
	for rows.Next() {
		var j job
		if rows.Scan(&j.id, &j.name, &j.url, &j.enc, &j.skip, &j.cr, &j.cp, &j.lastErr, &j.sv) == nil {
			jobs = append(jobs, j)
		}
	}
	rows.Close()
	for _, j := range jobs {
		if ctx.Err() != nil {
			return
		}
		if j.sv < 4 {
			j.cr, j.cp = 0, 0
		}
		c, err := a.mcCreds(j.url, j.enc, j.skip)
		var ver string
		cr, cp := j.cr, j.cp
		if err == nil {
			sc, cancel := context.WithTimeout(ctx, 4*time.Minute)
			cr, cp, ver, err = a.mailcowSync(sc, j.id, c, j.cr, j.cp)
			cancel()
		}
		if err != nil {
			msg := trunc(err.Error(), 500)
			if msg != j.lastErr {
				log.Printf("mailcow %q: %v", j.name, err)
			}
			_, _ = a.exec(ctx, `UPDATE nodes SET last_error=? WHERE id=?`, msg, j.id)
			continue
		}
		_, _ = a.exec(ctx, `UPDATE nodes SET cur_rspamd=?, cur_postfix=?, last_sync=UTC_TIMESTAMP(6), last_seen=UTC_TIMESTAMP(6), last_error='', sync_v=4, version=? WHERE id=?`, cr, cp, ver, j.id)
	}
}

type mcNode struct {
	ID       int64      `json:"id"`
	Name     string     `json:"name"`
	URL      string     `json:"url"`
	Disabled bool       `json:"disabled"`
	Skip     bool       `json:"skip_verify"`
	LastSync *time.Time `json:"last_sync"`
	Error    string     `json:"last_error"`
	Version  string     `json:"version"`
	Messages int64      `json:"messages"`
}

func (a *App) handleListMailcow(w http.ResponseWriter, r *http.Request) {
	rows, err := a.query(r.Context(), `SELECT n.id,n.name,n.url,n.disabled,n.skip_verify,n.last_sync,n.last_error,n.version,
		(SELECT count(*) FROM messages m WHERE m.node_id=n.id) FROM nodes n WHERE n.kind='mailcow' ORDER BY n.name`)
	if err != nil {
		writeErr(w, 500, "erro de banco")
		return
	}
	defer rows.Close()
	out := []mcNode{}
	for rows.Next() {
		var n mcNode
		if rows.Scan(&n.ID, &n.Name, &n.URL, &n.Disabled, &n.Skip, &n.LastSync, &n.Error, &n.Version, &n.Messages) == nil {
			out = append(out, n)
		}
	}
	writeJSON(w, 200, out)
}

type mcInput struct {
	Name   string `json:"name"`
	URL    string `json:"url"`
	APIKey string `json:"api_key"`
	Skip   bool   `json:"skip_verify"`
}

func (a *App) mcCheck(ctx context.Context, in mcInput) (mcCreds, string, error) {
	base, err := normalizeMailcowURL(in.URL)
	if err != nil {
		return mcCreds{}, "", err
	}
	key := strings.TrimSpace(in.APIKey)
	if key == "" {
		return mcCreds{}, "", errors.New("informe a chave de API (somente leitura)")
	}
	c := mcCreds{base: base, key: key, skip: in.Skip}
	ctx, cancel := context.WithTimeout(ctx, 40*time.Second)
	defer cancel()
	v, err := c.version(ctx)
	return c, v, err
}

func (a *App) handleTestMailcow(w http.ResponseWriter, r *http.Request) {
	var in mcInput
	if !readJSON(w, r, &in) {
		return
	}
	if in.APIKey == "" && in.Name != "" {
		var u, enc string
		var skip bool
		if a.row(r.Context(), `SELECT url,COALESCE(api_key,''),skip_verify FROM nodes WHERE kind='mailcow' AND name=?`, in.Name).Scan(&u, &enc, &skip) == nil {
			if k, err := a.decrypt(enc); err == nil {
				in.APIKey = k
			}
		}
	}
	_, v, err := a.mcCheck(r.Context(), in)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "version": v})
}

func (a *App) handleCreateMailcow(w http.ResponseWriter, r *http.Request) {
	var in mcInput
	if !readJSON(w, r, &in) {
		return
	}
	in.Name = strings.TrimSpace(in.Name)
	if !reNodeName.MatchString(in.Name) {
		writeErr(w, 400, mcNameErr.Error())
		return
	}
	c, v, err := a.mcCheck(r.Context(), in)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	enc, err := a.encrypt(c.key)
	if err != nil {
		writeErr(w, 500, "erro ao proteger a chave")
		return
	}
	res, err := a.exec(r.Context(), `INSERT INTO nodes(name,token_hash,kind,url,api_key,skip_verify,version,sync_v) VALUES(?,?,'mailcow',?,?,?,?,4)`,
		in.Name, hashToken(newNodeToken()), c.base, enc, c.skip, v)
	if err != nil {
		writeErr(w, 409, "já existe um servidor com esse nome")
		return
	}
	id, _ := res.LastInsertId()
	a.audit(r.Context(), userFrom(r).Username, "mailcow.create", in.Name)
	writeJSON(w, 201, map[string]any{"id": id, "version": v})
}

func (a *App) handleEditMailcow(w http.ResponseWriter, r *http.Request) {
	var in mcInput
	if !readJSON(w, r, &in) {
		return
	}
	var name, u, enc string
	if err := a.row(r.Context(), `SELECT name,url,COALESCE(api_key,'') FROM nodes WHERE id=? AND kind='mailcow'`, r.PathValue("id")).Scan(&name, &u, &enc); err != nil {
		writeErr(w, 404, "servidor não encontrado")
		return
	}
	if strings.TrimSpace(in.URL) == "" {
		in.URL = u
	}
	if strings.TrimSpace(in.APIKey) == "" {
		k, err := a.decrypt(enc)
		if err != nil {
			writeErr(w, 400, "informe a chave de API novamente")
			return
		}
		in.APIKey = k
	}
	c, v, err := a.mcCheck(r.Context(), in)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	enc, err = a.encrypt(c.key)
	if err != nil {
		writeErr(w, 500, "erro ao proteger a chave")
		return
	}
	if _, err := a.exec(r.Context(), `UPDATE nodes SET url=?,api_key=?,skip_verify=?,version=?,last_error='' WHERE id=?`, c.base, enc, c.skip, v, r.PathValue("id")); err != nil {
		writeErr(w, 500, "erro de banco")
		return
	}
	a.audit(r.Context(), userFrom(r).Username, "mailcow.update", name)
	writeJSON(w, 200, map[string]any{"ok": true, "version": v})
}
