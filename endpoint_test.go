package jev

import (
	"strings"
	"testing"
)

// A guarda de endpoint roda na CONSTRUÇÃO do cliente: uma instância que existe é
// uma instância autorizada a falar com aquele host. Estes testes vivem no SDK Go
// além dos equivalentes em TypeScript e Python porque postura de segurança que
// difere por linguagem é uma armadilha para quem escreve a mesma integração duas
// vezes — os três conjuntos precisam cobrir os mesmos casos.
func TestPublicEndpoint_RecusaOsCasosQueImportam(t *testing.T) {
	proibidos := []string{
		// nomes locais
		"http://localhost:8094", "http://LOCALHOST:8094", "http://localhost.:8094",
		"http://gw.localhost", "http://servidor.local", "http://gw.internal",
		// IPv4 loopback, privado, link-local, sem endereço, multicast
		"http://127.0.0.1:8094", "http://127.1.2.3", "http://10.0.0.5",
		"http://172.16.0.1", "http://172.31.255.254", "http://192.168.1.1",
		"http://169.254.1.1", "http://0.0.0.0", "http://224.0.0.1", "http://255.255.255.255",
		// faixas que a stdlib não classifica
		"http://240.0.0.1", "http://100.64.0.1", "http://192.0.2.1",
		"http://198.51.100.5", "http://203.0.113.9", "http://198.18.0.1",
		// IPv6
		"http://[::1]:8094", "http://[::]", "http://[fc00::1]", "http://[fd12:3456::1]",
		"http://[fe80::1]", "http://[ff02::1]",
		// IPv6 com IPv4 embutido
		"http://[::ffff:127.0.0.1]", "http://[::ffff:10.0.0.1]", "http://[::7f00:1]",
		// esquema
		"file:///etc/passwd", "ftp://exemplo.com.br", "gopher://exemplo.com.br",
		// sem host / sem esquema / vazio
		"http:///decisoes", "api.jev.vertikon.com.br", "",
	}
	for _, u := range proibidos {
		if PublicEndpoint(u) {
			t.Errorf("%q deveria ser RECUSADA", u)
		}
	}
}

func TestPublicEndpoint_AceitaOsCasosLegitimos(t *testing.T) {
	permitidos := []string{
		// o endpoint de produção NUNCA pode ser recusado
		DefaultBaseURL,
		"https://api.jev.vertikon.com.br/",
		"https://api.jev.vertikon.com.br:443",
		// outros públicos
		"https://exemplo.com.br", "http://8.8.8.8", "http://1.1.1.1",
		"https://[2606:4700:4700::1111]", "http://[2001:4860:4860::8888]",
		// IPv4 público mapeado em IPv6: o embutido é público
		"http://[::ffff:8.8.8.8]",
	}
	for _, u := range permitidos {
		if !PublicEndpoint(u) {
			t.Errorf("%q NÃO deveria ser recusada", u)
		}
	}
}

// O cliente recusa a construção quando o endpoint é interno, e a mensagem diz
// COMO proceder se for intencional.
func TestNew_RecusaEndpointInternoComMensagemUtil(t *testing.T) {
	_, err := New(Options{APIKey: chaveTeste, BaseURL: "http://127.0.0.1:8094"})
	if err == nil {
		t.Fatal("endpoint de loopback deveria ser recusado")
	}
	msg := err.Error()
	for _, esperado := range []string{"127.0.0.1", "AllowPrivateEndpoints", "nunca em produção"} {
		if !strings.Contains(msg, esperado) {
			t.Fatalf("a mensagem deveria conter %q, veio: %s", esperado, msg)
		}
	}
}

// A permissão explícita libera o alvo privado — e SÓ o alvo: esquema errado
// continua recusado.
func TestNew_PermissaoDePrivadoNaoLiberaEsquema(t *testing.T) {
	if _, err := New(Options{
		APIKey: chaveTeste, BaseURL: "http://127.0.0.1:8094", AllowPrivateEndpoints: true,
	}); err != nil {
		t.Fatalf("com a permissão o endereço local é legítimo: %v", err)
	}
	for _, u := range []string{"file:///etc/passwd", "ftp://127.0.0.1/x"} {
		if _, err := New(Options{APIKey: chaveTeste, BaseURL: u, AllowPrivateEndpoints: true}); err == nil {
			t.Fatalf("%q deveria continuar recusada mesmo com permissão de privado", u)
		}
	}
}

// O default do SDK é o gateway público: quem não passa BaseURL não precisa de
// permissão nenhuma, e a vertical consumidora funciona sem configuração extra.
func TestNew_DefaultNaoPrecisaDePermissao(t *testing.T) {
	cli, err := New(Options{APIKey: chaveTeste})
	if err != nil {
		t.Fatalf("o default deveria passar sem permissão: %v", err)
	}
	if cli.baseURL != DefaultBaseURL {
		t.Fatalf("base URL default: %q", cli.baseURL)
	}
}
