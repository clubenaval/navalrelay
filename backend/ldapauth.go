package main

import (
	"crypto/tls"
	"fmt"
	"log"
	"net"
	"strings"
	"time"

	"github.com/go-ldap/ldap/v3"
)

type ldapInfo struct {
	name  string
	mail  string
	admin bool
}

func (a *App) ldapDial() (*ldap.Conn, error) {
	host := a.sStr("ldap_host")
	port := a.sInt("ldap_port")
	if host == "" || port == 0 {
		return nil, fmt.Errorf("LDAP não configurado")
	}
	addr := net.JoinHostPort(host, fmt.Sprint(port))
	tlsCfg := &tls.Config{ServerName: host, InsecureSkipVerify: a.sBool("ldap_skip_verify"), MinVersion: tls.VersionTLS12}
	dialer := &net.Dialer{Timeout: 8 * time.Second}
	var conn *ldap.Conn
	var err error
	switch a.sStr("ldap_tls") {
	case "ldaps":
		conn, err = ldap.DialURL("ldaps://"+addr, ldap.DialWithDialer(dialer), ldap.DialWithTLSConfig(tlsCfg))
	default:
		conn, err = ldap.DialURL("ldap://"+addr, ldap.DialWithDialer(dialer))
		if err == nil && a.sStr("ldap_tls") == "starttls" {
			if err = conn.StartTLS(tlsCfg); err != nil {
				conn.Close()
			}
		}
	}
	if err != nil {
		return nil, err
	}
	conn.SetTimeout(10 * time.Second)
	return conn, nil
}

func (a *App) ldapSearchUser(conn *ldap.Conn, username string) (*ldap.Entry, error) {
	filter := strings.ReplaceAll(a.sStr("ldap_user_filter"), "%s", ldap.EscapeFilter(username))
	if g := a.sStr("ldap_group_dn"); g != "" {
		member := fmt.Sprintf("(memberOf:1.2.840.113556.1.4.1941:=%s)", ldap.EscapeFilter(g))
		if ag := a.sStr("ldap_admin_group_dn"); ag != "" {
			member = fmt.Sprintf("(|%s(memberOf:1.2.840.113556.1.4.1941:=%s))", member, ldap.EscapeFilter(ag))
		}
		filter = fmt.Sprintf("(&%s%s)", filter, member)
	}
	res, err := conn.Search(ldap.NewSearchRequest(a.sStr("ldap_base_dn"), ldap.ScopeWholeSubtree,
		ldap.NeverDerefAliases, 2, 10, false, filter,
		[]string{"dn", "displayName", "cn", "mail"}, nil))
	if err != nil {
		return nil, err
	}
	if len(res.Entries) != 1 {
		return nil, fmt.Errorf("usuário não encontrado ou sem acesso")
	}
	return res.Entries[0], nil
}

func (a *App) ldapUPN(username string) string {
	d := a.sStr("ldap_domain")
	if d == "" || strings.Contains(username, "@") {
		return username
	}
	return username + d
}

func (a *App) ldapAuth(username, password string) (ldapInfo, bool) {
	var info ldapInfo
	if password == "" || len(username) > 128 {
		return info, false
	}
	svc, err := a.ldapDial()
	if err != nil {
		log.Printf("ldap: %v", err)
		return info, false
	}
	defer svc.Close()
	upn := a.ldapUPN(username)
	if !strings.Contains(upn, "@") {
		log.Printf("ldap: informe o domínio UPN nas configurações")
		return info, false
	}
	if err := svc.Bind(upn, password); err != nil {
		log.Printf("ldap: bind de %s recusado: %v", upn, err)
		return info, false
	}
	entry, err := a.ldapSearchUser(svc, strings.SplitN(username, "@", 2)[0])
	if err != nil {
		log.Printf("ldap: %s autenticado, mas fora dos grupos de acesso/administrador: %v", upn, err)
		return info, false
	}
	info.name = entry.GetAttributeValue("displayName")
	if info.name == "" {
		info.name = entry.GetAttributeValue("cn")
	}
	info.mail = entry.GetAttributeValue("mail")
	if ag := a.sStr("ldap_admin_group_dn"); ag != "" {
		res, err := svc.Search(ldap.NewSearchRequest(entry.DN, ldap.ScopeBaseObject, ldap.NeverDerefAliases, 1, 10, false,
			fmt.Sprintf("(memberOf:1.2.840.113556.1.4.1941:=%s)", ldap.EscapeFilter(ag)), []string{"dn"}, nil))
		info.admin = err == nil && len(res.Entries) == 1
	}
	return info, true
}

func (a *App) testLDAP() error {
	conn, err := a.ldapDial()
	if err != nil {
		return err
	}
	defer conn.Close()
	return nil
}
