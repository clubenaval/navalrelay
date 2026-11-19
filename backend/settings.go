package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/netip"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type Field struct {
	Key      string   `json:"key"`
	Group    string   `json:"group"`
	Label    string   `json:"label"`
	Help     string   `json:"help,omitempty"`
	Type     string   `json:"type"`
	Default  any      `json:"default"`
	Secret   bool     `json:"secret,omitempty"`
	Options  []string `json:"options,omitempty"`
	Depends  string   `json:"depends,omitempty"`
	Example  string   `json:"example,omitempty"`
	Value    any      `json:"value"`
	HasVal   bool     `json:"has_value,omitempty"`
	Required bool     `json:"required,omitempty"`
}

type Group struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

var settingGroups = []Group{
	{"geral", "Geral"},
	{"coleta", "Envio para o servidor central"},
	{"mta", "Servidor de e-mail"},
	{"smarthost", "Smarthost"},
	{"redes", "Redes e acesso"},
	{"entrega", "Entrega e DNS"},
	{"ldap", "Autenticação"},
	{"seguranca", "E-mail do sistema (2FA)"},
	{"logs", "Logs e retenção"},
}

var settingFields = []Field{
	{Key: "app_name", Group: "geral", Label: "Nome do sistema", Type: "text", Default: "NavalRelay"},
	{Key: "default_theme", Group: "geral", Label: "Modo de cores padrão", Type: "select", Default: "dark", Options: []string{"dark", "light"}, Help: "dark = escuro, light = claro. É o que aparece para quem ainda não escolheu o próprio modo no botão do canto da tela"},
	{Key: "timezone", Group: "geral", Label: "Fuso horário", Type: "text", Default: "America/Sao_Paulo", Help: "Nome IANA, ex.: America/Sao_Paulo"},
	{Key: "deploy_mode", Group: "geral", Label: "Implantação", Type: "select", Default: "standalone", Options: []string{"standalone", "server", "client"}, Help: "standalone = instalação única; servidor = recebe eventos de outros relays; cliente = envia eventos a um servidor central"},
	{Key: "trust_proxy", Group: "geral", Label: "Estou atrás de um proxy reverso", Type: "bool", Default: false, Help: "Confia em X-Forwarded-For/Proto para identificar o IP real do cliente. Ative só se um proxy confiável estiver na frente"},
	{Key: "collect_scheme", Group: "coleta", Label: "Protocolo", Type: "select", Default: "https", Options: []string{"https", "http"}, Depends: "deploy_mode=client", Help: "HTTPS é o recomendado. Com HTTP o token trafega sem criptografia"},
	{Key: "collect_host", Group: "coleta", Label: "Endereço do servidor central", Type: "text", Default: "", Depends: "deploy_mode=client"},
	{Key: "collect_port", Group: "coleta", Label: "Porta", Type: "number", Default: 443, Depends: "deploy_mode=client", Help: "443 para HTTPS e 80 para HTTP, ou a porta do seu proxy"},
	{Key: "collect_path", Group: "coleta", Label: "Caminho base (opcional)", Type: "text", Default: "", Depends: "deploy_mode=client", Help: "Só se o proxy publicar o sistema em um subcaminho"},
	{Key: "collect_token", Group: "coleta", Label: "Token do servidor", Type: "password", Default: "", Secret: true, Depends: "deploy_mode=client", Help: "Gerado no servidor central, em Servidores"},
	{Key: "collect_skip_verify", Group: "coleta", Label: "Ignorar validação do certificado (HTTPS)", Type: "bool", Default: false, Depends: "deploy_mode=client", Help: "Use apenas com certificado autoassinado"},
	{Key: "collect_interval", Group: "coleta", Label: "Intervalo de envio (segundos)", Type: "number", Default: 10, Depends: "deploy_mode=client"},
	{Key: "relay_mode", Group: "mta", Label: "Modo de operação", Type: "select", Default: "relay", Options: []string{"relay", "smarthost"}, Help: "relay = entrega direta (domínios, DKIM, SPF, DMARC); smarthost = encaminha por outro servidor SMTP"},
	{Key: "mta_hostname", Group: "mta", Label: "Hostname do relay (HELO)", Type: "text", Default: "", Required: true, Help: "Nome DNS deste servidor, usado no HELO. Use o mesmo nome do DNS reverso (PTR) e do registro SPF. Preenchido com o endereço pelo qual você acessa o painel"},
	{Key: "relay_nets", Group: "redes", Label: "Redes autorizadas a enviar", Type: "textarea", Default: "127.0.0.1\n::1\n10.0.0.0/8\n192.168.0.0/16\nfd00::/8", Help: "IPs ou redes (CIDR, IPv4/IPv6) autorizados a enviar por este relay"},
	{Key: "relay_nets_auto", Group: "redes", Label: "Incluir automaticamente as redes detectadas", Type: "bool", Default: true, Help: "Soma à lista acima o loopback, a rede Docker em que o contêiner está e os IPs da máquina conhecidos. Veja a lista detectada abaixo"},
	{Key: "enforce_sender_domains", Group: "mta", Label: "Restringir remetentes aos domínios cadastrados", Type: "bool", Default: true, Depends: "relay_mode=relay"},
	{Key: "ip_mode", Group: "redes", Label: "Modo IP", Type: "select", Default: "dual", Options: []string{"dual", "ipv4"}, Help: "dual = IPv4 + IPv6"},
	{Key: "max_message_mb", Group: "mta", Label: "Tamanho máximo da mensagem (MB)", Type: "number", Default: 25},
	{Key: "dkim_selector", Group: "entrega", Label: "Seletor DKIM", Type: "text", Default: "dkim", Depends: "relay_mode=relay"},
	{Key: "public_ips", Group: "entrega", Label: "IPs públicos de saída (IPv4/IPv6)", Type: "textarea", Default: "", Depends: "relay_mode=relay", Help: "IPs (IPv4/IPv6) pelos quais o relay envia. Usados para montar o registro SPF"},
	{Key: "dmarc_policy", Group: "entrega", Label: "Política DMARC", Type: "select", Default: "none", Options: []string{"none", "quarantine", "reject"}, Depends: "relay_mode=relay"},
	{Key: "dmarc_rua", Group: "entrega", Label: "E-mail para relatórios DMARC (rua)", Type: "text", Default: "", Depends: "relay_mode=relay"},
	{Key: "smarthost_host", Group: "smarthost", Label: "Servidor", Type: "text", Default: "", Depends: "relay_mode=smarthost"},
	{Key: "smarthost_port", Group: "smarthost", Label: "Porta", Type: "number", Default: 587, Depends: "relay_mode=smarthost"},
	{Key: "smarthost_tls", Group: "smarthost", Label: "Criptografia", Type: "select", Default: "starttls", Options: []string{"none", "starttls", "tls"}, Help: "tls = SMTPS implícito (porta 465)", Depends: "relay_mode=smarthost"},
	{Key: "smarthost_user", Group: "smarthost", Label: "Usuário", Type: "text", Default: "", Depends: "relay_mode=smarthost"},
	{Key: "smarthost_pass", Group: "smarthost", Label: "Senha", Type: "password", Default: "", Secret: true, Depends: "relay_mode=smarthost"},
	{Key: "auth_mode", Group: "ldap", Label: "Tipo de autenticação do painel", Type: "select", Default: "local", Options: []string{"local", "ldap"}, Help: "local = só usuários locais; ldap = LDAP/AD além dos usuários locais"},
	{Key: "ldap_host", Group: "ldap", Label: "Servidor", Type: "text", Default: "", Depends: "auth_mode=ldap"},
	{Key: "ldap_port", Group: "ldap", Label: "Porta", Type: "number", Default: 389, Depends: "auth_mode=ldap"},
	{Key: "ldap_tls", Group: "ldap", Label: "Criptografia", Type: "select", Default: "none", Options: []string{"none", "starttls", "ldaps"}, Depends: "auth_mode=ldap"},
	{Key: "ldap_skip_verify", Group: "ldap", Label: "Ignorar validação do certificado", Type: "bool", Default: false, Depends: "auth_mode=ldap"},
	{Key: "ldap_domain", Group: "ldap", Label: "Domínio UPN (sufixo)", Type: "text", Default: "", Depends: "auth_mode=ldap", Help: "Obrigatório. O login é feito como usuario@dominio"},
	{Key: "ldap_base_dn", Group: "ldap", Label: "Base DN", Type: "text", Default: "", Depends: "auth_mode=ldap"},
	{Key: "ldap_user_filter", Group: "ldap", Label: "Filtro de usuário", Type: "text", Default: "(sAMAccountName=%s)", Help: "%s é substituído pelo login", Depends: "auth_mode=ldap"},
	{Key: "ldap_group_dn", Group: "ldap", Label: "Grupo com acesso (DN)", Type: "text", Default: "", Help: "Vazio libera qualquer usuário do Base DN", Depends: "auth_mode=ldap"},
	{Key: "ldap_admin_group_dn", Group: "ldap", Label: "Grupo administrador (DN)", Type: "text", Default: "", Help: "Membros entram como administradores, mesmo fora do grupo com acesso", Depends: "auth_mode=ldap"},
	{Key: "smtp_host", Group: "seguranca", Label: "Servidor SMTP", Type: "text", Default: "", Help: "Usado para enviar códigos de verificação (2FA por e-mail). Pode ser o seu próprio relay ou qualquer SMTP"},
	{Key: "smtp_port", Group: "seguranca", Label: "Porta", Type: "number", Default: 587},
	{Key: "smtp_tls", Group: "seguranca", Label: "Criptografia", Type: "select", Default: "starttls", Options: []string{"none", "starttls", "tls"}, Help: "tls = SMTPS implícito (porta 465)"},
	{Key: "smtp_skip_verify", Group: "seguranca", Label: "Ignorar validação do certificado", Type: "bool", Default: false},
	{Key: "smtp_user", Group: "seguranca", Label: "Usuário (opcional)", Type: "text", Default: ""},
	{Key: "smtp_pass", Group: "seguranca", Label: "Senha (opcional)", Type: "password", Default: "", Secret: true},
	{Key: "smtp_from", Group: "seguranca", Label: "Remetente", Type: "text", Default: "", Help: "Endereço que aparece como remetente dos códigos"},
	{Key: "db_retention_days", Group: "logs", Label: "Retenção no banco (dias)", Type: "number", Default: 365, Help: "Registros mais antigos são apagados automaticamente"},
	{Key: "log_rotate_mb", Group: "logs", Label: "Rotacionar log do Exim ao atingir (MB)", Type: "number", Default: 50},
	{Key: "known_host_ips", Group: "_sistema", Label: "IPs da máquina detectados", Type: "textarea", Default: ""},
	{Key: "public_url", Group: "geral", Label: "Endereço público do painel", Type: "text", Default: "", Help: "Usado nos links dos e-mails (recuperação de senha, confirmação de e-mail). Preenchido sozinho com o endereço pelo qual você acessa o painel; ajuste se houver proxy ou outro nome. Ex.: https://relay.exemplo.com.br"},
	{Key: "wizard_done", Group: "_sistema", Label: "Assistente concluído", Type: "bool", Default: false},
	{Key: "hide_unknown_rcpt", Group: "logs", Label: "Ocultar por padrão falhas por caixa inexistente", Type: "bool", Default: false, Help: "Esconde da lista, do painel e dos relatórios as mensagens que falharam só porque o destinatário não existe (ex.: 550 5.1.1 user unknown). Os dados não são apagados e o filtro pode ser alterado em cada tela"},
	{Key: "hide_rspamd_rejects", Group: "logs", Label: "Ocultar por padrão mensagens rejeitadas pelo Rspamd (Mailcow)", Type: "bool", Default: false, Help: "Esconde as mensagens barradas como spam pelo Rspamd. Os dados não são apagados"},
	{Key: "log_keep_files", Group: "logs", Label: "Arquivos de log mantidos em disco", Type: "number", Default: 7, Help: "Rotação diária; arquivos antigos são compactados"},
}

var (
	reHost    = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9.-]{0,251}[A-Za-z0-9])?$`)
	reDomain  = regexp.MustCompile(`^([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z]{2,63}$`)
	reSel     = regexp.MustCompile(`^[A-Za-z0-9-]{1,32}$`)
	reEmail   = regexp.MustCompile(`^[A-Za-z0-9._%+-]{1,64}@[A-Za-z0-9.-]{1,253}\.[A-Za-z]{2,63}$`)
	reURLPath = regexp.MustCompile(`^(/[A-Za-z0-9._~-]+)+$`)
	reNoCtrl  = regexp.MustCompile(`^[^\x00-\x08\x0a-\x1f\x7f]*$`)
)

var fieldExamples = map[string]string{
	"app_name": "Relay SMTP da Empresa", "timezone": "America/Sao_Paulo", "mta_hostname": "relay.dominio.local",
	"relay_nets": "10.0.0.0/8, 192.168.1.0/24, 2001:db8::/32", "max_message_mb": "25", "dkim_selector": "dkim",
	"public_ips": "203.0.113.10 e 2001:db8::10", "dmarc_policy": "none (monitorar) ou quarantine", "dmarc_rua": "dmarc@dominio.local",
	"smarthost_host": "smtp.dominio.local", "smarthost_port": "587", "smarthost_user": "usuario@dominio.local", "smarthost_pass": "••••••••", "public_url": "https://relay.dominio.local", "smtp_host": "smtp.dominio.local", "smtp_port": "587", "smtp_from": "orbita@dominio.local",
	"ldap_host": "ad.dominio.local ou 192.168.0.10", "ldap_port": "389 (LDAP) ou 636 (LDAPS)", "ldap_domain": "@dominio.local",
	"ldap_base_dn": "DC=dominio,DC=local", "ldap_user_filter": "(sAMAccountName=%s)",
	"ldap_group_dn": "CN=GrupoAcesso,CN=Users,DC=dominio,DC=local", "ldap_admin_group_dn": "CN=Administradores,CN=Users,DC=dominio,DC=local",
	"collect_host": "orbita.dominio.com.br", "collect_port": "443", "collect_path": "/orbita", "collect_interval": "10",
	"db_retention_days": "365", "log_rotate_mb": "50", "log_keep_files": "7",
}

func init() {
	for i := range settingFields {
		settingFields[i].Example = fieldExamples[settingFields[i].Key]
	}
}

func fieldByKey(k string) *Field {
	for i := range settingFields {
		if settingFields[i].Key == k {
			return &settingFields[i]
		}
	}
	return nil
}

func validateSetting(f *Field, v any) (any, error) {
	switch f.Type {
	case "bool":
		b, ok := v.(bool)
		if !ok {
			return nil, errors.New("valor booleano esperado")
		}
		return b, nil
	case "number":
		var n float64
		switch t := v.(type) {
		case float64:
			n = t
		case string:
			x, err := strconv.ParseFloat(strings.TrimSpace(t), 64)
			if err != nil {
				return nil, errors.New("número inválido")
			}
			n = x
		default:
			return nil, errors.New("número inválido")
		}
		if n < 0 || n > 1_000_000 || n != float64(int(n)) {
			return nil, errors.New("número fora do intervalo")
		}
		if (f.Key == "collect_port" && (n < 1 || n > 65535)) || (f.Key == "collect_interval" && (n < 2 || n > 3600)) {
			return nil, errors.New("valor fora do intervalo permitido")
		}
		return int(n), nil
	case "select":
		s, _ := v.(string)
		for _, o := range f.Options {
			if o == s {
				return s, nil
			}
		}
		return nil, errors.New("opção inválida")
	}
	s, ok := v.(string)
	if !ok {
		return nil, errors.New("texto esperado")
	}
	if len(s) > 4000 {
		return nil, errors.New("valor muito longo")
	}
	if f.Type != "textarea" {
		s = strings.TrimSpace(s)
		if !reNoCtrl.MatchString(s) {
			return nil, errors.New("caracteres inválidos")
		}
	}
	switch f.Key {
	case "mta_hostname", "smarthost_host", "smtp_host", "ldap_host", "collect_host":
		if f.Key == "mta_hostname" && s == "" {
			return nil, errors.New("informe o hostname do relay (obrigatório)")
		}
		if s != "" && !reHost.MatchString(s) {
			if _, err := netip.ParseAddr(s); err != nil {
				return nil, errors.New("hostname inválido")
			}
		}
		if f.Key == "smarthost_host" {
			if ip, err := netip.ParseAddr(s); err == nil && ip.Is6() {
				return nil, errors.New("use o hostname do servidor em vez de um endereço IPv6")
			}
		}
	case "collect_path":
		if s != "" {
			s = "/" + strings.Trim(s, "/")
			if !reURLPath.MatchString(s) {
				return nil, errors.New("caminho inválido")
			}
		}
	case "ldap_domain":
		if s != "" {
			s = "@" + strings.TrimPrefix(s, "@")
			if !reHost.MatchString(strings.TrimPrefix(s, "@")) {
				return nil, errors.New("domínio inválido")
			}
		}
	case "dkim_selector":
		if !reSel.MatchString(s) {
			return nil, errors.New("seletor inválido")
		}
	case "dmarc_rua":
		if s != "" && !reEmail.MatchString(s) {
			return nil, errors.New("e-mail inválido")
		}
	case "public_ips":
		for _, l := range strings.Split(s, "\n") {
			if l = strings.TrimSpace(l); l == "" {
				continue
			}
			if _, err := netip.ParseAddr(l); err != nil {
				return nil, fmt.Errorf("IP inválido: %s", l)
			}
		}
	case "timezone":
		if _, err := time.LoadLocation(s); err != nil {
			return nil, errors.New("fuso horário inválido")
		}
	case "known_host_ips":
		for _, l := range strings.Split(s, "\n") {
			if l = strings.TrimSpace(l); l != "" {
				if _, err := netip.ParseAddr(l); err != nil {
					return nil, fmt.Errorf("IP inválido: %s", l)
				}
			}
		}
	case "public_url":
		if s != "" {
			u, err := url.Parse(s)
			if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.RawQuery != "" || u.Fragment != "" {
				return nil, errors.New("informe uma URL completa, como https://relay.exemplo.com.br")
			}
			s = strings.TrimRight(u.Scheme+"://"+u.Host+u.Path, "/")
		}
	case "relay_nets":
		for _, l := range strings.Split(s, "\n") {
			l = strings.TrimSpace(l)
			if l == "" {
				continue
			}
			if _, err := netip.ParsePrefix(l); err != nil {
				if _, err2 := netip.ParseAddr(l); err2 != nil {
					return nil, fmt.Errorf("rede inválida: %s", l)
				}
			}
		}
	case "smtp_from":
		if s != "" && !reEmail.MatchString(s) {
			return nil, errors.New("e-mail inválido")
		}
	case "smtp_user", "smtp_pass":
		if strings.ContainsAny(s, "\r\n") {
			return nil, errors.New("caractere inválido")
		}
	case "smarthost_user", "smarthost_pass":
		if strings.ContainsAny(s, "^\n\r") {
			return nil, errors.New("o caractere ^ não é suportado")
		}
	case "app_name":
		if s == "" || len(s) > 60 {
			return nil, errors.New("nome inválido")
		}
	}
	return s, nil
}

func (a *App) loadSettings(ctx context.Context) error {
	rows, err := a.query(ctx, `SELECT k, v FROM settings`)
	if err != nil {
		return err
	}
	defer rows.Close()
	m := map[string]any{}
	for rows.Next() {
		var k string
		var raw []byte
		if err := rows.Scan(&k, &raw); err != nil {
			return err
		}
		var v any
		if json.Unmarshal(raw, &v) == nil {
			m[k] = v
		}
	}
	a.mu.Lock()
	a.settings = m
	a.mu.Unlock()
	a.applyRuntime()
	return rows.Err()
}

func (a *App) raw(key string) (any, bool) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	v, ok := a.settings[key]
	return v, ok
}

func (a *App) sGet(key string) any {
	if v, ok := a.raw(key); ok {
		return v
	}
	if f := fieldByKey(key); f != nil {
		return f.Default
	}
	return nil
}

func (a *App) sStr(key string) string {
	s, _ := a.sGet(key).(string)
	if f := fieldByKey(key); f != nil && f.Secret && s != "" {
		if p, err := a.decrypt(s); err == nil {
			return p
		}
		return ""
	}
	return s
}

func (a *App) sInt(key string) int {
	switch t := a.sGet(key).(type) {
	case float64:
		return int(t)
	case int:
		return t
	}
	return 0
}

func (a *App) sBool(key string) bool {
	b, _ := a.sGet(key).(bool)
	return b
}

func (a *App) setRaw(ctx context.Context, key string, v any) error {
	b, _ := json.Marshal(v)
	_, err := a.exec(ctx, `INSERT INTO settings(k,v) VALUES(?,?) ON DUPLICATE KEY UPDATE v=VALUES(v)`, key, string(b))
	if err != nil {
		return err
	}
	a.mu.Lock()
	a.settings[key] = v
	a.mu.Unlock()
	return nil
}

func (a *App) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	a.autoPublicURL(r)
	out := make([]Field, 0, len(settingFields))
	for _, f := range settingFields {
		if f.Secret {
			f.HasVal = a.sStr(f.Key) != ""
			f.Value = ""
		} else {
			f.Value = a.sGet(f.Key)
		}
		if f.Key == "relay_nets" {
			if _, saved := a.raw("relay_nets"); !saved {
				f.Value = a.mergedRelayNetsText(r)
			}
		}
		if f.Key == "mta_hostname" && strings.TrimSpace(fmt.Sprint(f.Value)) == "" {
			f.Value = hostnameFromRequest(r)
		}
		out = append(out, f)
	}
	writeJSON(w, 200, map[string]any{"groups": settingGroups, "fields": out})
}

func (a *App) handlePutSettings(w http.ResponseWriter, r *http.Request) {
	var in map[string]any
	if !readJSON(w, r, &in) {
		return
	}
	for k, v := range in {
		f := fieldByKey(k)
		if f == nil {
			writeErr(w, 400, "configuração desconhecida: "+k)
			return
		}
		if f.Secret {
			if s, _ := v.(string); s == "" {
				continue
			}
		}
		nv, err := validateSetting(f, v)
		if err != nil {
			writeErr(w, 400, f.Label+": "+err.Error())
			return
		}
		if f.Secret {
			enc, err := a.encrypt(nv.(string))
			if err != nil {
				writeErr(w, 500, "falha ao criptografar")
				return
			}
			nv = enc
		}
		if err := a.setRaw(r.Context(), k, nv); err != nil {
			writeErr(w, 500, "falha ao salvar")
			return
		}
	}
	a.applyRuntime()
	u := userFrom(r)
	a.audit(r.Context(), u.Username, "settings.update", fmt.Sprintf("%d campos", len(in)))
	if err := a.renderMTA(r.Context()); err != nil {
		writeErr(w, 500, "configurações salvas, mas falha ao gerar config do MTA: "+err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}
