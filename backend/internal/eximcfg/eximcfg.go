// Package eximcfg gera a configuração do Exim usada pelo painel e pelo contêiner CLI.
package eximcfg

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/template"
)

// View reúne os valores usados pelo modelo do exim.conf.
type View struct {
	Hostname      string
	IPv4Only      bool
	RelayNets     string
	Gateways      string
	MaxMB         int
	SenderDomains string
	DKIMDomains   string
	Selector      string
	Smarthost     bool
	SmarthostHost string
	Port          int
	TLS           string
	HasAuth       bool
	User          string
	Pass          string
}

const eximTemplate = `# Gerado automaticamente pelo NavalRelay. Não edite manualmente.
{{if .Hostname}}primary_hostname = {{.Hostname}}
{{end}}spool_directory = /var/spool/exim4
pid_file_path = /run/exim4/exim.pid
log_file_path = /var/log/exim/%s.log
log_selector = +subject +smtp_confirmation +tls_cipher +delivery_size +sender_on_delivery
log_timezone = true
message_logs = false
host_lookup = *
rfc1413_query_timeout = 0s
never_users = root
{{if .IPv4Only}}disable_ipv6 = true
local_interfaces = <; 0.0.0.0
{{else}}local_interfaces = <; 0.0.0.0 ; ::0
{{end}}daemon_smtp_ports = 25 : 587
domainlist local_domains = @
hostlist relay_from_hosts = <; {{.RelayNets}}
{{if .Gateways}}hostlist docker_gateways = <; {{.Gateways}}
{{end}}acl_smtp_mail = acl_check_mail
acl_smtp_rcpt = acl_check_rcpt
message_size_limit = {{.MaxMB}}M
smtp_accept_max = 200
smtp_accept_max_per_host = 50
smtp_banner = $smtp_active_hostname ESMTP NavalRelay
ignore_bounce_errors_after = 2d
timeout_frozen_after = 7d
tls_advertise_hosts = *
tls_certificate = /etc/exim4/tls/cert.pem
tls_privatekey = /etc/exim4/tls/key.pem

begin acl

acl_check_mail:
{{if .Gateways}}  deny    message = Connection through Docker gateway refused (source IP masked); connect using the server IP address instead of localhost
          hosts = +docker_gateways
{{end}}  deny    message = Relay not permitted
          !hosts = +relay_from_hosts
  accept  senders = :
{{if .SenderDomains}}  deny    message = Sender domain not allowed: $sender_address_domain
          condition = ${if inlist{${lc:$sender_address_domain}}{<; {{.SenderDomains}}}{no}{yes}}
{{end}}  accept

acl_check_rcpt:
  accept  hosts = +relay_from_hosts
  deny    message = Relay not permitted

begin routers

{{if .Smarthost}}smarthost:
  driver = manualroute
  domains = ! +local_domains
  transport = remote_smtp_smarthost
  route_list = * {{.SmarthostHost}} byname
  no_more
{{else}}dnslookup:
  driver = dnslookup
  domains = ! +local_domains
  transport = remote_smtp
  ignore_target_hosts = <; 0.0.0.0 ; 127.0.0.0/8 ; ::1
  no_more
{{end}}
begin transports

remote_smtp:
  driver = smtp
{{template "dkim" .}}
{{if .Smarthost}}remote_smtp_smarthost:
  driver = smtp
  port = {{.Port}}
{{if eq .TLS "tls"}}  protocol = smtps
{{end}}{{if ne .TLS "none"}}  hosts_require_tls = *
{{end}}{{if .HasAuth}}  hosts_require_auth = *
{{end}}{{template "dkim" .}}{{end}}
begin retry

* * F,2h,15m; G,16h,1h,1.5; F,4d,6h
{{if .HasAuth}}
begin authenticators

smarthost_plain:
  driver = plaintext
  public_name = PLAIN
  hide client_send = ^{{.User}}^{{.Pass}}

smarthost_login:
  driver = plaintext
  public_name = LOGIN
  hide client_send = : {{.User}} : {{.Pass}}
{{end}}`

const dkimTemplate = `{{define "dkim"}}{{if .DKIMDomains}}  dkim_domain = ${if inlist{${lc:${domain:$h_from:}}}{<; {{.DKIMDomains}}}{${lc:${domain:$h_from:}}}{}}
  dkim_selector = {{.Selector}}
  dkim_private_key = ${lookup{$dkim_domain}lsearch{/etc/exim4/dkim/map}}
  dkim_canon = relaxed
{{end}}{{end}}`

var eximTmpl = template.Must(template.Must(template.New("exim").Parse(eximTemplate)).Parse(dkimTemplate))

var logrotateTmpl = template.Must(template.New("lr").Parse(`/var/log/exim/*.log {
  daily
  maxsize {{.SizeMB}}M
  rotate {{.Keep}}
  compress
  delaycompress
  missingok
  notifempty
  nomail
  create 0644 Debian-exim adm
}
`))

// EscClientSend escapa um valor para uso em "client_send" dos autenticadores do Exim.
func EscClientSend(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `$`, `\$`)
	return strings.ReplaceAll(s, `:`, `::`)
}

// Render gera o exim.conf e o logrotate.conf.
func Render(v View, logSizeMB, logKeep int) (conf, logrotate []byte, err error) {
	var c, l bytes.Buffer
	if err := eximTmpl.Execute(&c, v); err != nil {
		return nil, nil, err
	}
	if err := logrotateTmpl.Execute(&l, map[string]int{"SizeMB": max(logSizeMB, 1), "Keep": max(logKeep, 1)}); err != nil {
		return nil, nil, err
	}
	return c.Bytes(), l.Bytes(), nil
}

// WriteDir grava exim.conf, logrotate.conf, as chaves DKIM (dkim/<dominio>.key), o mapa de chaves e o arquivo
// "version", que muda sempre que algo muda e faz o MTA recarregar. keys mapeia domínio para a chave privada em PEM.
func WriteDir(dir string, conf, logrotate []byte, keys map[string]string) error {
	if err := os.MkdirAll(filepath.Join(dir, "dkim"), 0o750); err != nil {
		return err
	}
	h := sha256.New()
	h.Write(conf)
	h.Write(logrotate)
	names := make([]string, 0, len(keys))
	for d := range keys {
		names = append(names, d)
	}
	sort.Strings(names)
	keep := map[string]bool{"map": true}
	var dmap strings.Builder
	for _, d := range names {
		dmap.WriteString(d + ": /etc/exim4/dkim/" + d + ".key\n")
	}
	h.Write([]byte("map" + dmap.String()))
	if err := WriteAtomic(filepath.Join(dir, "dkim", "map"), []byte(dmap.String()), 0o600); err != nil {
		return err
	}
	for _, d := range names {
		fn := d + ".key"
		keep[fn] = true
		h.Write([]byte(fn + keys[d]))
		if err := WriteAtomic(filepath.Join(dir, "dkim", fn), []byte(keys[d]), 0o600); err != nil {
			return err
		}
	}
	if ents, err := os.ReadDir(filepath.Join(dir, "dkim")); err == nil {
		for _, e := range ents {
			if !keep[e.Name()] {
				_ = os.Remove(filepath.Join(dir, "dkim", e.Name()))
			}
		}
	}
	if err := WriteAtomic(filepath.Join(dir, "exim.conf"), conf, 0o600); err != nil {
		return err
	}
	if err := WriteAtomic(filepath.Join(dir, "logrotate.conf"), logrotate, 0o644); err != nil {
		return err
	}
	return WriteAtomic(filepath.Join(dir, "version"), []byte(hex.EncodeToString(h.Sum(nil))), 0o644)
}

// WriteAtomic grava o arquivo por rename, para o leitor nunca ver conteúdo parcial.
func WriteAtomic(path string, data []byte, mode os.FileMode) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, mode); err != nil {
		return fmt.Errorf("gravar %s: %w", filepath.Base(path), err)
	}
	return os.Rename(tmp, path)
}
