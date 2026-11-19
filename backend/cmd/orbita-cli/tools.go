package main

import (
	"context"
	"fmt"
	"net"
	"net/smtp"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

var reSecret = regexp.MustCompile(`(client_send = .*)`)

func runTool(cmd string, args []string) int {
	c, errs := loadConfig(os.Getenv)
	if len(errs) > 0 {
		for _, e := range errs {
			fmt.Fprintln(os.Stderr, "erro de configuração:", e)
		}
		return 2
	}
	switch cmd {
	case "dns":
		return c.toolDNS()
	case "config":
		return c.toolConfig(len(args) > 0 && args[0] == "--exim")
	case "status":
		return c.toolStatus()
	case "queue":
		return passthrough("exim4", "-bp")
	case "test-mail":
		return c.toolTestMail(args)
	}
	return 2
}

func health() int {
	conn, err := net.DialTimeout("tcp", "127.0.0.1:25", 3*time.Second)
	if err != nil {
		return 1
	}
	conn.Close()
	return 0
}

func passthrough(name string, args ...string) int {
	cmd := exec.Command(name, args...)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		return 1
	}
	return 0
}

func (c Config) toolDNS() int {
	if c.Mode != "relay" {
		fmt.Println("No modo smarthost, SPF, DKIM e DMARC são responsabilidade do provedor do smarthost.")
		return 0
	}
	if len(c.Domains) == 0 {
		fmt.Println("Nenhum domínio em ORBITA_DOMAINS.")
		return 1
	}
	for _, d := range c.Domains {
		fmt.Printf("# %s\n%s\tTXT\t%s\n", d, d, c.spfRecord())
		pub, err := os.ReadFile(filepath.Join(c.dkimDir(), d+".pub"))
		if err != nil {
			fmt.Printf("%s._domainkey.%s\tTXT\t(chave ainda não gerada; inicie o contêiner)\n", c.Selector, d)
		} else {
			fmt.Printf("%s._domainkey.%s\tTXT\tv=DKIM1; k=rsa; p=%s\n", c.Selector, d, strings.TrimSpace(string(pub)))
		}
		fmt.Printf("_dmarc.%s\tTXT\t%s\n\n", d, c.dmarcRecord())
	}
	return 0
}

func (c Config) toolConfig(showExim bool) int {
	if showExim {
		b, err := os.ReadFile(eximConf)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		fmt.Println(reSecret.ReplaceAllString(string(b), "client_send = (oculto)"))
		return 0
	}
	pass := ""
	if c.SHPass != "" {
		pass = "********"
	}
	fmt.Printf("modo=%s\nhostname=%s\nip=%s\nlimite_mb=%d\nredes=%s\ndominios=%s\nseletor=%s\nrestringir_remetentes=%v\n",
		c.Mode, c.Hostname, map[bool]string{true: "ipv4", false: "dual"}[c.IPv4Only], c.MaxMB, strings.Join(c.effectiveNets(), ","), strings.Join(c.Domains, ","), c.Selector, c.Enforce)
	if c.Mode == "smarthost" {
		fmt.Printf("smarthost=%s:%d tls=%s usuario=%s senha=%s\n", c.SHHost, c.SHPort, c.SHTLS, c.SHUser, pass)
	}
	if c.centralOn() {
		fmt.Printf("central=%s://%s:%d%s intervalo=%s\n", c.CentralScheme, c.CentralHost, c.CentralPort, c.CentralPath, c.CentralEvery)
	}
	return 0
}

func (c Config) toolStatus() int {
	rc := 0
	conn, err := net.DialTimeout("tcp", "127.0.0.1:25", 3*time.Second)
	if err != nil {
		fmt.Println("smtp (porta 25): INDISPONÍVEL:", err)
		rc = 1
	} else {
		conn.Close()
		fmt.Println("smtp (porta 25): ok")
	}
	if out, err := exec.Command("exim4", "-bpc").Output(); err == nil {
		fmt.Printf("fila: %s mensagem(ns)\n", strings.TrimSpace(string(out)))
	}
	if c.signing() {
		for _, d := range c.Domains {
			state := "chave presente"
			if _, err := os.Stat(filepath.Join(c.dkimDir(), d+".key")); err != nil {
				state = "SEM chave"
				rc = 1
			}
			fmt.Printf("dkim %s: %s\n", d, state)
		}
	}
	if c.centralOn() {
		if info, err := c.client().Ping(context.Background()); err != nil {
			fmt.Println("central: FALHA:", err)
		} else {
			fmt.Printf("central: ok (nó %q, versão %s)\n", info.Node, info.Version)
		}
	}
	return rc
}

func (c Config) toolTestMail(args []string) int {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "uso: orbita test-mail <destino> [remetente]")
		return 2
	}
	to := args[0]
	from := "orbita-teste@" + c.Hostname
	if len(c.Domains) > 0 {
		from = "orbita-teste@" + c.Domains[0]
	}
	if len(args) > 1 {
		from = args[1]
	}
	cl, err := smtp.Dial("127.0.0.1:25")
	if err != nil {
		fmt.Fprintln(os.Stderr, "falha ao conectar no Exim local:", err)
		return 1
	}
	defer cl.Close()
	steps := []func() error{
		func() error { return cl.Hello(c.Hostname) },
		func() error { return cl.Mail(from) },
		func() error { return cl.Rcpt(to) },
	}
	for _, s := range steps {
		if err := s(); err != nil {
			fmt.Fprintln(os.Stderr, "recusado:", err)
			return 1
		}
	}
	w, err := cl.Data()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	msg := fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: Teste do NavalRelay CLI\r\nDate: %s\r\nMessage-ID: <%d.cli@%s>\r\nContent-Type: text/plain; charset=utf-8\r\n\r\nMensagem de teste enviada pelo relay %s.\r\n",
		from, to, time.Now().Format(time.RFC1123Z), time.Now().UnixNano(), c.Hostname, c.Hostname)
	if _, err := w.Write([]byte(msg)); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if err := w.Close(); err != nil {
		fmt.Fprintln(os.Stderr, "recusado:", err)
		return 1
	}
	_ = cl.Quit()
	fmt.Printf("mensagem de teste aceita (de %s para %s). Acompanhe com: docker logs -f <contêiner>\n", from, to)
	return 0
}
