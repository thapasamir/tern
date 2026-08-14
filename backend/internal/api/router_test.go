package api

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"gopkg.in/yaml.v3"

	"github.com/samirthapa/tern/internal/shared/config"
	"github.com/samirthapa/tern/internal/shared/testutil"
)

// newTestRouter builds the real router. Nothing is called on the database or Redis, so none are
// needed: the tests only look at which routes exist.
func newTestRouter() *chi.Mux {
	return NewRouter(Deps{
		Config:         config.Config{AdminToken: strings.Repeat("a", 32)},
		Log:            testutil.QuietLogger(),
		MetricsHandler: http.NotFoundHandler(), // so that /metrics is registered
	})
}

type document = map[string]any

func loadSpec(t *testing.T) document {
	t.Helper()
	var doc document
	if err := yaml.Unmarshal(openAPISpec, &doc); err != nil {
		t.Fatalf("openapi.yaml is not valid YAML: %v", err)
	}
	return doc
}

// operations lists every documented operation as "METHOD /path".
func operations(t *testing.T, doc document) map[string]document {
	t.Helper()
	paths, ok := doc["paths"].(document)
	if !ok || len(paths) == 0 {
		t.Fatal("the spec has no paths")
	}
	out := map[string]document{}
	for path, item := range paths {
		for key, op := range item.(document) {
			switch key {
			case "get", "post", "put", "patch", "delete":
				out[strings.ToUpper(key)+" "+path] = op.(document)
			}
		}
	}
	return out
}

// routes lists every route of the real router as "METHOD /path".
func routes(t *testing.T) map[string]bool {
	t.Helper()
	out := map[string]bool{}
	err := chi.Walk(newTestRouter(), func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		out[method+" "+route] = true
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func TestSpec_DocumentsExactlyTheRoutesTheAPIServes(t *testing.T) {
	documented := operations(t, loadSpec(t))
	served := routes(t)

	var undocumented, nonexistent []string
	for route := range served {
		if _, ok := documented[route]; !ok {
			undocumented = append(undocumented, route)
		}
	}
	for op := range documented {
		if !served[op] {
			nonexistent = append(nonexistent, op)
		}
	}
	sort.Strings(undocumented)
	sort.Strings(nonexistent)

	if len(undocumented) > 0 {
		t.Errorf("routes the API serves but openapi.yaml does not document (add them):\n  %s", strings.Join(undocumented, "\n  "))
	}
	if len(nonexistent) > 0 {
		t.Errorf("operations openapi.yaml documents but the API does not serve (remove or fix them):\n  %s", strings.Join(nonexistent, "\n  "))
	}
	if len(served) < 25 {
		t.Errorf("only %d routes found; the router walk is probably broken", len(served))
	}
}

func TestSpec_EveryOperationIsDescribedProperly(t *testing.T) {
	seen := map[string]string{}
	for name, op := range operations(t, loadSpec(t)) {
		id, _ := op["operationId"].(string)
		if id == "" {
			t.Errorf("%s has no operationId, which API client generators need", name)
		} else if other, dup := seen[id]; dup {
			t.Errorf("%s and %s share the operationId %q", name, other, id)
		}
		seen[id] = name

		if s, _ := op["summary"].(string); s == "" {
			t.Errorf("%s has no summary", name)
		}
		if tags, _ := op["tags"].([]any); len(tags) == 0 {
			t.Errorf("%s has no tag, so documentation tools would not group it", name)
		}
		if responses, _ := op["responses"].(document); len(responses) == 0 {
			t.Errorf("%s documents no responses", name)
		}
	}
}

var pathParam = regexp.MustCompile(`\{([^}]+)\}`)

func TestSpec_PathParametersAreDeclared(t *testing.T) {
	doc := loadSpec(t)
	for name, op := range operations(t, doc) {
		path := strings.SplitN(name, " ", 2)[1]

		want := map[string]bool{}
		for _, m := range pathParam.FindAllStringSubmatch(path, -1) {
			want[m[1]] = true
		}

		got := map[string]bool{}
		params, _ := op["parameters"].([]any)
		for _, p := range params {
			param := resolve(t, doc, p.(document))
			if param["in"] == "path" {
				got[param["name"].(string)] = true
				if param["required"] != true {
					t.Errorf("%s: path parameter %v must be required", name, param["name"])
				}
			}
		}
		if !equalSets(want, got) {
			t.Errorf("%s: the path has parameters %v but the operation declares %v", name, sortedKeys(want), sortedKeys(got))
		}
	}
}

func TestSpec_EveryReferenceResolves(t *testing.T) {
	doc := loadSpec(t)
	checked := 0
	var walk func(node any, where string)
	walk = func(node any, where string) {
		switch n := node.(type) {
		case document:
			if ref, ok := n["$ref"].(string); ok {
				checked++
				if _, found := lookup(doc, ref); !found {
					t.Errorf("%s: $ref %q points at nothing", where, ref)
				}
			}
			for k, v := range n {
				walk(v, where+"/"+k)
			}
		case []any:
			for i, v := range n {
				walk(v, where+"/"+string(rune('0'+i%10)))
			}
		}
	}
	walk(doc, "")
	if checked < 50 {
		t.Errorf("only %d references checked; the walk is probably broken", checked)
	}
}

func TestSpec_ProtectedRoutesDeclareTheirSecurity(t *testing.T) {
	doc := loadSpec(t)
	if _, ok := doc["security"].([]any); !ok {
		t.Fatal("the spec needs a default security requirement (the tenant API key)")
	}
	for name, op := range operations(t, doc) {
		path := strings.SplitN(name, " ", 2)[1]
		sec, explicit := op["security"].([]any)

		switch {
		case strings.HasPrefix(path, "/admin") || path == "/metrics":
			if !explicit || len(sec) == 0 || sec[0].(document)["AdminToken"] == nil {
				t.Errorf("%s needs the admin token, and the spec must say so", name)
			}
		case path == "/healthz" || path == "/readyz" || path == "/openapi.yaml":
			if !explicit || len(sec) != 0 {
				t.Errorf("%s is public, so it must opt out of the default security with an empty list", name)
			}
		case strings.HasPrefix(path, "/v1/"):
			if explicit {
				t.Errorf("%s should use the default API key security", name)
			}
		}
	}
}

func TestServesTheSpec(t *testing.T) {
	rec := httptest.NewRecorder()
	newTestRouter().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/openapi.yaml", nil))

	if rec.Code != http.StatusOK || !strings.HasPrefix(rec.Header().Get("Content-Type"), "application/yaml") {
		t.Fatalf("GET /openapi.yaml = %d %q", rec.Code, rec.Header().Get("Content-Type"))
	}
	if rec.Body.String() != string(openAPISpec) {
		t.Fatal("the served document differs from the embedded one")
	}
}

func TestMetricsRequireTheAdminToken(t *testing.T) {
	r := newTestRouter()
	for name, auth := range map[string]string{"no token": "", "a wrong token": "Bearer nope"} {
		req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
		if auth != "" {
			req.Header.Set("Authorization", auth)
		}
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s: /metrics = %d, want 401: metrics describe the whole platform", name, rec.Code)
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	req.Header.Set("Authorization", "Bearer "+strings.Repeat("a", 32))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code == http.StatusUnauthorized {
		t.Fatal("the admin token must open /metrics")
	}
}

func TestMetricsAreNotServedWithoutAHandler(t *testing.T) {
	r := NewRouter(Deps{Config: config.Config{AdminToken: strings.Repeat("a", 32)}, Log: testutil.QuietLogger()})
	if routes := collect(r); routes["GET /metrics"] {
		t.Fatal("/metrics must not exist when no metrics handler is configured")
	}
}

// ---- helpers ---------------------------------------------------------------------------------

func collect(r *chi.Mux) map[string]bool {
	out := map[string]bool{}
	_ = chi.Walk(r, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		out[method+" "+route] = true
		return nil
	})
	return out
}

// lookup follows a local reference such as "#/components/parameters/Limit".
func lookup(doc document, ref string) (any, bool) {
	if !strings.HasPrefix(ref, "#/") {
		return nil, false
	}
	var node any = doc
	for _, part := range strings.Split(strings.TrimPrefix(ref, "#/"), "/") {
		m, ok := node.(document)
		if !ok {
			return nil, false
		}
		if node, ok = m[part]; !ok {
			return nil, false
		}
	}
	return node, true
}

// resolve returns the object a parameter refers to, or the parameter itself if it is inline.
func resolve(t *testing.T, doc document, p document) document {
	t.Helper()
	ref, ok := p["$ref"].(string)
	if !ok {
		return p
	}
	node, found := lookup(doc, ref)
	if !found {
		t.Fatalf("unresolvable reference %q", ref)
	}
	return node.(document)
}

func equalSets(a, b map[string]bool) bool {
	if len(a) != len(b) {
		return false
	}
	for k := range a {
		if !b[k] {
			return false
		}
	}
	return true
}
