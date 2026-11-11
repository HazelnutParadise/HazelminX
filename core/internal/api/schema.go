package api

import (
	"encoding/json"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// jsonSchema is a JSON Schema (OpenAPI 3.1 dialect) object.
type jsonSchema = map[string]any

// schemaOf derives the JSON Schema of a Go type as encoding/json would
// marshal it, so the published document follows the structs the handlers
// actually send. Named struct types become components and are referenced
// with {"$ref": "#/components/schemas/<Name>"}; comps collects them.
//
// Expected results:
//   - string → {"type":"string"}; bool → {"type":"boolean"};
//     int, int8..int64, uint..uint64 → {"type":"integer"} (int64/uint64 add
//     "format":"int64", int32 adds "format":"int32");
//     float32/float64 → {"type":"number"}.
//   - time.Time → {"type":"string","format":"date-time"}.
//   - json.RawMessage and interface types (any) → {} (any JSON value).
//   - []byte → {"type":"string","contentEncoding":"base64"}.
//   - pointer to T → schema of T (nullability is not modelled; omitempty
//     already drops nil).
//   - slice or array of T → {"type":"array","items": schema of T}.
//   - map[string]T → {"type":"object","additionalProperties": schema of T};
//     gin.H and map[string]any → {"type":"object"}.
//   - a named string type with a known value set registered in enumValues
//     (key: reflect.Type) → {"type":"string","enum":[...]} in registration
//     order; otherwise {"type":"string"}.
//   - struct (named) → {"$ref":"#/components/schemas/<TypeName>"} and
//     comps[TypeName] = {"type":"object","properties":{...},"required":[...]}
//     where:
//   - property names come from the json tag name, or the Go field name
//     when the tag has no name; fields tagged "-" and unexported fields
//     are skipped;
//   - a field is required unless its tag has omitempty or omitzero;
//   - embedded structs without a json tag have their fields inlined (the
//     encoding/json rule); with a tag they are a property;
//   - "required" is omitted when empty; properties keep field order is
//     not required (maps), but "required" is sorted.
//   - an anonymous struct → the object schema inline (no component).
//   - the same named struct seen twice (including recursion through a
//     pointer or slice) is described once; recursion must terminate.
//   - two different types with the same Name() in different packages:
//     the second gets the component name "<pkg>.<Name>" where pkg is the
//     last element of its package path.
func schemaOf(t reflect.Type, comps map[string]jsonSchema) jsonSchema {
	return (&reflector{comps: comps, owners: ownersOf(comps)}).schema(t)
}

var (
	rawMessageType = reflect.TypeFor[json.RawMessage]()
	timeType       = reflect.TypeFor[time.Time]()

	ownersMu sync.Mutex
	// owners remembers which Go type each component name stands for, per
	// components map, so a later call with the same map tells "the same
	// type again" from "another type with the same name".
	owners = map[uintptr]map[string]reflect.Type{}
)

func ownersOf(comps map[string]jsonSchema) map[string]reflect.Type {
	ownersMu.Lock()
	defer ownersMu.Unlock()
	k := reflect.ValueOf(comps).Pointer()
	if owners[k] == nil {
		owners[k] = map[string]reflect.Type{}
	}
	return owners[k]
}

type reflector struct {
	comps  map[string]jsonSchema
	owners map[string]reflect.Type
}

func (r *reflector) schema(t reflect.Type) jsonSchema {
	switch {
	case t == timeType:
		return jsonSchema{"type": "string", "format": "date-time"}
	case t == rawMessageType:
		return jsonSchema{}
	}
	switch t.Kind() {
	case reflect.Pointer:
		return r.schema(t.Elem())
	case reflect.Interface:
		return jsonSchema{}
	case reflect.Bool:
		return jsonSchema{"type": "boolean"}
	case reflect.Int32:
		return jsonSchema{"type": "integer", "format": "int32"}
	case reflect.Int64, reflect.Uint64:
		return jsonSchema{"type": "integer", "format": "int64"}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uintptr:
		return jsonSchema{"type": "integer"}
	case reflect.Float32, reflect.Float64:
		return jsonSchema{"type": "number"}
	case reflect.String:
		if vals, ok := enumValues[t]; ok {
			return jsonSchema{"type": "string", "enum": append([]string(nil), vals...)}
		}
		return jsonSchema{"type": "string"}
	case reflect.Slice:
		if t.Elem().Kind() == reflect.Uint8 {
			return jsonSchema{"type": "string", "contentEncoding": "base64"}
		}
		return jsonSchema{"type": "array", "items": r.schema(t.Elem())}
	case reflect.Array:
		return jsonSchema{"type": "array", "items": r.schema(t.Elem())}
	case reflect.Map:
		if t.Elem().Kind() == reflect.Interface {
			return jsonSchema{"type": "object"}
		}
		return jsonSchema{"type": "object", "additionalProperties": r.schema(t.Elem())}
	case reflect.Struct:
		if t.Name() == "" {
			return r.object(t)
		}
		name := r.componentName(t)
		ref := jsonSchema{"$ref": "#/components/schemas/" + name}
		if _, done := r.comps[name]; done {
			return ref
		}
		r.comps[name] = jsonSchema{} // placeholder ends recursion
		r.comps[name] = r.object(t)
		return ref
	}
	return jsonSchema{}
}

// componentName is the type's name, or "<pkg>.<Name>" when another type
// already owns the plain name. Generic instantiations drop package paths
// from their type arguments.
func (r *reflector) componentName(t reflect.Type) string {
	name := sanitizeName(t.Name())
	if owner, taken := r.owners[name]; !taken || owner == t {
		r.owners[name] = t
		return name
	}
	pkg := t.PkgPath()
	if i := strings.LastIndex(pkg, "/"); i >= 0 {
		pkg = pkg[i+1:]
	}
	q := sanitizeName(pkg + "." + t.Name())
	r.owners[q] = t
	return q
}

var typeArgPath = regexp.MustCompile(`[A-Za-z0-9_.\-]+/`)

func sanitizeName(n string) string {
	n = typeArgPath.ReplaceAllString(n, "")
	return strings.Map(func(c rune) rune {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '.', c == '_', c == '-':
			return c
		case c == '*' || c == ']':
			return -1 // ListResponse[*jobs.Job] → ListResponse_jobs.Job
		}
		return '_'
	}, n)
}

func (r *reflector) object(t reflect.Type) jsonSchema {
	props := map[string]any{}
	var required []string
	r.fields(t, props, &required)
	out := jsonSchema{"type": "object", "properties": props}
	if len(required) > 0 {
		sort.Strings(required)
		out["required"] = required
	}
	return out
}

func (r *reflector) fields(t reflect.Type, props map[string]any, required *[]string) {
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		tag, hasTag := f.Tag.Lookup("json")
		if tag == "-" {
			continue
		}
		name, opts, _ := strings.Cut(tag, ",")
		if f.Anonymous && name == "" {
			ft := f.Type
			if ft.Kind() == reflect.Pointer {
				ft = ft.Elem()
			}
			if ft.Kind() == reflect.Struct {
				r.fields(ft, props, required)
				continue
			}
		}
		if !f.IsExported() {
			continue
		}
		if name == "" || !hasTag {
			if name == "" {
				name = f.Name
			}
		}
		props[name] = r.schema(f.Type)
		optional := false
		for _, o := range strings.Split(opts, ",") {
			if o == "omitempty" || o == "omitzero" {
				optional = true
			}
		}
		if !optional {
			*required = append(*required, name)
		}
	}
}

// enumValues lists the allowed values of named string types, so OpenAPI
// can publish them as enums (job states, controllers, takeover states).
var enumValues = map[reflect.Type][]string{}
