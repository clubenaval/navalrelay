package main

import (
	"context"
	"encoding/csv"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type msgRow struct {
	ID         int64     `json:"id"`
	EximID     string    `json:"exim_id"`
	ReceivedAt time.Time `json:"received_at"`
	Sender     string    `json:"sender"`
	OriginHost string    `json:"origin_host"`
	OriginIP   string    `json:"origin_ip"`
	Subject    string    `json:"subject"`
	Size       int       `json:"size"`
	Status     string    `json:"status"`
	Recipients []string  `json:"recipients"`
	Direction  string    `json:"direction"`
	NodeID     int64     `json:"node_id"`
	Node       string    `json:"node"`
}

var likeEsc = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)

func (a *App) location() *time.Location {
	if l, err := time.LoadLocation(a.sStr("timezone")); err == nil {
		return l
	}
	return time.UTC
}

func nodeParam(r *http.Request) (int64, bool) {
	s := r.URL.Query().Get("node")
	if s == "local" {
		return 0, true
	}
	id, err := strconv.ParseInt(s, 10, 64)
	return id, err == nil && id > 0
}

const unknownRcptRE = `5[.]1[.][01]|user unknown|unknown user|does not exist|no such user|mailbox unavailable|mailbox not found|unknown recipient|invalid recipient|user not found|no mailbox|undeliverable address`

func (a *App) hideModes(r *http.Request) []string {
	q := r.URL.Query()
	if !q.Has("hide") {
		var out []string
		if a.sBool("hide_unknown_rcpt") {
			out = append(out, "unknown")
		}
		if a.sBool("hide_rspamd_rejects") {
			out = append(out, "rspamd")
		}
		return out
	}
	var out []string
	for _, p := range strings.Split(q.Get("hide"), ",") {
		if p = strings.TrimSpace(p); p == "unknown" || p == "rspamd" {
			out = append(out, p)
		}
	}
	return out
}

func hideSQL(al string, modes []string) (string, []any) {
	var parts []string
	var args []any
	for _, m := range modes {
		var inner string
		switch m {
		case "unknown":
			inner, args = "d.detail REGEXP ?", append(args, unknownRcptRE)
		case "rspamd":
			inner = "d.detail LIKE 'Rspamd:%'"
		default:
			continue
		}
		c := `(AL.status='failed'
			AND EXISTS (SELECT 1 FROM deliveries d WHERE d.message_id=AL.id AND d.status='failed')
			AND NOT EXISTS (SELECT 1 FROM deliveries d WHERE d.message_id=AL.id AND d.status='failed' AND NOT (` + inner + `))
			AND NOT EXISTS (SELECT 1 FROM deliveries d WHERE d.message_id=AL.id AND d.status IN ('delivered','accepted','deferred')))`
		parts = append(parts, strings.ReplaceAll(c, "AL", al))
	}
	if len(parts) == 0 {
		return "", nil
	}
	return "NOT (" + strings.Join(parts, " OR ") + ")", args
}

func (a *App) buildFilter(r *http.Request) (string, []any) { return a.filterConds(r, true) }

func (a *App) filterConds(r *http.Request, withHide bool) (string, []any) {
	q := r.URL.Query()
	var conds []string
	var args []any
	loc := a.location()
	if s := q.Get("from"); s != "" {
		if t, err := time.ParseInLocation("2006-01-02", s, loc); err == nil {
			conds, args = append(conds, "m.received_at >= ?"), append(args, t)
		}
	}
	if s := q.Get("to"); s != "" {
		if t, err := time.ParseInLocation("2006-01-02", s, loc); err == nil {
			conds, args = append(conds, "m.received_at < ?"), append(args, t.AddDate(0, 0, 1))
		}
	}
	if id, ok := nodeParam(r); ok {
		conds, args = append(conds, "m.node_id = ?"), append(args, id)
	}
	if s := q.Get("direction"); s == "in" || s == "out" {
		conds, args = append(conds, "m.direction = ?"), append(args, s)
	}
	if s := q.Get("status"); s == "queued" || s == "accepted" || s == "delivered" || s == "failed" || s == "deferred" {
		conds, args = append(conds, "m.status = ?"), append(args, s)
	}
	if s := strings.TrimSpace(q.Get("sender")); s != "" {
		conds, args = append(conds, `m.sender LIKE ? ESCAPE '\\'`), append(args, "%"+likeEsc.Replace(s)+"%")
	}
	if s := strings.TrimSpace(q.Get("host")); s != "" {
		p := "%" + likeEsc.Replace(s) + "%"
		conds, args = append(conds, `(m.origin_host LIKE ? ESCAPE '\\' OR m.origin_ip LIKE ? ESCAPE '\\')`), append(args, p, p)
	}
	if s := strings.TrimSpace(q.Get("recipient")); s != "" {
		conds, args = append(conds, `EXISTS (SELECT 1 FROM deliveries d WHERE d.message_id=m.id AND d.recipient LIKE ? ESCAPE '\\')`), append(args, "%"+likeEsc.Replace(s)+"%")
	}
	if s := strings.TrimSpace(q.Get("q")); s != "" {
		p := "%" + likeEsc.Replace(s) + "%"
		conds = append(conds, `(m.subject LIKE ? ESCAPE '\\' OR m.sender LIKE ? ESCAPE '\\' OR m.exim_id LIKE ? ESCAPE '\\' OR EXISTS (SELECT 1 FROM deliveries d WHERE d.message_id=m.id AND d.recipient LIKE ? ESCAPE '\\'))`)
		args = append(args, p, p, p, p)
	}
	if withHide {
		if h, ha := hideSQL("m", a.hideModes(r)); h != "" {
			conds, args = append(conds, h), append(args, ha...)
		}
	}
	if len(conds) == 0 {
		return "", args
	}
	return " WHERE " + strings.Join(conds, " AND "), args
}

const msgSelect = `SELECT m.id,m.exim_id,m.received_at,m.sender,m.origin_host,m.origin_ip,m.subject,m.size,m.status,m.direction,
	COALESCE((SELECT GROUP_CONCAT(DISTINCT d.recipient ORDER BY d.recipient SEPARATOR '\n') FROM deliveries d WHERE d.message_id=m.id),''),
	m.node_id, COALESCE(n.name,'') FROM messages m LEFT JOIN nodes n ON n.id=m.node_id`

func scanMsg(sc interface{ Scan(...any) error }) (msgRow, error) {
	var m msgRow
	var rc string
	err := sc.Scan(&m.ID, &m.EximID, &m.ReceivedAt, &m.Sender, &m.OriginHost, &m.OriginIP, &m.Subject, &m.Size, &m.Status, &m.Direction, &rc, &m.NodeID, &m.Node)
	m.Recipients = []string{}
	if rc != "" {
		m.Recipients = strings.Split(rc, "\n")
	}
	return m, err
}

func (a *App) handleMessages(w http.ResponseWriter, r *http.Request) {
	where, args := a.buildFilter(r)
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	size, _ := strconv.Atoi(r.URL.Query().Get("size"))
	if page < 1 {
		page = 1
	}
	if size < 1 || size > 200 {
		size = 50
	}
	var total int
	if err := a.row(r.Context(), `SELECT count(*) FROM messages m`+where, args...).Scan(&total); err != nil {
		writeErr(w, 500, "erro de banco")
		return
	}
	rows, err := a.query(r.Context(), msgSelect+where+` ORDER BY m.received_at DESC LIMIT ? OFFSET ?`,
		append(args, size, (page-1)*size)...)
	if err != nil {
		writeErr(w, 500, "erro de banco")
		return
	}
	defer rows.Close()
	out := []msgRow{}
	for rows.Next() {
		if m, err := scanMsg(rows); err == nil {
			out = append(out, m)
		}
	}
	hidden := 0
	if len(a.hideModes(r)) > 0 {
		w2, a2 := a.filterConds(r, false)
		var all int
		if a.row(r.Context(), `SELECT count(*) FROM messages m`+w2, a2...).Scan(&all) == nil && all > total {
			hidden = all - total
		}
	}
	writeJSON(w, 200, map[string]any{"total": total, "page": page, "size": size, "items": out, "hidden": hidden})
}

func (a *App) handleMessageDetail(w http.ResponseWriter, r *http.Request) {
	m, err := scanMsg(a.row(r.Context(), msgSelect+` WHERE m.id=?`, r.PathValue("id")))
	if err != nil {
		writeErr(w, 404, "mensagem não encontrada")
		return
	}
	rows, err := a.query(r.Context(), `SELECT at,recipient,status,remote_host,remote_ip,detail FROM deliveries WHERE message_id=? ORDER BY at,id`, m.ID)
	if err != nil {
		writeErr(w, 500, "erro de banco")
		return
	}
	defer rows.Close()
	type dl struct {
		At         time.Time `json:"at"`
		Recipient  string    `json:"recipient"`
		Status     string    `json:"status"`
		RemoteHost string    `json:"remote_host"`
		RemoteIP   string    `json:"remote_ip"`
		Detail     string    `json:"detail"`
	}
	ds := []dl{}
	for rows.Next() {
		var d dl
		if rows.Scan(&d.At, &d.Recipient, &d.Status, &d.RemoteHost, &d.RemoteIP, &d.Detail) == nil {
			ds = append(ds, d)
		}
	}
	writeJSON(w, 200, map[string]any{"message": m, "deliveries": ds})
}

func csvSafe(s string) string {
	if s != "" && strings.ContainsRune("=+-@\t\r", rune(s[0])) {
		return "'" + s
	}
	return s
}

func (a *App) handleExport(w http.ResponseWriter, r *http.Request) {
	where, args := a.buildFilter(r)
	rows, err := a.query(r.Context(), msgSelect+where+` ORDER BY m.received_at `+map[bool]string{true: "ASC", false: "DESC"}[r.URL.Query().Get("order") == "asc"]+` LIMIT 200000`, args...)
	if err != nil {
		writeErr(w, 500, "erro de banco")
		return
	}
	defer rows.Close()
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="navalrelay-`+time.Now().Format("20060102-150405")+`.csv"`)
	_, _ = w.Write([]byte("\xef\xbb\xbf"))
	cw := csv.NewWriter(w)
	_ = cw.Write([]string{"Data/Hora", "Servidor", "Direcao", "ID", "Remetente", "Destinatarios", "Assunto", "Status", "Host de origem", "IP de origem", "Tamanho"})
	loc := a.location()
	for rows.Next() {
		m, err := scanMsg(rows)
		if err != nil {
			continue
		}
		_ = cw.Write([]string{m.ReceivedAt.In(loc).Format("2006-01-02 15:04:05"), csvSafe(m.Node), m.Direction, m.EximID, csvSafe(m.Sender),
			csvSafe(strings.Join(m.Recipients, "; ")), csvSafe(m.Subject), m.Status, csvSafe(m.OriginHost), m.OriginIP, strconv.Itoa(m.Size)})
	}
	cw.Flush()
}

type kv struct {
	Key   string `json:"key"`
	Count int    `json:"count"`
}

func (a *App) topList(ctx context.Context, q string, args ...any) []kv {
	out := []kv{}
	rows, err := a.query(ctx, q, args...)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var x kv
		if rows.Scan(&x.Key, &x.Count) == nil {
			out = append(out, x)
		}
	}
	return out
}

func (a *App) handleStats(w http.ResponseWriter, r *http.Request) {
	days, _ := strconv.Atoi(r.URL.Query().Get("days"))
	if days < 1 || days > 365 {
		days = 7
	}
	ctx := r.Context()
	_, offSec := time.Now().In(a.location()).Zone()
	sign := "+"
	if offSec < 0 {
		sign, offSec = "-", -offSec
	}
	tz := fmt.Sprintf("%s%02d:%02d", sign, offSec/3600, (offSec%3600)/60)
	totals := map[string]int{"queued": 0, "accepted": 0, "delivered": 0, "failed": 0, "deferred": 0}
	type pt struct {
		Day    string `json:"day"`
		Status string `json:"status"`
		Count  int    `json:"count"`
	}
	series := []pt{}
	nf, nargs := "", []any{}
	if id, ok := nodeParam(r); ok {
		nf, nargs = " AND node_id=?", []any{id}
	}
	df := strings.ReplaceAll(nf, "node_id", "m.node_id")
	modes := a.hideModes(r)
	hs, hargs := hideSQL("messages", modes)
	hd, hdargs := hideSQL("m", modes)
	hf, hdf := "", ""
	if hs != "" {
		hf, hdf = " AND "+hs, " AND "+hd
	}
	rows, err := a.query(ctx, `SELECT DATE_FORMAT(CONVERT_TZ(received_at,'+00:00',?),'%Y-%m-%d') AS d, status, count(*)
		FROM messages WHERE received_at >= DATE_SUB(UTC_TIMESTAMP(6), INTERVAL ? DAY)`+nf+hf+` GROUP BY d, status ORDER BY d`, append(append([]any{tz, days}, nargs...), hargs...)...)
	if err != nil {
		writeErr(w, 500, "erro de banco")
		return
	}
	for rows.Next() {
		var p pt
		if rows.Scan(&p.Day, &p.Status, &p.Count) == nil {
			series = append(series, p)
			totals[p.Status] += p.Count
		}
	}
	rows.Close()
	writeJSON(w, 200, map[string]any{
		"days": days, "totals": totals, "series": series,
		"top_senders":           a.topList(ctx, `SELECT sender_domain, count(*) c FROM messages WHERE received_at >= DATE_SUB(UTC_TIMESTAMP(6), INTERVAL ? DAY) AND sender_domain<>''`+nf+hf+` GROUP BY 1 ORDER BY c DESC LIMIT 8`, append(append([]any{days}, nargs...), hargs...)...),
		"top_hosts":             a.topList(ctx, `SELECT CASE WHEN origin_host<>'' THEN origin_host ELSE origin_ip END, count(*) c FROM messages WHERE received_at >= DATE_SUB(UTC_TIMESTAMP(6), INTERVAL ? DAY)`+nf+hf+` GROUP BY 1 ORDER BY c DESC LIMIT 8`, append(append([]any{days}, nargs...), hargs...)...),
		"top_recipient_domains": a.topList(ctx, `SELECT SUBSTRING_INDEX(d.recipient,'@',-1), count(*) c FROM deliveries d JOIN messages m ON m.id=d.message_id WHERE d.at >= DATE_SUB(UTC_TIMESTAMP(6), INTERVAL ? DAY)`+df+hdf+` GROUP BY 1 ORDER BY c DESC LIMIT 8`, append(append([]any{days}, nargs...), hdargs...)...),
	})
}
