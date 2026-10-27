package workerprotocol

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"io"
	"math/big"
	"net/url"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

//go:embed testdata/schema.json
var schemaJSON []byte
var definitions = loadDefinitions()

func JSONSchema() []byte { return bytes.Clone(schemaJSON) }

func loadDefinitions() map[string]any {
	var doc map[string]any
	if err := json.Unmarshal(schemaJSON, &doc); err != nil {
		panic("invalid embedded v1 schema")
	}
	return doc["$defs"].(map[string]any)
}

// Decode rejects missing, null, unknown, duplicate and wrongly-cased envelope
// fields before Go's permissive JSON struct decoding can discard evidence.
func Decode[T Message](data []byte) (T, error) {
	var v T
	if typ := reflect.TypeOf(v); typ != nil && typ.Kind() == reflect.Pointer {
		v = reflect.New(typ.Elem()).Interface().(T)
	}
	if err := ValidateWire(v.SchemaName(), data); err != nil {
		return v, err
	}
	if err := json.Unmarshal(data, &v); err != nil {
		return v, NewError("REQUEST_SCHEMA_INVALID")
	}
	if err := v.Validate(); err != nil {
		var zero T
		return zero, err
	}
	return v, nil
}

// ValidateWire validates a named $defs entry in the unmodified authoritative
// schema. This evaluator deliberately supports the vocabulary used by v1,
// not arbitrary caller-supplied schemas. Semantic cross-field checks live in
// the DTO validators.
func ValidateWire(definition string, data []byte) error {
	s, ok := definitions[definition]
	if !ok {
		return NewError("REQUEST_SCHEMA_INVALID")
	}
	v, err := parseJSON(data)
	if err != nil {
		return err
	}
	if !matchesSchema(s.(map[string]any), v) {
		return NewError("REQUEST_SCHEMA_INVALID")
	}
	return nil
}
func validateDTO(v interface{ SchemaName() string }) error {
	b, err := json.Marshal(v)
	if err != nil {
		return NewError("REQUEST_SCHEMA_INVALID")
	}
	return ValidateWire(v.SchemaName(), b)
}

func parseJSON(data []byte) (any, error) {
	if !utf8.Valid(data) {
		return nil, NewError("REQUEST_SCHEMA_INVALID")
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	v, err := readValue(d, 0)
	if err != nil {
		return nil, NewError("REQUEST_SCHEMA_INVALID")
	}
	if _, err = d.Token(); err != io.EOF {
		return nil, NewError("REQUEST_SCHEMA_INVALID")
	}
	return v, nil
}
func readValue(d *json.Decoder, depth int) (any, error) {
	if depth > 10000 {
		return nil, NewError("REQUEST_SCHEMA_INVALID")
	}
	t, err := d.Token()
	if err != nil {
		return nil, err
	}
	if delimiter, ok := t.(json.Delim); ok {
		switch delimiter {
		case '{':
			m := map[string]any{}
			for d.More() {
				k, err := d.Token()
				if err != nil {
					return nil, err
				}
				key, ok := k.(string)
				if !ok {
					return nil, NewError("REQUEST_SCHEMA_INVALID")
				}
				if _, ok = m[key]; ok {
					return nil, NewError("REQUEST_SCHEMA_INVALID")
				}
				v, err := readValue(d, depth+1)
				if err != nil {
					return nil, err
				}
				m[key] = v
			}
			_, err = d.Token()
			return m, err
		case '[':
			a := []any{}
			for d.More() {
				v, err := readValue(d, depth+1)
				if err != nil {
					return nil, err
				}
				a = append(a, v)
			}
			_, err = d.Token()
			return a, err
		default:
			return nil, NewError("REQUEST_SCHEMA_INVALID")
		}
	}
	return t, nil
}

func number(v any) (*big.Rat, bool) {
	switch x := v.(type) {
	case json.Number:
		// Bound arithmetic work before constructing a rational. These bounds
		// exceed every representable v1 DTO integer; arbitrary payload fields
		// with schema {} never enter numeric validation.
		text := string(x)
		if len(text) > 4096 {
			return nil, false
		}
		if i := strings.IndexAny(text, "eE"); i >= 0 {
			exponent, err := strconv.ParseInt(text[i+1:], 10, 32)
			if err != nil || exponent < -4096 || exponent > 4096 {
				return nil, false
			}
		}
		r, ok := new(big.Rat).SetString(string(x))
		return r, ok
	case float64:
		r := new(big.Rat)
		r.SetFloat64(x)
		return r, true
	}
	return nil, false
}
func equalJSON(a, b any) bool {
	if x, ok := number(a); ok {
		y, ok := number(b)
		return ok && x.Cmp(y) == 0
	}
	return reflect.DeepEqual(a, b)
}
func matchesSchema(s map[string]any, v any) bool {
	if ref, ok := s["$ref"].(string); ok {
		d, ok := definitions[strings.TrimPrefix(ref, "#/$defs/")]
		return ok && matchesSchema(d.(map[string]any), v)
	}
	if c, ok := s["const"]; ok && !equalJSON(c, v) {
		return false
	}
	if e, ok := s["enum"].([]any); ok {
		found := false
		for _, x := range e {
			found = found || equalJSON(x, v)
		}
		if !found {
			return false
		}
	}
	for _, choice := range []string{"oneOf", "anyOf"} {
		if a, ok := s[choice].([]any); ok {
			count := 0
			for _, x := range a {
				if matchesSchema(x.(map[string]any), v) {
					count++
				}
			}
			if count == 0 || (choice == "oneOf" && count != 1) {
				return false
			}
		}
	}
	if typ, ok := s["type"].(string); ok {
		valid := false
		switch typ {
		case "object":
			_, valid = v.(map[string]any)
		case "array":
			_, valid = v.([]any)
		case "string":
			_, valid = v.(string)
		case "boolean":
			_, valid = v.(bool)
		case "null":
			valid = v == nil
		case "integer":
			n, ok := number(v)
			valid = ok && n.IsInt()
		case "number":
			_, valid = number(v)
		}
		if !valid {
			return false
		}
	}
	if m, ok := v.(map[string]any); ok {
		if req, ok := s["required"].([]any); ok {
			for _, k := range req {
				if _, ok := m[k.(string)]; !ok {
					return false
				}
			}
		}
		p, _ := s["properties"].(map[string]any)
		for k, value := range m {
			if f, ok := p[k]; ok {
				if !matchesSchema(f.(map[string]any), value) {
					return false
				}
			} else if s["additionalProperties"] == false {
				return false
			}
		}
	}
	if a, ok := v.([]any); ok {
		if min, ok := s["minItems"].(float64); ok && len(a) < int(min) {
			return false
		}
		if max, ok := s["maxItems"].(float64); ok && len(a) > int(max) {
			return false
		}
		for i, x := range a {
			if item, ok := s["items"].(map[string]any); ok && !matchesSchema(item, x) {
				return false
			}
			if s["uniqueItems"] == true {
				for j := 0; j < i; j++ {
					if equalJSON(x, a[j]) {
						return false
					}
				}
			}
		}
	}
	if text, ok := v.(string); ok {
		n := utf8.RuneCountInString(text)
		if min, ok := s["minLength"].(float64); ok && n < int(min) {
			return false
		}
		if max, ok := s["maxLength"].(float64); ok && n > int(max) {
			return false
		}
		if pattern, ok := s["pattern"].(string); ok && !regexp.MustCompile(pattern).MatchString(text) {
			return false
		}
		switch s["format"] {
		case "date-time":
			if _, err := time.Parse(time.RFC3339Nano, text); err != nil {
				return false
			}
		case "uri":
			u, err := url.Parse(text)
			if err != nil || !u.IsAbs() {
				return false
			}
		}
	}
	if _, hasMin := s["minimum"]; hasMin || s["maximum"] != nil {
		n, ok := number(v)
		if !ok {
			return false
		}
		if min, ok := number(s["minimum"]); ok && n.Cmp(min) < 0 {
			return false
		}
		if max, ok := number(s["maximum"]); ok && n.Cmp(max) > 0 {
			return false
		}
	}
	return true
}
