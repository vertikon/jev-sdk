package jev

import (
	"context"
	crand "crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// A credencial dos testes é GERADA, nunca literal.
//
// Uma string com forma de chave escrita no repositório é indistinguível de
// credencial vazada — para um scanner de segredos e para quem lê um diff com
// pressa. Gerar elimina a classe inteira: não existe valor nenhum para vazar, e o
// teste passa a verificar o CONTRATO do formato (prefixo + corpo), que é o que o
// SDK de fato valida, em vez de uma string específica.
var chaveTeste = "jev_sk_" + aleatorio(32)

// Chave bem formada de OUTRO servico: o que se testa e a recusa por prefixo.
var chaveDeOutroServico = "rag_sk_" + aleatorio(32)

func aleatorio(n int) string {
	buf := make([]byte, (n*6+7)/8)
	if _, err := crand.Read(buf); err != nil {
		return strings.Repeat("0", n)
	}
	return base64.RawURLEncoding.EncodeToString(buf)[:n]
}

// O mesmo contrato de resiliência dos SDKs TypeScript e Python: um comportamento
// sob rate limit diferente por linguagem seria uma armadilha para quem escreve a
// mesma integração duas vezes.
func TestRetry_503DuasVezesDepois200(t *testing.T) {
	var tentativas int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&tentativas, 1) <= 2 {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"error":"indisponível","code":"unavailable"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"receipt_id":"rcp_1","status":"decided","route":"auto","enforced":true,
			"contract":{"id":"c","version":1,"ref":"c@1","fingerprint":"f","mode":"enforce","action_class":"read"},
			"answers":[],"warnings":[],"injected_options":[],"escalation":null,"improvement_hint":null,
			"usage":{"input_tokens":10,"output_tokens":0},"cost_micros_usd":1,"latency_ms":5,
			"state_hash":"h","request_id":"req_1"}`))
	}))
	defer srv.Close()

	cli := novoCliente(t, srv.URL)
	res, err := cli.Judge(context.Background(), JudgeRequest{Contract: "c", State: map[string]string{"a": "b"}})
	if err != nil {
		t.Fatalf("deveria ter sucesso na terceira tentativa: %v", err)
	}
	if got := atomic.LoadInt32(&tentativas); got != 3 {
		t.Fatalf("esperava 3 tentativas, houve %d", got)
	}
	if res.ReceiptID != "rcp_1" || !res.IsActionable() {
		t.Fatalf("resposta inesperada: %+v", res)
	}
}

// ADR-009: a 1ª tentativa morre DEPOIS de o servidor ler o corpo (o julgamento
// pode ter sido cobrado). O retry precisa ser a MESMA decisão — mesmo
// decision_id —, senão o serviço julga e cobra de novo.
func TestRetry_FalhaDeRedeReenviaMesmoDecisionID(t *testing.T) {
	var ids []string
	derrubou := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			DecisionID string `json:"decision_id"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		ids = append(ids, body.DecisionID)
		if !derrubou {
			derrubou = true
			conn, _, _ := w.(http.Hijacker).Hijack()
			_ = conn.Close() // resposta perdida
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"receipt_id":"rcp_1","status":"decided","route":"auto"}`))
	}))
	defer srv.Close()

	cli := novoCliente(t, srv.URL)
	if _, err := cli.Judge(context.Background(), JudgeRequest{Contract: "c", State: map[string]string{"a": "b"}}); err != nil {
		t.Fatalf("deveria ter sucesso na segunda tentativa: %v", err)
	}
	if len(ids) != 2 || ids[0] == "" || ids[0] != ids[1] {
		t.Fatalf("as tentativas deveriam carregar o mesmo decision_id não vazio, vieram %q", ids)
	}

	// id do consumidor é preservado; chamadas lógicas distintas geram ids distintos.
	ids = ids[:0]
	for _, id := range []string{"tkt_9931", "", ""} {
		_, _ = cli.Judge(context.Background(), JudgeRequest{Contract: "c", State: map[string]string{}, DecisionID: id})
	}
	if ids[0] != "tkt_9931" || ids[1] == ids[2] {
		t.Fatalf("esperava tkt_9931 preservado e dois ids distintos, vieram %q", ids)
	}
}

// ADR-009 C: fanout não é idempotente no serviço. Rede/timeout e 504 são falhas
// ambíguas (a cobrança pode ter ocorrido) e NÃO são repetidas; 503 é, porque vem
// antes da cobrança.
func TestFanout_FalhaAmbiguaNaoEhRepetida(t *testing.T) {
	for _, caso := range []struct {
		nome       string
		responde   func(w http.ResponseWriter, n int32)
		tentativas int32
	}{
		{"rede", func(w http.ResponseWriter, n int32) {
			if n == 1 {
				conn, _, _ := w.(http.Hijacker).Hijack()
				_ = conn.Close()
				return
			}
			_, _ = w.Write([]byte(`{"answers":[]}`))
		}, 1},
		{"504", func(w http.ResponseWriter, _ int32) { w.WriteHeader(http.StatusGatewayTimeout) }, 1},
		{"503", func(w http.ResponseWriter, n int32) {
			if n == 1 {
				w.WriteHeader(http.StatusServiceUnavailable)
				return
			}
			_, _ = w.Write([]byte(`{"answers":[]}`))
		}, 2},
	} {
		t.Run(caso.nome, func(t *testing.T) {
			var n int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				caso.responde(w, atomic.AddInt32(&n, 1))
			}))
			defer srv.Close()
			_, _ = novoCliente(t, srv.URL).Fanout(context.Background(), FanoutRequest{})
			if got := atomic.LoadInt32(&n); got != caso.tentativas {
				t.Fatalf("esperava %d tentativa(s), houve %d", caso.tentativas, got)
			}
		})
	}
}

// 422 NÃO é repetido: o JSON está bem formado e a REGRA é que não passa. Repetir
// só gasta tempo e cota.
func TestRetry_422NaoEhRepetido(t *testing.T) {
	var tentativas int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&tentativas, 1)
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{"error":"contrato reprovado: choice sem saída de escape","code":"invalid_contract","request_id":"req_x"}`))
	}))
	defer srv.Close()

	cli := novoCliente(t, srv.URL)
	_, err := cli.Judge(context.Background(), JudgeRequest{Contract: "c", State: map[string]string{"a": "b"}})
	if err == nil {
		t.Fatal("esperava erro")
	}
	if got := atomic.LoadInt32(&tentativas); got != 1 {
		t.Fatalf("422 não pode ser repetido: houve %d tentativas", got)
	}
	var apiErr *Error
	if !errors.As(err, &apiErr) {
		t.Fatalf("esperava *Error, veio %T", err)
	}
	if apiErr.Status != 422 || apiErr.Code != "invalid_contract" {
		t.Fatalf("erro inesperado: %+v", apiErr)
	}
	if apiErr.Retryable() {
		t.Fatal("422 não é retentável")
	}
	if apiErr.RequestID != "req_x" {
		t.Fatalf("request_id deveria vir do corpo: %q", apiErr.RequestID)
	}
	// A mensagem precisa nomear o invariante: é ela que o desenvolvedor lê.
	if !strings.Contains(apiErr.Error(), "saída de escape") {
		t.Fatalf("mensagem perdeu a causa: %q", apiErr.Error())
	}
}

// Esgotar as tentativas num erro retentável devolve erro marcado como retentável,
// para o chamador decidir enfileirar.
func TestRetry_EsgotarTentativas(t *testing.T) {
	var tentativas int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&tentativas, 1)
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(`{"error":"typesafe: upstream 500","code":"provider_error"}`))
	}))
	defer srv.Close()

	cli := novoCliente(t, srv.URL)
	cli.maxRetries = 2
	_, err := cli.Judge(context.Background(), JudgeRequest{Contract: "c", State: map[string]string{"a": "b"}})
	var apiErr *Error
	if !errors.As(err, &apiErr) {
		t.Fatalf("esperava *Error, veio %v", err)
	}
	if apiErr.Status != 502 || !apiErr.Retryable() {
		t.Fatalf("502 deveria ser retentável: %+v", apiErr)
	}
	if got := atomic.LoadInt32(&tentativas); got != 2 {
		t.Fatalf("esperava 2 tentativas, houve %d", got)
	}
}

// 429 respeita o Retry-After do serviço.
func TestRetry_RespeitaRetryAfter(t *testing.T) {
	var tentativas int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&tentativas, 1)
		if n == 1 {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error":"teto de tokens do inquilino","code":"quota_exceeded"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"receipt_id":"rcp_2","status":"decided","route":"human_review","enforced":false,
			"contract":{"id":"c","version":1,"ref":"c@1","fingerprint":"f","mode":"shadow","action_class":"read"},
			"answers":[],"warnings":[],"injected_options":[],"escalation":null,"improvement_hint":null,
			"usage":{"input_tokens":1,"output_tokens":0},"cost_micros_usd":0,"latency_ms":1,
			"state_hash":"h","request_id":"req_2"}`))
	}))
	defer srv.Close()

	cli := novoCliente(t, srv.URL)
	inicio := time.Now()
	_, err := cli.Judge(context.Background(), JudgeRequest{Contract: "c", State: map[string]string{"a": "b"}})
	if err != nil {
		t.Fatalf("deveria ter sucesso: %v", err)
	}
	if decorrido := time.Since(inicio); decorrido < 900*time.Millisecond {
		t.Fatalf("deveria ter esperado ~1s do Retry-After, esperou %v", decorrido)
	}
}

// O prefixo da credencial é validado na CONSTRUÇÃO: falhar na primeira chamada de
// produção é pior que não construir o cliente.
func TestNew_ValidaPrefixoDaChave(t *testing.T) {
	if _, err := New(Options{APIKey: ""}); err == nil {
		t.Fatal("chave vazia deveria ser recusada")
	}
	if _, err := New(Options{APIKey: chaveDeOutroServico}); err == nil {
		t.Fatal("prefixo de outro serviço deveria ser recusado")
	}
	if _, err := New(Options{APIKey: chaveTeste}); err != nil {
		t.Fatalf("chave válida recusada: %v", err)
	}
}

// IsActionable é a única condição que autoriza agir. Testar a rota sem o
// `enforced` é o erro que transforma observação em ação real.
func TestResponse_IsActionable(t *testing.T) {
	autoEnforce := Response{Route: RouteAuto, Enforced: true}
	if !autoEnforce.IsActionable() {
		t.Fatal("auto + enforce autoriza")
	}
	autoSombra := Response{Route: RouteAuto, Enforced: false}
	if autoSombra.IsActionable() {
		t.Fatal("auto em sombra NÃO autoriza")
	}
	for _, rota := range []Route{RouteCollectEvidence, RouteHumanReview, RouteAbstain} {
		r := Response{Route: rota, Enforced: true}
		if r.IsActionable() {
			t.Fatalf("%s nunca autoriza, mesmo com enforced=true", rota)
		}
	}
}

// Explain é o que vai para log e para a fila humana: precisa separar "não
// perguntamos" de "disse não".
func TestResponse_Explain(t *testing.T) {
	conf := 0.93
	sel := &Answer{QuestionID: "fila", Type: KindChoice, Choice: "suporte", Confidence: &conf}
	casos := []struct {
		nome string
		res  Response
		quer string
	}{
		{"autorizado", Response{Route: RouteAuto, Enforced: true, Allow: "support:route", Selected: sel},
			"suporte"},
		{"sombra", Response{Route: RouteAuto, Enforced: false, Selected: sel}, "NÃO AUTORIZADO"},
		{"abstencao", Response{Route: RouteAbstain}, "menu"},
		{"escalada", Response{Route: RouteHumanReview, Status: StatusProviderError}, "provider_error"},
	}
	for _, tc := range casos {
		t.Run(tc.nome, func(t *testing.T) {
			got := tc.res.Explain()
			if !strings.Contains(got, tc.quer) {
				t.Fatalf("Explain()=%q deveria conter %q", got, tc.quer)
			}
		})
	}
}

// O trace do contexto é propagado para a borda, que o repassa ao evento NATS.
func TestTrace_PropagadoNoCabecalho(t *testing.T) {
	var recebido string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		recebido = r.Header.Get("X-Trace-Id")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"receipt_id":"r","status":"decided","route":"human_review","enforced":false,
			"contract":{"id":"c","version":1,"ref":"c@1","fingerprint":"f","mode":"shadow","action_class":"read"},
			"answers":[],"warnings":[],"injected_options":[],"escalation":null,"improvement_hint":null,
			"usage":{"input_tokens":0,"output_tokens":0},"cost_micros_usd":0,"latency_ms":0,
			"state_hash":"h","request_id":"req"}`))
	}))
	defer srv.Close()

	cli := novoCliente(t, srv.URL)
	ctx := WithTraceID(context.Background(), "trace_abc123")
	if _, err := cli.Judge(ctx, JudgeRequest{Contract: "c", State: 1}); err != nil {
		t.Fatalf("julgar: %v", err)
	}
	if recebido != "trace_abc123" {
		t.Fatalf("trace não propagado: %q", recebido)
	}
}

// noul: 0 é resposta válida e precisa sobreviver à desserialização.
func TestAnswer_NoulZeroSobrevive(t *testing.T) {
	var a Answer
	if err := json.Unmarshal([]byte(`{"question_id":"x","type":"noul","noul":0}`), &a); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if a.Noul == nil || *a.Noul != 0 {
		t.Fatalf("noul:0 virou %v", a.Noul)
	}
	if a.Value() != "0" {
		t.Fatalf("Value()=%q", a.Value())
	}
	// Ausente continua distinguível de zero.
	var b Answer
	_ = json.Unmarshal([]byte(`{"question_id":"x","type":"noul"}`), &b)
	if b.Noul != nil {
		t.Fatal("ausente virou presente")
	}
	if b.Value() != "" {
		t.Fatalf("ausente deveria dar valor vazio, veio %q", b.Value())
	}
}

// OnRequest é o gancho de observabilidade: precisa ver TODAS as tentativas, não
// só a bem-sucedida.
func TestOnRequest_VeCadaTentativa(t *testing.T) {
	var tentativas int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&tentativas, 1) == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"receipt_id":"r","status":"decided","route":"human_review","enforced":false,
			"contract":{"id":"c","version":1,"ref":"c@1","fingerprint":"f","mode":"shadow","action_class":"read"},
			"answers":[],"warnings":[],"injected_options":[],"escalation":null,"improvement_hint":null,
			"usage":{"input_tokens":0,"output_tokens":0},"cost_micros_usd":0,"latency_ms":0,
			"state_hash":"h","request_id":"req"}`))
	}))
	defer srv.Close()

	var vistos []RequestInfo
	cli, err := New(Options{
		APIKey: chaveTeste, BaseURL: srv.URL, MaxRetries: 3,
		OnRequest: func(i RequestInfo) { vistos = append(vistos, i) },
		// Servidor local: a guarda de endpoint exige a declaração explícita.
		AllowPrivateEndpoints: true,
	})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	if _, err := cli.Judge(context.Background(), JudgeRequest{Contract: "c", State: 1}); err != nil {
		t.Fatalf("julgar: %v", err)
	}
	if len(vistos) != 2 {
		t.Fatalf("esperava 2 eventos (503 e 200), veio %d", len(vistos))
	}
	if vistos[0].Status != 503 || vistos[1].Status != 200 {
		t.Fatalf("status inesperados: %+v", vistos)
	}
	if vistos[0].Attempt != 1 || vistos[1].Attempt != 2 {
		t.Fatalf("tentativas inesperadas: %+v", vistos)
	}
}

// O cancellation do chamador encerra o retry: insistir depois do cancelamento é o
// que transforma um Ctrl-C em espera longa.
func TestRetry_CancelamentoInterrompe(t *testing.T) {
	var tentativas int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&tentativas, 1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	cli := novoCliente(t, srv.URL)
	cli.maxRetries = 10
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	_, err := cli.Judge(ctx, JudgeRequest{Contract: "c", State: 1})
	if err == nil {
		t.Fatal("esperava erro com contexto cancelado")
	}
	if got := atomic.LoadInt32(&tentativas); got >= 10 {
		t.Fatalf("o cancelamento deveria ter interrompido o retry, houve %d tentativas", got)
	}
}

// Cursor e filtros precisam virar query string corretamente — é o contrato de
// paginação da listagem.
func TestListReceipts_QueryString(t *testing.T) {
	var recebida string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		recebida = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"receipts":[]}`))
	}))
	defer srv.Close()

	cli := novoCliente(t, srv.URL)
	list, err := cli.ListReceipts(context.Background(), ReceiptFilter{
		Contract: "ticket-router", Route: RouteHumanReview, Limit: 25, Cursor: "rcp_9",
	})
	if err != nil {
		t.Fatalf("listar: %v", err)
	}
	if len(list.Receipts) != 0 || list.Cursor != "" {
		t.Fatalf("resposta inesperada: %+v", list)
	}
	for _, esperado := range []string{"contract=ticket-router", "route=human_review", "limit=25", "cursor=rcp_9"} {
		if !strings.Contains(recebida, esperado) {
			t.Fatalf("query %q deveria conter %q", recebida, esperado)
		}
	}
}

// Sem contrato, fanout NÃO devolve rota: é o que impede o consumidor de tratar
// julgamento avulso como autorização.
func TestFanout_SemCamposDeAutorizacao(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"answers":[{"question_id":"q","type":"noul","noul":0.9}],"usage":{"input_tokens":5,"output_tokens":0},"latency_ms":30,"request_id":"req_9"}`))
	}))
	defer srv.Close()

	cli := novoCliente(t, srv.URL)
	res, err := cli.Fanout(context.Background(), FanoutRequest{
		State:     map[string]string{"a": "b"},
		Questions: map[string]Question{"q": {Type: KindNoul, Instructions: "É o caso?"}},
	})
	if err != nil {
		t.Fatalf("fanout: %v", err)
	}
	if len(res.Answers) != 1 || res.Answers[0].Noul == nil || *res.Answers[0].Noul != 0.9 {
		t.Fatalf("resposta inesperada: %+v", res.Answers)
	}
}

func novoCliente(t *testing.T, baseURL string) *Client {
	t.Helper()
	cli, err := New(Options{
		APIKey: chaveTeste, BaseURL: baseURL, MaxRetries: 3, Timeout: 5 * time.Second,
		// O servidor de teste é LOCAL: a guarda de endpoint recusaria sem esta
		// declaração explícita. É o comportamento desejado — quem aponta para
		// loopback tem de dizer que é de propósito.
		AllowPrivateEndpoints: true,
	})
	if err != nil {
		t.Fatalf("montar cliente: %v", err)
	}
	return cli
}
