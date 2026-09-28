package jev_test

import (
	"context"
	"fmt"
	"os"

	jev "github.com/vertikon/jev-sdk"
)

// Perguntas tipadas sem contrato. Mesmo trecho do README: se ele parar de compilar,
// o README está errado.
func Example_fanout() {
	c, err := jev.New(jev.Options{APIKey: os.Getenv("JEVAAAS_API_KEY")})
	if err != nil {
		fmt.Println(err)
		return
	}
	r, err := c.Fanout(context.Background(), jev.FanoutRequest{
		State: map[string]any{"mensagem": "Fui cobrado duas vezes e quero o estorno."},
		Questions: map[string]jev.Question{
			"fila": {Type: jev.KindChoice, Instructions: "Qual equipe deve receber esta mensagem?",
				Criteria: map[string]string{"financeiro": "Cobrança, estorno ou pagamento", "tecnico": "Falha de acesso", "indeterminado": "Evidência insuficiente"}},
		},
		RedactIdentity: true,
	})
	if err != nil {
		fmt.Println(err) // falhe fechado: sem decisão, o caso vai para uma pessoa
		return
	}
	fmt.Println(r.Answers[0].Choice)
}
