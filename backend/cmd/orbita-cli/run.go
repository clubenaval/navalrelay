package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"orbitarelay/internal/eximlog"
	"orbitarelay/internal/push"
)

var outMu sync.Mutex

func logf(f string, a ...any) { emit("[cli] " + fmt.Sprintf(f, a...)) }

func emit(line string) {
	outMu.Lock()
	fmt.Fprintln(os.Stdout, line)
	outMu.Unlock()
}

func (c Config) banner() {
	logf("NavalRelay CLI %s", version)
	logf("modo: %s | hostname: %s | IP: %s | limite: %d MB", c.Mode, c.Hostname, map[bool]string{true: "somente IPv4", false: "IPv4+IPv6"}[c.IPv4Only], c.MaxMB)
	logf("redes autorizadas: %s", strings.Join(c.effectiveNets(), ", "))
	if gw := gatewayIPs(); len(gw) > 0 {
		logf("gateways Docker recusados (origem mascarada): %s", strings.Join(gw, ", "))
	}
	if c.Mode == "smarthost" {
		auth := "sem autenticação"
		if c.SHUser != "" {
			auth = "usuário " + c.SHUser
		}
		logf("smarthost: %s:%d (%s, %s)", c.SHHost, c.SHPort, c.SHTLS, auth)
	} else if len(c.Domains) == 0 {
		logf("atenção: ORBITA_DOMAINS vazio; nenhuma mensagem será assinada com DKIM nem restrita por remetente")
	}
	if c.centralOn() {
		logf("servidor central: %s://%s:%d%s (intervalo %s)", c.CentralScheme, c.CentralHost, c.CentralPort, c.CentralPath, c.CentralEvery)
	} else {
		logf("servidor central não configurado: funcionando apenas como relay")
	}
	if !isMount(c.DataDir) {
		logf("atenção: %s não parece ser um volume persistente; as chaves DKIM e o cursor se perdem ao recriar o contêiner", c.DataDir)
	}
}

func isMount(dir string) bool {
	var a, b syscall.Stat_t
	if syscall.Stat(dir, &a) != nil || syscall.Stat(filepath.Dir(filepath.Clean(dir)), &b) != nil {
		return false
	}
	return a.Dev != b.Dev || filepath.Clean(dir) == "/"
}

func (c Config) printDNS() {
	if c.Mode != "relay" || len(c.Domains) == 0 {
		return
	}
	logf("registros DNS para publicar:")
	for _, d := range c.Domains {
		logf("  %s  TXT  %s", d, c.spfRecord())
		if pub, err := os.ReadFile(filepath.Join(c.dkimDir(), d+".pub")); err == nil {
			logf("  %s._domainkey.%s  TXT  v=DKIM1; k=rsa; p=%s", c.Selector, d, strings.TrimSpace(string(pub)))
		}
		logf("  _dmarc.%s  TXT  %s", d, c.dmarcRecord())
	}
}

func run() int {
	c, errs := loadConfig(os.Getenv)
	if len(errs) > 0 {
		for _, e := range errs {
			fmt.Fprintln(os.Stderr, "[cli] erro de configuração:", e)
		}
		return 2
	}
	c.banner()
	if err := c.prepare(); err != nil {
		logf("falha ao preparar o ambiente: %v", err)
		return 1
	}
	if err := c.apply(); err != nil {
		logf("falha ao aplicar a configuração: %v", err)
		return 1
	}
	c.printDNS()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()
	var wg sync.WaitGroup
	wg.Add(1)
	go func() { defer wg.Done(); superviseExim(ctx) }()
	go tailLogs(ctx)
	go rotateLoop(ctx)
	if c.centralOn() {
		go c.pushLoop(ctx)
	}
	<-ctx.Done()
	logf("encerrando")
	wg.Wait()
	return 0
}

func superviseExim(ctx context.Context) {
	backoff := time.Second
	for ctx.Err() == nil {
		cmd := exec.Command("exim4", "-bdf", "-q1m")
		cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
		if err := cmd.Start(); err != nil {
			logf("falha ao iniciar o Exim: %v", err)
		} else {
			logf("exim iniciado (pid %d)", cmd.Process.Pid)
			done := make(chan error, 1)
			go func() { done <- cmd.Wait() }()
			select {
			case <-ctx.Done():
				_ = cmd.Process.Signal(syscall.SIGTERM)
				select {
				case <-done:
				case <-time.After(15 * time.Second):
					_ = cmd.Process.Kill()
					<-done
				}
				return
			case err := <-done:
				logf("o Exim parou (%v); reiniciando em %s", err, backoff)
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		backoff = min(backoff*2, 30*time.Second)
	}
}

func rotateLoop(ctx context.Context) {
	t := time.NewTicker(10 * time.Minute)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if out, err := exec.Command("logrotate", "-s", "/var/lib/logrotate-exim.status", logrotConf).CombinedOutput(); err != nil {
				logf("logrotate: %v: %s", err, strings.TrimSpace(string(out)))
			}
		}
	}
}

type tailer struct {
	path, prefix string
	ino          uint64
	pos          int64
	partial      string
}

func (t *tailer) poll() {
	fi, err := os.Stat(t.path)
	if err != nil {
		return
	}
	ino := uint64(0)
	if st, ok := fi.Sys().(*syscall.Stat_t); ok {
		ino = st.Ino
	}
	if t.ino != 0 && ino != t.ino {
		t.pos, t.partial = 0, ""
	}
	t.ino = ino
	if fi.Size() < t.pos {
		t.pos, t.partial = 0, ""
	}
	if fi.Size() == t.pos {
		return
	}
	f, err := os.Open(t.path)
	if err != nil {
		return
	}
	defer f.Close()
	if _, err := f.Seek(t.pos, io.SeekStart); err != nil {
		return
	}
	r := bufio.NewReader(io.LimitReader(f, 4<<20))
	for {
		s, err := r.ReadString('\n')
		t.pos += int64(len(s))
		if err != nil {
			t.partial += s
			if len(t.partial) > 1<<16 {
				t.partial = ""
			}
			return
		}
		emit(t.prefix + strings.TrimRight(t.partial+s, "\r\n"))
		t.partial = ""
	}
}

func tailLogs(ctx context.Context) {
	ts := []*tailer{
		{path: filepath.Join(logDir, "main.log")},
		{path: filepath.Join(logDir, "reject.log"), prefix: "[reject] "},
		{path: filepath.Join(logDir, "panic.log"), prefix: "[panic] "},
	}
	for _, t := range ts {
		if fi, err := os.Stat(t.path); err == nil {
			t.pos = fi.Size()
			if st, ok := fi.Sys().(*syscall.Stat_t); ok {
				t.ino = st.Ino
			}
		}
	}
	tk := time.NewTicker(500 * time.Millisecond)
	defer tk.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tk.C:
			for _, t := range ts {
				t.poll()
			}
		}
	}
}

func (c Config) client() *push.Client {
	return &push.Client{Scheme: c.CentralScheme, Host: c.CentralHost, Port: c.CentralPort, BasePath: push.TrimBase(c.CentralPath),
		Token: c.CentralToken, SkipVerify: c.CentralSkip, UserAgent: "NavalRelay-CLI/" + version}
}

func (c Config) pushLoop(ctx context.Context) {
	cl := c.client()
	st := eximlog.FileStore{Path: filepath.Join(c.DataDir, "state.json")}
	lastErr := ""
	lastOK := time.Time{}
	sent := 0
	for {
		err := eximlog.Once(ctx, filepath.Join(logDir, "main.log"), st, func(ctx context.Context, evs []eximlog.Event) error {
			if err := cl.Send(ctx, evs); err != nil {
				return err
			}
			sent += len(evs)
			lastOK = time.Now()
			return nil
		})
		if err == nil && time.Since(lastOK) > time.Minute {
			if _, err = cl.Ping(ctx); err == nil {
				lastOK = time.Now()
			}
		}
		switch {
		case err != nil && ctx.Err() == nil:
			if msg := err.Error(); msg != lastErr {
				logf("envio ao servidor central falhou: %s (nova tentativa a cada %s)", msg, c.CentralEvery)
				lastErr = msg
			}
		case err == nil && lastErr != "":
			logf("envio ao servidor central restabelecido")
			lastErr = ""
		}
		if err == nil && sent > 0 {
			logf("%d evento(s) enviado(s) ao servidor central", sent)
			sent = 0
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(c.CentralEvery):
		}
	}
}
