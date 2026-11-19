// Package push envia eventos do log do Exim a um servidor central NavalRelay (POST /api/collect).
package push

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"orbitarelay/internal/eximlog"
)

// Client conversa com o servidor central.
type Client struct {
	Scheme     string // "https" (padrão) ou "http"
	Host       string
	Port       int
	BasePath   string // prefixo opcional, por exemplo "/orbita"
	Token      string
	SkipVerify bool
	UserAgent  string
}

// Info é a resposta do ping.
type Info struct {
	Node    string `json:"node"`
	Version string `json:"version"`
}

// WireEvent é o evento no formato de rede.
type WireEvent struct {
	At      time.Time `json:"at"`
	ID      string    `json:"id"`
	Kind    string    `json:"k"`
	Sender  string    `json:"sender,omitempty"`
	Host    string    `json:"host,omitempty"`
	IP      string    `json:"ip,omitempty"`
	Subject string    `json:"subject,omitempty"`
	Size    int       `json:"size,omitempty"`
	Rcpt    string    `json:"rcpt,omitempty"`
	RHost   string    `json:"rhost,omitempty"`
	RIP     string    `json:"rip,omitempty"`
	Detail  string    `json:"detail,omitempty"`
}

// URL monta o endereço de um caminho da API.
func (c *Client) URL(path string) (string, error) {
	if c.Host == "" || c.Port == 0 {
		return "", errors.New("servidor central não configurado")
	}
	scheme := "https"
	if c.Scheme == "http" {
		scheme = "http"
	}
	return scheme + "://" + net.JoinHostPort(c.Host, strconv.Itoa(c.Port)) + c.BasePath + path, nil
}

func (c *Client) do(ctx context.Context, method, path string, body []byte, gz bool) (*http.Response, func(), error) {
	u, err := c.URL(path)
	if err != nil {
		return nil, nil, err
	}
	if c.Token == "" {
		return nil, nil, errors.New("token do servidor não informado")
	}
	tr := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		TLSClientConfig:       &tls.Config{InsecureSkipVerify: c.SkipVerify, MinVersion: tls.VersionTLS12},
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 30 * time.Second,
		DialContext:           (&net.Dialer{Timeout: 10 * time.Second}).DialContext,
	}
	cl := &http.Client{
		Transport:     tr,
		Timeout:       45 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	req, err := http.NewRequestWithContext(ctx, method, u, bytes.NewReader(body))
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	ua := c.UserAgent
	if ua == "" {
		ua = "NavalRelay"
	}
	req.Header.Set("User-Agent", ua)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
		if gz {
			req.Header.Set("Content-Encoding", "gzip")
		}
	}
	resp, err := cl.Do(req)
	if err != nil {
		tr.CloseIdleConnections()
		return nil, nil, err
	}
	return resp, tr.CloseIdleConnections, nil
}

// RespError traduz uma resposta HTTP de falha em uma mensagem para o usuário.
func RespError(resp *http.Response) error {
	switch {
	case resp.StatusCode >= 300 && resp.StatusCode < 400:
		return fmt.Errorf("o servidor respondeu com redirecionamento (%d); ajuste protocolo, porta e caminho para a URL final", resp.StatusCode)
	case resp.StatusCode == 401 || resp.StatusCode == 403:
		return errors.New("token recusado pelo servidor central")
	case resp.StatusCode == 404:
		return errors.New("endpoint não encontrado (o destino está em modo Servidor? o caminho está correto?)")
	case resp.StatusCode == 429:
		return errors.New("servidor central limitou as requisições (muitas tentativas)")
	}
	return fmt.Errorf("resposta inesperada do servidor central: HTTP %d", resp.StatusCode)
}

// Send envia um lote de eventos (JSON compactado com gzip).
func (c *Client) Send(ctx context.Context, evs []eximlog.Event) error {
	if len(evs) == 0 {
		return nil
	}
	wire := make([]WireEvent, len(evs))
	for i, e := range evs {
		wire[i] = WireEvent{At: e.At, ID: e.ID, Kind: e.Kind, Sender: e.Sender, Host: e.Host, IP: e.IP,
			Subject: e.Subject, Size: e.Size, Rcpt: e.Rcpt, RHost: e.RHost, RIP: e.RIP, Detail: e.Detail}
	}
	raw, err := json.Marshal(map[string]any{"events": wire})
	if err != nil {
		return err
	}
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	_, _ = zw.Write(raw)
	_ = zw.Close()
	resp, done, err := c.do(ctx, http.MethodPost, "/api/collect", buf.Bytes(), true)
	if err != nil {
		return err
	}
	defer done()
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
	if resp.StatusCode != 200 {
		return RespError(resp)
	}
	return nil
}

// Ping confere a conexão e o token.
func (c *Client) Ping(ctx context.Context) (Info, error) {
	var out Info
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	resp, done, err := c.do(ctx, http.MethodGet, "/api/collect/ping", nil, false)
	if err != nil {
		return out, fmt.Errorf("falha na conexão: %w", err)
	}
	defer done()
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return out, RespError(resp)
	}
	_ = json.NewDecoder(io.LimitReader(resp.Body, 1<<16)).Decode(&out)
	return out, nil
}

// TrimBase normaliza o prefixo de caminho.
func TrimBase(p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return ""
	}
	return "/" + strings.Trim(p, "/")
}
