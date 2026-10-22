package controllers

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"sort"
	"strings"

	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/envelope"
)

// invalidJSONError names the body field that a strict decode rejected. Path
// is the JSON path, such as config.coordinators.foo.
type invalidJSONError struct {
	Path   string
	Reason string
}

func (e *invalidJSONError) Error() string {
	return e.Path + ": " + e.Reason
}

// decodeJSONStrict rejects keys outside the target type, so misspelled fields
// surface as a 400. An unknown or mistyped field returns an *invalidJSONError.
func decodeJSONStrict(r *http.Request, out any) error {
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		return err
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(out); err != nil {
		return namedDecodeError(err, raw, reflect.TypeOf(out))
	}
	return nil
}

// namedDecodeError maps a decoder failure to the field it names. Syntax errors
// and failures without a nameable field keep the decoder's error.
func namedDecodeError(err error, raw []byte, target reflect.Type) error {
	var syntaxErr *json.SyntaxError
	if errors.As(err, &syntaxErr) {
		return err
	}
	var typeErr *json.UnmarshalTypeError
	if errors.As(err, &typeErr) {
		if typeErr.Field == "" {
			return err
		}
		return &invalidJSONError{
			Path:   typeErr.Field,
			Reason: fmt.Sprintf("expected %s, got %s", typeErr.Type, typeErr.Value),
		}
	}
	var generic any
	if json.Unmarshal(raw, &generic) != nil {
		return err
	}
	if path := unknownJSONField(target, generic, ""); path != "" {
		return &invalidJSONError{Path: path, Reason: "unknown field"}
	}
	return err
}

// unknownJSONField returns the path of the first undeclared key, or "". Keys
// are visited in sorted order so the reported path is stable.
func unknownJSONField(t reflect.Type, value any, path string) string {
	for t != nil && t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	switch v := value.(type) {
	case map[string]any:
		if t == nil {
			return ""
		}
		switch t.Kind() {
		case reflect.Struct:
			fields := jsonFieldTypes(t)
			for _, key := range sortedKeys(v) {
				child := joinJSONPath(path, key)
				ft, ok := fields[strings.ToLower(key)]
				if !ok {
					return child
				}
				if found := unknownJSONField(ft, v[key], child); found != "" {
					return found
				}
			}
		case reflect.Map:
			for _, key := range sortedKeys(v) {
				if found := unknownJSONField(t.Elem(), v[key], joinJSONPath(path, key)); found != "" {
					return found
				}
			}
		}
	case []any:
		if t == nil || (t.Kind() != reflect.Slice && t.Kind() != reflect.Array) {
			return ""
		}
		for i, item := range v {
			if found := unknownJSONField(t.Elem(), item, fmt.Sprintf("%s[%d]", path, i)); found != "" {
				return found
			}
		}
	}
	return ""
}

// jsonFieldTypes maps lower-cased JSON names to field types, as encoding/json
// matches keys case-insensitively. Unnamed embedded structs are flattened.
func jsonFieldTypes(t reflect.Type) map[string]reflect.Type {
	fields := map[string]reflect.Type{}
	for i := range t.NumField() {
		f := t.Field(i)
		tag := f.Tag.Get("json")
		if tag == "-" {
			continue
		}
		name := strings.Split(tag, ",")[0]
		embedded := f.Anonymous && name == ""
		if embedded {
			inner := f.Type
			for inner.Kind() == reflect.Pointer {
				inner = inner.Elem()
			}
			if inner.Kind() == reflect.Struct {
				for k, v := range jsonFieldTypes(inner) {
					fields[k] = v
				}
				continue
			}
		}
		if f.PkgPath != "" {
			continue
		}
		if name == "" {
			name = f.Name
		}
		fields[strings.ToLower(name)] = f.Type
	}
	return fields
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func joinJSONPath(path, key string) string {
	if path == "" {
		return key
	}
	return path + "." + key
}

// writeInvalidJSON answers a failed strict decode with INVALID_JSON. A named
// field goes in details; any other failure keeps the generic message.
func writeInvalidJSON(w http.ResponseWriter, r *http.Request, err error) {
	var fieldErr *invalidJSONError
	if errors.As(err, &fieldErr) {
		envelope.WriteAPIError(w, r, http.StatusBadRequest, "bad_request", "INVALID_JSON", "Invalid JSON body", map[string]any{
			"field":  fieldErr.Path,
			"reason": fieldErr.Reason,
		})
		return
	}
	envelope.WriteAPIError(w, r, http.StatusBadRequest, "bad_request", "INVALID_JSON", "Invalid JSON body", nil)
}
