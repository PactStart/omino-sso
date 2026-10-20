package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
)

const swagger2Fixture = `{
  "swagger": "2.0",
  "info": {"title": "fixture", "version": "1.0"},
  "basePath": "/api/v1",
  "paths": {
    "/users": {
      "post": {
        "summary": "创建用户",
        "parameters": [
          {"name": "body", "in": "body", "required": true, "schema": {"$ref": "#/definitions/CreateUserRequest"}}
        ],
        "responses": {
          "200": {"description": "ok", "schema": {"$ref": "#/definitions/User"}},
          "400": {"description": "validation failed"}
        }
      }
    }
  },
  "definitions": {
    "CreateUserRequest": {
      "type": "object",
      "required": ["username"],
      "properties": {
        "username": {"type": "string"},
        "email": {"type": "string"}
      }
    },
    "User": {
      "type": "object",
      "properties": {"id": {"type": "string"}}
    }
  }
}`

func TestConvertProducesOpenAPI303(t *testing.T) {
	out, err := convert([]byte(swagger2Fixture))
	if err != nil {
		t.Fatalf("convert: %v", err)
	}

	var doc map[string]any
	if err := json.Unmarshal(out, &doc); err != nil {
		t.Fatalf("unmarshal output: %v", err)
	}
	if got := doc["openapi"]; got != "3.0.3" {
		t.Fatalf("openapi version = %v, want 3.0.3", got)
	}
	servers, ok := doc["servers"].([]any)
	if !ok || len(servers) != 1 {
		t.Fatalf("servers = %v, want exactly one entry", doc["servers"])
	}
	if url := servers[0].(map[string]any)["url"]; url != "/" {
		t.Fatalf("servers[0].url = %v, want /", url)
	}
	if _, ok := doc["definitions"]; ok {
		t.Fatal("swagger 2.0 definitions must be converted to components.schemas")
	}
	components, ok := doc["components"].(map[string]any)
	if !ok {
		t.Fatalf("components missing from output")
	}
	schemas, ok := components["schemas"].(map[string]any)
	if !ok {
		t.Fatalf("components.schemas missing from output")
	}
	if _, ok := schemas["CreateUserRequest"]; !ok {
		t.Fatalf("CreateUserRequest schema missing: %v", schemas)
	}
}

func TestConvertIsDeterministic(t *testing.T) {
	first, err := convert([]byte(swagger2Fixture))
	if err != nil {
		t.Fatalf("convert #1: %v", err)
	}
	second, err := convert([]byte(swagger2Fixture))
	if err != nil {
		t.Fatalf("convert #2: %v", err)
	}
	if string(first) != string(second) {
		t.Fatal("conversion is not deterministic; drift checks would never converge")
	}
}

func TestDiffDocsDetectsNewRequiredField(t *testing.T) {
	base := mustDoc(t, swagger2Fixture)

	changed := strings.Replace(swagger2Fixture,
		`"required": ["username"],`,
		`"required": ["username", "email"],`, 1)
	if changed == swagger2Fixture {
		t.Fatal("fixture mutation failed")
	}
	fresh := mustDoc(t, changed)

	diffs := diffDocs(base, fresh)
	if len(diffs) == 0 {
		t.Fatal("expected drift, got none")
	}
	joined := strings.Join(diffs, "\n")
	if !strings.Contains(joined, "components.schemas.CreateUserRequest.email") {
		t.Fatalf("drift output must locate the changed field, got:\n%s", joined)
	}
	if !strings.Contains(joined, "required") {
		t.Fatalf("drift output must mention the changed aspect, got:\n%s", joined)
	}
}

func TestDiffDocsDetectsRemovedOperation(t *testing.T) {
	base := mustDoc(t, swagger2Fixture)

	var doc map[string]any
	if err := json.Unmarshal([]byte(swagger2Fixture), &doc); err != nil {
		t.Fatalf("unmarshal fixture: %v", err)
	}
	doc["paths"] = map[string]any{}
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal mutated fixture: %v", err)
	}
	fresh := mustDoc(t, string(raw))

	diffs := diffDocs(base, fresh)
	if len(diffs) == 0 {
		t.Fatal("expected drift after removing the operation, got none")
	}
	if !strings.Contains(strings.Join(diffs, "\n"), "POST /users: operation removed") {
		t.Fatalf("unexpected drift output: %v", diffs)
	}
}

func TestDiffDocsOnIdenticalDocs(t *testing.T) {
	base := mustDoc(t, swagger2Fixture)
	if diffs := diffDocs(base, base); len(diffs) != 0 {
		t.Fatalf("identical documents must not report drift, got: %v", diffs)
	}
}

func mustDoc(t *testing.T, swagger2 string) *openapi3.T {
	t.Helper()
	out, err := convert([]byte(swagger2))
	if err != nil {
		t.Fatalf("convert: %v", err)
	}
	doc, err := loadDoc(out)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	return doc
}
