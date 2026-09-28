package jev

import (
	"fmt"
	"net"
	"net/url"
	"strings"
)

// Guarda de endpoint (SSRF).
//
// A regra, a mesma dos SDKs TypeScript e Python: antes de o SDK fazer a
// requisição, o endpoint tem de ser http/https e o host não pode ser local, de
// loopback nem de faixa privada ou reservada.
//
// Por que um SDK de cliente valida isto: ele roda dentro da vertical consumidora,
// no lado servidor, e manda o `state` do usuário para o endereço configurado. Um
// `JEVAAAS_URL` apontado para endereço interno — por `.env` herdado, engano de
// configuração ou intenção — passa a entregar conteúdo de usuário a um alvo que
// não é o JEVaaS. Validar na construção é o momento em que dá para recusar sem
// custo.
//
// Os três SDKs usam a MESMA régua de propósito: postura de segurança que difere
// por linguagem é uma armadilha para quem escreve a mesma integração duas vezes.
//
// O limite declarado: um NOME que resolve para endereço privado não é pego.
// Resolver DNS aqui seria uma checagem que quem ataca controla (basta o TTL mudar
// entre a validação e a conexão) e daria falsa sensação de segurança.

// cidrsNaoPublicos são as faixas que a stdlib não classifica e que ainda assim não
// podem ser alvo.
var cidrsNaoPublicos = func() []*net.IPNet {
	faixas := []string{
		"240.0.0.0/4",     // reservado
		"100.64.0.0/10",   // CGNAT
		"192.0.0.0/24",    // atribuições de protocolo IETF
		"192.0.2.0/24",    // TEST-NET-1
		"198.18.0.0/15",   // benchmark
		"198.51.100.0/24", // TEST-NET-2
		"203.0.113.0/24",  // TEST-NET-3
		"::/96",           // IPv4-compatível (obsoleta; a stdlib não a trata como IPv4)
		"64:ff9b::/96",    // NAT64
	}
	out := make([]*net.IPNet, 0, len(faixas))
	for _, f := range faixas {
		if _, n, err := net.ParseCIDR(f); err == nil {
			out = append(out, n)
		}
	}
	return out
}()

// hostNaoPublico diz se o host é local, de loopback ou de faixa privada/reservada.
func hostNaoPublico(host string) bool {
	h := strings.ToLower(strings.TrimSuffix(strings.Trim(strings.TrimSpace(host), "[]"), "."))
	if h == "" {
		return true
	}
	for _, sufixo := range []string{"localhost", ".localhost", ".local", ".internal"} {
		if h == sufixo || strings.HasSuffix(h, sufixo) {
			return true
		}
	}
	ip := net.ParseIP(h)
	if ip == nil {
		return false // é nome; ver o limite declarado no cabeçalho
	}
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified() ||
		ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsInterfaceLocalMulticast() || ip.IsMulticast() {
		return true
	}
	for _, cidr := range cidrsNaoPublicos {
		if cidr.Contains(ip) {
			return true
		}
	}
	return false
}

// validarBaseURL confere esquema e host antes da primeira requisição.
func validarBaseURL(raw string, permitirPrivado bool) error {
	bruto := strings.TrimSpace(raw)
	if bruto == "" {
		return fmt.Errorf("jev: BaseURL vazia")
	}
	u, err := url.Parse(bruto)
	if err != nil {
		return fmt.Errorf("jev: BaseURL ilegível: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("jev: BaseURL %q usa esquema %q — só http e https são aceitos", bruto, u.Scheme)
	}
	host := u.Hostname()
	if u.Host == "" || host == "" {
		return fmt.Errorf("jev: BaseURL %q não tem host", bruto)
	}
	if permitirPrivado {
		return nil
	}
	if hostNaoPublico(host) {
		return fmt.Errorf("jev: BaseURL %q aponta para host local, de loopback ou de faixa privada/reservada (%q) — recusado para não enviar conteúdo de usuário a um alvo interno. Para desenvolvimento local de propósito, use Options.AllowPrivateEndpoints (e nunca em produção)",
			bruto, host)
	}
	return nil
}

// PublicEndpoint diz se a URL passa na guarda, para o consumidor conferir a
// configuração antes de construir o cliente. Espelha `endpoint_publico` (Python)
// e `isPublicEndpoint` (TypeScript).
func PublicEndpoint(baseURL string) bool {
	return validarBaseURL(baseURL, false) == nil
}
