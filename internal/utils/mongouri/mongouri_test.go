package mongouri

import (
	"strings"
	"testing"
)

func uriOf(t *testing.T, o Options) string {
	t.Helper()
	uri, _, err := Build(o)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	return uri
}

func TestBuildWithoutAuth(t *testing.T) {
	got := uriOf(t, Options{Host: "127.0.0.1", Port: 27017})
	if got != "mongodb://127.0.0.1:27017" {
		t.Fatalf("uri = %q", got)
	}
}

func TestBuildDefaultsPort(t *testing.T) {
	got := uriOf(t, Options{Host: "mongo"})
	if !strings.HasSuffix(got, ":27017") {
		t.Fatalf("uri = %q, want default port 27017", got)
	}
}

func TestBuildWithAuth(t *testing.T) {
	got := uriOf(t, Options{Host: "mongo", Port: 27017, Username: "admin", Password: "p@ss word"})
	if !strings.Contains(got, "admin:p%40ss%20word@") {
		t.Fatalf("password not escaped as URL userinfo: %q", got)
	}
}

func TestBuildAuthSourceAndMechanism(t *testing.T) {
	got := uriOf(t, Options{
		Host: "mongo", Port: 27017, Username: "u", Password: "p",
		AuthSource: "admin", AuthMechanism: "SCRAM-SHA-256",
	})
	for _, want := range []string{"authSource=admin", "authMechanism=SCRAM-SHA-256"} {
		if !strings.Contains(got, want) {
			t.Fatalf("uri %q missing %q", got, want)
		}
	}
}

func TestBuildTLS(t *testing.T) {
	got := uriOf(t, Options{Host: "mongo", TLS: true, TLSCAFile: "/etc/ssl/ca.pem"})
	if !strings.Contains(got, "tls=true") || !strings.Contains(got, "tlsCAFile=%2Fetc%2Fssl%2Fca.pem") {
		t.Fatalf("tls params missing: %q", got)
	}
}

func TestBuildRejectsEmptyHost(t *testing.T) {
	if _, _, err := Build(Options{}); err == nil {
		t.Fatal("expected error for empty host")
	}
}

func TestDisplayURIHidesPassword(t *testing.T) {
	got := URI(Options{Host: "mongo", Username: "admin", Password: "secret"})
	if strings.Contains(got, "secret") {
		t.Fatalf("display URI leaks password: %q", got)
	}
	if !strings.Contains(got, "admin@") {
		t.Fatalf("display URI = %q", got)
	}
}
