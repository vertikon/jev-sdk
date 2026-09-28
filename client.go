// Package jev é o SDK Go oficial do JEVaaS (jev.vertikon.com.br).
//
// É a forma de uma vertical Vertikon consumir decisão tipada: contrato
// versionado, rota por confiança e recibo auditável, sem falar HTTP na mão.
//
// Dependência zero, de propósito: só a biblioteca padrão. Um SDK que arrasta
// transitivas para dentro de uma vertical contamina o go.mod de todo mundo que
// só queria julgar uma coisa.
//
// A regra de uso que o SDK não pode esquecer por você: `Route == RouteAuto` e
// `Enforced == true` são o ÚNICO caso em que o consumidor está autorizado a agir.
// O serviço devolve julgamento e rota; quem executa é o seu motor de política.
// Use [Response.IsActionable] em vez de comparar strings à mão.
//
//	cli := jev.New(jev.Options{APIKey: os.Getenv("JEVAAAS_API_KEY")})
//	res, err := cli.Judge(ctx, jev.JudgeRequest{
//	    Contract: "ticket-router",
//	    State:    map[string]any{"ticket": map[string]any{"messages": msgs}},
//	})
//	if err != nil { return err }
//	switch {
//	case res.IsActionable():
//	    return fila.Enfileirar(ctx, res.Allow, res.Selected.Choice)
//	case res.Route == jev.RouteHumanReview:
//	    return fila.Escalar(ctx, res.Escalation, res.ReceiptID)
//	default:
//	    return nil
//	}
package jev

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// DefaultBaseURL é a borda de produção da vertical.
const DefaultBaseURL = "https://jev.api.br/v1"

// KeyPrefix é o prefixo obrigatório da credencial. Validar aqui dá a mensagem
// certa em vez de um 401 opaco depois.
const KeyPrefix = "jev_sk_"

// DefaultTimeout é o limite por requisição. Julgamento é rápido (o provedor
// roda na casa de segundos), então 30s é generoso.
const DefaultTimeout = 30 * time.Second

// DefaultMaxRetries é o número de tentativas TOTAIS (não de repetições).
const DefaultMaxRetries = 3

// retryableStatus são os códigos em que repetir é seguro: 0 (falha de rede ou
// timeout do cliente), 429 (limite) e a família 502/503/504. Repetir um 422 é
// inútil: o JSON está bem formado e a REGRA é que não passa.
var retryableStatus = map[int]bool{0: true, 429: true, 502: true, 503: true, 504: true}

// Options configura o cliente.
type Options struct {
	// APIKey é obrigatória: `jev_sk_…` do inquilino no painel.
	APIKey string
	// BaseURL é a borda; vazio usa DefaultBaseURL.
	BaseURL string
	// Timeout por requisição; zero usa DefaultTimeout.
	Timeout time.Duration
	// MaxRetries é o total de tentativas; zero usa DefaultMaxRetries (mínimo 1).
	MaxRetries int
	// HTTPClient permite injetar transporte próprio (proxy, TLS, instrumentação).
	// Vazio usa um cliente com o Timeout acima.
	HTTPClient *http.Client
	// OnRequest é chamado a cada requisição FINALIZADA — para métrica e log sem
	// que o SDK imponha um logger.
	OnRequest func(RequestInfo)
	// AllowPrivateEndpoints permite que BaseURL aponte para host local ou de faixa
	// privada. Existe para desenvolvimento (JEVaaS na própria máquina); o default
	// recusa, porque o SDK roda no lado servidor e manda o `state` do usuário para
	// esse endereço. Ver endpoint.go.
	AllowPrivateEndpoints bool
}

// RequestInfo descreve uma requisição finalizada, para observabilidade.
type RequestInfo struct {
	Method    string
	Path      string
	Status    int
	RequestID string
	Attempt   int
	Latency   time.Duration
}

// Client é o cliente do JEVaaS. É seguro para uso concorrente.
type Client struct {
	apiKey     string
	baseURL    string
	timeout    time.Duration
	maxRetries int
	http       *http.Client
	onRequest  func(RequestInfo)
}

// New monta o cliente. Devolve erro quando a credencial está ausente ou tem o
// prefixo errado — falhar na construção é melhor que falhar na primeira chamada
// de produção.
func New(opts Options) (*Client, error) {
	if strings.TrimSpace(opts.APIKey) == "" {
		return nil, fmt.Errorf("jev: APIKey obrigatória (jev_sk_… do inquilino)")
	}
	if !strings.HasPrefix(opts.APIKey, KeyPrefix) {
		return nil, fmt.Errorf("jev: APIKey inválida — esperado prefixo %s, veio %q", KeyPrefix, safePrefix(opts.APIKey))
	}
	base := strings.TrimRight(strings.TrimSpace(opts.BaseURL), "/")
	if base == "" {
		base = DefaultBaseURL
	}
	// Guarda de endpoint: o SDK roda no lado servidor e manda o `state` do usuário
	// para este endereço, então ele é validado ANTES de o cliente existir — uma
	// instância construída é uma instância autorizada a falar com aquele host.
	if err := validarBaseURL(base, opts.AllowPrivateEndpoints); err != nil {
		return nil, err
	}
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	retries := opts.MaxRetries
	if retries <= 0 {
		retries = DefaultMaxRetries
	}
	httpClient := opts.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: timeout}
	}
	return &Client{
		apiKey: opts.APIKey, baseURL: base, timeout: timeout,
		maxRetries: retries, http: httpClient, onRequest: opts.OnRequest,
	}, nil
}

// FromEnv monta o cliente a partir do ambiente, no contrato do DEVKIT:
// JEVAAAS_API_KEY, JEVAAAS_URL e JEVAAAS_TIMEOUT_S.
func FromEnv() (*Client, error) {
	key := getenv("JEVAAAS_API_KEY")
	if key == "" {
		return nil, fmt.Errorf("jev: JEVAAAS_API_KEY não definida")
	}
	opts := Options{APIKey: key, BaseURL: getenv("JEVAAAS_URL")}
	if s := getenv("JEVAAAS_TIMEOUT_S"); s != "" {
		if n, err := strconv.Atoi(s); err == nil && n > 0 {
			opts.Timeout = time.Duration(n) * time.Second
		}
	}
	return New(opts)
}

// Error é o erro do serviço. Carrega o status HTTP, o código estável do envelope
// e o request_id, que é o que se cita ao abrir suporte.
type Error struct {
	Status    int
	Code      string
	Message   string
	RequestID string
	// Body é o corpo cru, quando não foi possível interpretar o envelope.
	Body []byte
}

func (e *Error) Error() string {
	msg := e.Message
	if msg == "" {
		msg = http.StatusText(e.Status)
	}
	if e.RequestID != "" {
		return fmt.Sprintf("jev: status %d (%s): %s [request %s]", e.Status, e.Code, msg, e.RequestID)
	}
	return fmt.Sprintf("jev: status %d (%s): %s", e.Status, e.Code, msg)
}

// Retryable diz se repetir a chamada é seguro E pode funcionar.
func (e *Error) Retryable() bool { return retryableStatus[e.Status] }

// Is permite comparar com erros alvo por status, quando o chamador só quer saber
// a categoria sem importar os detalhes.
func (e *Error) Is(target error) bool {
	var t *Error
	if !errors.As(target, &t) {
		return false
	}
	return t.Status == e.Status && (t.Code == "" || t.Code == e.Code)
}

// Erros de borda reconhecíveis, para o chamador decidir sem olhar strings.
var (
	// ErrContractNotFound: contrato inexistente (ou de outro inquilino).
	ErrContractNotFound = &Error{Status: http.StatusNotFound, Code: "not_found"}
	// ErrInvalidContract: a regra do contrato não passa na validação. Corrigir o
	// contrato, não a chamada.
	ErrInvalidContract = &Error{Status: http.StatusUnprocessableEntity, Code: "invalid_contract"}
	// ErrVersionConflict: já existe versão com esse número.
	ErrVersionConflict = &Error{Status: http.StatusConflict, Code: "version_conflict"}
	// ErrBudgetExceeded: teto do inquilino estourou. NÃO é "o modelo disse não".
	ErrBudgetExceeded = &Error{Status: http.StatusTooManyRequests, Code: "quota_exceeded"}
	// ErrProviderError: a borda de IA falhou. NÃO é julgamento negativo.
	ErrProviderError = &Error{Status: http.StatusBadGateway, Code: "provider_error"}
	// ErrUnauthorized: credencial ausente, revogada ou malformada.
	ErrUnauthorized = &Error{Status: http.StatusUnauthorized, Code: "unauthorized"}
)

// ---------- transporte ----------

// do executa a requisição com retry e devolve o corpo cru. É o ÚNICO ponto que
// fala HTTP: manter um só é o que garante que timeout, retry, backoff e leitura
// do envelope de erro valham para todos os métodos.
func (c *Client) do(ctx context.Context, method, path string, body any, out any) error {
	return c.send(ctx, method, path, body, out, true)
}

// send é o laço de do. retryAmbiguous=false é para chamada que o serviço NÃO
// deduplica (fanout): em falha ambígua — rede/timeout, 504 — a cobrança pode ter
// ocorrido, e repetir seria cobrar de novo (ADR-009 C).
func (c *Client) send(ctx context.Context, method, path string, body any, out any, retryAmbiguous bool) error {
	var payload []byte
	var err error
	if body != nil {
		payload, err = json.Marshal(body)
		if err != nil {
			return fmt.Errorf("jev: serializar corpo: %w", err)
		}
	}

	var lastErr error
	for attempt := 1; attempt <= c.maxRetries; attempt++ {
		started := time.Now()
		res := c.attempt(ctx, method, path, payload)

		if c.onRequest != nil {
			c.onRequest(RequestInfo{
				Method: method, Path: path, Status: res.status,
				RequestID: res.requestID, Attempt: attempt, Latency: time.Since(started),
			})
		}

		if res.err != nil {
			lastErr = &Error{Status: 0, Code: "network", Message: res.err.Error()}
			if retryAmbiguous && attempt < c.maxRetries && ctx.Err() == nil {
				if !sleep(ctx, backoff(attempt)) {
					return ctx.Err()
				}
				continue
			}
			return lastErr
		}

		if res.status >= 200 && res.status < 300 {
			if out == nil || len(res.raw) == 0 {
				return nil
			}
			if err := json.Unmarshal(res.raw, out); err != nil {
				return fmt.Errorf("jev: decodificar resposta: %w", err)
			}
			return nil
		}

		apiErr := decodeError(res.status, res.requestID, res.raw)
		if !apiErr.Retryable() || (!retryAmbiguous && res.status == 504) || attempt == c.maxRetries || ctx.Err() != nil {
			return apiErr
		}
		lastErr = apiErr
		// O Retry-After do serviço manda: ele sabe quando o teto do inquilino
		// reabre, e um backoff próprio mais curto só bateria na porta de novo. Sem
		// o cabeçalho, vale a curva exponencial com jitter.
		wait := backoff(attempt)
		if res.retryAfter > 0 {
			wait = res.retryAfter
		}
		if !sleep(ctx, wait) {
			return ctx.Err()
		}
	}
	if lastErr != nil {
		return lastErr
	}
	return fmt.Errorf("jev: tentativas esgotadas")
}

// result é o produto de uma tentativa, antes de qualquer interpretação.
type result struct {
	status     int
	requestID  string
	raw        []byte
	retryAfter time.Duration
	err        error
}

// attempt faz UMA tentativa e devolve o resultado cru.
func (c *Client) attempt(ctx context.Context, method, path string, payload []byte) result {
	var reader io.Reader
	if payload != nil {
		reader = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return result{err: err}
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Accept", "application/json")
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if v := traceFromContext(ctx); v != "" {
		req.Header.Set("X-Trace-Id", v)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return result{err: err}
	}
	defer func() {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
		_ = resp.Body.Close()
	}()

	// Teto de leitura: uma resposta gigante não pode virar consumo de memória do
	// consumidor. 8 MiB cabe qualquer recibo real.
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return result{status: resp.StatusCode, requestID: resp.Header.Get("X-Request-Id"), err: err}
	}
	return result{
		status:     resp.StatusCode,
		requestID:  resp.Header.Get("X-Request-Id"),
		raw:        raw,
		retryAfter: retryAfterFrom(resp.Header),
	}
}

// retryAfterFrom lê o Retry-After em segundos. O serviço só emite o formato
// numérico; uma data HTTP cai no backoff próprio em vez de virar espera
// imprevisível.
func retryAfterFrom(h http.Header) time.Duration {
	v := strings.TrimSpace(h.Get("Retry-After"))
	if v == "" {
		return 0
	}
	secs, err := strconv.Atoi(v)
	if err != nil || secs <= 0 {
		return 0
	}
	// Teto de 60s: uma resposta malformada não pode travar o chamador por horas.
	if secs > 60 {
		secs = 60
	}
	return time.Duration(secs) * time.Second
}

// decodeError lê o envelope de erro do serviço (devkit §2).
func decodeError(status int, headerRequestID string, raw []byte) *Error {
	e := &Error{Status: status, Body: raw}
	var env struct {
		Error     string `json:"error"`
		Code      string `json:"code"`
		RequestID string `json:"request_id"`
	}
	if err := json.Unmarshal(raw, &env); err == nil {
		e.Message = env.Error
		e.Code = env.Code
		e.RequestID = env.RequestID
	}
	// O cabeçalho é a fonte de verdade do id de correlação: o corpo pode faltar.
	if e.RequestID == "" {
		e.RequestID = headerRequestID
	}
	if e.Code == "" {
		e.Code = codeForStatus(status)
	}
	return e
}

// codeForStatus espelha o mapa do serviço, para o caso de um proxy devolver um
// erro que não passou pelo serviço.
func codeForStatus(status int) string {
	switch status {
	case 400:
		return "invalid_body"
	case 401:
		return "unauthorized"
	case 403:
		return "forbidden"
	case 404:
		return "not_found"
	case 409:
		return "version_conflict"
	case 413:
		return "state_too_large"
	case 422:
		return "invalid_contract"
	case 429:
		return "quota_exceeded"
	case 502:
		return "provider_error"
	case 503:
		return "unavailable"
	case 504:
		return "gateway_timeout"
	}
	return "http_" + strconv.Itoa(status)
}

// backoff é exponencial com jitter, teto 8s — a mesma curva dos SDKs TypeScript
// e Python, para que o comportamento sob rate limit seja idêntico em qualquer
// linguagem.
func backoff(attempt int) time.Duration {
	base := 250 * time.Millisecond * time.Duration(1<<uint(attempt-1))
	jitter := time.Duration(rand.Int63n(int64(250 * time.Millisecond)))
	d := base + jitter
	if d > 8*time.Second {
		d = 8 * time.Second
	}
	return d
}

// sleep devolve false se o contexto terminar antes — o chamador cancela, e
// insistir depois do cancelamento é o que transforma um Ctrl-C em espera longa.
func sleep(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

func safePrefix(s string) string {
	if len(s) <= 12 {
		return s[:min(len(s), 6)] + "…"
	}
	return s[:12] + "…"
}
