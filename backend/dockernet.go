package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"orbitarelay/internal/netdet"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const gatewayMarker = "Docker gateway"

func readDefaultGateways() []string { return netdet.ReadDefaultGateways() }

func dockerGateways() []string { return netdet.DockerGateways() }

type dockerStatus struct {
	Available    bool   `json:"available"`
	Error        string `json:"error,omitempty"`
	Version      string `json:"version,omitempty"`
	Major        int    `json:"major,omitempty"`
	APIVersion   string `json:"api_version,omitempty"`
	Experimental bool   `json:"experimental"`
	DaemonIPv6   *bool  `json:"daemon_ipv6"`
}

func dockerGet(ctx context.Context, base, path string, v any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+path, nil)
	if err != nil {
		return err
	}
	resp, err := (&http.Client{Timeout: 5 * time.Second}).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("HTTP %d em %s", resp.StatusCode, path)
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(v)
}

func queryDocker(ctx context.Context) dockerStatus {
	var st dockerStatus
	base := strings.TrimRight(os.Getenv("ORBITA_DOCKER_API"), "/")
	if base == "" {
		st.Error = "serviço navalrelay-proxy não configurado"
		return st
	}
	var ver struct{ Version, ApiVersion string }
	if err := dockerGet(ctx, base, "/version", &ver); err != nil {
		st.Error = "navalrelay-proxy inacessível: " + err.Error()
		return st
	}
	st.Available, st.Version, st.APIVersion = true, ver.Version, ver.ApiVersion
	st.Major, _ = strconv.Atoi(strings.SplitN(ver.Version, ".", 2)[0])
	var info struct{ ExperimentalBuild bool }
	if dockerGet(ctx, base, "/info", &info) == nil {
		st.Experimental = info.ExperimentalBuild
	}
	var nw struct{ EnableIPv6 bool }
	if dockerGet(ctx, base, "/networks/bridge", &nw) == nil {
		st.DaemonIPv6 = &nw.EnableIPv6
	}
	return st
}

func containerIPv6() (bool, []string) {
	var addrs []string
	ifs, _ := net.Interfaces()
	for _, i := range ifs {
		as, _ := i.Addrs()
		for _, a := range as {
			if ip, _, err := net.ParseCIDR(a.String()); err == nil && ip.To4() == nil && ip.IsGlobalUnicast() {
				addrs = append(addrs, ip.String())
			}
		}
	}
	return len(addrs) > 0, addrs
}

func outbound6(ctx context.Context) (bool, string) {
	d := net.Dialer{Timeout: 4 * time.Second}
	var last error
	for _, t := range []string{"[2606:4700:4700::1111]:53", "[2001:4860:4860::8888]:53"} {
		c, err := d.DialContext(ctx, "tcp6", t)
		if err == nil {
			c.Close()
			return true, "conexão TCP IPv6 estabelecida"
		}
		last = err
	}
	return false, last.Error()
}

func (a *App) maskedCount(ctx context.Context, gws []string) (int, string) {
	if len(gws) == 0 {
		return 0, ""
	}
	n, last := 0, ""
	f, err := os.Open(filepath.Join(a.logDir, "main.log"))
	if err == nil {
		defer f.Close()
		if fi, err := f.Stat(); err == nil && fi.Size() > 4<<20 {
			_, _ = f.Seek(fi.Size()-4<<20, io.SeekStart)
		}
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 64*1024), 1<<20)
		for sc.Scan() {
			if l := sc.Text(); strings.Contains(l, gatewayMarker) && strings.Contains(l, "rejected") {
				n++
				if len(l) >= 19 {
					last = l[:19]
				}
			}
		}
	}
	if a.ready() {
		ph := strings.TrimSuffix(strings.Repeat("?,", len(gws)), ",")
		args := make([]any, len(gws))
		for i, g := range gws {
			args[i] = g
		}
		var c int
		if a.row(ctx, `SELECT count(*) FROM messages WHERE node_id=0 AND origin_ip IN (`+ph+`) AND received_at >= DATE_SUB(UTC_TIMESTAMP(6), INTERVAL 7 DAY)`, args...).Scan(&c) == nil {
			n += c
		}
	}
	return n, last
}

func (a *App) handleIPv6Status(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	dk := queryDocker(ctx)
	has6, addrs := containerIPv6()
	gws := dockerGateways()
	ok6, detail := outbound6(ctx)
	masked, last := a.maskedCount(ctx, gws)

	issues := []string{}
	if dk.Available && dk.DaemonIPv6 != nil && !*dk.DaemonIPv6 {
		issues = append(issues, "O daemon do Docker não está com IPv6 habilitado.")
	}
	if !has6 {
		issues = append(issues, "O container não recebeu endereço IPv6. A rede do compose precisa de enable_ipv6 e o Docker precisa ter IPv6 funcionando.")
	}
	if !ok6 {
		issues = append(issues, "Sem saída IPv6 para a internet. O host precisa ter IPv6 global e o Docker precisa fazer NAT66 (ip6tables).")
	}
	if masked > 0 {
		issues = append(issues, fmt.Sprintf("%d conexão(ões) chegaram com o IP do gateway do Docker (IP de origem mascarado) e foram recusadas por segurança. Isso ocorre quando a publicação de portas IPv6 passa pelo docker-proxy; habilite o ip6tables.", masked))
	}
	level := "ok"
	if len(issues) > 0 {
		level = "partial"
		if !has6 || !ok6 {
			level = "off"
		}
	}
	if addrs == nil {
		addrs = []string{}
	}
	if gws == nil {
		gws = []string{}
	}
	writeJSON(w, 200, map[string]any{
		"level": level, "issues": issues, "docker": dk,
		"container": map[string]any{"ipv6": has6, "addresses": addrs, "gateways": gws},
		"outbound6": map[string]any{"ok": ok6, "detail": detail},
		"masked":    map[string]any{"count": masked, "last": last},
	})
}
