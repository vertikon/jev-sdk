package jev

import (
	"context"
	"crypto/rand"
	"net/url"
	"strconv"
)

// Judge julga um estado contra um contrato publicado e devolve a decisão roteada.
//
// Erro devolvido aqui é de BORDA (404 contrato, 429 orçamento, 502 provedor…).
// Desfechos de decisão — inclusive provedor fora do ar e abstenção — vêm dentro
// da Response com Status próprio, porque "não perguntamos" e "o modelo disse não"
// são fatos diferentes com custos diferentes. Use [Response.IsActionable] antes de
// agir.
func (c *Client) Judge(ctx context.Context, req JudgeRequest) (*Response, error) {
	// A chave nasce aqui, uma vez por chamada lógica — nunca em do(), que roda
	// uma vez por tentativa. O retry após timeout vira a MESMA decisão e o
	// serviço devolve o recibo em vez de julgar e cobrar de novo (ADR-009).
	if req.DecisionID == "" {
		req.DecisionID = rand.Text()
	}
	var out Response
	if err := c.do(ctx, "POST", "/decisions/judge", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Fanout julga perguntas soltas, sem contrato publicado.
//
// Sem contrato não existe declaração de consequência, então NÃO existe
// autorização: a resposta é julgamento puro e a decisão de agir é sua. Para
// automação governada, publique um contrato e use Judge.
func (c *Client) Fanout(ctx context.Context, req FanoutRequest) (*FanoutResponse, error) {
	var out FanoutResponse
	// O serviço não deduplica fanout: repetir em timeout seria cobrar de novo (ADR-009 C).
	if err := c.send(ctx, "POST", "/decisions/fanout", req, &out, false); err != nil {
		return nil, err
	}
	return &out, nil
}

// ---------- contratos ----------

// CreateContract publica a versão 1 de um contrato novo. Devolve também os avisos
// de validação (o que é suspeito mas não letal).
func (c *Client) CreateContract(ctx context.Context, in ContractInput) (*ContractCreated, error) {
	var out ContractCreated
	if err := c.do(ctx, "POST", "/contracts", in, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// UpdateContract publica uma VERSÃO NOVA. A anterior fica imutável, que é o que
// permite reverter. Conteúdo decisório idêntico ao da versão vigente é recusado
// com ErrVersionConflict: versão vazia poluiria a calibração por versão.
func (c *Client) UpdateContract(ctx context.Context, id string, in ContractInput) (*ContractCreated, error) {
	in.ID = id
	var out ContractCreated
	if err := c.do(ctx, "PUT", "/contracts/"+url.PathEscape(id), in, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ListContracts devolve a versão vigente de cada contrato do inquilino.
func (c *Client) ListContracts(ctx context.Context) ([]Contract, error) {
	var out []Contract
	if err := c.do(ctx, "GET", "/contracts", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// GetContract devolve a versão vigente, ou a versão pedida quando version > 0.
func (c *Client) GetContract(ctx context.Context, id string, version int) (*Contract, error) {
	path := "/contracts/" + url.PathEscape(id)
	if version > 0 {
		path = "/contracts/" + url.PathEscape(id) + "/versions/" + strconv.Itoa(version)
	}
	var out Contract
	if err := c.do(ctx, "GET", path, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ContractVersions devolve o histórico, da mais antiga para a mais nova.
func (c *Client) ContractVersions(ctx context.Context, id string) ([]Contract, error) {
	var out []Contract
	if err := c.do(ctx, "GET", "/contracts/"+url.PathEscape(id)+"/versions", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// PromoteVersion faz uma versão antiga voltar a valer, republicando-a sob um
// número novo com o MESMO fingerprint (a decisão não mudou, só voltou a valer).
// É a operação de rollback: sem ela, publicar uma regra ruim seria irreversível.
func (c *Client) PromoteVersion(ctx context.Context, id string, version int) (*Contract, error) {
	var out Contract
	path := "/contracts/" + url.PathEscape(id) + "/versions/" + strconv.Itoa(version) + "/promote"
	if err := c.do(ctx, "POST", path, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// SetMode troca sombra/enforce na versão vigente. Passar para enforce é dar
// AUTORIDADE a uma automação — o serviço registra em nível de aviso.
func (c *Client) SetMode(ctx context.Context, id string, mode Mode) (*Contract, error) {
	body := map[string]string{"mode": string(mode)}
	var out Contract
	if err := c.do(ctx, "POST", "/contracts/"+url.PathEscape(id)+"/mode", body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ---------- recibos ----------

// ListReceipts devolve uma página de recibos e o cursor da próxima (vazio na
// última).
func (c *Client) ListReceipts(ctx context.Context, f ReceiptFilter) (*ReceiptList, error) {
	q := url.Values{}
	if f.Contract != "" {
		q.Set("contract", f.Contract)
	}
	if f.Route != "" {
		q.Set("route", string(f.Route))
	}
	if f.Status != "" {
		q.Set("status", string(f.Status))
	}
	if !f.From.IsZero() {
		q.Set("from", f.From.UTC().Format("2006-01-02T15:04:05Z07:00"))
	}
	if !f.To.IsZero() {
		q.Set("to", f.To.UTC().Format("2006-01-02T15:04:05Z07:00"))
	}
	if f.Limit > 0 {
		q.Set("limit", strconv.Itoa(f.Limit))
	}
	if f.Cursor != "" {
		q.Set("cursor", f.Cursor)
	}
	path := "/receipts"
	if encoded := q.Encode(); encoded != "" {
		path += "?" + encoded
	}
	var out ReceiptList
	if err := c.do(ctx, "GET", path, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetReceipt devolve o recibo completo: a distribuição INTEIRA, o limiar
// aplicado, a rota e o custo — não só o rótulo vencedor.
func (c *Client) GetReceipt(ctx context.Context, id string) (*Receipt, error) {
	var out Receipt
	if err := c.do(ctx, "GET", "/receipts/"+url.PathEscape(id), nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// LabelReceipt grava o rótulo humano — o insumo da calibração.
func (c *Client) LabelReceipt(ctx context.Context, id, humanLabel, labeledBy string) (*Receipt, error) {
	body := map[string]string{"human_label": humanLabel}
	if labeledBy != "" {
		body["labeled_by"] = labeledBy
	}
	var out Receipt
	if err := c.do(ctx, "POST", "/receipts/"+url.PathEscape(id)+"/label", body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// RecordOutcome registra o que a produção respondeu de fato, para o modo sombra.
// A DIVERGÊNCIA é calculada pelo serviço: pedir que o cliente a calcule
// convidaria a duas implementações discordarem.
func (c *Client) RecordOutcome(ctx context.Context, id, productionAnswer string, actionTaken bool) (*Receipt, error) {
	body := map[string]any{"production_answer": productionAnswer, "action_taken": actionTaken}
	var out Receipt
	if err := c.do(ctx, "POST", "/receipts/"+url.PathEscape(id)+"/outcome", body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ---------- calibração e avaliação ----------

// Calibration responde se as respostas de confiança alta são de fato mais
// confiáveis nesta carga — a pergunta que decide onde automatizar.
func (c *Client) Calibration(ctx context.Context, contractID string) (*Calibration, error) {
	var out Calibration
	if err := c.do(ctx, "GET", "/calibration/"+url.PathEscape(contractID), nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// RunEval roda o golden set de um contrato pelo caminho REAL de julgamento.
func (c *Client) RunEval(ctx context.Context, contractID string, cases []GoldenCase) (*Eval, error) {
	var out Eval
	if err := c.do(ctx, "POST", "/evals/"+url.PathEscape(contractID)+"/run", cases, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ---------- conta ----------

// Me devolve quem é a credencial.
func (c *Client) Me(ctx context.Context) (*AccountInfo, error) {
	var out AccountInfo
	if err := c.do(ctx, "GET", "/auth/me", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Quota devolve o consumo do dia contra o teto. PendingReconciliation é o que
// saiu e não teve `usage` de volta: NÃO é custo zero, é custo ainda não
// contabilizado.
func (c *Client) Quota(ctx context.Context) (*Quota, error) {
	var out Quota
	if err := c.do(ctx, "GET", "/quota", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Usage devolve a série diária. from/to vazios usam os últimos 30 dias.
func (c *Client) Usage(ctx context.Context, from, to string) (*UsageReport, error) {
	q := url.Values{}
	if from != "" {
		q.Set("from", from)
	}
	if to != "" {
		q.Set("to", to)
	}
	path := "/usage"
	if encoded := q.Encode(); encoded != "" {
		path += "?" + encoded
	}
	var out UsageReport
	if err := c.do(ctx, "GET", path, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Health consulta a saúde da borda. Não exige credencial, então funciona como
// sonda de infraestrutura — mas passa pelo cliente para herdar timeout e retry.
func (c *Client) Health(ctx context.Context) (map[string]any, error) {
	var out map[string]any
	if err := c.do(ctx, "GET", "/health", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}
