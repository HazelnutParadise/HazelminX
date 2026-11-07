package api

import (
	"encoding/json"
	"image"
	"image/color"
	"reflect"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

// Test-local named types. The schemaTest prefix keeps them apart from the
// rest of package api.

// schemaTestName is a named string type that is never registered in enumValues.
type schemaTestName string

// schemaTestMode is a named string type; the enum tests register it.
type schemaTestMode string

// schemaTestIface is a named interface type; it must describe any JSON value.
type schemaTestIface interface{ schemaTestMethod() }

// schemaTestMap is a named map type with string keys and any values.
type schemaTestMap map[string]any

// SchemaTestBase is embedded without a tag in one struct and with a tag in another.
type SchemaTestBase struct {
	ID      int64     `json:"id"`
	Created time.Time `json:"created"`
}

type schemaTestPoint struct {
	X int `json:"x"`
	Y int `json:"y"`
}

type schemaTestSegment struct {
	From schemaTestPoint   `json:"from"`
	To   *schemaTestPoint  `json:"to"`
	Path []schemaTestPoint `json:"path"`
}

// schemaTestTags covers name fallback, omitempty, "-", unexported fields.
type schemaTestTags struct {
	Plain   string
	Named   string `json:"named"`
	Ptr     *int64 `json:"ptr,omitempty"`
	Blank   string `json:",omitempty"`
	Skipped string `json:"-"`
	hidden  string
}

// schemaTestOptional covers omitempty and omitzero on the required rule.
type schemaTestOptional struct {
	A int       `json:"a,omitempty"`
	B time.Time `json:"b,omitzero"`
	C int       `json:"c"`
}

type schemaTestAllOptional struct {
	A string `json:"a,omitempty"`
	B int    `json:"b,omitzero"`
}

// schemaTestOrder checks that "required" is sorted, not in field order.
type schemaTestOrder struct {
	Zed   int
	Alpha int
	Mid   int
}

// schemaTestEmpty has no exported, non-skipped fields.
type schemaTestEmpty struct {
	hidden int
	Skip   string `json:"-"`
}

type schemaTestEmbedInline struct {
	SchemaTestBase
	Title string `json:"title"`
}

type schemaTestEmbedTagged struct {
	SchemaTestBase `json:"base"`
	N              int `json:"n,omitempty"`
}

type schemaTestAnon struct {
	Meta struct {
		A int `json:"a"`
	} `json:"meta"`
}

// schemaTestKinds covers the scalar, raw, and collection kinds as fields.
// json.RawMessage must be {} even though its underlying type is []byte.
type schemaTestKinds struct {
	Stamp    time.Time       `json:"stamp"`
	Raw      json.RawMessage `json:"raw"`
	Blob     []byte          `json:"blob"`
	Anything any             `json:"anything"`
	Counts   map[string]int  `json:"counts"`
	Extra    schemaTestMap   `json:"extra"`
	Wide     uint64          `json:"wide"`
	Small    int32           `json:"small"`
	Ratio    float32         `json:"ratio"`
	Ptr      *string         `json:"ptr,omitempty"`
	Tags     []string        `json:"tags"`
	Arr      [2]bool         `json:"arr"`
	Iface    schemaTestIface `json:"iface"`
}

// node is recursive through a pointer and a slice.
type node struct {
	Name     string  `json:"name"`
	Children []*node `json:"children"`
}

// schemaTestEnumHolder uses a registered and an unregistered named string as fields.
type schemaTestEnumHolder struct {
	Mode schemaTestMode `json:"mode"`
	Name schemaTestName `json:"name"`
}

// schemaTestOfCase is one row of the table in TestSchemaOf.
type schemaTestOfCase struct {
	name      string
	typ       reflect.Type
	want      jsonSchema
	wantComps map[string]jsonSchema
}

// callSchemaOfForTest runs schemaOf and turns a panic into a test failure.
func callSchemaOfForTest(t *testing.T, typ reflect.Type, comps map[string]jsonSchema) (out jsonSchema) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("schemaOf(%v) panicked: %v", typ, r)
		}
	}()
	return schemaOf(typ, comps)
}

// canonicalSchemaJSON round-trips a value through JSON so that the comparison
// checks the document shape, not the Go container types (map[string]any vs
// map[string]jsonSchema, []string vs []any).
func canonicalSchemaJSON(t *testing.T, v any) any {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal %T: %v", v, err)
	}
	var out any
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal %s: %v", b, err)
	}
	return out
}

// assertSameSchema compares got and want with reflect.DeepEqual after
// canonicalizing both through JSON.
func assertSameSchema(t *testing.T, what string, got, want any) {
	t.Helper()
	g := canonicalSchemaJSON(t, got)
	w := canonicalSchemaJSON(t, want)
	if !reflect.DeepEqual(g, w) {
		gb, _ := json.Marshal(got)
		wb, _ := json.Marshal(want)
		t.Errorf("%s mismatch\n got: %s\nwant: %s", what, gb, wb)
	}
}

func TestSchemaOf(t *testing.T) {
	cases := []schemaTestOfCase{
		// Scalars.
		{name: "string", typ: reflect.TypeFor[string](), want: jsonSchema{"type": "string"}},
		{name: "bool", typ: reflect.TypeFor[bool](), want: jsonSchema{"type": "boolean"}},
		{name: "int", typ: reflect.TypeFor[int](), want: jsonSchema{"type": "integer"}},
		{name: "int8", typ: reflect.TypeFor[int8](), want: jsonSchema{"type": "integer"}},
		{name: "int16", typ: reflect.TypeFor[int16](), want: jsonSchema{"type": "integer"}},
		{name: "int32 adds format int32", typ: reflect.TypeFor[int32](), want: jsonSchema{"type": "integer", "format": "int32"}},
		{name: "int64 adds format int64", typ: reflect.TypeFor[int64](), want: jsonSchema{"type": "integer", "format": "int64"}},
		{name: "uint", typ: reflect.TypeFor[uint](), want: jsonSchema{"type": "integer"}},
		{name: "uint8", typ: reflect.TypeFor[uint8](), want: jsonSchema{"type": "integer"}},
		{name: "uint16", typ: reflect.TypeFor[uint16](), want: jsonSchema{"type": "integer"}},
		// The spec names format only for int32, int64 and uint64, so uint32 gets no format.
		{name: "uint32 has no format", typ: reflect.TypeFor[uint32](), want: jsonSchema{"type": "integer"}},
		{name: "uint64 adds format int64", typ: reflect.TypeFor[uint64](), want: jsonSchema{"type": "integer", "format": "int64"}},
		{name: "float32", typ: reflect.TypeFor[float32](), want: jsonSchema{"type": "number"}},
		{name: "float64", typ: reflect.TypeFor[float64](), want: jsonSchema{"type": "number"}},
		{name: "unregistered named string is plain string", typ: reflect.TypeFor[schemaTestName](), want: jsonSchema{"type": "string"}},

		// time.Time is a struct in Go but a string in JSON, and must not become a component.
		{name: "time.Time", typ: reflect.TypeFor[time.Time](), want: jsonSchema{"type": "string", "format": "date-time"}},

		// Raw and interface types describe any JSON value.
		{name: "json.RawMessage is any value, not base64", typ: reflect.TypeFor[json.RawMessage](), want: jsonSchema{}},
		{name: "any is any value", typ: reflect.TypeFor[any](), want: jsonSchema{}},
		{name: "named interface is any value", typ: reflect.TypeFor[schemaTestIface](), want: jsonSchema{}},
		{name: "[]byte is base64 string", typ: reflect.TypeFor[[]byte](), want: jsonSchema{"type": "string", "contentEncoding": "base64"}},

		// Pointers are transparent.
		{name: "*int is integer", typ: reflect.TypeFor[*int](), want: jsonSchema{"type": "integer"}},
		{name: "*string is string", typ: reflect.TypeFor[*string](), want: jsonSchema{"type": "string"}},

		// Slices and arrays.
		{name: "[]string", typ: reflect.TypeFor[[]string](), want: jsonSchema{
			"type": "array", "items": jsonSchema{"type": "string"},
		}},
		{name: "[3]int", typ: reflect.TypeFor[[3]int](), want: jsonSchema{
			"type": "array", "items": jsonSchema{"type": "integer"},
		}},
		{name: "[][]bool nests arrays", typ: reflect.TypeFor[[][]bool](), want: jsonSchema{
			"type": "array", "items": jsonSchema{"type": "array", "items": jsonSchema{"type": "boolean"}},
		}},

		// Maps.
		{name: "map[string]int", typ: reflect.TypeFor[map[string]int](), want: jsonSchema{
			"type": "object", "additionalProperties": jsonSchema{"type": "integer"},
		}},
		{name: "map[string][]string", typ: reflect.TypeFor[map[string][]string](), want: jsonSchema{
			"type": "object", "additionalProperties": jsonSchema{"type": "array", "items": jsonSchema{"type": "string"}},
		}},
		{name: "map[string]any is object without additionalProperties", typ: reflect.TypeFor[map[string]any](), want: jsonSchema{"type": "object"}},
		{name: "gin.H is object without additionalProperties", typ: reflect.TypeFor[gin.H](), want: jsonSchema{"type": "object"}},
		{name: "named map[string]any is object without additionalProperties", typ: reflect.TypeFor[schemaTestMap](), want: jsonSchema{"type": "object"}},

		// Recursive named struct, reached through a pointer and a slice.
		{name: "recursive node", typ: reflect.TypeFor[node](), want: jsonSchema{"$ref": "#/components/schemas/node"}, wantComps: map[string]jsonSchema{
			"node": {
				"type": "object",
				"properties": map[string]any{
					"name":     jsonSchema{"type": "string"},
					"children": jsonSchema{"type": "array", "items": jsonSchema{"$ref": "#/components/schemas/node"}},
				},
				"required": []string{"children", "name"},
			},
		}},
		{name: "pointer to recursive node is the same ref", typ: reflect.TypeFor[*node](), want: jsonSchema{"$ref": "#/components/schemas/node"}, wantComps: map[string]jsonSchema{
			"node": {
				"type": "object",
				"properties": map[string]any{
					"name":     jsonSchema{"type": "string"},
					"children": jsonSchema{"type": "array", "items": jsonSchema{"$ref": "#/components/schemas/node"}},
				},
				"required": []string{"children", "name"},
			},
		}},

		// Anonymous struct at the top level: inline, no component.
		{name: "anonymous struct is inline", typ: reflect.TypeFor[struct {
			X bool `json:"x"`
		}](), want: jsonSchema{
			"type":       "object",
			"properties": map[string]any{"x": jsonSchema{"type": "boolean"}},
			"required":   []string{"x"},
		}},

		// Named structs.
		{name: "name fallback, omitempty, - and unexported", typ: reflect.TypeFor[schemaTestTags](), want: jsonSchema{"$ref": "#/components/schemas/schemaTestTags"}, wantComps: map[string]jsonSchema{
			"schemaTestTags": {
				"type": "object",
				"properties": map[string]any{
					"Plain": jsonSchema{"type": "string"},
					"named": jsonSchema{"type": "string"},
					"ptr":   jsonSchema{"type": "integer", "format": "int64"},
					"Blank": jsonSchema{"type": "string"},
				},
				"required": []string{"Plain", "named"},
			},
		}},
		{name: "omitempty and omitzero are not required", typ: reflect.TypeFor[schemaTestOptional](), want: jsonSchema{"$ref": "#/components/schemas/schemaTestOptional"}, wantComps: map[string]jsonSchema{
			"schemaTestOptional": {
				"type": "object",
				"properties": map[string]any{
					"a": jsonSchema{"type": "integer"},
					"b": jsonSchema{"type": "string", "format": "date-time"},
					"c": jsonSchema{"type": "integer"},
				},
				"required": []string{"c"},
			},
		}},
		{name: "no required key when nothing is required", typ: reflect.TypeFor[schemaTestAllOptional](), want: jsonSchema{"$ref": "#/components/schemas/schemaTestAllOptional"}, wantComps: map[string]jsonSchema{
			"schemaTestAllOptional": {
				"type": "object",
				"properties": map[string]any{
					"a": jsonSchema{"type": "string"},
					"b": jsonSchema{"type": "integer"},
				},
			},
		}},
		{name: "required is sorted, not field order", typ: reflect.TypeFor[schemaTestOrder](), want: jsonSchema{"$ref": "#/components/schemas/schemaTestOrder"}, wantComps: map[string]jsonSchema{
			"schemaTestOrder": {
				"type": "object",
				"properties": map[string]any{
					"Zed":   jsonSchema{"type": "integer"},
					"Alpha": jsonSchema{"type": "integer"},
					"Mid":   jsonSchema{"type": "integer"},
				},
				"required": []string{"Alpha", "Mid", "Zed"},
			},
		}},
		// Assumes "properties" is always present, as the doc comment describes it.
		{name: "struct with no serialized fields has empty properties", typ: reflect.TypeFor[schemaTestEmpty](), want: jsonSchema{"$ref": "#/components/schemas/schemaTestEmpty"}, wantComps: map[string]jsonSchema{
			"schemaTestEmpty": {
				"type":       "object",
				"properties": map[string]any{},
			},
		}},
		{name: "embedded struct without tag is inlined", typ: reflect.TypeFor[schemaTestEmbedInline](), want: jsonSchema{"$ref": "#/components/schemas/schemaTestEmbedInline"}, wantComps: map[string]jsonSchema{
			"schemaTestEmbedInline": {
				"type": "object",
				"properties": map[string]any{
					"id":      jsonSchema{"type": "integer", "format": "int64"},
					"created": jsonSchema{"type": "string", "format": "date-time"},
					"title":   jsonSchema{"type": "string"},
				},
				"required": []string{"created", "id", "title"},
			},
		}},
		{name: "embedded struct with tag is a property", typ: reflect.TypeFor[schemaTestEmbedTagged](), want: jsonSchema{"$ref": "#/components/schemas/schemaTestEmbedTagged"}, wantComps: map[string]jsonSchema{
			"schemaTestEmbedTagged": {
				"type": "object",
				"properties": map[string]any{
					"base": jsonSchema{"$ref": "#/components/schemas/SchemaTestBase"},
					"n":    jsonSchema{"type": "integer"},
				},
				"required": []string{"base"},
			},
			"SchemaTestBase": {
				"type": "object",
				"properties": map[string]any{
					"id":      jsonSchema{"type": "integer", "format": "int64"},
					"created": jsonSchema{"type": "string", "format": "date-time"},
				},
				"required": []string{"created", "id"},
			},
		}},
		{name: "anonymous struct field is inline", typ: reflect.TypeFor[schemaTestAnon](), want: jsonSchema{"$ref": "#/components/schemas/schemaTestAnon"}, wantComps: map[string]jsonSchema{
			"schemaTestAnon": {
				"type": "object",
				"properties": map[string]any{
					"meta": jsonSchema{
						"type":       "object",
						"properties": map[string]any{"a": jsonSchema{"type": "integer"}},
						"required":   []string{"a"},
					},
				},
				"required": []string{"meta"},
			},
		}},
		{name: "every field kind", typ: reflect.TypeFor[schemaTestKinds](), want: jsonSchema{"$ref": "#/components/schemas/schemaTestKinds"}, wantComps: map[string]jsonSchema{
			"schemaTestKinds": {
				"type": "object",
				"properties": map[string]any{
					"stamp":    jsonSchema{"type": "string", "format": "date-time"},
					"raw":      jsonSchema{},
					"blob":     jsonSchema{"type": "string", "contentEncoding": "base64"},
					"anything": jsonSchema{},
					"counts":   jsonSchema{"type": "object", "additionalProperties": jsonSchema{"type": "integer"}},
					"extra":    jsonSchema{"type": "object"},
					"wide":     jsonSchema{"type": "integer", "format": "int64"},
					"small":    jsonSchema{"type": "integer", "format": "int32"},
					"ratio":    jsonSchema{"type": "number"},
					"ptr":      jsonSchema{"type": "string"},
					"tags":     jsonSchema{"type": "array", "items": jsonSchema{"type": "string"}},
					"arr":      jsonSchema{"type": "array", "items": jsonSchema{"type": "boolean"}},
					"iface":    jsonSchema{},
				},
				"required": []string{"anything", "arr", "blob", "counts", "extra", "iface", "ratio", "raw", "small", "stamp", "tags", "wide"},
			},
		}},
		// schemaTestPoint is used twice; it must be described once and both refs must resolve to it.
		{name: "same named struct used twice", typ: reflect.TypeFor[schemaTestSegment](), want: jsonSchema{"$ref": "#/components/schemas/schemaTestSegment"}, wantComps: map[string]jsonSchema{
			"schemaTestSegment": {
				"type": "object",
				"properties": map[string]any{
					"from": jsonSchema{"$ref": "#/components/schemas/schemaTestPoint"},
					"to":   jsonSchema{"$ref": "#/components/schemas/schemaTestPoint"},
					"path": jsonSchema{"type": "array", "items": jsonSchema{"$ref": "#/components/schemas/schemaTestPoint"}},
				},
				"required": []string{"from", "path", "to"},
			},
			"schemaTestPoint": {
				"type": "object",
				"properties": map[string]any{
					"x": jsonSchema{"type": "integer"},
					"y": jsonSchema{"type": "integer"},
				},
				"required": []string{"x", "y"},
			},
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			comps := map[string]jsonSchema{}
			got := callSchemaOfForTest(t, tc.typ, comps)
			assertSameSchema(t, "schema", got, tc.want)

			wantComps := tc.wantComps
			if wantComps == nil {
				wantComps = map[string]jsonSchema{}
			}
			assertSameSchema(t, "components", comps, wantComps)
		})
	}
}

// registerSchemaTestEnum adds an enumValues entry for the test and restores
// the previous contents of enumValues when the test ends.
func registerSchemaTestEnum(t *testing.T, typ reflect.Type, values []string) {
	t.Helper()
	saved := make(map[reflect.Type][]string, len(enumValues))
	for k, v := range enumValues {
		saved[k] = v
	}
	t.Cleanup(func() {
		for k := range enumValues {
			delete(enumValues, k)
		}
		for k, v := range saved {
			enumValues[k] = v
		}
	})
	enumValues[typ] = values
}

func TestSchemaOfEnumValues(t *testing.T) {
	registerSchemaTestEnum(t, reflect.TypeFor[schemaTestMode](), []string{"beta", "alpha"})

	t.Run("registered named string keeps registration order", func(t *testing.T) {
		comps := map[string]jsonSchema{}
		got := callSchemaOfForTest(t, reflect.TypeFor[schemaTestMode](), comps)
		assertSameSchema(t, "schema", got, jsonSchema{"type": "string", "enum": []string{"beta", "alpha"}})
		assertSameSchema(t, "components", comps, map[string]jsonSchema{})
	})

	t.Run("registered enum and unregistered string as struct fields", func(t *testing.T) {
		comps := map[string]jsonSchema{}
		got := callSchemaOfForTest(t, reflect.TypeFor[schemaTestEnumHolder](), comps)
		assertSameSchema(t, "schema", got, jsonSchema{"$ref": "#/components/schemas/schemaTestEnumHolder"})
		assertSameSchema(t, "components", comps, map[string]jsonSchema{
			"schemaTestEnumHolder": {
				"type": "object",
				"properties": map[string]any{
					"mode": jsonSchema{"type": "string", "enum": []string{"beta", "alpha"}},
					"name": jsonSchema{"type": "string"},
				},
				"required": []string{"mode", "name"},
			},
		})
	})
}

// TestSchemaOfSameNameDifferentPackage uses image.RGBA and color.RGBA, two
// standard-library struct types with the same Name() and only exported fields.
func TestSchemaOfSameNameDifferentPackage(t *testing.T) {
	t.Run("second package gets the qualified component name", func(t *testing.T) {
		comps := map[string]jsonSchema{}
		gotImage := callSchemaOfForTest(t, reflect.TypeFor[image.RGBA](), comps)
		gotColor := callSchemaOfForTest(t, reflect.TypeFor[color.RGBA](), comps)

		assertSameSchema(t, "image.RGBA ref", gotImage, jsonSchema{"$ref": "#/components/schemas/RGBA"})
		assertSameSchema(t, "image/color.RGBA ref", gotColor, jsonSchema{"$ref": "#/components/schemas/color.RGBA"})
		assertSameSchema(t, "components", comps, map[string]jsonSchema{
			"RGBA": {
				"type": "object",
				"properties": map[string]any{
					"Pix":    jsonSchema{"type": "string", "contentEncoding": "base64"},
					"Stride": jsonSchema{"type": "integer"},
					"Rect":   jsonSchema{"$ref": "#/components/schemas/Rectangle"},
				},
				"required": []string{"Pix", "Rect", "Stride"},
			},
			"Rectangle": {
				"type": "object",
				"properties": map[string]any{
					"Min": jsonSchema{"$ref": "#/components/schemas/Point"},
					"Max": jsonSchema{"$ref": "#/components/schemas/Point"},
				},
				"required": []string{"Max", "Min"},
			},
			"Point": {
				"type": "object",
				"properties": map[string]any{
					"X": jsonSchema{"type": "integer"},
					"Y": jsonSchema{"type": "integer"},
				},
				"required": []string{"X", "Y"},
			},
			"color.RGBA": {
				"type": "object",
				"properties": map[string]any{
					"R": jsonSchema{"type": "integer"},
					"G": jsonSchema{"type": "integer"},
					"B": jsonSchema{"type": "integer"},
					"A": jsonSchema{"type": "integer"},
				},
				"required": []string{"A", "B", "G", "R"},
			},
		})
	})

	t.Run("same type seen twice keeps the plain name", func(t *testing.T) {
		comps := map[string]jsonSchema{}
		typ := reflect.TypeFor[schemaTestPoint]()
		first := callSchemaOfForTest(t, typ, comps)
		second := callSchemaOfForTest(t, typ, comps)

		want := jsonSchema{"$ref": "#/components/schemas/schemaTestPoint"}
		assertSameSchema(t, "first ref", first, want)
		assertSameSchema(t, "second ref", second, want)
		assertSameSchema(t, "components", comps, map[string]jsonSchema{
			"schemaTestPoint": {
				"type": "object",
				"properties": map[string]any{
					"x": jsonSchema{"type": "integer"},
					"y": jsonSchema{"type": "integer"},
				},
				"required": []string{"x", "y"},
			},
		})
	})
}
