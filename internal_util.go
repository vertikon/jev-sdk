package jev

import (
	"context"
	"os"
	"strconv"
)

// getenv existe para que o pacote não espalhe os.Getenv por toda a fiação de
// configuração (e para poder ser substituído em teste, se um dia for preciso).
func getenv(key string) string { return os.Getenv(key) }

// strconvFormat formata um float sem zeros à direita, para log e para comparar com
// rótulo humano. `-1` de precisão é o menor número de dígitos que preserva o
// valor.
func strconvFormat(f float64) string { return strconv.FormatFloat(f, 'f', -1, 64) }

// traceContextKey é a chave privada do trace no contexto. É um tipo próprio para
// não colidir com a chave de outro pacote — duas strings iguais de pacotes
// diferentes são o mesmo slot.
type traceContextKey struct{}

// WithTraceID propaga um identificador de correlação para a borda, que o repassa
// ao evento NATS. Correlação entre vertical → JEVaaS → barramento só funciona se
// o consumidor puder semear o trace.
func WithTraceID(ctx context.Context, traceID string) context.Context {
	return context.WithValue(ctx, traceContextKey{}, traceID)
}

func traceFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if v, ok := ctx.Value(traceContextKey{}).(string); ok {
		return v
	}
	return ""
}
