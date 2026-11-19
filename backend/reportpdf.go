package main

import (
	"bytes"
	_ "embed"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-pdf/fpdf"
)

//go:embed assets/logo-pdf.png
var logoPDF []byte

//go:embed fonts/LiberationSans-Regular.ttf
var fontRegular []byte

//go:embed fonts/LiberationSans-Bold.ttf
var fontBold []byte

type rgb struct{ r, g, b int }

var (
	cInk     = rgb{27, 34, 48}
	cMuted   = rgb{100, 112, 130}
	cPrimary = rgb{200, 16, 46}
	cAccent  = rgb{139, 106, 43}
	cStripe  = rgb{250, 247, 242}
	cHead    = rgb{249, 234, 236}
	cLine    = rgb{228, 220, 210}
	cCard    = rgb{252, 250, 246}
)

type badgeStyle struct{ bg, fg rgb }

var badgeColors = map[string]badgeStyle{
	"delivered": {rgb{220, 252, 231}, rgb{22, 101, 52}},
	"failed":    {rgb{254, 226, 226}, rgb{153, 27, 27}},
	"deferred":  {rgb{254, 243, 199}, rgb{146, 64, 14}},
	"queued":    {rgb{226, 232, 240}, rgb{51, 65, 85}},
	"accepted":  {rgb{224, 242, 254}, rgb{7, 89, 133}},
}

var statusPT = map[string]string{"delivered": "Entregue", "failed": "Falha", "deferred": "Adiada", "queued": "Em fila", "accepted": "Aceita"}

const (
	pgW, pgH   = 210.0, 297.0
	mL, mR     = 12.0, 12.0
	mTop       = 12.0
	mBottom    = 16.0
	contentW   = pgW - mL - mR
	maxPDFRows = 10000
)

func pdfText(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == '\n' || r == '\t':
			b.WriteRune(' ')
		case r < 32 || r == 0x7f || r > 0xFFFF:
		case r >= 0x200B && r <= 0x200F, r >= 0x202A && r <= 0x202E, r == 0xFE0F, r == 0xFEFF, r == 0x2060:
		default:
			b.WriteRune(r)
		}
	}
	return strings.TrimSpace(b.String())
}

type pdfRep struct {
	p      *fpdf.Fpdf
	title  string
	app    string
	loc    *time.Location
	logoOn bool
}

func (g *pdfRep) color(c rgb)  { g.p.SetTextColor(c.r, c.g, c.b) }
func (g *pdfRep) fill(c rgb)   { g.p.SetFillColor(c.r, c.g, c.b) }
func (g *pdfRep) stroke(c rgb) { g.p.SetDrawColor(c.r, c.g, c.b) }

func (g *pdfRep) logo(cx, cy, s float64) {
	p := g.p
	opt := fpdf.ImageOptions{ImageType: "PNG"}
	if !g.logoOn {
		p.RegisterImageOptionsReader("logo", opt, bytes.NewReader(logoPDF))
		g.logoOn = true
	}
	w := s * 0.904
	p.ImageOptions("logo", cx-w/2, cy-s/2, w, s, false, opt, 0, "")
}

func (g *pdfRep) firstHeader(sub string) float64 {
	p := g.p
	g.logo(mL+8, mTop+8, 16)
	g.color(cInk)
	p.SetFont("lib", "B", 17)
	p.Text(mL+20, mTop+7, pdfText(g.app))
	g.color(cPrimary)
	p.SetFont("lib", "B", 11)
	p.Text(mL+20, mTop+13.2, pdfText(g.title))
	g.color(cMuted)
	p.SetFont("lib", "", 8)
	p.SetXY(pgW-mR-80, mTop+2)
	p.CellFormat(80, 4, "Emitido em "+time.Now().In(g.loc).Format("02/01/2006 15:04"), "", 2, "R", false, 0, "")
	p.CellFormat(80, 4, pdfText(sub), "", 0, "R", false, 0, "")
	g.stroke(cPrimary)
	p.SetLineWidth(0.6)
	p.Line(mL, mTop+19, pgW-mR, mTop+19)
	return mTop + 24
}

func (g *pdfRep) slimHeader() float64 {
	p := g.p
	g.logo(mL+3.5, mTop+3, 7)
	g.color(cInk)
	p.SetFont("lib", "B", 9)
	p.Text(mL+9, mTop+4, pdfText(g.app+" · "+g.title))
	g.stroke(cLine)
	p.SetLineWidth(0.3)
	p.Line(mL, mTop+8, pgW-mR, mTop+8)
	return mTop + 12
}

func (g *pdfRep) wrap(s, style string, size, w float64) []string {
	g.p.SetFont("lib", style, size)
	s = pdfText(s)
	if s == "" {
		return nil
	}
	return g.p.SplitText(s, w)
}

type cline struct {
	txt   string
	style string
	size  float64
	col   rgb
}

func (g *pdfRep) lines(s, style string, size float64, col rgb, w float64) []cline {
	var out []cline
	for _, l := range g.wrap(s, style, size, w) {
		out = append(out, cline{l, style, size, col})
	}
	return out
}

func lineH(c cline) float64 { return c.size*0.4 + 0.55 }

func linesH(ls []cline) float64 {
	h := 0.0
	for _, l := range ls {
		h += lineH(l)
	}
	return h
}

func (g *pdfRep) drawLines(x, y float64, ls []cline) {
	for _, l := range ls {
		g.p.SetFont("lib", l.style, l.size)
		g.color(l.col)
		h := lineH(l)
		g.p.SetXY(x, y)
		g.p.CellFormat(0, h, l.txt, "", 0, "L", false, 0, "")
		y += h
	}
}

func (g *pdfRep) badge(x, y float64, status string) float64 {
	st, ok := badgeColors[status]
	if !ok {
		st = badgeColors["queued"]
	}
	label := statusPT[status]
	if label == "" {
		label = status
	}
	g.p.SetFont("lib", "B", 7)
	w := g.p.GetStringWidth(label) + 4.4
	g.fill(st.bg)
	g.p.RoundedRect(x, y, w, 4.4, 2.2, "1234", "F")
	g.color(st.fg)
	g.p.SetXY(x, y)
	g.p.CellFormat(w, 4.4, label, "", 0, "C", false, 0, "")
	return 4.4
}

type pdfOpts struct {
	node, origin, size, id, summary bool
	asc                             bool
	max                             int
}

func parsePDFOpts(r *http.Request) pdfOpts {
	q := r.URL.Query()
	cols := map[string]bool{}
	for _, c := range strings.Split(q.Get("cols"), ",") {
		cols[strings.TrimSpace(c)] = true
	}
	o := pdfOpts{node: cols["node"], origin: cols["origin"], size: cols["size"], id: cols["id"], summary: q.Get("summary") != "0", asc: q.Get("order") == "asc", max: 2000}
	if n, err := strconv.Atoi(q.Get("max")); err == nil && n > 0 {
		o.max = n
	}
	if o.max > maxPDFRows {
		o.max = maxPDFRows
	}
	return o
}

func (a *App) filterSummary(r *http.Request) []string {
	q := r.URL.Query()
	var out []string
	add := func(k, v string) {
		if strings.TrimSpace(v) != "" {
			out = append(out, k+": "+pdfText(v))
		}
	}
	loc := a.location()
	d := func(s string) string {
		if t, err := time.ParseInLocation("2006-01-02", s, loc); err == nil {
			return t.Format("02/01/2006")
		}
		return ""
	}
	switch from, to := d(q.Get("from")), d(q.Get("to")); {
	case from != "" && to != "":
		add("Período", from+" a "+to)
	case from != "":
		add("Período", "a partir de "+from)
	case to != "":
		add("Período", "até "+to)
	}
	if s := statusPT[q.Get("status")]; s != "" {
		add("Status", s)
	}
	switch q.Get("direction") {
	case "in":
		add("Sentido", "Recebidas")
	case "out":
		add("Sentido", "Enviadas")
	}
	if id, ok := nodeParam(r); ok {
		if id == 0 {
			add("Servidor", "Local")
		} else {
			var name string
			_ = a.row(r.Context(), `SELECT name FROM nodes WHERE id=?`, id).Scan(&name)
			add("Servidor", name)
		}
	}
	var hs []string
	for _, m := range a.hideModes(r) {
		hs = append(hs, map[string]string{"unknown": "falhas por caixa inexistente", "rspamd": "rejeições do Rspamd"}[m])
	}
	if len(hs) > 0 {
		add("Ocultando", strings.Join(hs, " e "))
	}
	add("Remetente", q.Get("sender"))
	add("Destinatário", q.Get("recipient"))
	add("Origem", q.Get("host"))
	add("Busca", q.Get("q"))
	return out
}

func (a *App) handleReportPDF(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	o := parsePDFOpts(r)
	where, args := a.buildFilter(r)
	title := pdfText(r.URL.Query().Get("title"))
	if title == "" {
		title = "Relatório de mensagens"
	}
	if len([]rune(title)) > 80 {
		title = string([]rune(title)[:80])
	}
	loc := a.location()

	var total int
	if err := a.row(ctx, `SELECT count(*) FROM messages m`+where, args...).Scan(&total); err != nil {
		writeErr(w, 500, "erro de banco")
		return
	}
	order := "DESC"
	if o.asc {
		order = "ASC"
	}
	rows, err := a.query(ctx, msgSelect+where+` ORDER BY m.received_at `+order+` LIMIT ?`, append(append([]any{}, args...), o.max)...)
	if err != nil {
		writeErr(w, 500, "erro de banco")
		return
	}
	var items []msgRow
	for rows.Next() {
		if m, err := scanMsg(rows); err == nil {
			items = append(items, m)
		}
	}
	rows.Close()

	byStatus := map[string]int{}
	byDir := map[string]int{}
	daily := map[dayKey]int{}
	if o.summary {
		for _, q := range []struct {
			sql string
			dst map[string]int
		}{
			{`SELECT m.status, count(*) FROM messages m` + where + ` GROUP BY m.status`, byStatus},
			{`SELECT m.direction, count(*) FROM messages m` + where + ` GROUP BY m.direction`, byDir},
		} {
			if rs, err := a.query(ctx, q.sql, args...); err == nil {
				for rs.Next() {
					var k string
					var n int
					if rs.Scan(&k, &n) == nil {
						q.dst[k] = n
					}
				}
				rs.Close()
			}
		}
		_, off := time.Now().In(loc).Zone()
		sign := "+"
		if off < 0 {
			sign, off = "-", -off
		}
		tz := fmt.Sprintf("%s%02d:%02d", sign, off/3600, (off%3600)/60)
		if rs, err := a.query(ctx, `SELECT DATE_FORMAT(CONVERT_TZ(m.received_at,'+00:00',?),'%Y-%m-%d'), m.status, count(*) FROM messages m`+where+` GROUP BY 1,2`,
			append([]any{tz}, args...)...); err == nil {
			for rs.Next() {
				var d, s string
				var n int
				if rs.Scan(&d, &s, &n) == nil {
					daily[dayKey{d, s}] = n
				}
			}
			rs.Close()
		}
	}

	g := &pdfRep{p: fpdf.New("P", "mm", "A4", ""), title: title, app: a.sStr("app_name"), loc: loc}
	p := g.p
	p.AddUTF8FontFromBytes("lib", "", fontRegular)
	p.AddUTF8FontFromBytes("lib", "B", fontBold)
	p.SetMargins(mL, mTop, mR)
	p.SetAutoPageBreak(false, 0)
	p.SetCompression(true)
	p.SetTitle(title, true)
	p.SetCreator("NavalRelay v"+version, true)
	p.SetAuthor(userFrom(r).Username, true)
	p.AliasNbPages("{nb}")
	p.SetFooterFunc(func() {
		g.stroke(cLine)
		p.SetLineWidth(0.3)
		p.Line(mL, pgH-12, pgW-mR, pgH-12)
		p.SetFont("lib", "", 7.5)
		g.color(cMuted)
		p.SetXY(mL, pgH-10.5)
		p.CellFormat(contentW/2, 4, pdfText(g.app)+" v"+version, "", 0, "L", false, 0, "")
		p.CellFormat(contentW/2, 4, fmt.Sprintf("Página %d de {nb}", p.PageNo()), "", 0, "R", false, 0, "")
	})
	p.AddPage()
	sub := fmt.Sprintf("%d mensagem(ns)", total)
	if u := userFrom(r); u.Username != "" {
		sub += " · " + u.Username
	}
	y := g.firstHeader(sub)

	if fs := a.filterSummary(r); len(fs) > 0 {
		g.fill(cCard)
		g.stroke(cLine)
		p.SetLineWidth(0.3)
		txt := strings.Join(fs, "   ·   ")
		ls := g.lines(txt, "", 8, cInk, contentW-8)
		h := linesH(ls) + 9
		p.RoundedRect(mL, y, contentW, h, 1.8, "1234", "DF")
		g.color(cPrimary)
		p.SetFont("lib", "B", 7)
		p.Text(mL+4, y+4.2, "FILTROS APLICADOS")
		g.drawLines(mL+4, y+5.6, ls)
		y += h + 5
	}

	if o.summary {
		y = g.summary(y, total, byStatus, byDir, daily, loc)
	}

	cw := [4]float64{30, 68, 60, 28}
	pad := 1.6
	head := func(y float64) float64 {
		g.fill(cHead)
		p.Rect(mL, y, contentW, 6.4, "F")
		p.SetFont("lib", "B", 7.5)
		g.color(cPrimary)
		x := mL
		for i, t := range []string{"DATA", "REMETENTE E DESTINATÁRIOS", "ASSUNTO", "STATUS"} {
			p.SetXY(x+pad, y)
			p.CellFormat(cw[i]-pad, 6.4, t, "", 0, "L", false, 0, "")
			x += cw[i]
		}
		return y + 6.4
	}
	newPage := func() float64 {
		p.AddPage()
		return head(g.slimHeader())
	}

	if y > pgH-60 {
		p.AddPage()
		y = g.slimHeader()
	}
	g.color(cInk)
	p.SetFont("lib", "B", 11)
	p.Text(mL, y+4, "Mensagens")
	if total > len(items) {
		g.color(cMuted)
		p.SetFont("lib", "", 8)
		p.Text(mL+26, y+4, fmt.Sprintf("exibindo %d de %d (ajuste o limite ou refine os filtros)", len(items), total))
	}
	y = head(y + 7)

	if len(items) == 0 {
		g.color(cMuted)
		p.SetFont("lib", "", 9)
		p.Text(mL+pad, y+7, "Nenhuma mensagem encontrada para os filtros selecionados.")
	}
	for i, m := range items {
		c1 := g.lines(m.ReceivedAt.In(loc).Format("02/01/2006"), "B", 7.5, cInk, cw[0]-2*pad)
		c1 = append(c1, g.lines(m.ReceivedAt.In(loc).Format("15:04:05"), "", 7.5, cMuted, cw[0]-2*pad)...)
		if o.node {
			n := m.Node
			if n == "" {
				n = "Local"
			}
			c1 = append(c1, g.lines(n, "", 6.8, cPrimary, cw[0]-2*pad)...)
		}
		if o.id {
			c1 = append(c1, g.lines(m.EximID, "", 6.4, cMuted, cw[0]-2*pad)...)
		}
		sender := m.Sender
		sc := cInk
		if sender == "" {
			sender, sc = "Sem remetente", cMuted
		}
		c2 := g.lines(sender, "B", 7.5, sc, cw[1]-2*pad)
		if len(m.Recipients) == 0 {
			c2 = append(c2, g.lines("Sem destinatário", "", 7.2, cMuted, cw[1]-2*pad)...)
		} else {
			rc := m.Recipients
			extra := 0
			if len(rc) > 40 {
				extra, rc = len(rc)-40, rc[:40]
			}
			t := "Para: " + strings.Join(rc, ", ")
			if extra > 0 {
				t += fmt.Sprintf(" (+%d destinatários)", extra)
			}
			c2 = append(c2, g.lines(t, "", 7.2, cMuted, cw[1]-2*pad)...)
		}
		subj, subC := m.Subject, cInk
		if pdfText(subj) == "" {
			subj, subC = "Sem assunto", cMuted
		}
		c3 := g.lines(subj, "", 7.5, subC, cw[2]-2*pad)
		if o.origin {
			ot := strings.TrimSpace(m.OriginHost + " " + m.OriginIP)
			if ot == "" {
				ot = "Sem origem"
			}
			c3 = append(c3, g.lines("Origem: "+ot, "", 6.6, cMuted, cw[2]-2*pad)...)
		}
		if o.size {
			c3 = append(c3, g.lines(fmt.Sprintf("Tamanho: %.1f KB", float64(m.Size)/1024), "", 6.6, cMuted, cw[2]-2*pad)...)
		}
		var c4 []cline
		switch m.Direction {
		case "in":
			c4 = g.lines("Recebida", "", 6.8, cMuted, cw[3]-2*pad)
		case "out":
			c4 = g.lines("Enviada", "", 6.8, cMuted, cw[3]-2*pad)
		}
		h := linesH(c1)
		for _, x := range []float64{linesH(c2), linesH(c3), 4.4 + 0.8 + linesH(c4)} {
			if x > h {
				h = x
			}
		}
		h += 2*pad - 0.4
		if y+h > pgH-mBottom {
			y = newPage()
		}
		if i%2 == 1 {
			g.fill(cStripe)
			p.Rect(mL, y, contentW, h, "F")
		}
		g.drawLines(mL+pad, y+pad-0.2, c1)
		g.drawLines(mL+cw[0]+pad, y+pad-0.2, c2)
		g.drawLines(mL+cw[0]+cw[1]+pad, y+pad-0.2, c3)
		bx := mL + cw[0] + cw[1] + cw[2] + pad
		g.badge(bx, y+pad, m.Status)
		g.drawLines(bx, y+pad+5.2, c4)
		g.stroke(cLine)
		p.SetLineWidth(0.15)
		p.Line(mL, y+h, pgW-mR, y+h)
		y += h
	}

	if p.Err() {
		writeErr(w, 500, "falha ao gerar o PDF: "+p.Error().Error())
		return
	}
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", `attachment; filename="navalrelay-relatorio-`+time.Now().In(loc).Format("20060102-150405")+`.pdf"`)
	w.Header().Set("Cache-Control", "no-store")
	_ = p.Output(w)
}

type dayKey struct{ day, status string }

func (g *pdfRep) card(x, y, w float64, label string, n int, col rgb) {
	p := g.p
	g.fill(cCard)
	g.stroke(cLine)
	p.SetLineWidth(0.3)
	p.RoundedRect(x, y, w, 15, 1.8, "1234", "DF")
	g.fill(col)
	p.RoundedRect(x, y+2.5, 1.1, 10, 0.5, "1234", "F")
	g.color(col)
	p.SetFont("lib", "B", 14)
	p.Text(x+4, y+8, strconv.Itoa(n))
	g.color(cMuted)
	p.SetFont("lib", "", 7)
	p.Text(x+4, y+12.2, label)
}

func (g *pdfRep) summary(y float64, total int, byStatus, byDir map[string]int, daily map[dayKey]int, loc *time.Location) float64 {
	p := g.p
	g.color(cInk)
	p.SetFont("lib", "B", 11)
	p.Text(mL, y+4, "Resumo")
	y += 7
	type c struct {
		label string
		n     int
		col   rgb
	}
	cards := []c{
		{"Total", total, cPrimary},
		{"Entregues", byStatus["delivered"], badgeColors["delivered"].fg},
		{"Falhas", byStatus["failed"], badgeColors["failed"].fg},
		{"Adiadas", byStatus["deferred"], badgeColors["deferred"].fg},
	}
	if byStatus["queued"] > 0 {
		cards = append(cards, c{"Em fila", byStatus["queued"], badgeColors["queued"].fg})
	}
	if byStatus["accepted"] > 0 {
		cards = append(cards, c{"Aceitas", byStatus["accepted"], badgeColors["accepted"].fg})
	}
	if byDir["in"] > 0 || byDir["out"] > 0 {
		cards = append(cards, c{"Recebidas", byDir["in"], cPrimary}, c{"Enviadas", byDir["out"], cAccent})
	}
	const perRow = 4
	gap := 3.0
	cwid := (contentW - gap*(perRow-1)) / perRow
	for i, cd := range cards {
		col, row := i%perRow, i/perRow
		g.card(mL+float64(col)*(cwid+gap), y+float64(row)*18, cwid, cd.label, cd.n, cd.col)
	}
	y += float64((len(cards)+perRow-1)/perRow)*18 + 2

	days := map[string]bool{}
	for k := range daily {
		days[k.day] = true
	}
	if len(days) >= 2 && len(days) <= 45 {
		keys := make([]string, 0, len(days))
		for d := range days {
			keys = append(keys, d)
		}
		sort.Strings(keys)
		g.color(cInk)
		p.SetFont("lib", "B", 8.5)
		p.Text(mL, y+3.5, "Mensagens por dia")
		y += 6
		chartH := 34.0
		maxV := 1
		sums := make([]int, len(keys))
		for i, d := range keys {
			for _, st := range []string{"delivered", "failed", "deferred", "queued", "accepted"} {
				sums[i] += daily[dayKey{d, st}]
			}
			if sums[i] > maxV {
				maxV = sums[i]
			}
		}
		g.stroke(cLine)
		p.SetLineWidth(0.2)
		for i := 0; i <= 4; i++ {
			ly := y + chartH - chartH*float64(i)/4
			p.Line(mL+8, ly, pgW-mR, ly)
			g.color(cMuted)
			p.SetFont("lib", "", 6)
			p.SetXY(mL, ly-1.4)
			p.CellFormat(7, 3, strconv.Itoa(maxV*i/4), "", 0, "R", false, 0, "")
		}
		area := contentW - 10
		slot := area / float64(len(keys))
		bw := slot * 0.62
		for i, d := range keys {
			bx := mL + 9 + float64(i)*slot + (slot-bw)/2
			base := y + chartH
			for _, st := range []string{"delivered", "accepted", "queued", "deferred", "failed"} {
				n := daily[dayKey{d, st}]
				if n == 0 {
					continue
				}
				h := chartH * float64(n) / float64(maxV)
				col := badgeColors[st].fg
				g.fill(col)
				p.Rect(bx, base-h, bw, h, "F")
				base -= h
			}
			if len(keys) <= 16 || i%3 == 0 {
				g.color(cMuted)
				p.SetFont("lib", "", 6)
				p.SetXY(bx-slot/2+bw/2, y+chartH+0.8)
				p.CellFormat(slot, 3, d[8:10]+"/"+d[5:7], "", 0, "C", false, 0, "")
			}
		}
		y += chartH + 6
		lx := mL + 9
		for _, st := range []string{"delivered", "accepted", "queued", "deferred", "failed"} {
			g.fill(badgeColors[st].fg)
			p.Rect(lx, y+0.6, 2.4, 2.4, "F")
			g.color(cMuted)
			p.SetFont("lib", "", 7)
			p.Text(lx+3.4, y+2.6, statusPT[st])
			lx += 24
		}
		y += 7
	}
	return y + 3
}
