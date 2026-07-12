package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	httpSwagger "github.com/swaggo/http-swagger"
)

// TestSwaggerAssetServing guards the Swagger UI: it mounts the handler exactly
// as routes.go does and asserts that the UI shell AND its static assets all
// serve 200. A regression here means the docs page renders blank (the exact
// symptom seen on an earlier deployed build), so this test locks it down.
func TestSwaggerAssetServing(t *testing.T) {
	r := chi.NewRouter()
	r.Get("/swagger/*", httpSwagger.Handler(httpSwagger.URL("/swagger/doc.json")))
	srv := httptest.NewServer(r)
	defer srv.Close()

	for _, path := range []string{
		"/swagger/index.html",
		"/swagger/swagger-ui.css",
		"/swagger/swagger-ui-bundle.js",
		"/swagger/swagger-ui-standalone-preset.js",
	} {
		resp, err := http.Get(srv.URL + path)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("GET %s -> %d, want 200 (swagger UI would render blank)", path, resp.StatusCode)
		}
	}
}
