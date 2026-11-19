package main

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/base64"
	"errors"
	"fmt"
	"html/template"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/textproto"
	"strings"
	"time"
)

//go:embed assets/logo-mail.png
var mailLogo []byte

type mailMsg struct {
	Preheader   string
	Title       string
	Paragraphs  []string
	Code        string
	CodeNote    string
	ButtonLabel string
	ButtonURL   string
	Notice      string
}

type mailView struct {
	mailMsg
	App     string
	Version string
	Year    int
}

var mailTpl = template.Must(template.New("mail").Parse(`<!doctype html>
<html lang="pt-BR"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<meta name="color-scheme" content="light dark"><meta name="supported-color-schemes" content="light dark">
<title>{{.Title}}</title></head>
<body style="margin:0;padding:0;background:#f4f1ec;">
<div style="display:none;max-height:0;overflow:hidden;opacity:0;color:transparent;">{{.Preheader}}</div>
<table role="presentation" width="100%" cellpadding="0" cellspacing="0" style="background:#f4f1ec;padding:28px 12px;">
<tr><td align="center">
<table role="presentation" width="560" cellpadding="0" cellspacing="0" style="width:100%;max-width:560px;border-radius:18px;overflow:hidden;background:#ffffff;box-shadow:0 8px 30px rgba(8,16,32,0.12);">
<tr><td style="background:#0a1020;background-image:linear-gradient(135deg,#0a1020 0%,#10213a 100%);padding:26px 32px;border-bottom:3px solid #c8102e;">
<table role="presentation" cellpadding="0" cellspacing="0"><tr>
<td style="vertical-align:middle;"><img src="cid:logo@navalrelay" width="52" height="52" alt="{{.App}}" style="display:block;border:0;"></td>
<td style="vertical-align:middle;padding-left:14px;font-family:'Segoe UI',Helvetica,Arial,sans-serif;">
<div style="font-size:22px;font-weight:700;letter-spacing:0.3px;color:#ffffff;">{{.App}}</div>
<div style="font-size:12px;letter-spacing:1.6px;text-transform:uppercase;color:#e2c27d;margin-top:2px;">Relay SMTP e relatórios</div></td>
</tr></table></td></tr>
<tr><td style="padding:34px 32px 8px 32px;font-family:'Segoe UI',Helvetica,Arial,sans-serif;color:#1e293b;">
<h1 style="margin:0 0 16px 0;font-size:22px;line-height:1.3;color:#0a1020;">{{.Title}}</h1>
{{range .Paragraphs}}<p style="margin:0 0 14px 0;font-size:15px;line-height:1.6;color:#334155;">{{.}}</p>{{end}}
{{if .Code}}<table role="presentation" width="100%" cellpadding="0" cellspacing="0" style="margin:22px 0 10px 0;"><tr><td align="center" style="background:#0a1020;border:1px solid #c8102e;border-radius:14px;padding:20px 12px;">
<div style="font-family:'SFMono-Regular',Consolas,'Liberation Mono',Menlo,monospace;font-size:34px;font-weight:700;letter-spacing:10px;color:#e2c27d;padding-left:10px;">{{.Code}}</div></td></tr></table>
{{if .CodeNote}}<p style="margin:0 0 14px 0;font-size:13px;color:#64748b;text-align:center;">{{.CodeNote}}</p>{{end}}{{end}}
{{if .ButtonURL}}<table role="presentation" cellpadding="0" cellspacing="0" style="margin:22px auto 10px auto;"><tr><td align="center" style="border-radius:12px;background:#c8102e;">
<a href="{{.ButtonURL}}" style="display:inline-block;padding:13px 28px;font-size:15px;font-weight:700;color:#ffffff;text-decoration:none;border-radius:12px;">{{.ButtonLabel}}</a></td></tr></table>
<p style="margin:0 0 14px 0;font-size:12px;color:#94a3b8;text-align:center;word-break:break-all;">Se o botão não funcionar, copie e cole este endereço no navegador:<br>{{.ButtonURL}}</p>{{end}}
{{if .Notice}}<div style="margin:22px 0 10px 0;padding:12px 14px;border-left:3px solid #f59e0b;background:#fffbeb;border-radius:6px;font-size:13px;line-height:1.5;color:#78350f;">{{.Notice}}</div>{{end}}
</td></tr>
<tr><td style="padding:18px 32px 28px 32px;font-family:'Segoe UI',Helvetica,Arial,sans-serif;">
<div style="border-top:1px solid #e2e8f0;padding-top:16px;font-size:12px;line-height:1.6;color:#94a3b8;text-align:center;">
Esta é uma mensagem automática de segurança do {{.App}}. Não responda a este e-mail.<br>
{{.App}} v{{.Version}} · Desenvolvido por <a href="https://clubenaval.org.br" style="color:#c8102e;text-decoration:none;">Assessoria de TI</a></div></td></tr>
</table></td></tr></table></body></html>`))

func (a *App) renderMail(m mailMsg) (string, string, error) {
	v := mailView{mailMsg: m, App: a.sStr("app_name"), Version: version, Year: time.Now().Year()}
	var h bytes.Buffer
	if err := mailTpl.Execute(&h, v); err != nil {
		return "", "", err
	}
	var t strings.Builder
	t.WriteString(v.App + "\n" + strings.Repeat("=", len([]rune(v.App))) + "\n\n" + m.Title + "\n\n")
	for _, p := range m.Paragraphs {
		t.WriteString(p + "\n\n")
	}
	if m.Code != "" {
		t.WriteString("    " + m.Code + "\n\n")
		if m.CodeNote != "" {
			t.WriteString(m.CodeNote + "\n\n")
		}
	}
	if m.ButtonURL != "" {
		t.WriteString(m.ButtonLabel + ": " + m.ButtonURL + "\n\n")
	}
	if m.Notice != "" {
		t.WriteString(m.Notice + "\n\n")
	}
	t.WriteString("--\nMensagem automática do " + v.App + " v" + version + ". Não responda a este e-mail.\n")
	return t.String(), h.String(), nil
}

func qp(s string) []byte {
	var b bytes.Buffer
	w := quotedprintable.NewWriter(&b)
	_, _ = w.Write([]byte(s))
	_ = w.Close()
	return b.Bytes()
}

func (a *App) buildMIME(from, to, subject, text, html string) ([]byte, error) {
	if strings.ContainsAny(to+from+subject, "\r\n") {
		return nil, errors.New("endereço inválido")
	}
	var out bytes.Buffer
	rel := multipart.NewWriter(&out)
	hdr := func(k, v string) { fmt.Fprintf(&out, "%s: %s\r\n", k, v) }
	hdr("From", mime.QEncoding.Encode("utf-8", a.sStr("app_name"))+" <"+from+">")
	hdr("To", "<"+to+">")
	hdr("Subject", mime.QEncoding.Encode("utf-8", subject))
	hdr("Date", time.Now().Format(time.RFC1123Z))
	hdr("Message-ID", "<"+randomToken(12)+"@navalrelay>")
	hdr("MIME-Version", "1.0")
	hdr("Auto-Submitted", "auto-generated")
	hdr("X-Auto-Response-Suppress", "All")
	hdr("Content-Type", "multipart/related; boundary="+rel.Boundary()+"; type=\"multipart/alternative\"")
	out.WriteString("\r\n")

	var alt bytes.Buffer
	am := multipart.NewWriter(&alt)
	for _, p := range []struct{ ct, body string }{{"text/plain", text}, {"text/html", html}} {
		pw, err := am.CreatePart(textproto.MIMEHeader{
			"Content-Type":              {p.ct + "; charset=utf-8"},
			"Content-Transfer-Encoding": {"quoted-printable"},
		})
		if err != nil {
			return nil, err
		}
		_, _ = pw.Write(qp(p.body))
	}
	_ = am.Close()
	ap, err := rel.CreatePart(textproto.MIMEHeader{"Content-Type": {"multipart/alternative; boundary=" + am.Boundary()}})
	if err != nil {
		return nil, err
	}
	_, _ = ap.Write(alt.Bytes())

	ip, err := rel.CreatePart(textproto.MIMEHeader{
		"Content-Type":              {`image/png; name="logo.png"`},
		"Content-Transfer-Encoding": {"base64"},
		"Content-ID":                {"<logo@navalrelay>"},
		"Content-Disposition":       {`inline; filename="logo.png"`},
	})
	if err != nil {
		return nil, err
	}
	enc := base64.StdEncoding.EncodeToString(mailLogo)
	for len(enc) > 76 {
		_, _ = ip.Write([]byte(enc[:76] + "\r\n"))
		enc = enc[76:]
	}
	_, _ = ip.Write([]byte(enc + "\r\n"))
	_ = rel.Close()
	return bytes.ReplaceAll(bytes.ReplaceAll(out.Bytes(), []byte("\r\n"), []byte("\n")), []byte("\n"), []byte("\r\n")), nil
}

func (a *App) sendBranded(ctx context.Context, to, subject string, m mailMsg) error {
	return a.sendBrandedCfg(ctx, a.smtpSaved(), to, subject, m)
}

func (a *App) sendBrandedCfg(ctx context.Context, cfg smtpCfg, to, subject string, m mailMsg) error {
	text, html, err := a.renderMail(m)
	if err != nil {
		return err
	}
	raw, err := a.buildMIME(cfg.From, to, subject, text, html)
	if err != nil {
		return err
	}
	return a.smtpDeliver(ctx, cfg, to, raw)
}
