# jev-sdk — SDK Go do JEVaaS

Cliente Go do [JEVaaS](https://jevaas.com.br): decisão tipada, roteada e auditável. O modelo que
escreve continua escrevendo; o JEV responde às perguntas que não geram texto (choice, score, noul)
e devolve rota e recibo.

```bash
go get github.com/vertikon/jev-sdk@latest
```

```go
import jev "github.com/vertikon/jev-sdk"

c, err := jev.New(jev.Options{APIKey: os.Getenv("JEVAAAS_API_KEY")})
r, err := c.Fanout(ctx, jev.FanoutRequest{
	State: map[string]any{"mensagem": "Fui cobrado duas vezes e quero o estorno."},
	Questions: map[string]jev.Question{
		"fila": {Type: jev.KindChoice, Instructions: "Qual equipe deve receber esta mensagem?",
			Criteria: map[string]string{"financeiro": "Cobrança, estorno ou pagamento",
				"tecnico": "Falha de acesso", "indeterminado": "Evidência insuficiente"}},
	},
	RedactIdentity: true,
})
if err != nil {
	// falhe fechado: sem decisão, o caso vai para uma pessoa
}
```

Com contrato publicado, `c.Judge(ctx, jev.JudgeRequest{Contract: "ticket-router", State: …})`
devolve `Route` e `Enforced`: só aja com `auto` **e** `Enforced`.

- Chave `jev_sk_…`: https://dashboard.jevaas.com.br/chaves-api
- Host padrão: `https://jev.api.br/v1`. Host local ou privado exige `AllowPrivateEndpoints`
  (proteção contra SSRF).
- Documentação: https://jevaas.com.br/docs · Boas práticas:
  https://jevaas.com.br/docs/documentation/resources/boas-praticas

Este repositório espelha `packages/sdk-go` do repositório do JEVaaS; as versões seguem as do SDK
TypeScript `@jevaas/sdk`.

Licença MIT.
