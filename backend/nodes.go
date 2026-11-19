package main

import (
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var reNodeName = regexp.MustCompile(`^[\p{L}\p{N}][\p{L}\p{N} ._-]{1,59}$`)

type Node struct {
	ID        int64      `json:"id"`
	Name      string     `json:"name"`
	Disabled  bool       `json:"disabled"`
	CreatedAt time.Time  `json:"created_at"`
	LastSeen  *time.Time `json:"last_seen"`
	LastIP    string     `json:"last_ip"`
	Version   string     `json:"version"`
	Kind      string     `json:"kind"`
	Ver       string     `json:"ver"`
	Outdated  bool       `json:"outdated"`
	Messages  int64      `json:"messages"`
}

func parseClientUA(ua string) (kind, ver string) {
	name, v, _ := strings.Cut(ua, "/")
	switch name {
	case "NavalRelay-CLI":
		return "cli", v
	case "NavalRelay":
		return "app", v
	}
	return "", ""
}

func verParts(v string) [3]int {
	var out [3]int
	core, _, _ := strings.Cut(v, "-")
	for i, p := range strings.SplitN(core, ".", 3) {
		out[i], _ = strconv.Atoi(p)
	}
	return out
}

func verLess(a, b string) bool {
	x, y := verParts(a), verParts(b)
	for i := range x {
		if x[i] != y[i] {
			return x[i] < y[i]
		}
	}
	return false
}

func (a *App) handleListNodes(w http.ResponseWriter, r *http.Request) {
	rows, err := a.query(r.Context(), `SELECT n.id,n.name,n.disabled,n.created_at,n.last_seen,n.last_ip,n.version,
		(SELECT count(*) FROM messages m WHERE m.node_id=n.id) FROM nodes n WHERE n.kind='relay' ORDER BY n.name`)
	if err != nil {
		writeErr(w, 500, "erro de banco")
		return
	}
	defer rows.Close()
	out := []Node{}
	for rows.Next() {
		var n Node
		if rows.Scan(&n.ID, &n.Name, &n.Disabled, &n.CreatedAt, &n.LastSeen, &n.LastIP, &n.Version, &n.Messages) == nil {
			n.Kind, n.Ver = parseClientUA(n.Version)
			n.Outdated = n.Ver != "" && verLess(n.Ver, version)
			out = append(out, n)
		}
	}
	writeJSON(w, 200, out)
}

func (a *App) handleNodeNames(w http.ResponseWriter, r *http.Request) {
	rows, err := a.query(r.Context(), `SELECT id,name FROM nodes ORDER BY name`)
	if err != nil {
		writeErr(w, 500, "erro de banco")
		return
	}
	defer rows.Close()
	type nn struct {
		ID   int64  `json:"id"`
		Name string `json:"name"`
	}
	out := []nn{}
	for rows.Next() {
		var n nn
		if rows.Scan(&n.ID, &n.Name) == nil {
			out = append(out, n)
		}
	}
	writeJSON(w, 200, out)
}

func newNodeToken() string { return "orb_" + randomToken(24) }

func (a *App) handleCreateNode(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name string `json:"name"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	name := strings.TrimSpace(in.Name)
	if !reNodeName.MatchString(name) {
		writeErr(w, 400, "nome inválido (2 a 60 caracteres: letras, números, espaço, ponto, hífen)")
		return
	}
	tok := newNodeToken()
	res, err := a.exec(r.Context(), `INSERT INTO nodes(name,token_hash) VALUES(?,?)`, name, hashToken(tok))
	if err != nil {
		writeErr(w, 409, "já existe um servidor com esse nome")
		return
	}
	id, _ := res.LastInsertId()
	a.audit(r.Context(), userFrom(r).Username, "node.create", name)
	writeJSON(w, 201, map[string]any{"id": id, "name": name, "token": tok})
}

func (a *App) handleRotateNode(w http.ResponseWriter, r *http.Request) {
	tok := newNodeToken()
	res, err := a.exec(r.Context(), `UPDATE nodes SET token_hash=? WHERE id=?`, hashToken(tok), r.PathValue("id"))
	if n, _ := res.RowsAffected(); err != nil || n == 0 {
		writeErr(w, 404, "servidor não encontrado")
		return
	}
	a.audit(r.Context(), userFrom(r).Username, "node.token", r.PathValue("id"))
	writeJSON(w, 200, map[string]any{"token": tok})
}

func (a *App) handleUpdateNode(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Disabled bool `json:"disabled"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	if _, err := a.exec(r.Context(), `UPDATE nodes SET disabled=? WHERE id=?`, in.Disabled, r.PathValue("id")); err != nil {
		writeErr(w, 500, "erro de banco")
		return
	}
	a.audit(r.Context(), userFrom(r).Username, "node.update", r.PathValue("id"))
	writeJSON(w, 200, map[string]any{"ok": true})
}

func (a *App) handleDeleteNode(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var name string
	if err := a.row(r.Context(), `SELECT name FROM nodes WHERE id=?`, id).Scan(&name); err != nil {
		writeErr(w, 404, "servidor não encontrado")
		return
	}
	_, _ = a.exec(r.Context(), `DELETE FROM messages WHERE node_id=?`, id)
	_, _ = a.exec(r.Context(), `DELETE FROM nodes WHERE id=?`, id)
	a.audit(r.Context(), userFrom(r).Username, "node.delete", name)
	writeJSON(w, 200, map[string]any{"ok": true})
}
