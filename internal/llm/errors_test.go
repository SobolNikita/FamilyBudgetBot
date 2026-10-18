package llm

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestSafeHTTPDiagnostics(t *testing.T) {
	for _, sample := range []struct {
		status int
		kind   string
	}{
		{400, "request"}, {401, "authentication"}, {403, "permission"}, {404, "model_or_endpoint"},
		{408, "timeout"}, {429, "rate_limit"}, {500, "unavailable"}, {503, "unavailable"}, {504, "timeout"},
	} {
		t.Run(sample.kind+fmt.Sprint(sample.status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				writer.WriteHeader(sample.status)
				fmt.Fprint(writer, `{"error":{"message":"secret-key private expenses"}}`)
			}))
			defer server.Close()
			_, err := New(server.URL, "secret-key", "test").Plan(context.Background(), Request{})
			if err == nil {
				t.Fatal("expected failure")
			}
			failure := Diagnose(err)
			if failure.Kind != sample.kind || failure.Status != sample.status {
				t.Fatalf("unexpected failure: %v", failure)
			}
			if strings.Contains(err.Error()+failure.UserMessage(), "secret-key") || strings.Contains(err.Error(), "private expenses") {
				t.Fatal("sensitive response exposed")
			}
			if !strings.Contains(failure.UserMessage(), "не изменены") {
				t.Fatal("outcome not explained")
			}
			for _, technical := range []string{"LLM_", "HTTP", "JSON", "ключ", "модел", "сертификат", "квот"} {
				if strings.Contains(failure.UserMessage(), technical) {
					t.Fatalf("technical diagnostic in user message: %s", failure.UserMessage())
				}
			}
		})
	}
}

func TestTransportDiagnostics(t *testing.T) {
	for _, sample := range []struct {
		err  error
		kind string
	}{
		{context.DeadlineExceeded, "timeout"}, {context.Canceled, "canceled"},
		{&net.DNSError{Err: "secret-host", Name: "secret-host", IsNotFound: true}, "dns"},
		{&tls.CertificateVerificationError{Err: x509.UnknownAuthorityError{}}, "tls"},
		{errors.New("secret connection details"), "connection"},
	} {
		failure := Diagnose(&url.Error{Op: "Post", URL: "https://secret-key@example.test", Err: sample.err})
		if failure.Kind != sample.kind || strings.Contains(failure.Error(), "secret") {
			t.Fatalf("unexpected diagnostic: %v", failure)
		}
	}
}

func TestTimeoutIncludesReadingResponseBody(t *testing.T) {
	for _, headersFirst := range []bool{false, true} {
		server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			_, _ = io.Copy(io.Discard, request.Body)
			if headersFirst {
				writer.WriteHeader(http.StatusOK)
				writer.(http.Flusher).Flush()
			}
			<-request.Context().Done()
		}))
		client := New(server.URL, "secret-key", "test")
		client.Timeout = 50 * time.Millisecond
		started := time.Now()
		_, err := client.Plan(context.Background(), Request{})
		server.Close()
		if err == nil || Diagnose(err).Kind != "timeout" || time.Since(started) > time.Second {
			t.Fatalf("deadline not applied: %v", err)
		}
	}
}
