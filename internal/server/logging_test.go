package server

import (
	"bytes"
	"log"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRequestLog(t *testing.T) {
	var buf bytes.Buffer
	old := log.Writer()
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(old) })

	api := newTestAPI(t)
	handler := logRequests(api.handler)

	do := func(method, path, body string, headers ...string) {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		for i := 0; i+1 < len(headers); i += 2 {
			req.Header.Set(headers[i], headers[i+1])
		}
		handler.ServeHTTP(httptest.NewRecorder(), req)
	}

	auth := []string{"Authorization", "Bearer " + testToken, "X-Cove-Source", "myapp"}
	do("POST", "/v0/secrets/app.key", `{"value":"super-secret-value"}`, auth...)
	do("GET", "/v0/secrets/app.key", "", auth...)
	do("GET", "/v0/secrets/missing", "", auth...)
	do("GET", "/v0/health", "")
	do("GET", "/v0/ready", "")

	out := buf.String()
	for _, want := range []string{
		"POST /v0/secrets/app.key 201",
		"GET /v0/secrets/app.key 200",
		"GET /v0/secrets/missing 404",
		"source=myapp",
		"from=192.0.2.1",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("log is missing %q:\n%s", want, out)
		}
	}
	for _, never := range []string{"super-secret-value", testToken, "/v0/health", "/v0/ready"} {
		if strings.Contains(out, never) {
			t.Errorf("log contains %q:\n%s", never, out)
		}
	}
}
