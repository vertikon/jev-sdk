package jev

import (
	"encoding/json"
	"time"
)

// Route é o destino operacional de uma decisão. As quatro rotas têm consequências
// DIFERENTES — é isso que faz a confiança deixar de ser decoração.
type Route string

const (
	// RouteAuto: vencedor claro E confiança acima da barra da classe da ação.
	// É a única rota que autoriza agir.
	RouteAuto Route = "auto"
	// RouteCollectEvidence: faixa do meio. Busque evidência NOVA e rejulgue; não
	// aja. Repetir com o mesmo estado devolve o mesmo resultado e custa de novo.
	RouteCollectEvidence Route = "collect_evidence"
	// RouteHumanReview: confiança abaixo do piso, ou consequência irreversível.
	RouteHumanReview Route = "human_review"
	// RouteAbstain: a saída de escape venceu — nenhuma opção do menu serve. O
	// problema é o MENU, não a pergunta.
	RouteAbstain Route = "abstain"
)

// Status é o desfecho da chamada. Cada um pede uma ação diferente do consumidor.
type Status string

const (
	// StatusDecided: julgamento válido e roteado.
	StatusDecided Status = "decided"
	// StatusAbstained: o menu é o problema.
	StatusAbstained Status = "abstained"
	// StatusProviderError: a borda de IA falhou. NÃO é "o modelo disse não" — não
	// houve julgamento.
	StatusProviderError Status = "provider_error"
	// StatusInvalidContract: o contrato reprovou na validação.
	StatusInvalidContract Status = "invalid_contract"
	// StatusBudgetExceeded: o teto do inquilino estourou. NÃO é negativa semântica.
	StatusBudgetExceeded Status = "budget_exceeded"
	// StatusStale: o estado mudou desde o julgamento.
	StatusStale Status = "stale"
	// StatusPendingReconcile: timeout DEPOIS do envio. O consumo pode ter
	// ocorrido e ainda não está contabilizado.
	StatusPendingReconcile Status = "pending_reconciliation"
)

// Mode diz se o consumidor está autorizado a agir ou apenas observando.
type Mode string

const (
	ModeShadow  Mode = "shadow"
	ModeEnforce Mode = "enforce"
)

// ActionClass é a consequência da ação que a decisão alimenta. A barra de
// confiança pertence à CLASSE, não ao modelo.
type ActionClass string

const (
	ActionRead          ActionClass = "read"
	ActionInternalWrite ActionClass = "internal_write"
	ActionExternalWrite ActionClass = "external_write"
	ActionMoney         ActionClass = "money"
	ActionPermission    ActionClass = "permission"
	ActionIrreversible  ActionClass = "irreversible"
)

// QuestionKind é uma das três primitivas de julgamento.
type QuestionKind string

const (
	KindChoice QuestionKind = "choice"
	KindScore  QuestionKind = "score"
	KindNoul   QuestionKind = "noul"
)

// StateRole é a função de um campo do estado, na separação por função que o
// contrato declara.
type StateRole string

const (
	RoleGoal       StateRole = "goal"
	RoleFact       StateRole = "fact"
	RoleArtifact   StateRole = "artifact"
	RoleEvidence   StateRole = "evidence"
	RoleConstraint StateRole = "constraint"
	RoleOption     StateRole = "option"
)

// Question é uma pergunta tipada. Instructions carrega TODO o significado: o
// modelo nunca vê o id, então o id não instrui nada.
type Question struct {
	Type         QuestionKind `json:"type"`
	Instructions any          `json:"instructions"`
	// Criteria é objeto {chave: descrição} em choice, array de níveis em score, e
	// {true,false} opcional em noul.
	Criteria any `json:"criteria,omitempty"`
	// OptionsFrom aponta um caminho no state cuja lista VIVA vira o critério desta
	// chamada — o padrão "reconstrua o menu depois de cada ação".
	OptionsFrom string `json:"options_from,omitempty"`
	// Round agrupa perguntas independentes sobre o MESMO estado.
	Round int `json:"round,omitempty"`
}

// StateField declara um campo esperado no estado, por caminho pontuado.
type StateField struct {
	Path     string    `json:"path"`
	Role     StateRole `json:"role"`
	Required bool      `json:"required,omitempty"`
	Note     string    `json:"note,omitempty"`
}

// Condition é o predicado de uma regra. Campos declarados são combinados com E.
type Condition struct {
	Question          string   `json:"question,omitempty"`
	Equals            string   `json:"equals,omitempty"`
	ScoreAtLeast      *float64 `json:"score_at_least,omitempty"`
	NoulAtLeast       *float64 `json:"noul_at_least,omitempty"`
	ConfidenceAtLeast *float64 `json:"confidence_at_least,omitempty"`
	ConfidenceBelow   *float64 `json:"confidence_below,omitempty"`
	EscapeHatch       *bool    `json:"escape_hatch,omitempty"`
	// All: condições sobre perguntas diferentes, combinadas com E (um nível só).
	All []Condition `json:"all,omitempty"`
}

// RouteRule mapeia condição para rota. A ordem importa: a primeira que casar vence.
type RouteRule struct {
	When   Condition `json:"when"`
	Route  Route     `json:"route"`
	Allow  string    `json:"allow,omitempty"`
	Reason string    `json:"reason,omitempty"`
}

// Contract é o documento versionado. Na leitura vem com Version, Fingerprint e os
// carimbos de tempo preenchidos; na escrita (ContractInput) esses campos não vão.
type Contract struct {
	ID           string      `json:"id"`
	Version      int         `json:"version,omitempty"`
	Description  string      `json:"description,omitempty"`
	Owner        string      `json:"owner,omitempty"`
	Mode         Mode        `json:"mode,omitempty"`
	ActionClass  ActionClass `json:"action_class"`
	Bar          *float64    `json:"bar,omitempty"`
	CollectFloor *float64    `json:"collect_floor,omitempty"`
	AlwaysHuman  *bool       `json:"always_human,omitempty"`
	// AuditSampleRate: fração (0 a 1) das decisões auto sorteadas para auditoria.
	AuditSampleRate float64 `json:"audit_sample_rate,omitempty"`
	// Model é o modelo de julgamento do contrato (veja Models); vazio = padrão do serviço.
	Model           string              `json:"model,omitempty"`
	MaxStateChars   int                 `json:"max_state_chars,omitempty"`
	StateFields     []StateField        `json:"state_fields,omitempty"`
	Questions       map[string]Question `json:"questions"`
	PrimaryQuestion string              `json:"primary_question"`
	Routes          []RouteRule         `json:"routes,omitempty"`
	DefaultRoute    Route               `json:"default_route"`
	Escalations     map[string]string   `json:"escalations,omitempty"`
	Fingerprint     string              `json:"fingerprint,omitempty"`
	CreatedAt       *time.Time          `json:"created_at,omitempty"`
	UpdatedAt       *time.Time          `json:"updated_at,omitempty"`
}

// ContractInput é o que a criação e a atualização enviam: sem versão (o serviço
// atribui) e com `mode` opcional. Ausente, o serviço assume SOMBRA — automação
// nova começa observando.
type ContractInput struct {
	ID           string      `json:"id"`
	Description  string      `json:"description,omitempty"`
	Owner        string      `json:"owner,omitempty"`
	Mode         Mode        `json:"mode,omitempty"`
	ActionClass  ActionClass `json:"action_class"`
	Bar          *float64    `json:"bar,omitempty"`
	CollectFloor *float64    `json:"collect_floor,omitempty"`
	AlwaysHuman  *bool       `json:"always_human,omitempty"`
	// AuditSampleRate: fração (0 a 1) das decisões auto sorteadas para auditoria.
	AuditSampleRate float64 `json:"audit_sample_rate,omitempty"`
	// Model é o modelo de julgamento do contrato (veja Models); vazio = padrão do serviço.
	Model           string              `json:"model,omitempty"`
	MaxStateChars   int                 `json:"max_state_chars,omitempty"`
	StateFields     []StateField        `json:"state_fields,omitempty"`
	Questions       map[string]Question `json:"questions"`
	PrimaryQuestion string              `json:"primary_question"`
	Routes          []RouteRule         `json:"routes,omitempty"`
	DefaultRoute    Route               `json:"default_route"`
	Escalations     map[string]string   `json:"escalations,omitempty"`
}

// ContractCreated é a resposta da criação/atualização: o documento mais os
// avisos de validação (o que é suspeito mas não letal).
type ContractCreated struct {
	Contract
	Warnings []string `json:"warnings,omitempty"`
}

// Answer é uma resposta tipada. Os numéricos são ponteiros porque ZERO é resposta
// válida: `noul: 0` é um não convicto, e ausente é falha de contrato.
type Answer struct {
	QuestionID    string             `json:"question_id"`
	Type          QuestionKind       `json:"type"`
	Choice        string             `json:"choice,omitempty"`
	Score         *float64           `json:"score,omitempty"`
	Legend        map[string]string  `json:"legend,omitempty"`
	Noul          *float64           `json:"noul,omitempty"`
	Confidence    *float64           `json:"confidence,omitempty"`
	Probabilities map[string]float64 `json:"probabilities,omitempty"`
	EscapeHatch   bool               `json:"escape_hatch,omitempty"`
}

// Value devolve o valor como texto, para log e comparação com rótulo humano.
func (a Answer) Value() string {
	switch a.Type {
	case KindChoice:
		return a.Choice
	case KindScore:
		if a.Score == nil {
			return ""
		}
		return trimFloat(*a.Score)
	case KindNoul:
		if a.Noul == nil {
			return ""
		}
		return trimFloat(*a.Noul)
	}
	return ""
}

// Usage são os tokens contados pelo provedor.
type Usage struct {
	InputTokens  int64 `json:"input_tokens"`
	OutputTokens int64 `json:"output_tokens"`
}

// Threshold registra QUAL limiar foi aplicado e por quê. Sem ele não se audita a
// decisão depois.
type Threshold struct {
	ActionClass  ActionClass `json:"action_class"`
	Bar          float64     `json:"bar"`
	CollectFloor float64     `json:"collect_floor"`
	// MatchedRule é o índice da regra que casou; -1 significa a rota default.
	MatchedRule int    `json:"matched_rule"`
	Reason      string `json:"reason,omitempty"`
}

// ContractRef identifica a versão EXATA que julgou.
type ContractRef struct {
	ID          string      `json:"id"`
	Version     int         `json:"version"`
	Ref         string      `json:"ref"`
	Fingerprint string      `json:"fingerprint"`
	Mode        Mode        `json:"mode"`
	ActionClass ActionClass `json:"action_class"`
}

// JudgeRequest é o corpo de POST /decisions/judge.
type JudgeRequest struct {
	Contract string `json:"contract"`
	// ContractVersion fixa a versão; nil usa a vigente.
	ContractVersion *int   `json:"contract_version,omitempty"`
	State           any    `json:"state"`
	StateVersion    string `json:"state_version,omitempty"`
	Operation       string `json:"operation,omitempty"`
	// DecisionID é a chave idempotente: repetir com o MESMO estado devolve o
	// recibo anterior sem nova inferência e sem novo custo.
	DecisionID string `json:"decision_id,omitempty"`
}

// Response é a decisão roteada. Note que NÃO existe campo de execução: quem age é
// o consumidor, depois do motor de política dele. `Enforced` diz apenas se ele
// está autorizado.
type Response struct {
	ReceiptID string      `json:"receipt_id"`
	Contract  ContractRef `json:"contract"`
	Status    Status      `json:"status"`
	Route     Route       `json:"route"`
	// Allow é o token OPACO que o SEU motor de política mapeia para uma permissão
	// concreta. Vazio quando a rota não autoriza.
	Allow    string `json:"allow,omitempty"`
	Enforced bool   `json:"enforced"`
	// Audit: decisão auto sorteada para auditoria; a rota não muda.
	Audit    bool    `json:"audit,omitempty"`
	Selected *Answer `json:"selected,omitempty"`
	// DecisiveConfidence é a incerteza comparada à barra. Para noul é derivada da
	// distância a 0,5 — noul não tem `confidence` própria.
	DecisiveConfidence *float64   `json:"decisive_confidence,omitempty"`
	Answers            []Answer   `json:"answers"`
	Threshold          *Threshold `json:"threshold,omitempty"`
	Escalation         *string    `json:"escalation"`
	Warnings           []string   `json:"warnings"`
	InjectedOptions    []string   `json:"injected_options"`
	ImprovementHint    *string    `json:"improvement_hint"`
	Usage              Usage      `json:"usage"`
	CostMicrosUSD      int64      `json:"cost_micros_usd"`
	LatencyMS          int64      `json:"latency_ms"`
	StateHash          string     `json:"state_hash"`
	ModelRequested     string     `json:"model_requested,omitempty"`
	ModelReal          string     `json:"model_real,omitempty"`
	KeySource          string     `json:"key_source,omitempty"`
	RequestID          string     `json:"request_id"`
}

// IsActionable é a ÚNICA condição em que o consumidor pode agir: rota automática
// E modo enforce. Comparar só a rota é o erro que transforma um julgamento de
// observação em ação real.
func (r Response) IsActionable() bool {
	return r.Route == RouteAuto && r.Enforced
}

// Explain devolve uma frase curta para log e para fila humana.
func (r Response) Explain() string {
	switch {
	case r.IsActionable():
		return "AUTORIZADO: " + decisionSummary(r) + ", dentro do allow " + r.Allow
	case r.Route == RouteAuto:
		return "JULGADO, NÃO AUTORIZADO (modo sombra): " + decisionSummary(r)
	case r.Route == RouteAbstain:
		return "ABSTENÇÃO: nenhuma opção do menu serve — corrija o menu, não a pergunta"
	case r.Route == RouteCollectEvidence:
		return "EVIDÊNCIA INSUFICIENTE: " + deref(r.ImprovementHint)
	case r.Route == RouteHumanReview:
		return "REVISÃO HUMANA (" + string(r.Status) + "): " + deref(r.Escalation)
	}
	return "status " + string(r.Status) + ", rota " + string(r.Route)
}

// FanoutRequest julga perguntas soltas, sem contrato publicado.
type FanoutRequest struct {
	State        any                 `json:"state"`
	Questions    map[string]Question `json:"questions"`
	Model        string              `json:"model,omitempty"`
	StateVersion string              `json:"state_version,omitempty"`
	Operation    string              `json:"operation,omitempty"`
	DecisionID   string              `json:"decision_id,omitempty"`
	// RedactIdentity tira do estado as menções declaradas de raça, religião,
	// orientação sexual, gênero, deficiência e idade antes do julgamento.
	RedactIdentity bool `json:"redact_identity,omitempty"`
}

// FanoutResponse devolve SÓ as respostas. Sem contrato não há rota nem barra, e
// portanto não há autorização possível: a decisão de agir é inteiramente sua.
type FanoutResponse struct {
	Answers   []Answer `json:"answers"`
	Usage     Usage    `json:"usage"`
	LatencyMS int64    `json:"latency_ms"`
	RequestID string   `json:"request_id"`
	// Redaction diz quantas menções saíram e de que tipo (só com RedactIdentity).
	Redaction *Redaction `json:"redaction,omitempty"`
}

// Redaction é o relatório de redact_identity.
type Redaction struct {
	Mentions int      `json:"mentions"`
	Kinds    []string `json:"kinds"`
}

// Receipt é o registro de auditoria: existe MESMO quando a decisão falhou.
type Receipt struct {
	ReceiptID string `json:"receipt_id"`
	Status    Status `json:"status"`
	Route     Route  `json:"route"`
	Allow     string `json:"allow,omitempty"`

	TenantID            string      `json:"tenant_id"`
	ContractID          string      `json:"contract_id"`
	ContractVersion     int         `json:"contract_version"`
	ContractFingerprint string      `json:"contract_fingerprint"`
	ActionClass         ActionClass `json:"action_class"`
	Mode                Mode        `json:"mode"`
	Operation           string      `json:"operation,omitempty"`
	DecisionID          string      `json:"decision_id,omitempty"`
	TraceID             string      `json:"trace_id,omitempty"`

	Answers            []Answer   `json:"answers"`
	DecisiveQuestion   string     `json:"decisive_question,omitempty"`
	DecisiveValue      string     `json:"decisive_value,omitempty"`
	DecisiveConfidence *float64   `json:"decisive_confidence,omitempty"`
	Threshold          *Threshold `json:"threshold,omitempty"`
	Escalation         string     `json:"escalation,omitempty"`
	Warnings           []string   `json:"warnings,omitempty"`

	StateHash    string `json:"state_hash"`
	StateChars   int    `json:"state_chars"`
	StateVersion string `json:"state_version,omitempty"`

	ModelRequested string `json:"model_requested,omitempty"`
	ModelReal      string `json:"model_real,omitempty"`
	KeySource      string `json:"key_source,omitempty"`
	Usage          Usage  `json:"usage"`
	CostMicrosUSD  int64  `json:"cost_micros_usd"`
	LatencyMS      int64  `json:"latency_ms"`

	ProductionAnswer string     `json:"production_answer,omitempty"`
	HumanLabel       string     `json:"human_label,omitempty"`
	Audit            bool       `json:"audit,omitempty"`
	Diverged         bool       `json:"diverged,omitempty"`
	ActionTaken      bool       `json:"action_taken"`
	LabeledBy        string     `json:"labeled_by,omitempty"`
	LabeledAt        *time.Time `json:"labeled_at,omitempty"`

	Error     string    `json:"error,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

// ReceiptList é o envelope paginado. Cursor vazio significa última página.
type ReceiptList struct {
	Receipts []Receipt `json:"receipts"`
	Cursor   string    `json:"cursor,omitempty"`
}

// ReceiptFilter filtra a listagem.
type ReceiptFilter struct {
	Contract string
	Route    Route
	Status   Status
	From     time.Time
	To       time.Time
	Limit    int
	Cursor   string
}

// CalibrationBand é o acerto numa faixa de confiança.
type CalibrationBand struct {
	Band     string  `json:"band"`
	Total    int     `json:"total"`
	Labeled  int     `json:"labeled"`
	Correct  int     `json:"correct"`
	Accuracy float64 `json:"accuracy"`
}

// CalibrationDivergence é o quanto o modo sombra discorda da produção.
type CalibrationDivergence struct {
	ShadowTotal int     `json:"shadow_total"`
	Diverged    int     `json:"diverged"`
	Rate        float64 `json:"rate"`
}

// CalibrationVersion é o acerto por versão do contrato, para ver deriva.
type CalibrationVersion struct {
	Version  int     `json:"version"`
	Labeled  int     `json:"labeled"`
	Correct  int     `json:"correct"`
	Accuracy float64 `json:"accuracy"`
}

// Calibration responde à pergunta que decide onde automatizar: as respostas de
// confiança ALTA são de fato mais confiáveis nesta carga?
type Calibration struct {
	ContractID string `json:"contract_id"`
	Contract   string `json:"contract"`
	Version    int    `json:"version"`
	Mode       Mode   `json:"mode"`

	Total    int     `json:"total"`
	Labeled  int     `json:"labeled"`
	Correct  int     `json:"correct"`
	Accuracy float64 `json:"accuracy"`

	ByBand     []CalibrationBand     `json:"by_band"`
	Routes     map[string]int        `json:"routes"`
	Divergence CalibrationDivergence `json:"divergence"`

	HumanReviewRate  float64              `json:"human_review_rate"`
	PendingReconcile int                  `json:"pending_reconciliation"`
	Drift            []CalibrationVersion `json:"drift"`
}

// GoldenCase é um caso do golden set: estado + resposta esperada da pergunta
// decisiva. Dado autorado à mão, não produção capturada.
type GoldenCase struct {
	Name            string         `json:"name"`
	State           map[string]any `json:"state"`
	Expected        string         `json:"expected"`
	ContractVersion int            `json:"contract_version,omitempty"`
	Hard            bool           `json:"hard,omitempty"`
	Why             string         `json:"why,omitempty"`
}

// EvalFailure é um caso que não bateu.
type EvalFailure struct {
	Case       string  `json:"case"`
	Expected   string  `json:"expected"`
	Got        string  `json:"got"`
	Confidence float64 `json:"confidence"`
}

// Eval é o resultado de rodar o golden set pelo caminho real de julgamento.
type Eval struct {
	Contract string `json:"contract"`
	// Version e Fingerprint são os efetivamente julgados em todos os casos.
	Version     int               `json:"version"`
	Fingerprint string            `json:"fingerprint"`
	Total       int               `json:"total"`
	Correct     int               `json:"correct"`
	Accuracy    float64           `json:"accuracy"`
	ByBand      []CalibrationBand `json:"by_band"`
	Failures    []EvalFailure     `json:"failures"`
}

// AccountInfo é GET /auth/me.
type AccountInfo struct {
	TenantID  string    `json:"tenant_id"`
	KeyID     string    `json:"key_id"`
	KeyPrefix string    `json:"key_prefix"`
	Scopes    []string  `json:"scopes"`
	Plan      string    `json:"plan"`
	CreatedAt time.Time `json:"created_at"`
}

// Quota é o consumo do dia contra o teto.
type Quota struct {
	TenantID              string `json:"tenant_id"`
	Period                string `json:"period"`
	TokensUsed            int64  `json:"tokens_used"`
	TokensCap             int64  `json:"tokens_cap"`
	TokensRemaining       int64  `json:"tokens_remaining"`
	CostMicrosUSD         int64  `json:"cost_micros_usd"`
	RequestsToday         int64  `json:"requests_today"`
	RateLimitPerMin       int    `json:"rate_limit_per_min"`
	PendingReconciliation int64  `json:"pending_reconciliation"`
	PendingMicrosUSD      int64  `json:"pending_micros_usd"`
}

// UsageDay é um dia da série.
type UsageDay struct {
	Date             string           `json:"date"`
	Requests         int64            `json:"requests"`
	InputTokens      int64            `json:"input_tokens"`
	OutputTokens     int64            `json:"output_tokens"`
	CostMicrosUSD    int64            `json:"cost_micros_usd"`
	Routes           map[string]int64 `json:"routes"`
	PendingReconcile int64            `json:"pending_reconciliation"`
	BudgetRejected   int64            `json:"budget_rejected"`
}

// UsageReport é a série diária.
type UsageReport struct {
	TenantID string     `json:"tenant_id"`
	From     string     `json:"from"`
	To       string     `json:"to"`
	Days     []UsageDay `json:"days"`
}

// Raw é um JSON cru, para campos que o SDK não tipa (robustez a evolução do
// serviço sem quebrar o cliente).
type Raw = json.RawMessage

func decisionSummary(r Response) string {
	if r.Selected == nil {
		return "sem resposta decisiva"
	}
	conf := ""
	if r.DecisiveConfidence != nil {
		conf = " (confiança " + trimFloat(*r.DecisiveConfidence) + ")"
	}
	return string(r.Selected.Type) + "=" + r.Selected.Value() + conf
}

func deref(s *string) string {
	if s == nil {
		return "(sem detalhe)"
	}
	return *s
}

// trimFloat formata sem zeros à direita, para log legível.
func trimFloat(f float64) string {
	s := strconvFormat(f)
	return s
}
