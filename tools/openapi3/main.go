// Command openapi3 converts the Swagger 2.0 document emitted by swag into an
// OpenAPI 3.0.3 document, and reports drift between a committed document and a
// freshly generated one.
//
// Usage:
//
//	go run ./openapi3 -in ../.tmp/swagger/swagger.json -out ../internal/api/docs/openapi.json
//	go run ./openapi3 -in ../.tmp/swagger/swagger.json -check -baseline ../internal/api/docs/openapi.json
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/getkin/kin-openapi/openapi2"
	"github.com/getkin/kin-openapi/openapi2conv"
	"github.com/getkin/kin-openapi/openapi3"
)

func main() {
	var (
		in       = flag.String("in", ".tmp/swagger/swagger.json", "path to the Swagger 2.0 document produced by swag")
		out      = flag.String("out", "internal/api/docs/openapi.json", "path to write the OpenAPI 3.0.3 document")
		check    = flag.Bool("check", false, "compare instead of write; exits non-zero when drift is found")
		baseline = flag.String("baseline", "", "committed document to compare against (defaults to -out)")
	)
	flag.Parse()

	raw, err := os.ReadFile(*in)
	if err != nil {
		fail("read input %s: %v", *in, err)
	}
	converted, err := convert(raw)
	if err != nil {
		fail("convert %s: %v", *in, err)
	}

	if !*check {
		if err := os.MkdirAll(dirOf(*out), 0o755); err != nil {
			fail("create output directory for %s: %v", *out, err)
		}
		if err := os.WriteFile(*out, converted, 0o644); err != nil {
			fail("write %s: %v", *out, err)
		}
		return
	}

	base := *baseline
	if base == "" {
		base = *out
	}
	committedRaw, err := os.ReadFile(base)
	if err != nil {
		fail("read baseline %s: %v", base, err)
	}
	committed, err := loadDoc(committedRaw)
	if err != nil {
		fail("parse baseline %s: %v", base, err)
	}
	fresh, err := loadDoc(converted)
	if err != nil {
		fail("parse generated document: %v", err)
	}

	diffs := diffDocs(committed, fresh)
	if len(diffs) == 0 {
		return
	}
	fmt.Fprintf(os.Stderr, "%s is out of date with the source annotations (%d difference(s)):\n", base, len(diffs))
	for _, d := range diffs {
		fmt.Fprintf(os.Stderr, "  %s\n", d)
	}
	fmt.Fprintln(os.Stderr, "\nRun `make docs` and commit the result together with the annotation change.")
	os.Exit(1)
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(2)
}

func dirOf(path string) string {
	if i := strings.LastIndex(path, "/"); i >= 0 {
		return path[:i]
	}
	return "."
}

// convert turns a Swagger 2.0 document into an OpenAPI 3.0.3 document.
func convert(raw []byte) ([]byte, error) {
	var doc2 openapi2.T
	if err := json.Unmarshal(raw, &doc2); err != nil {
		return nil, fmt.Errorf("unmarshal swagger 2.0: %w", err)
	}
	doc3, err := openapi2conv.ToV3(&doc2)
	if err != nil {
		return nil, fmt.Errorf("openapi2conv: %w", err)
	}
	// The project publishes one document for every environment and resolves the
	// host at runtime, so the base URL stays relative.
	doc3.OpenAPI = "3.0.3"
	doc3.Servers = openapi3.Servers{{URL: "/"}}
	if doc3.Info == nil {
		doc3.Info = &openapi3.Info{Title: "omino-sso", Version: "1.0"}
	}
	out, err := json.MarshalIndent(doc3, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal openapi 3.0.3: %w", err)
	}
	return append(out, '\n'), nil
}

func loadDoc(raw []byte) (*openapi3.T, error) {
	var doc openapi3.T
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, err
	}
	return &doc, nil
}

// diffDocs reports how got differs from want, one line per field-level change.
func diffDocs(want, got *openapi3.T) []string {
	var diffs []string
	add := func(format string, args ...any) { diffs = append(diffs, fmt.Sprintf(format, args...)) }

	wantOps := operations(want)
	gotOps := operations(got)
	for _, key := range sortedKeys(wantOps) {
		if _, ok := gotOps[key]; !ok {
			add("%s: operation removed", key)
		}
	}
	for _, key := range sortedKeys(gotOps) {
		if _, ok := wantOps[key]; !ok {
			add("%s: operation added", key)
		}
	}
	for _, key := range sortedKeys(gotOps) {
		wantOp, ok := wantOps[key]
		if !ok {
			continue
		}
		compareOperation(add, key, wantOp, gotOps[key])
	}
	compareSchemas(add, want, got)
	sort.Strings(diffs)
	return diffs
}

func operations(doc *openapi3.T) map[string]*openapi3.Operation {
	out := map[string]*openapi3.Operation{}
	if doc == nil || doc.Paths == nil {
		return out
	}
	for path, item := range doc.Paths.Map() {
		if item == nil {
			continue
		}
		for method, op := range item.Operations() {
			if op == nil {
				continue
			}
			out[strings.ToUpper(method)+" "+path] = op
		}
	}
	return out
}

func compareOperation(add func(string, ...any), key string, want, got *openapi3.Operation) {
	if want.Summary != got.Summary {
		add("%s: summary changed", key)
	}

	wantParams := parameters(want)
	gotParams := parameters(got)
	for _, name := range sortedKeys(wantParams) {
		gotVal, ok := gotParams[name]
		if !ok {
			add("%s: parameter %s removed", key, name)
			continue
		}
		if gotVal != wantParams[name] {
			add("%s: parameter %s %s -> %s", key, name, wantParams[name], gotVal)
		}
	}
	for _, name := range sortedKeys(gotParams) {
		if _, ok := wantParams[name]; !ok {
			add("%s: parameter %s added (%s)", key, name, gotParams[name])
		}
	}

	wantBody := requestBodyProps(want)
	gotBody := requestBodyProps(got)
	for _, name := range sortedKeys(wantBody) {
		gotVal, ok := gotBody[name]
		if !ok {
			add("%s: requestBody.%s removed", key, name)
			continue
		}
		if gotVal != wantBody[name] {
			add("%s: requestBody.%s %s -> %s", key, name, wantBody[name], gotVal)
		}
	}
	for _, name := range sortedKeys(gotBody) {
		if _, ok := wantBody[name]; !ok {
			add("%s: requestBody.%s added (%s)", key, name, gotBody[name])
		}
	}

	wantResp := responseCodes(want)
	gotResp := responseCodes(got)
	for _, code := range sortedKeys(wantResp) {
		if _, ok := gotResp[code]; !ok {
			add("%s: response %s removed", key, code)
		}
	}
	for _, code := range sortedKeys(gotResp) {
		if _, ok := wantResp[code]; !ok {
			add("%s: response %s added", key, code)
		}
	}
}

// parameters maps "in:name" to a stable descriptor of its constraints.
func parameters(op *openapi3.Operation) map[string]string {
	out := map[string]string{}
	for _, p := range op.Parameters {
		if p == nil || p.Value == nil {
			continue
		}
		v := p.Value
		desc := "type=" + typeOf(v.Schema)
		if v.Required {
			desc += ",required"
		}
		out[v.In+":"+v.Name] = desc
	}
	return out
}

// requestBodyProps flattens the JSON request body schema into "field" -> descriptor.
func requestBodyProps(op *openapi3.Operation) map[string]string {
	out := map[string]string{}
	if op.RequestBody == nil || op.RequestBody.Value == nil {
		return out
	}
	content, ok := op.RequestBody.Value.Content["application/json"]
	if !ok || content == nil || content.Schema == nil {
		return out
	}
	for name, desc := range schemaProps(content.Schema) {
		out[name] = desc
	}
	return out
}

func responseCodes(op *openapi3.Operation) map[string]string {
	out := map[string]string{}
	for code := range op.Responses.Map() {
		out[code] = code
	}
	return out
}

// schemaProps describes a schema one level deep. Refs collapse to the component
// name so a renamed-but-equivalent schema still shows up as a change.
func schemaProps(ref *openapi3.SchemaRef) map[string]string {
	out := map[string]string{}
	if ref == nil {
		return out
	}
	if ref.Ref != "" {
		out["$ref"] = refName(ref.Ref)
		return out
	}
	if ref.Value == nil {
		return out
	}
	required := map[string]bool{}
	for _, name := range ref.Value.Required {
		required[name] = true
	}
	for name, prop := range ref.Value.Properties {
		desc := "type=" + typeOf(prop)
		if required[name] {
			desc += ",required"
		}
		out[name] = desc
	}
	return out
}

func typeOf(ref *openapi3.SchemaRef) string {
	if ref == nil {
		return "unknown"
	}
	if ref.Ref != "" {
		return "ref:" + refName(ref.Ref)
	}
	if ref.Value == nil {
		return "unknown"
	}
	if ref.Value.Type.Is("array") && ref.Value.Items != nil {
		return "array<" + typeOf(ref.Value.Items) + ">"
	}
	return joinTypes(ref.Value.Type)
}

// joinTypes renders the (usually single-entry) type set of a schema.
func joinTypes(t *openapi3.Types) string {
	if t == nil || len(*t) == 0 {
		return "object"
	}
	return strings.Join(t.Slice(), "|")
}

func refName(ref string) string {
	if i := strings.LastIndex(ref, "/"); i >= 0 {
		return ref[i+1:]
	}
	return ref
}

func compareSchemas(add func(string, ...any), want, got *openapi3.T) {
	wantSchemas := componentSchemas(want)
	gotSchemas := componentSchemas(got)
	for _, name := range sortedKeys(wantSchemas) {
		gotProps, ok := gotSchemas[name]
		if !ok {
			add("components.schemas.%s: schema removed", name)
			continue
		}
		for _, field := range sortedKeys(wantSchemas[name]) {
			gotVal, ok := gotProps[field]
			if !ok {
				add("components.schemas.%s.%s: removed", name, field)
				continue
			}
			if gotVal != wantSchemas[name][field] {
				add("components.schemas.%s.%s %s -> %s", name, field, wantSchemas[name][field], gotVal)
			}
		}
		for _, field := range sortedKeys(gotProps) {
			if _, ok := wantSchemas[name][field]; !ok {
				add("components.schemas.%s.%s: added (%s)", name, field, gotProps[field])
			}
		}
	}
	for _, name := range sortedKeys(gotSchemas) {
		if _, ok := wantSchemas[name]; !ok {
			add("components.schemas.%s: schema added", name)
		}
	}
}

func componentSchemas(doc *openapi3.T) map[string]map[string]string {
	out := map[string]map[string]string{}
	if doc == nil || doc.Components == nil {
		return out
	}
	for name, ref := range doc.Components.Schemas {
		out[name] = schemaProps(ref)
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
