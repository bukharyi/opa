// Copyright 2016 The OPA Authors.  All rights reserved.
// Use of this source code is governed by an Apache2
// license that can be found in the LICENSE file.

package topdown

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/open-policy-agent/opa/ast"
	"github.com/open-policy-agent/opa/storage"
	"github.com/open-policy-agent/opa/util"
	testutil "github.com/open-policy-agent/opa/util/test"
)

func TestEvalRef(t *testing.T) {

	var tests = []struct {
		ref      string
		expected interface{}
	}{
		{"data.c[i][j]", `[
		    {"i": 0, "j": "x"},
		    {"i": 0, "j": "y"},
		    {"i": 0, "j": "z"}
		 ]`},
		{"data.c[i][j][k]", `[
		    {"i": 0, "j": "x", "k": 0},
		    {"i": 0, "j": "x", "k": 1},
		    {"i": 0, "j": "x", "k": 2},
		    {"i": 0, "j": "y", "k": 0},
		    {"i": 0, "j": "y", "k": 1},
		    {"i": 0, "j": "z", "k": "p"},
		    {"i": 0, "j": "z", "k": "q"}
		]`},
		{"data.d[x][y]", `[
		    {"x": "e", "y": 0},
		    {"x": "e", "y": 1}
		]`},
		{`data.c[i]["x"][k]`, `[
		    {"i": 0, "k": 0},
		    {"i": 0, "k": 1},
		    {"i": 0, "k": 2}
		]`},
		{"data.c[i][j][i]", `[
		    {"i": 0, "j": "x"},
		    {"i": 0, "j": "y"}
		]`},
		{`data.c[i]["deadbeef"][k]`, nil},
		{`data.c[999]`, nil},
	}

	ctx := context.Background()
	compiler := ast.NewCompiler()
	store := storage.New(storage.InMemoryWithJSONConfig(loadSmallTestData()))
	txn := storage.NewTransactionOrDie(ctx, store)
	defer store.Close(ctx, txn)

	top := New(ctx, nil, compiler, store, txn)

	for _, tc := range tests {

		testutil.Subtest(t, tc.ref, func(t *testing.T) {

			switch e := tc.expected.(type) {
			case nil:
				var tmp *Topdown
				err := evalRef(top, ast.MustParseRef(tc.ref), ast.Ref{}, func(t *Topdown) error {
					tmp = t
					return nil
				})
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
					return
				}
				if tmp != nil {
					t.Errorf("Expected no bindings (nil) but got: %v", tmp)
				}
			case string:
				expected := loadExpectedBindings(e)
				err := evalRef(top, ast.MustParseRef(tc.ref), ast.Ref{}, func(t *Topdown) error {
					if len(expected) > 0 {
						for j, exp := range expected {
							if exp.Equal(t.Locals) {
								tmp := expected[:j]
								expected = append(tmp, expected[j+1:]...)
								return nil
							}
						}
					}
					// If there was not a matching expected binding, treat this case as a failure.
					return fmt.Errorf("unexpected bindings: %v", t.Locals)
				})
				if err != nil {
					t.Errorf("Expected success but got error: %v", err)
					return
				}
				if len(expected) > 0 {
					t.Errorf("Missing expected bindings: %v", expected)
				}
			}
		})
	}
}

func TestEvalTerms(t *testing.T) {

	tests := []struct {
		body     string
		expected string
	}{
		{"data.c[i][j][k] = x", `[
            {"i": 0, "j": "x", "k": 0},
            {"i": 0, "j": "x", "k": 1},
            {"i": 0, "j": "x", "k": 2},
            {"i": 0, "j": "y", "k": 0},
            {"i": 0, "j": "y", "k": 1},
            {"i": 0, "j": "z", "k": "p"},
            {"i": 0, "j": "z", "k": "q"}
        ]`},
		{"data.a[i] = data.h[j][k]", `[
		    {"i": 0, "j": 0, "k": 0},
		    {"i": 1, "j": 0, "k": 1},
		    {"i": 1, "j": 1, "k": 0},
		    {"i": 2, "j": 0, "k": 2},
		    {"i": 2, "j": 1, "k": 1},
		    {"i": 3, "j": 1, "k": 2}
		]`},
		{`data.d[x][y] = "baz"`, `[
		    {"x": "e", "y": 1}
		]`},
		{"data.d[x][y] = data.d[x][y]", `[
		    {"x": "e", "y": 0},
		    {"x": "e", "y": 1}
		]`},
		{"data.d[x][y] = data.z[i]", `[]`},
		{"data.a[data.a[i]] = 3", `[
			{"i": 0, "data.a[i]": 1},
			{"i": 1, "data.a[i]": 2},
			{"i": 2, "data.a[i]": 3}
		]`},
	}

	ctx := context.Background()
	compiler := ast.NewCompiler()
	store := storage.New(storage.InMemoryWithJSONConfig(loadSmallTestData()))

	txn := storage.NewTransactionOrDie(ctx, store)
	defer store.Close(ctx, txn)

	for _, tc := range tests {

		testutil.Subtest(t, tc.body, func(t *testing.T) {

			top := New(ctx, ast.MustParseBody(tc.body), compiler, store, txn)

			expected := loadExpectedBindings(tc.expected)

			err := evalTerms(top, func(t *Topdown) error {
				if len(expected) > 0 {
					for j, exp := range expected {
						if exp.Equal(t.Locals) {
							tmp := expected[:j]
							expected = append(tmp, expected[j+1:]...)
							return nil
						}
					}
				}
				// If there was not a matching expected binding, treat this case as a failure.
				return fmt.Errorf("unexpected bindings: %v", t.Locals)
			})

			if err != nil {
				t.Errorf("Expected success but got error: %v", err)
				return
			}

			if len(expected) > 0 {
				t.Errorf("Missing expected bindings: %v", expected)
			}

		})
	}
}

func TestPlugValue(t *testing.T) {

	a := ast.Var("a")
	b := ast.Var("b")
	c := ast.Var("c")
	k := ast.Var("k")
	v := ast.Var("v")
	cs := ast.MustParseTerm("[c]").Value
	ks := ast.MustParseTerm(`{k: "world"}`).Value
	vs := ast.MustParseTerm(`{"hello": v}`).Value
	hello := ast.String("hello")
	world := ast.String("world")

	t1 := New(nil, nil, nil, nil, nil)
	t1.Bind(a, b, nil)
	t1.Bind(b, cs, nil)
	t1.Bind(c, ks, nil)
	t1.Bind(k, hello, nil)

	t2 := New(nil, nil, nil, nil, nil)
	t2.Bind(a, b, nil)
	t2.Bind(b, cs, nil)
	t2.Bind(c, vs, nil)
	t2.Bind(v, world, nil)

	expected := ast.MustParseTerm(`[{"hello": "world"}]`).Value

	r1 := PlugValue(a, t1.Binding)

	if !expected.Equal(r1) {
		t.Errorf("Expected %v but got %v", expected, r1)
		return
	}

	r2 := PlugValue(a, t2.Binding)

	if !expected.Equal(r2) {
		t.Errorf("Expected %v but got %v", expected, r2)
	}

	n := ast.MustParseTerm("a.b[x.y[i]]").Value

	t3 := New(nil, nil, nil, nil, nil)
	t3.Bind(ast.Var("i"), ast.IntNumberTerm(1).Value, nil)
	t3.Bind(ast.MustParseTerm("x.y[i]").Value, ast.IntNumberTerm(1).Value, nil)

	expected = ast.MustParseTerm("a.b[1]").Value

	r3 := PlugValue(n, t3.Binding)

	if !expected.Equal(r3) {
		t.Errorf("Expected %v but got: %v", expected, r3)
	}
}

func TestTopDownCompleteDoc(t *testing.T) {
	tests := []struct {
		note     string
		rule     string
		expected interface{}
	}{
		{"undefined", "p = null :- false", ""}, // "" will be converted to Undefined
		{"null", "p = null :- true", "null"},
		{"bool: true", "p = true :- true", "true"},
		{"bool: false", "p = false :- true", "false"},
		{"number: 3", "p = 3 :- true", "3"},
		{"number: 3.0", "p = 3.0 :- true", "3.0"},
		{"number: 66.66667", "p = 66.66667 :- true", "66.66667"},
		{`string: "hello"`, `p = "hello" :- true`, `"hello"`},
		{`string: ""`, `p = "" :- true`, `""`},
		{"array: [1,2,3,4]", "p = [1,2,3,4] :- true", "[1,2,3,4]"},
		{"array: []", "p = [] :- true", "[]"},
		{`object/nested composites: {"a": [1], "b": [2], "c": [3]}`,
			`p = {"a": [1], "b": [2], "c": [3]} :- true`,
			`{"a": [1], "b": [2], "c": [3]}`},
		{"set/nested: {{1,2},{2,3}}", "p = {{1,2},{2,3}} :- true", "[[1,2], [2,3]]"},
		{"vars", `p = {"a": [x,y]} :- x = 1, y = 2`, `{"a": [1,2]}`},
		{"vars conflict", `p = {"a": [x,y]} :- xs = [1,2], ys = [1,2], x = xs[_], y = ys[_]`,
			fmt.Errorf("evaluation error (code: 1): multiple values for data.p: rules must produce exactly one value for complete documents: check rule definition(s): p")},
	}

	data := loadSmallTestData()

	for _, tc := range tests {
		runTopDownTestCase(t, data, tc.note, []string{tc.rule}, tc.expected)
	}
}

func TestTopDownPartialSetDoc(t *testing.T) {

	tests := []struct {
		note     string
		rule     string
		expected string
	}{
		{"array values", "p[x] :- a[i] = x", `[1, 2, 3, 4]`},
		{"array indices", "p[x] :- a[x] = _", `[0, 1, 2, 3]`},
		{"object keys", "p[x] :- b[x] = _", `["v1", "v2"]`},
		{"object values", "p[x] :- b[i] = x", `["hello", "goodbye"]`},
		{"nested composites", "p[x] :- f[i] = x", `[{"xs": [1.0], "ys": [2.0]}, {"xs": [2.0], "ys": [3.0]}]`},
		{"deep ref/heterogeneous", "p[x] :- c[i][j][k] = x", `[null, 3.14159, true, false, true, false, "foo"]`},
		{"composite var value", "p[x] :- x = [i, a[i]]", "[[0,1],[1,2],[2,3],[3,4]]"},
		{"composite key", `p[[x,{"y": y}]] :- x = 1, y = 2`, `[[1,{"y": 2}]]`},
	}

	data := loadSmallTestData()

	for _, tc := range tests {
		runTopDownTestCase(t, data, tc.note, []string{tc.rule}, tc.expected)
	}
}

func TestTopDownPartialObjectDoc(t *testing.T) {
	tests := []struct {
		note     string
		rule     string
		expected interface{}
	}{
		{"identity", "p[k] = v :- b[k] = v", `{"v1": "hello", "v2": "goodbye"}`},
		{"composites", "p[k] = v :- d[k] = v", `{"e": ["bar", "baz"]}`},
		// TODO(tsandall): this error should be handled earlier during
		// evaluation but that will require updating a bunch of tests that are
		// currently producing non-string keys.
		{"non-var/string key", "p[k] = v :- a[k] = v", fmt.Errorf("object key type json.Number")},
		{"body/join var", "p[k] = v :- a[i] = v, g[k][i] = v", `{"a": 1, "b": 2, "c": 4}`},
		{"composite value", `p[k] = [v1,{"v2":v2}] :- g[k] = x, x[v1] = v2, v2 != 0`, `{
			"a": [0, {"v2": 1}],
			"b": [1, {"v2": 2}],
			"c": [3, {"v2": 4}]
		}`},
		{"same key/value pair", `p[k] = 1 :- ks=["a", "b", "c", "a"], ks[_] = k`, `{"a":1,"b":1,"c":1}`},
	}

	data := loadSmallTestData()

	for _, tc := range tests {
		runTopDownTestCase(t, data, tc.note, []string{tc.rule}, tc.expected)
	}
}

func TestTopDownEvalTermExpr(t *testing.T) {

	tests := []struct {
		note     string
		rule     string
		expected string
	}{
		{"true", "p :- true", "true"},
		{"false", "p :- false", ""},
		{"number non-zero", "p :- -3.14", "true"},
		{"number zero", "p :- null", "true"},
		{"null", "p :- null", "true"},
		{"string non-empty", `p :- "abc"`, "true"},
		{"string empty", `p :- ""`, "true"},
		{"array non-empty", "p :- [1,2,3]", "true"},
		{"array empty", "p :- []", "true"},
		{"object non-empty", `p :- {"a": 1}`, "true"},
		{"object empty", `p :- {}`, "true"},
		{"set non-empty", `p :- {1,2,3}`, "true"},
		{"set empty", "p :- set()", "true"},
		{"ref", "p :- a[i]", "true"},
		{"ref undefined", "p :- data.deadbeef[i]", ""},
		{"ref false", "p :- data.c[0].x[1]", ""},
		{"array comprehension", "p :- [x | x = 1]", "true"},
		{"array comprehension empty", "p :- [x | x = 1, x = 2]", "true"},
		{"arbitrary position", "p :- a[i] = x, x, i", "true"},
	}

	data := loadSmallTestData()

	for _, tc := range tests {
		runTopDownTestCase(t, data, tc.note, []string{tc.rule}, tc.expected)
	}
}

func TestTopDownEqExpr(t *testing.T) {

	tests := []struct {
		note     string
		rule     string
		expected interface{}
	}{
		// undefined cases
		{"undefined: same type", `p = true :- true = false`, ""},
		{"undefined: diff type", `p = true :- 42 = "hello"`, ""},
		{"undefined: array order", `p = true :- [1,2,3] = [1,3,2]`, ""},
		{"undefined: ref value", "p = true :- a[3] = 9999", ""},
		{"undefined: ref values", "p = true :- a[i] = 9999", ""},
		{"undefined: ground var", "p = true :- a[3] = x, x = 3", ""},
		{"undefined: array var 1", "p = true :- [1,x,x] = [1,2,3]", ""},
		{"undefined: array var 2", "p = true :- [1,x,3] = [1,2,x]", ""},
		{"undefined: object var 1", `p = true :- {"a": 1, "b": 2} = {"a": a, "b": a}`, ""},
		{"undefined: array deep var 1", "p = true :- [[1,x],[3,x]] = [[1,2],[3,4]]", ""},
		{"undefined: array deep var 2", "p = true :- [[1,x],[3,4]] = [[1,2],[x,4]]", ""},
		{"undefined: array uneven", `p = true :- [true, false, "foo", "deadbeef"] = c[i][j]`, ""},
		{"undefined: object uneven", `p = true :- {"a": 1, "b": 2} = {"a": 1}`, ""},
		{"undefined: set", "p :- {1,2,3} = {1,2,4}", ""},

		// ground terms
		{"ground: bool", `p = true :- true = true`, "true"},
		{"ground: string", `p = true :- "string" = "string"`, "true"},
		{"ground: number", `p = true :- 17 = 17`, "true"},
		{"ground: null", `p = true :- null = null`, "true"},
		{"ground: array", `p = true :- [1,2,3] = [1,2,3]`, "true"},
		{"ground: set", "p = true :- {1,2,3} = {3,2,1}", "true"},
		{"ground: object", `p = true :- {"b": false, "a": [1,2,3]} = {"a": [1,2,3], "b": false}`, "true"},
		{"ground: ref 1", `p = true :- a[2] = 3`, "true"},
		{"ground: ref 2", `p = true :- b["v2"] = "goodbye"`, "true"},
		{"ground: ref 3", `p = true :- d["e"] = ["bar", "baz"]`, "true"},
		{"ground: ref 4", `p = true :- c[0].x[1] = c[0].z["q"]`, "true"},

		// variables
		{"var: x=y=z", "p[x] :- x = y, z = 42, y = z", "[42]"},
		{"var: ref value", "p = true :- a[3] = x, x = 4", "true"},
		{"var: ref values", "p = true :- a[i] = x, x = 2", "true"},
		{"var: ref key", "p = true :- a[i] = 4, x = 3", "true"},
		{"var: ref keys", "p = true :- a[i] = x, i = 2", "true"},
		{"var: ref ground var", "p[x] :- i = 2, a[i] = x", "[3]"},
		{"var: ref ref", "p[x] :- c[0].x[i] = c[0].z[j], x = [i, j]", `[[0, "p"], [1, "q"]]`},

		// arrays and variables
		{"pattern: array", "p[x] :- [1,x,3] = [1,2,3]", "[2]"},
		{"pattern: array 2", "p[x] :- [[1,x],[3,4]] = [[1,2],[3,4]]", "[2]"},
		{"pattern: array same var", "p[x] :- [2,x,3] = [x,2,3]", "[2]"},
		{"pattern: array multiple vars", "p[z] :- [1,x,y] = [1,2,3], z = [x, y]", "[[2, 3]]"},
		{"pattern: array multiple vars 2", "p[z] :- [1,x,3] = [y,2,3], z = [x, y]", "[[2, 1]]"},
		{"pattern: array ref", "p[x] :- [1,2,3,x] = [a[0], a[1], a[2], a[3]]", "[4]"},
		{"pattern: array non-ground ref", "p[x] :- [1,2,3,x] = [a[0], a[1], a[2], a[i]]", "[1,2,3,4]"},
		{"pattern: array = ref", "p[x] :- [true, false, x] = c[i][j]", `["foo"]`},
		{"pattern: array = ref (reversed)", "p[x] :-  c[i][j] = [true, false, x]", `["foo"]`},
		{"pattern: array = var", "p[y] :- [1,2,x] = y, x = 3", "[[1,2,3]]"},

		// objects and variables
		{"pattern: object val", `p[y] :- {"x": y} = {"x": "y"}`, `["y"]`},
		{"pattern: object same var", `p[x] :- {"x": x, "y": x} = {"x": 1, "y": 1}`, "[1]"},
		{"pattern: object multiple vars", `p[z] :- {"x": x, "y": y} = {"x": 1, "y": 2}, z = [x, y]`, "[[1, 2]]"},
		{"pattern: object multiple vars 2", `p[z] :- {"x": x, "y": 2} = {"x": 1, "y": y}, z = [x, y]`, "[[1, 2]]"},
		{"pattern: object ref", `p[x] :- {"p": c[0].x[0], "q": x} = c[i][j]`, `[false]`},
		{"pattern: object non-ground ref", `p[x] :- {"a": 1, "b": x} = {"a": 1, "b": c[0].x[i]}`, `[true, false, "foo"]`},
		{"pattern: object = ref", `p[x] :- {"p": y, "q": z} = c[i][j], x = [i, j, y, z]`, `[[0, "z", true, false]]`},
		{"pattern: object = ref (reversed)", `p[x] :- c[i][j] = {"p": y, "q": z}, x = [i, j, y, z]`, `[[0, "z", true, false]]`},
		{"pattern: object = var", `p[x] :- {"a": 1, "b": y} = x, y = 2`, `[{"a": 1, "b": 2}]`},
		{"pattern: object/array nested", `p[ys] :- f[i] = {"xs": [2.0], "ys": ys}`, `[[3.0]]`},
		{"pattern: object/array nested 2", `p[v] :- f[i] = {"xs": [x], "ys": [y]}, v = [x, y]`, `[[1.0, 2.0], [2.0, 3.0]]`},

		// indexing
		{"indexing: intersection", "p :- a[i] = g[i][j]", ""},
	}

	data := loadSmallTestData()

	for _, tc := range tests {
		runTopDownTestCase(t, data, tc.note, []string{tc.rule}, tc.expected)
	}
}

func TestTopDownIneqExpr(t *testing.T) {

	tests := []struct {
		note     string
		rule     string
		expected interface{}
	}{
		{"noteq", "p = true :- 0 != 1, a[i] = x, x != 2", "true"},
		{"gt", "p = true :- 1 > 0, a[i] = x, x > 2", "true"},
		{"gteq", "p = true :- 1 >= 1, a[i] = x, x >= 4", "true"},
		{"lt", "p = true :- -1 < 0, a[i] = x, x < 5", "true"},
		{"lteq", "p = true :- -1 <= 0, a[i] = x, x <= 1", "true"},
		{"undefined: noteq", "p = true :- 0 != 0", ""},
		{"undefined: gt", "p = true :- 1 > 2", ""},
		{"undefined: gteq", "p = true :- 1 >= 2", ""},
		{"undefined: lt", "p = true :- 1 < -1", ""},
		{"undefined: lteq", "p = true :- 1 < -1", ""},
	}

	data := loadSmallTestData()

	for _, tc := range tests {
		runTopDownTestCase(t, data, tc.note, []string{tc.rule}, tc.expected)
	}
}

func TestTopDownVirtualDocs(t *testing.T) {

	tests := []struct {
		note     string
		rules    []string
		expected interface{}
	}{
		// input to partial set and object docs
		{"input: set 1", []string{"p = true :- q[1]", "q[x] :- a[i] = x"}, "true"},
		{"input: set 2", []string{"p[x] :- q[1] = x", "q[x] :- a[i] = x"}, "[true]"},
		{"input: set embedded", []string{`p[x] :- x = {"b": [q[2]]}`, `q[x] :- a[i] = x`}, `[{"b": [true]}]`},
		{"input: set undefined", []string{"p = true :- q[1000]", "q[x] :- a[x] = y"}, ""},
		{"input: set dereference error", []string{"p :- x = [1], q[x][0]", "q[[x]] :- a[_] = x"}, fmt.Errorf("evaluation error (code: 2): 1:15: q is a set but data.q[x][0] attempts to dereference lookup result")},
		{"input: set ground var", []string{"p[x] :- x = 1, q[x]", "q[y] :- a = [1,2,3,4], a[y] = i"}, "[1]"},
		{"input: set ground composite (1)", []string{
			"p :- z = [[1,2], 2], q[z]",
			"q[[x,y]] :- x = [1,y], y = 2",
		}, "true"},
		{"input: set ground composite (2)", []string{
			"p :- y = 2, z = [[1,y], y], q[z]",
			"q[[x,y]] :- x = [1,y], y = 2",
		}, "true"},
		{"input: set ground composite (3)", []string{
			"p :- y = 2, x = [1,y], z = [x,y], q[z]",
			"q[[x,y]] :- x = [1,y], y = 2",
		}, "true"},
		{"input: set partially ground composite", []string{
			"p[u] :- y = 2, x = [1, u], z = [x,y], q[z]", // "u" is not ground here
			"q[[x,y]] :- x = [1,y], y = 2",
		}, "[2]"},
		{"input: object 1", []string{"p = true :- q[1] = 2", "q[i] = x :- a[i] = x"}, "true"},
		{"input: object 2", []string{"p = true :- q[1] = 0", "q[x] = i :- a[i] = x"}, "true"},
		{"input: object embedded 1", []string{"p[x] :- x = [1, q[3], q[2]]", "q[i] = x :- a[i] = x"}, "[[1,4,3]]"},
		{"input: object embedded 2", []string{`p[x] :- x = {"a": [q[3]], "b": [q[2]]}`, `q[i] = x :- a[i] = x`}, `[{"a": [4], "b": [3]}]`},
		{"input: object undefined val", []string{`p = true :- q[1] = 9999`, `q[i] = x :- a[i] = x`}, ""},
		{"input: object undefined key 1", []string{`p = true :- q[9999] = 2`, `q[i] = x :- a[i] = x`}, ""},
		{"input: object undefined key 2", []string{`p = true :- q["foo"] = 2`, `q[i] = x :- a[i] = x`}, ""},
		{"input: object dereference ground", []string{`p = true :- q[0]["x"][1] = false`, `q[i] = x :- x = c[i]`}, "true"},
		{"input: object dereference ground 2", []string{`p[v] :- x = "a", q[x][y] = v`, `q[k] = v :- k = "a", v = data.a`}, "[1,2,3,4]"},
		{"input: object defererence non-ground", []string{`p = true :- q[0][x][y] = false`, `q[i] = x :- x = c[i]`}, "true"},
		{"input: object ground var key", []string{`p[y] :- x = "b", q[x] = y`, `q[k] = v :- x = {"a": 1, "b": 2}, x[k] = v`}, "[2]"},
		{"input: variable binding substitution", []string{
			"p[x] = y :- r[z] = y, q[x] = z",
			`r[k] = v :- x = {"a": 1, "b": 2, "c": 3, "d": 4}, x[k] = v`,
			`q[y] = x :- z = {"a": "a", "b": "b", "d": "d"}, z[y] = x`},
			`{"a": 1, "b": 2, "d": 4}`},

		// output from partial set and object docs
		{"output: set", []string{"p[x] :- q[x]", "q[y] :- a[i] = y"}, "[1,2,3,4]"},
		{"output: set embedded", []string{`p[i] :- {i: [true]} = {i: [q[i]]}`, `q[x] :- d.e[i] = x`}, `["bar", "baz"]`},
		{"output: set var binding", []string{"p[x] :- q[x]", "q[y] :- y = [i, j], i = 1, j = 2"}, `[[1,2]]`},
		{"output: set dereference error", []string{"p :- q[x][0]", "q[[x]] :- a[_] = x"}, fmt.Errorf("evaluation error (code: 2): 1:6: q is a set but data.q[x][0] attempts to dereference lookup result")},
		{"output: object key", []string{"p[x] :- q[x] = 4", "q[i] = x :- a[i] = x"}, "[3]"},
		{"output: object value", []string{"p[x] = y :- q[x] = y", "q[k] = v :- b[k] = v"}, `{"v1": "hello", "v2": "goodbye"}`},
		{"output: object embedded", []string{"p[k] = v :- {k: [q[k]]} = {k: [v]}", `q[x] = y :- b[x] = y`}, `{"v1": "hello", "v2": "goodbye"}`},
		{"output: object dereference ground", []string{`p[i] :- q[i]["x"][1] = false`, `q[i] = x :- x = c[i]`}, "[0]"},
		{"output: object defererence non-ground", []string{
			`p[r] :- q[x][y][z] = false, r = [x, y, z]`,
			`q[i] = x :- x = c[i]`},
			`[[0, "x", 1], [0, "z", "q"]]`},
		{"output: object dereference array of refs", []string{
			"p[x] :- q[_][0].c[_] = x",
			"q[k] = v :- d.e[_] = k, v = [ r | r = l[_] ]",
		}, "[1,2,3,4]"},
		{"output: object dereference array of refs within object", []string{
			"p[x] :- q[_].x[0].c[_] = x",
			`q[k] = v :- d.e[_] = k, v = {"x": [r | r = l[_]]}`,
		}, "[1,2,3,4]"},
		{"output: object dereference object with key refs", []string{
			"p :- q.bar[1].alice[0] = 1",
			"q[k] = v :- d.e[_] = k, v = [x | x = {l[_].a: [1]}]",
		}, "true"},
		{"output: object var binding", []string{
			"p[z] :- q[x] = y, z = [x, y]",
			`q[k] = v :- v = [x, y], x = "a", y = "b", k = "foo"`},
			`[["foo", ["a", "b"]]]`},
		{"output: object key var binding", []string{
			"p[z] :- q[x] = y, z = [x, y]",
			`q[k] = v :- k = y, y = x, x = "a", v = "foo"`},
			`[["a", "foo"]]`},

		// input+output from partial set/object docs
		{"i/o: objects", []string{
			"p[x] :- q[x] = r[x]",
			`q[x] = y :- z = {"a": 1, "b": 2, "d": 4}, z[x] = y`,
			`r[k] = v :- x = {"a": 1, "b": 2, "c": 4, "d": 3}, x[k] = v`},
			`["a", "b"]`},

		{"i/o: undefined keys", []string{
			"p[y] :- q[x], r[x] = y",
			`q[x] :- z = ["a", "b", "c", "d"], z[y] = x`,
			`r[k] = v :- x = {"a": 1, "b": 2, "d": 4}, x[k] = v`},
			`[1, 2, 4]`},

		// input/output to/from complete docs
		{"input: complete array", []string{"p = true :- q[1] = 2", "q = [1,2,3,4] :- true"}, "true"},
		{"input: complete object", []string{`p = true :- q["b"] = 2`, `q = {"a": 1, "b": 2} :- true`}, "true"},
		{"input: complete set", []string{"p :- q[3]", "q = {1,2,3,4} :- true"}, "true"},
		{"input: complete array dereference ground", []string{"p = true :- q[1][1] = 3", "q = [[0,1], [2,3]] :- true"}, "true"},
		{"input: complete object dereference ground", []string{`p = true :- q["b"][1] = 4`, `q = {"a": [1, 2], "b": [3, 4]} :- true`}, "true"},
		{"input: complete array ground index", []string{"p[x] :- z = [1, 2], z[i] = y, q[y] = x", "q = [1,2,3,4] :- true"}, "[2,3]"},
		{"input: complete object ground key", []string{`p[x] :- z = ["b", "c"], z[i] = y, q[y] = x`, `q = {"a":1,"b":2,"c":3,"d":4} :- true`}, "[2,3]"},
		{"input: complete vars", []string{
			`p :- q[1][1] = 2`,
			`q = [{"x": x, "y": y}, z] :- x = 1, y = 2, z = [1,2,3]`,
		}, `true`},
		{"output: complete array", []string{"p[x] :- q[i] = e, x = [i,e]", "q = [1,2,3,4] :- true"}, "[[0,1],[1,2],[2,3],[3,4]]"},
		{"output: complete object", []string{"p[x] :- q[i] = e, x = [i,e]", `q = {"a": 1, "b": 2} :- true`}, `[["a", 1], ["b", 2]]`},
		{"output: complete set", []string{"p[x] :- q[x]", "q = {1,2,3,4} :- true"}, "[1,2,3,4]"},
		{"output: complete array dereference non-ground", []string{"p[r] :- q[i][j] = 2, r = [i, j]", "q = [[1,2], [3,2]] :- true"}, "[[0, 1], [1, 1]]"},
		{"output: complete object defererence non-ground", []string{`p[r] :- q[x][y] = 2, r = [x, y]`, `q = {"a": {"x": 1}, "b": {"y": 2}, "c": {"z": 2}} :- true`}, `[["b", "y"], ["c", "z"]]`},
		{"output: complete vars", []string{
			`p[x] :- q[_][_] = x`,
			`q = [{"x": x, "y": y}, z] :- x = 1, y = 2, z = [1,2,3]`,
		}, `[1,2,3]`},

		// no dereferencing
		{"no suffix: complete", []string{"p = true :- q", "q = true :- true"}, "true"},
		{"no suffix: complete vars", []string{
			"p :- q", "q = x :- x = true",
		}, "true"},
		{"no suffix: complete incr (error)", []string{"p = true :- q", "q = false :- true", "q = true :- true"}, fmt.Errorf("evaluation error (code: 1): multiple values for data.q: rules must produce exactly one value for complete documents: check rule definition(s): q")},
		{"no suffix: complete incr", []string{"p = true :- not q", "q = true :- false", "q = false :- true"}, "true"},
		{"no suffix: object", []string{"p[x] = y :- q = o, o[x] = y", "q[x] = y :- b[x] = y"}, `{"v1": "hello", "v2": "goodbye"}`},
		{"no suffix: object incr", []string{
			"p[x] = y :- q = o, o[x] = y",
			"q[x] = y :- b[x] = y",
			`q[x1] = y1 :- d["e"][y1] = x1`},
			`{"v1": "hello", "v2": "goodbye", "bar": 0, "baz": 1}`},
		{"no suffix: chained", []string{
			"p = true :- q = x, x[i] = 4",
			"q[k] = v :- r = x, x[k] = v",
			"r[k] = v :- s = x, x[k] = v",
			"r[k] = v :- t = x, x[v] = k",
			`s = {"a": 1, "b": 2, "c": 4} :- true`,
			`t = ["d", "e", "g"] :- true`},
			"true"},
		{"no suffix: object var binding", []string{
			"p[x] :- q = x",
			`q[k] = v :- v = [i, j], k = i, i = "a", j = 1`},
			`[{"a": ["a", 1]}]`},
		{"no suffix: object composite value", []string{
			`p[x] :- q = x`,
			`q[k] = {"v": v} :- v = [i,j], k = i, i = "a", j = 1`},
			`[{"a": {"v": ["a", 1]}}]`},
		{"no suffix: object conflict (error)", []string{
			`p[x] = y :- xs = ["a","b","c","a"], x = xs[i], y = a[i]`},
			fmt.Errorf("evaluation error (code: 1): multiple values for data.p: rules must produce exactly one value for object document keys: check rule definition(s): p")},
		{"no suffix: set", []string{"p[x] :- q = s, s[x]", "q[x] :- a[i] = x"}, "[1,2,3,4]"},
	}

	data := loadSmallTestData()

	for _, tc := range tests {
		runTopDownTestCase(t, data, tc.note, tc.rules, tc.expected)
	}
}

func TestTopDownBaseAndVirtualDocs(t *testing.T) {

	// Define base docs that will overlap with virtual docs.
	var data map[string]interface{}

	input := `
	{
		"topdown": {
			"a": {
				"b": {
					"c": {
						"x": [100,200],
						"y": false,
						"z": {
							"a": "b"
						}
					}
				}
			},
			"g": {
				"h": {
					"k": [1,2,3]
				}
			},
			"set": {
				"u": [1,2,3,4]
			}
		}
	}
	`
	if err := util.UnmarshalJSON([]byte(input), &data); err != nil {
		panic(err)
	}

	compiler := compileModules([]string{
		// Define virtual docs that will overlap with base docs.
		`
			package topdown.a.b.c

			p = [1,2]
			q = [3,4]
			r["a"] = 1 :- true
			r["b"] = 2 :- true
		`,
		`
			package topdown.a.b.c.s
			w = {"f": 10.0, "g": 9.9}
		`,
		`
			package topdown.set

			v[data.topdown.set.u[_]] :- true
		`,
		`
			package topdown.no.base.doc
			p :- true
		`,
		`
			package topdown.a.b.c.undefined     # should not be included in result
			p :- false
		`,
		`
			package topdown.g.h                 # should not be included in result
			undefined :- false
		`,
		`
			package topdown.missing.request.value

			p :- request.foo
		`,
		// Define virtual docs that we can query to obtain merged result.
		`
			package topdown
			p[[x1,x2,x3,x4]] :- data.topdown.a.b[x1][x2][x3] = x4
			q[[x1,x2,x3]] :- data.topdown.a.b[x1][x2][0] = x3
			r[[x1,x2]] :- data.topdown.a.b[x1] = x2
			s = x :- data.topdown.no = x
			t :- data.topdown.a.b.c.undefined
		 	u :- data.topdown.missing.request.value
			v = x :- data.topdown.g = x
			w = data.topdown.set
		`,
	})

	store := storage.New(storage.InMemoryWithJSONConfig(data))

	assertTopDown(t, compiler, store, "base/virtual", []string{"topdown", "p"}, "{}", `[
		["c", "p", 0, 1],
		["c", "p", 1, 2],
		["c", "q", 0, 3],
		["c", "q", 1, 4],
		["c", "r", "a", 1],
		["c", "r", "b", 2],
		["c", "x", 0, 100],
		["c", "x", 1, 200],
		["c", "z", "a", "b"],
		["c", "s", "w", {"f":10.0, "g": 9.9}]
	]`)

	assertTopDown(t, compiler, store, "base/virtual: ground key", []string{"topdown", "q"}, "{}", `[
		["c", "p", 1],
		["c", "q", 3],
		["c", "x", 100]
	]`)

	assertTopDown(t, compiler, store, "base/virtual: prefix", []string{"topdown", "r"}, "{}", `[
		["c", {
			"p": [1,2],
			"q": [3,4],
			"r": {"a": 1, "b": 2},
			"s": {"w": {"f": 10.0, "g": 9.9}},
			"x": [100,200],
			"y": false,
			"z": {"a": "b"}}]
	]`)

	assertTopDown(t, compiler, store, "base/virtual: set", []string{"topdown", "w"}, "{}", `{
		"v": [1,2,3,4],
		"u": [1,2,3,4]
	}`)

	assertTopDown(t, compiler, store, "base/virtual: no base", []string{"topdown", "s"}, "{}", `{"base": {"doc": {"p": true}}}`)
	assertTopDown(t, compiler, store, "base/virtual: undefined", []string{"topdown", "t"}, "{}", "")
	assertTopDown(t, compiler, store, "base/virtual: undefined-2", []string{"topdown", "v"}, "{}", `{"h": {"k": [1,2,3]}}`)
	assertTopDown(t, compiler, store, "base/virtual: missing request value", []string{"topdown", "u"}, "{}", "")
}

func TestTopDownNestedReferences(t *testing.T) {
	tests := []struct {
		note     string
		rules    []string
		expected interface{}
	}{
		// nested base document references
		{"ground ref", []string{"p :- a[h[0][0]] = 2"}, "true"},
		{"non-ground ref", []string{"p[x] :- x = a[h[i][j]]"}, "[2,3,4]"},
		{"two deep", []string{"p[x] :- x = a[a[a[i]]]"}, "[3,4]"},
		{"two deep", []string{"p[x] :- x = a[h[i][a[j]]]"}, "[3,4]"},
		{"two deep repeated var", []string{"p[x] :- x = a[h[i][a[i]]]"}, "[3]"},
		{"no suffix", []string{"p :- 4 = a[three]"}, "true"},
		{"var ref", []string{"p[y] :- x = [1,2,3], y = a[x[_]]"}, "[2,3,4]"},
		{"undefined", []string{"p :- a[three.deadbeef] = x"}, ""},

		// nested virtual document references
		{"vdoc ref: complete", []string{"p[x] :- x = a[q[_]]", "q = [2,3] :- true"}, "[3,4]"},
		{"vdoc ref: complete: ground", []string{"p[x] :- x = a[q[1]]", "q = [2,3] :- true"}, "[4]"},
		{"vdoc ref: complete: no suffix", []string{"p :- 2 = a[q]", "q = 1 :- true"}, "true"},
		{"vdoc ref: partial object", []string{
			"p[x] :- x = a[q[_]]",
			`q[k] = v :- o = {"a": 2, "b": 3, "c": 100}, o[k] = v`},
			"[3,4]"},
		{"vdoc ref: partial object: ground", []string{
			`p[x] :- x = a[q["b"]]`,
			`q[k] = v :- o = {"a": 2, "b": 3, "c": 100}, o[k] = v`},
			"[4]"},

		// mixed cases
		{"vdoc ref: complete: nested bdoc ref", []string{
			"p[x] :- x = a[q[b[_]]]",
			`q = {"hello": 1, "goodbye": 3, "deadbeef": 1000} :- true`}, "[2,4]"},
		{"vdoc ref: partial object: nested bdoc ref", []string{
			"p[x] :- x = a[q[b[_]]]",
			// bind to value
			`q[k] = v :- o = {"hello": 1, "goodbye": 3, "deadbeef": 1000}, o[k] = v`}, "[2,4]"},
		{"vdoc ref: partial object: nested bdoc ref-2", []string{
			"p[x] :- x = a[q[d.e[_]]]",
			// bind to reference
			`q[k] = v :- strings[k] = v`}, "[3,4]"},
		{"vdoc ref: multiple", []string{
			"p[x] :- x = q[a[_]].v[r[a[_]]]",
			`q = [{"v": {}}, {"v": [0,0,1,2]}, {"v": [0,0,3,4]}, {"v": [0,0]}, {}] :- true`,
			"r = [1,2,3,4] :- true"}, "[1,2,3,4]"},
	}

	data := loadSmallTestData()

	for _, tc := range tests {
		runTopDownTestCase(t, data, tc.note, tc.rules, tc.expected)
	}
}

func TestTopDownVarReferences(t *testing.T) {

	tests := []struct {
		note     string
		rules    []string
		expected interface{}
	}{
		{"ground", []string{"p[x] :- v = [[1,2],[2,3],[3,4]], x = v[2][1]"}, "[4]"},
		{"non-ground", []string{"p[x] :- v = [[1,2],[2,3],[3,4]], x = v[i][j]"}, "[1,2,3,4]"},
		{"mixed", []string{`p[x] = y :- v = [{"a": 1, "b": 2}, {"c": 3, "z": [4]}], y = v[i][x][j]`}, `{"z": 4}`},
		{"ref binding", []string{"p[x] :- v = c[i][j], x = v[k], x = true"}, "[true, true]"},
		{"embedded", []string{`p[x] :- v = [1,2,3], x = [{"a": v[i]}]`}, `[[{"a": 1}], [{"a": 2}], [{"a": 3}]]`},
		{"embedded ref binding", []string{"p[x] :- v = c[i][j], w = [v[0], v[1]], x = w[y]"}, "[null, false, true, 3.14159]"},
		{"array: ground var", []string{"p[x] :- i = [1,2,3,4], j = [1,2,999], j[k] = y, i[y] = x"}, "[2,3]"},
		{"object: ground var", []string{`p[x] :- i = {"a": 1, "b": 2, "c": 3}, j = ["a", "c", "deadbeef"], j[k] = y, i[y] = x`}, "[1, 3]"},
		{"set: ground var", []string{"p[x] :- i = {1,2,3,4}, j = {1,2,99}, j[x], i[x]"}, "[1,2]"},
		{"set: lookup: base docs", []string{`p :- v = {[1,999],[3,4]}, pair = [a[2], 4], v[pair]`}, "true"},
		{"set: lookup: embedded", []string{"p :- x = [{}, {[1,2], [3,4]}], y = [3,4], x[i][y]"}, "true"},
		{"set: lookup: dereference: undefined", []string{"p :- x = [{}, {[1,2], [3,4]}], y = [3,4], x[i][y][z]"}, ""},
		{"avoids indexer", []string{"p = true :- somevar = [1,2,3], somevar[i] = 2"}, "true"},
	}

	data := loadSmallTestData()

	for _, tc := range tests {
		runTopDownTestCase(t, data, tc.note, tc.rules, tc.expected)
	}
}

func TestTopDownDisjunction(t *testing.T) {

	tests := []struct {
		note     string
		rules    []string
		expected interface{}
	}{
		{"incr: query set", []string{"p[x] :- a[i] = x", "p[y] :- b[j] = y"}, `[1,2,3,4,"hello","goodbye"]`},
		{"incr: query set constants", []string{
			"p[100] :- true",
			"p[x] :- a[x]"},
			"[0,1,2,3,100]"},
		{"incr: query object", []string{
			"p[k] = v :- b[v] = k",
			"p[k] = v :- a[i] = v, g[k][j] = v"},
			`{"b": 2, "c": 4, "hello": "v1", "goodbye": "v2", "a": 1}`},
		{"incr: query object constant key", []string{
			`p["a"] = 1 :- true`,
			`p["b"] = 2 :- true`},
			`{"a": 1, "b": 2}`},
		{"incr: iter set", []string{
			"p[x] :- q[x]",
			"q[x] :- a[i] = x",
			"q[y] :- b[j] = y"},
			`[1,2,3,4,"hello","goodbye"]`},
		{"incr: eval set", []string{
			"p[x] :- q = s, s[x]", // make p a set so that test assertion orders result
			"q[x] :- a[_] = x",
			"q[y] :- b[_] = y"},
			`[1,2,3,4,"hello","goodbye"]`},
		{"incr: eval object", []string{
			"p[k] = v :- q[k] = v",
			"q[k] = v :- b[v] = k",
			"q[k] = v :- a[i] = v, g[k][j] = v"},
			`{"b": 2, "c": 4, "hello": "v1", "goodbye": "v2", "a": 1}`},
		{"incr: eval object constant key", []string{
			"p[k] = v :- q[k] = v",
			`q["a"] = 1 :- true`,
			`q["b"] = 2 :- true`},
			`{"a": 1, "b": 2}`},
		{"complete: undefined", []string{"p :- false", "p :- false"}, ""},
		{"complete: error", []string{"p :- true", "p = false :- true"}, fmt.Errorf("evaluation error (code: 1): multiple values for data.p: rules must produce exactly one value for complete documents: check rule definition(s): p")},
		{"complete: valid", []string{"p :- true", "p = true :- true"}, "true"},
		{"complete: valid-2", []string{"p :- true", "p = false :- false"}, "true"},
		{"complete: reference error", []string{"p :- q", "q :- true", "q = false :- true"}, fmt.Errorf("evaluation error (code: 1): multiple values for data.q: rules must produce exactly one value for complete documents: check rule definition(s): q")},
		{"complete: reference valid", []string{"p :- q", "q :- true", "q = true :- true"}, "true"},
	}

	data := loadSmallTestData()

	for _, tc := range tests {
		runTopDownTestCase(t, data, tc.note, tc.rules, tc.expected)
	}
}

func TestTopDownNegation(t *testing.T) {
	tests := []struct {
		note     string
		rules    []string
		expected interface{}
	}{
		{"neg: constants", []string{"p = true :- not true = false"}, "true"},
		{"neg: constants", []string{"p = true :- not true = true"}, ""},
		{"neg: set contains", []string{`p = true :- not q["v0"]`, `q[x] :- b[x] = v`}, "true"},
		{"neg: set contains undefined", []string{`p = true :- not q["v2"]`, `q[x] :- b[x] = v`}, ""},
	}

	data := loadSmallTestData()

	for _, tc := range tests {
		runTopDownTestCase(t, data, tc.note, tc.rules, tc.expected)
	}
}

func TestTopDownComprehensions(t *testing.T) {

	tests := []struct {
		note     string
		rules    []string
		expected interface{}
	}{
		{"simple", []string{"p[i] :- xs = [x | x = a[_]], xs[i] > 1"}, "[1,2,3]"},
		{"nested", []string{"p[i] :- ys = [y | y = x[_], x = [z | z = a[_]]], ys[i] > 1"}, "[1,2,3]"},
		{"embedded array", []string{"p[i] :- xs = [[x | x = a[_]]], xs[0][i] > 1"}, "[1,2,3]"},
		{"embedded object", []string{`p[i] :- xs = {"a": [x | x = a[_]]}, xs["a"][i] > 1`}, "[1,2,3]"},
		{"embedded set", []string{"p = xs :- xs = {[x | x = a[_]]}"}, "[[1,2,3,4]]"},
		{"closure", []string{"p[x] :- y = 1, x = [y | y = 1]"}, "[[1]]"},
		{"dereference embedded", []string{
			"p[x] :- q.a[2][i] = x",
			`q[k] = v :- k = "a", v = [y | i[_] = _, i = y, i = [ z | z = a[_]] ]`,
		}, "[1,2,3,4]"},
	}

	data := loadSmallTestData()

	for _, tc := range tests {
		runTopDownTestCase(t, data, tc.note, tc.rules, tc.expected)
	}
}

func TestTopDownAggregates(t *testing.T) {

	tests := []struct {
		note     string
		rules    []string
		expected interface{}
	}{
		{"count", []string{"p[x] :- count(a, x)"}, "[4]"},
		{"count virtual", []string{"p[x] :- count([y | q[y]], x)", "q[x] :- x = a[_]"}, "[4]"},
		{"count keys", []string{"p[x] :- count(b, x)"}, "[2]"},
		{"count keys virtual", []string{"p[x] :- count([k | q[k] = _], x)", "q[k] = v :- b[k] = v"}, "[2]"},
		{"count set", []string{"p = x :- count(q, x)", "q[x] :- x = a[_]"}, "4"},
		{"sum", []string{"p[x] :- sum([1,2,3,4], x)"}, "[10]"},
		{"sum set", []string{"p = x :- sum({1,2,3,4}, x)"}, "10"},
		{"sum virtual", []string{"p[x] :- sum([y | q[y]], x)", "q[x] :- a[_] = x"}, "[10]"},
		{"sum virtual set", []string{"p = x :- sum(q, x)", "q[x] :- a[_] = x"}, "10"},
		{"max", []string{"p[x] :- max([1,2,3,4], x)"}, "[4]"},
		{"max set", []string{"p = x :- max({1,2,3,4}, x)"}, "4"},
		{"max virtual", []string{"p[x] :- max([y | q[y]], x)", "q[x] :- a[_] = x"}, "[4]"},
		{"max virtual set", []string{"p = x :- max(q, x)", "q[x] :- a[_] = x"}, "4"},
		{"reduce ref dest", []string{"p :- max([1,2,3,4], a[3])"}, "true"},
		{"reduce ref dest (2)", []string{"p :- not max([1,2,3,4,5], a[3])"}, "true"},
	}

	data := loadSmallTestData()

	for _, tc := range tests {
		runTopDownTestCase(t, data, tc.note, tc.rules, tc.expected)
	}
}

func TestTopDownArithmetic(t *testing.T) {
	tests := []struct {
		note     string
		rules    []string
		expected interface{}
	}{
		{"plus", []string{"p[y] :- a[i] = x, plus(i, x, y)"}, "[1,3,5,7]"},
		{"minus", []string{"p[y] :- a[i] = x, minus(i, x, y)"}, "[-1]"},
		{"multiply", []string{"p[y] :- a[i] = x, mul(i, x, y)"}, "[0,2,6,12]"},
		{"divide+round", []string{"p[z] :- a[i] = x, div(i, x, y), round(y, z)"}, "[0, 1]"},
		{"divide+error", []string{"p[y] :- a[i] = x, div(x, i, y)"}, fmt.Errorf("divide: by zero")},
		{"abs", []string{"p :- abs(-10, x), x = 10"}, "true"},
		{"arity 1 ref dest", []string{"p :- abs(-4, a[3])"}, "true"},
		{"arity 1 ref dest (2)", []string{"p :- not abs(-5, a[3])"}, "true"},
		{"arity 2 ref dest", []string{"p :- plus(1, 2, a[2])"}, "true"},
		{"arity 2 ref dest (2)", []string{"p :- not plus(2, 3, a[2])"}, "true"},
	}

	data := loadSmallTestData()

	for _, tc := range tests {
		runTopDownTestCase(t, data, tc.note, tc.rules, tc.expected)
	}
}

func TestTopDownCasts(t *testing.T) {
	tests := []struct {
		note     string
		rules    []string
		expected interface{}
	}{
		{"to_number", []string{`p[x] :- to_number("-42.0", y), to_number(false, z), x = [y, z]`}, "[[-42.0, 0]]"},
		{"to_number ref dest", []string{`p :- to_number("3", a[2])`}, "true"},
		{"to_number ref dest", []string{`p :- not to_number("-1", a[2])`}, "true"},
	}

	data := loadSmallTestData()

	for _, tc := range tests {
		runTopDownTestCase(t, data, tc.note, tc.rules, tc.expected)
	}
}

func TestTopDownRegex(t *testing.T) {
	tests := []struct {
		note     string
		rules    []string
		expected interface{}
	}{
		{"re_match", []string{`p :- re_match("^[a-z]+\\[[0-9]+\\]$", "foo[1]")`}, "true"},
		{"re_match: undefined", []string{`p :- re_match("^[a-z]+\\[[0-9]+\\]$", "foo[\"bar\"]")`}, ""},
		{"re_match: bad pattern err", []string{`p :- re_match("][", "foo[\"bar\"]")`}, fmt.Errorf("re_match: error parsing regexp: missing closing ]: `[`")},
		{"re_match: ref", []string{`p[x] :- re_match("^b.*$", d.e[x])`}, "[0,1]"},
	}

	data := loadSmallTestData()

	for _, tc := range tests {
		runTopDownTestCase(t, data, tc.note, tc.rules, tc.expected)
	}
}

func TestTopDownSets(t *testing.T) {
	tests := []struct {
		note     string
		rules    []string
		expected interface{}
	}{
		{"set_diff", []string{"p = x :- s1 = {1,2,3,4}, s2 = {1,3}, set_diff(s1, s2, x)"}, `[2,4]`},
		{"set_diff: refs", []string{"p = x :- s1 = {a[2], a[1], a[0]}, s2 = {a[0], 2}, set_diff(s1, s2, x)"}, "[3]"},
		{"set_diff: bad input", []string{"p = x :- s1 = [1,2,3], s2 = {1,2}, set_diff(s1, s2, x)"}, fmt.Errorf("evaluation error (code: 2): set_diff: first input argument must be set not ast.Array")},
		{"set_diff: bad input", []string{"p = x :- s1 = {1,2,3}, s2 = [1,2], set_diff(s1, s2, x)"}, fmt.Errorf("evaluation error (code: 2): set_diff: second input argument must be set not ast.Array")},
		{"set_diff: ground output", []string{"p :- set_diff({1,2,3}, {2,3}, {1})"}, "true"},
		{"set_diff: virt docs", []string{"p = x :- set_diff(s1, s2, x)", "s1[1] :- true", "s1[2] :- true", `s1["c"] :- true`, `s2 = {"c", 1} :- true`}, "[2]"},
	}

	data := loadSmallTestData()

	for _, tc := range tests {
		runTopDownTestCase(t, data, tc.note, tc.rules, tc.expected)
	}
}

func TestTopDownStrings(t *testing.T) {
	tests := []struct {
		note     string
		rules    []string
		expected interface{}
	}{
		{"format_int", []string{"p = x :- format_int(15.5, 16, x)"}, `"f"`},
		{"format_int: undefined", []string{`p :- format_int(15.5, 16, "10000")`}, ""},
		{"format_int: err", []string{"p :- format_int(null, 16, x)"}, fmt.Errorf("format_int: input must be a number: illegal argument: null")},
		{"format_int: ref dest", []string{"p :- format_int(3.1, 10, numbers[2])"}, "true"},
		{"format_int: ref dest (2)", []string{"p :- not format_int(4.1, 10, numbers[2])"}, "true"},
		{"concat", []string{`p = x :- concat("/", ["", "foo", "bar", "0", "baz"], x)`}, `"/foo/bar/0/baz"`},
		{"concat: set", []string{`p = x :- concat(",", {"1", "2", "3"}, x)`}, `"1,2,3"`},
		{"concat: undefined", []string{`p :- concat("/", ["a", "b"], "deadbeef")`}, ""},
		{"concat: non-string err", []string{`p = x :- concat("/", ["", "foo", "bar", 0, "baz"], x)`}, fmt.Errorf("concat: input value must be array of strings: illegal argument: 0")},
		{"concat: ref dest", []string{`p :- concat("", ["f", "o", "o"], c[0].x[2])`}, "true"},
		{"concat: ref dest (2)", []string{`p :- not concat("", ["b", "a", "r"], c[0].x[2])`}, "true"},
		{"indexof", []string{`p = x :- indexof("abcdefgh", "cde", x)`}, "2"},
		{"indexof: not found", []string{`p = x :- indexof("abcdefgh", "xyz", x)`}, "-1"},
		{"indexof: error", []string{`p = x :- indexof("abcdefgh", 1, x)`}, fmt.Errorf("indexof: search value must be a string: illegal argument: 1")},
		{"substring", []string{`p = x :- substring("abcdefgh", 2, 3, x)`}, `"cde"`},
		{"substring: remainder", []string{`p = x :- substring("abcdefgh", 2, -1, x)`}, `"cdefgh"`},
		{"substring: error 1", []string{`p = x :- substring(17, "xyz", 3, x)`}, fmt.Errorf("substring: base value must be a string: illegal argument: 17")},
		{"substring: error 2", []string{`p = x :- substring("abcdefgh", "xyz", 3, x)`}, fmt.Errorf(`substring: start index must be a number: illegal argument: "xyz"`)},
		{"substring: error 3", []string{`p = x :- substring("abcdefgh", 2, "xyz", x)`}, fmt.Errorf(`substring: length must be a number: illegal argument: "xyz"`)},
		{"contains", []string{`p :- contains("abcdefgh", "defg")`}, "true"},
		{"contains: undefined", []string{`p :- contains("abcdefgh", "ac")`}, ""},
		{"contains: error 1", []string{`p :- contains(17, "ac")`}, fmt.Errorf(`contains: base value must be a string: illegal argument: 17`)},
		{"contains: error 2", []string{`p :- contains("abcdefgh", 17)`}, fmt.Errorf(`contains: search must be a string: illegal argument: 17`)},
		{"startswith", []string{`p :- startswith("abcdefgh", "abcd")`}, "true"},
		{"startswith: undefined", []string{`p :- startswith("abcdefgh", "bcd")`}, ""},
		{"startswith: error 1", []string{`p :- startswith(17, "bcd")`}, fmt.Errorf(`startswith: base value must be a string: illegal argument: 17`)},
		{"startswith: error 2", []string{`p :- startswith("abcdefgh", 17)`}, fmt.Errorf(`startswith: search must be a string: illegal argument: 17`)},
		{"endswith", []string{`p :- endswith("abcdefgh", "fgh")`}, "true"},
		{"endswith: undefined", []string{`p :- endswith("abcdefgh", "fg")`}, ""},
		{"endswith: error 1", []string{`p :- endswith(17, "bcd")`}, fmt.Errorf(`endswith: base value must be a string: illegal argument: 17`)},
		{"endswith: error 2", []string{`p :- endswith("abcdefgh", 17)`}, fmt.Errorf(`endswith: search must be a string: illegal argument: 17`)},
		{"lower", []string{`p = x :- lower("AbCdEf", x)`}, `"abcdef"`},
		{"lower error", []string{`p = x :- lower(true, x)`}, fmt.Errorf("lower: original value must be a string: illegal argument: true")},
		{"upper", []string{`p = x :- upper("AbCdEf", x)`}, `"ABCDEF"`},
		{"upper error", []string{`p = x :- upper(true, x)`}, fmt.Errorf("upper: original value must be a string: illegal argument: true")},
	}

	data := loadSmallTestData()

	for _, tc := range tests {
		runTopDownTestCase(t, data, tc.note, tc.rules, tc.expected)
	}
}

func TestTopDownEmbeddedVirtualDoc(t *testing.T) {

	compiler := compileModules([]string{
		`package b.c.d

         import data.a
         import data.g

         p[x] :- a[i] = x, q[x]
         q[x] :- g[j][k] = x`})

	store := storage.New(storage.InMemoryWithJSONConfig(loadSmallTestData()))

	assertTopDown(t, compiler, store, "deep embedded vdoc", []string{"b", "c", "d", "p"}, "{}", "[1, 2, 4]")
}

func TestTopDownRequestValues(t *testing.T) {
	compiler := compileModules([]string{
		`package z
		 import data.a
		 import request.req1
		 import request.req2 as req2as
		 import request.req3.a.b
		 import request.req4.a.b as req4as
		 p = true :- a[i] = x, req1.foo = x, req2as.bar = x, q[x]
		 q[x] :- req1.foo = x, req2as.bar = x, r[x]
		 r[x] :- {"foo": req2as.bar, "bar": [x]} = {"foo": x, "bar": [req1.foo]}
		 s :- b.x[0] = 1
		 t :- req4as.x[0] = 1
		 u[x] :- b[_] = x, x > 1
		 w = [[1,2], [3,4]]
		 gt1 :- req1 > 1
		 keys[x] = y :- data.numbers[_] = x, to_number(x, y)
		 loopback = request
		 `})

	store := storage.New(storage.InMemoryWithJSONConfig(loadSmallTestData()))

	assertTopDown(t, compiler, store, "loopback", []string{"z", "loopback"}, `{"foo": 1}`, `{"foo": 1}`)

	assertTopDown(t, compiler, store, "loopback undefined", []string{"z", "loopback"}, ``, ``)

	assertTopDown(t, compiler, store, "simple", []string{"z", "p"}, `{
		"req1": {"foo": 4},
		"req2": {"bar": 4}
	}`, "true")

	assertTopDown(t, compiler, store, "missing", []string{"z", "p"}, `{
		"req1": {"foo": 4}
	}`, "")

	assertTopDown(t, compiler, store, "namespaced", []string{"z", "s"}, `{
		"req3": {
			"a": {
				"b": {
					"x": [1,2,3,4]
				}
			}
		}
	}`, "true")

	assertTopDown(t, compiler, store, "namespaced with alias", []string{"z", "t"}, `{
		"req4": {
			"a": {
				"b": {
					"x": [1,2,3,4]
				}
			}
		}
	}`, "true")

	assertTopDown(t, compiler, store, "embedded ref to base doc", []string{"z", "s"}, `{
		"req3": {
			"a": {
				"b": {
					"x": data.a
				}
			}
		}
	}`, "true")

	assertTopDown(t, compiler, store, "embedded non-ground ref to base doc", []string{"z", "u"}, `{
		"req3": {
			"a": {
				"b": data.l[x].c
			}
		}
	}`, [][2]string{
		{"[2,3,4]", `{"x": 0}`},
		{"[2,3,4,5]", `{"x": 1}`},
	})

	assertTopDown(t, compiler, store, "embedded non-ground ref to virtual doc", []string{"z", "u"}, `{
		"req3": {
			"a": {
				"b": data.z.w[x]
			}
		}
	}`, [][2]string{
		{"[2]", `{"x": 0}`},
		{"[3,4]", `{"x": 1}`},
	})

	assertTopDown(t, compiler, store, "non-ground ref to virtual doc-2", []string{"z", "gt1"}, `{
		"req1": data.z.keys[x]
	}`, [][2]string{
		{"true", `{"x": "2"}`},
		{"true", `{"x": "3"}`},
		{"true", `{"x": "4"}`},
	})
}

func TestTopDownCaching(t *testing.T) {
	compiler := compileModules([]string{`
	package topdown.caching

	p[x] :- q[x], q[y]

	q[x] :- data.d.e[_] = k, r[k] = x  # exercise caching with ref key:
	                                   # k will be bound to ref data.d.e[0], data.d.e.[1], etc.

	r[k] = v :- data.strings[k] = v

	err_top :- data.l[_] = x, err_obj[x] = _
	err_obj[k] = true :- k = data.l[_]  # data.l[_] refers to ast.Object
	`})

	store := storage.New(storage.InMemoryWithJSONConfig(loadSmallTestData()))

	assertTopDown(t, compiler, store, "reference lookup", []string{"topdown", "caching", "p"}, `{}`, "[2,3]")

	illegalObjectKeyMsg := fmt.Errorf("evaluation error (code: 2): 12:2: err_obj produced illegal object key type ast.Object")
	assertTopDown(t, compiler, store, "unhandled error", []string{"topdown", "caching", "err_top"}, "{}", illegalObjectKeyMsg)
	assertTopDown(t, compiler, store, "unhandled error", []string{"topdown", "caching", "err_obj"}, "{}", illegalObjectKeyMsg)
}

func TestTopDownStoragePlugin(t *testing.T) {

	compiler := compileModules([]string{`
	package topdown.plugins

	p[x] :- q[x], not r[x]
	q[x] :- data.a[_] = x
	r[x] :- data.plugin.b[_] = x
	`})

	store := storage.New(storage.InMemoryWithJSONConfig(loadSmallTestData()))

	plugin := storage.NewDataStoreFromReader(strings.NewReader(`{"b": [1,3,5,6]}`))
	mountPath, _ := storage.ParsePath("/plugin")

	if err := store.Mount(plugin, mountPath); err != nil {
		t.Fatalf("Unexpected mount error: %v", err)
	}

	assertTopDown(t, compiler, store, "rule with plugin", []string{"topdown", "plugins", "p"}, `{}`, "[2,4]")
}

func TestExample(t *testing.T) {

	bd := `
        {
            "servers": [
                {"id": "s1", "name": "app", "protocols": ["https", "ssh"], "ports": ["p1", "p2", "p3"]},
                {"id": "s2", "name": "db", "protocols": ["mysql"], "ports": ["p3"]},
                {"id": "s3", "name": "cache", "protocols": ["memcache", "http"], "ports": ["p3"]},
                {"id": "s4", "name": "dev", "protocols": ["http"], "ports": ["p1", "p2"]}
            ],
            "networks": [
                {"id": "n1", "public": false},
                {"id": "n2", "public": false},
                {"id": "n3", "public": true}
            ],
            "ports": [
                {"id": "p1", "networks": ["n1"]},
                {"id": "p2", "networks": ["n3"]},
                {"id": "p3", "networks": ["n2"]}
            ]
        }
    `

	vd := `
        package opa.example

        import data.servers
        import data.networks
        import data.ports

        public_servers[server] :-
            server = servers[_],
            server.ports[_] = ports[i].id,
            ports[i].networks[_] = networks[j].id,
            networks[j].public = true

        violations[server] :-
            server = servers[_],
            server.protocols[_] = "http",
            public_servers[server]
    `

	var doc map[string]interface{}

	if err := util.UnmarshalJSON([]byte(bd), &doc); err != nil {
		panic(err)
	}

	compiler := compileModules([]string{vd})

	store := storage.New(storage.InMemoryWithJSONConfig(doc))

	assertTopDown(t, compiler, store, "public servers", []string{"opa", "example", "public_servers"}, "{}", `
        [
            {"id": "s1", "name": "app", "protocols": ["https", "ssh"], "ports": ["p1", "p2", "p3"]},
            {"id": "s4", "name": "dev", "protocols": ["http"], "ports": ["p1", "p2"]}
        ]
    `)

	assertTopDown(t, compiler, store, "violations", []string{"opa", "example", "violations"}, "{}", `
	    [
	        {"id": "s4", "name": "dev", "protocols": ["http"], "ports": ["p1", "p2"]}
	    ]
	`)

	assertTopDown(t, compiler, store, "both", []string{"opa", "example"}, "{}", `
		{
			"public_servers": [
				{"id": "s1", "name": "app", "protocols": ["https", "ssh"], "ports": ["p1", "p2", "p3"]},
				{"id": "s4", "name": "dev", "protocols": ["http"], "ports": ["p1", "p2"]}
			],
			"violations": [
				{"id": "s4", "name": "dev", "protocols": ["http"], "ports": ["p1", "p2"]}
			]
		}
	`)
}

func TestTopDownUnsupportedBuiltin(t *testing.T) {

	ast.RegisterBuiltin(&ast.Builtin{
		Name: ast.Var("unsupported_builtin"),
	})

	body := ast.MustParseBody(`unsupported_builtin()`)
	ctx := context.Background()
	compiler := ast.NewCompiler()
	store := storage.New(storage.InMemoryConfig())
	txn := storage.NewTransactionOrDie(ctx, store)
	top := New(ctx, body, compiler, store, txn)

	err := Eval(top, func(*Topdown) error {
		return nil
	})

	expected := typeErrUnsupportedBuiltin(body[0])

	if !reflect.DeepEqual(err, expected) {
		t.Fatalf("Expected %v but got: %v", expected, err)
	}

}

type contextPropagationMock struct{}

// contextPropagationStore will accumulate values from the contexts provided to
// read calls so that the test can verify that contexts are being propagated as
// expected.
type contextPropagationStore struct {
	storage.WritesNotSupported
	storage.TriggersNotSupported
	calls []interface{}
}

func (m *contextPropagationStore) ID() string {
	return "mock"
}

func (m *contextPropagationStore) Begin(context.Context, storage.Transaction, storage.TransactionParams) error {
	return nil
}

func (m *contextPropagationStore) Close(context.Context, storage.Transaction) {
}

func (m *contextPropagationStore) Read(ctx context.Context, txn storage.Transaction, path storage.Path) (interface{}, error) {
	val := ctx.Value(contextPropagationMock{})
	m.calls = append(m.calls, val)
	return nil, nil
}

func TestTopDownContextPropagation(t *testing.T) {

	ctx := context.WithValue(context.Background(), contextPropagationMock{}, "bar")

	compiler := ast.NewCompiler()
	compiler.Compile(map[string]*ast.Module{
		"mod1": ast.MustParseModule(`
			package ex
			p[x] :- data.a[i] = x
		`),
	})

	mockStore := &contextPropagationStore{}
	store := storage.New(storage.Config{
		Builtin: mockStore,
	})
	txn := storage.NewTransactionOrDie(ctx, store)
	params := NewQueryParams(ctx, compiler, store, txn, nil, ast.MustParseRef("data.ex.p"))

	_, err := Query(params)
	if err != nil {
		t.Fatalf("Unexpected query error: %v", err)
	}

	expectedCalls := []interface{}{"bar"}

	if !reflect.DeepEqual(expectedCalls, mockStore.calls) {
		t.Fatalf("Expected %v but got: %v", expectedCalls, mockStore.calls)
	}
}

func TestTopDownTracingEval(t *testing.T) {
	module := `
	package test
	p :- arr = [1,2,3], x = arr[_], x != 2
	`
	p := ast.MustParseRule(`p :- arr = [1,2,3], x = arr[_], x != 2`)
	runTopDownTracingTestCase(t, module, 15, map[int]*Event{
		6:  &Event{ExitOp, p, 3, 2, parseBindings("{x: 1}")},
		7:  &Event{RedoOp, p, 3, 2, nil},
		8:  &Event{RedoOp, parseExpr("x = arr[_]", 1), 3, 2, nil},
		9:  &Event{EvalOp, parseExpr("x != 2", 2), 3, 2, parseBindings("{x: 2}")},
		10: &Event{FailOp, parseExpr("x != 2", 2), 3, 2, parseBindings("{x: 2}")},
		11: &Event{RedoOp, parseExpr("x = arr[_]", 1), 3, 2, parseBindings("{arr: [1,2,3]}")},
		12: &Event{EvalOp, parseExpr("x != 2", 2), 3, 2, parseBindings("{x: 3}")},
		13: &Event{ExitOp, p, 3, 2, parseBindings("{x: 3}")},
	})
}

func TestTopDownTracingNegation(t *testing.T) {
	module := `
	package test
	p :- arr = [1,2,3,4], x = arr[_], not x = 2
	`
	runTopDownTracingTestCase(t, module, 31, map[int]*Event{
		5:  &Event{EvalOp, parseExpr("not x = 2", 2), 3, 2, parseBindings("{x: 1}")},
		6:  &Event{EnterOp, ast.MustParseBody("x = 2"), 4, 3, parseBindings("{x: 1}")},
		16: &Event{FailOp, parseExpr("not x = 2", 2), 3, 2, parseBindings("{x: 2}")},
	})
}

func TestTopDownTracingCompleteDocs(t *testing.T) {
	module := `
	package test
	p :- q[1] = "b"
	q = ["a", "b", "c", "d"]
	q = null :- false
	`
	runTopDownTracingTestCase(t, module, 12, map[int]*Event{
		4: &Event{EnterOp, ast.MustParseRule(`q = ["a", "b", "c", "d"] :- true`), 4, 3, nil},
		6: &Event{ExitOp, ast.MustParseRule(`q = ["a", "b", "c", "d"] :- true`), 4, 3, nil},
		7: &Event{RedoOp, ast.MustParseRule(`q = null :- false`), 5, 3, nil},
		9: &Event{FailOp, parseExpr("false", 0), 5, 3, nil},
	})
}

func TestTopDownTracingPartialSets(t *testing.T) {
	module := `
	package test
	p :- q[x], x != 2, r[x], s[x]
	q[y] :- arr = [1,2,3,4], y = arr[i]
	r[z] :- z = data.a[i], z > 1
	s[x] :- x = 3
	s[y] :- y = 4
	`

	q := ast.MustParseRule(`q[y] :- arr = [1,2,3,4], y = arr[i]`)
	r := ast.MustParseRule(`r[z] :- z = data.a[i], z > 1`)
	sx := ast.MustParseRule(`s[x] :- x = 3`)
	sy := ast.MustParseRule(`s[y] :- y = 4`)

	runTopDownTracingTestCase(t, module, 60, map[int]*Event{
		4:  &Event{EnterOp, q, 4, 3, nil},
		7:  &Event{ExitOp, q, 4, 3, parseBindings("{y: 1}")},
		10: &Event{EnterOp, r, 5, 3, parseBindings("{z: 1}")},
		16: &Event{RedoOp, q, 4, 3, nil},
		17: &Event{RedoOp, parseExpr("y = arr[i]", 1), 4, 3, nil},
		18: &Event{ExitOp, q, 4, 3, parseBindings("{y: 2}")},
		30: &Event{ExitOp, r, 6, 3, parseBindings("{z: 3}")},
		32: &Event{EnterOp, sx, 7, 3, parseBindings("{x: 3}")},
		34: &Event{ExitOp, sx, 7, 3, parseBindings("{x: 3}")},
		38: &Event{RedoOp, sy, 8, 3, parseBindings("{y: 3}")},
		40: &Event{FailOp, parseExpr("y = 4", 0), 8, 3, parseBindings("{y: 3}")},
	})
}

func TestTopDownTracingPartialObjects(t *testing.T) {
	module := `
	package test
	p :- q[x] = y, x != "b", r[x] > y
	q[k] = v :- obj = {"a":1, "b":2, "c":3, "d":4}, obj[k] = v
	r["a"] = 0 :- true
	r["c"] = 4 :- true
	`

	q := ast.MustParseRule(`q[k] = v :- obj = {"a":1, "b":2, "c":3, "d":4}, obj[k] = v`)
	ra := ast.MustParseRule(`r["a"] = 0 :- true`)
	rc := ast.MustParseRule(`r["c"] = 4 :- true`)

	runTopDownTracingTestCase(t, module, 39, map[int]*Event{
		4:  &Event{EnterOp, q, 4, 3, nil},
		7:  &Event{ExitOp, q, 4, 3, parseBindings(`{k: "a", v: 1}`)},
		10: &Event{EnterOp, ra, 5, 3, nil},
		15: &Event{RedoOp, q, 4, 3, nil},
		16: &Event{RedoOp, parseExpr("obj[k] = v", 1), 4, 3, nil},
		17: &Event{ExitOp, q, 4, 3, parseBindings(`{k: "b", v: 2}`)},
		26: &Event{RedoOp, rc, 8, 3, nil},
		28: &Event{ExitOp, rc, 8, 3, nil},
	})
}

func TestTopDownTracingPartialObjectsFull(t *testing.T) {
	module := `
	package test
	p :- q = v, v["b"] != 0
	q[k] = 1 :- ks = ["a","b","c"], k = ks[_]
	q["x"] = 100 :- true
	`

	q := ast.MustParseRule(`q[k] = 1 :- ks = ["a","b","c"], k = ks[_]`)
	qx := ast.MustParseRule(`q["x"] = 100 :- true`)

	runTopDownTracingTestCase(t, module, 20, map[int]*Event{
		4:  &Event{EnterOp, q, 4, 3, nil},
		7:  &Event{ExitOp, q, 4, 3, parseBindings(`{k: "a"}`)},
		8:  &Event{RedoOp, q, 4, 3, nil},
		10: &Event{ExitOp, q, 4, 3, parseBindings(`{k: "b"}`)},
		11: &Event{RedoOp, q, 4, 3, nil},
		13: &Event{ExitOp, q, 4, 3, parseBindings(`{k: "c"}`)},
		14: &Event{RedoOp, qx, 5, 3, nil},
		16: &Event{ExitOp, qx, 5, 3, nil},
	})
}

func TestTopDownTracingComprehensions(t *testing.T) {
	module := `
	package test
	p :- m = 1, count([x | x = data.a[_], x > m], n), n = 3
	`

	compr := ast.MustParseBody(`x = data.a[_], x > m`)

	runTopDownTracingTestCase(t, module, 23, map[int]*Event{
		5:  &Event{EnterOp, compr, 4, 3, parseBindings(`{m: 1}`)},
		11: &Event{ExitOp, compr, 4, 3, parseBindings(`{m: 1, x: data.a[1]}`)},
		12: &Event{RedoOp, compr, 4, 3, parseBindings(`{m: 1}`)},
		15: &Event{ExitOp, compr, 4, 3, parseBindings(`{m: 1, x: data.a[2]}`)},
		16: &Event{RedoOp, compr, 4, 3, parseBindings(`{m: 1}`)},
		19: &Event{ExitOp, compr, 4, 3, parseBindings(`{m: 1, x: data.a[3]}`)},
	})
}

func compileModules(input []string) *ast.Compiler {

	mods := map[string]*ast.Module{}

	for idx, i := range input {
		id := fmt.Sprintf("testMod%d", idx)
		mods[id] = ast.MustParseModule(i)
	}

	c := ast.NewCompiler()
	if c.Compile(mods); c.Failed() {
		panic(c.Errors)
	}

	return c
}

func compileRules(imports []string, input []string) *ast.Compiler {

	rules := []*ast.Rule{}
	for _, i := range input {
		rules = append(rules, ast.MustParseRule(i))
	}

	is := []*ast.Import{}
	for _, i := range imports {
		is = append(is, &ast.Import{
			Path: ast.MustParseTerm(i),
		})
	}

	p := ast.Ref{ast.DefaultRootDocument}
	m := &ast.Module{
		Package: &ast.Package{
			Path: p,
		},
		Imports: is,
		Rules:   rules,
	}

	c := ast.NewCompiler()
	if c.Compile(map[string]*ast.Module{"testMod": m}); c.Failed() {
		panic(c.Errors)
	}

	return c
}

func parseExpr(s string, idx int) *ast.Expr {
	expr := ast.MustParseBody(s)[0]
	expr.Index = idx
	return expr
}

func parseBindings(s string) *ast.ValueMap {
	t := ast.MustParseTerm(s)
	obj, ok := t.Value.(ast.Object)
	if !ok {
		return nil
	}
	r := ast.NewValueMap()
	for _, pair := range obj {
		k, v := pair[0], pair[1]
		r.Put(k.Value, v.Value)
	}
	return r
}

// TODO(tsandall): replace loadExpectedBindings with parseBindings
func loadExpectedBindings(input string) []*ast.ValueMap {
	var data []map[string]interface{}
	if err := util.UnmarshalJSON([]byte(input), &data); err != nil {
		panic(err)
	}
	var expected []*ast.ValueMap
	for _, bindings := range data {
		buf := ast.NewValueMap()
		for k, v := range bindings {
			switch v := v.(type) {
			case string:
				buf.Put(ast.MustParseTerm(k).Value, ast.String(v))
			case json.Number:
				buf.Put(ast.MustParseTerm(k).Value, ast.Number(v))
			default:
				panic("unreachable")
			}
		}
		expected = append(expected, buf)
	}

	return expected
}

func parseJSON(input string) interface{} {
	var data interface{}
	if err := util.UnmarshalJSON([]byte(input), &data); err != nil {
		panic(err)
	}
	return data
}

func parseQueryResultSetJSON(input [][2]string) (result QueryResultSet) {
	for i := range input {
		result.Add(&QueryResult{parseJSON(input[i][0]), parseJSON(input[i][1]).(map[string]interface{})})
	}
	return result
}

func parseSortedJSON(input string) interface{} {
	data := parseJSON(input)
	switch data := data.(type) {
	case []interface{}:
		sort.Sort(resultSet(data))
		return data
	default:
		return data
	}
}

// loadSmallTestData returns base documents that are referenced
// throughout the topdown test suite.
//
// Avoid the following top-level keys: i, j, k, p, q, r, v, x, y, z.
// These are used for rule names, local variables, etc.
//
func loadSmallTestData() map[string]interface{} {
	var data map[string]interface{}
	err := util.UnmarshalJSON([]byte(`{
        "a": [1,2,3,4],
        "b": {
            "v1": "hello",
            "v2": "goodbye"
        },
        "c": [{
            "x": [true, false, "foo"],
            "y": [null, 3.14159],
            "z": {"p": true, "q": false}
        }],
        "d": {
            "e": ["bar", "baz"]
        },
        "f": [
            {"xs": [1.0], "ys": [2.0]},
            {"xs": [2.0], "ys": [3.0]}
        ],
        "g": {
            "a": [1, 0, 0, 0],
            "b": [0, 2, 0, 0],
            "c": [0, 0, 0, 4]
        },
        "h": [
            [1,2,3],
            [2,3,4]
        ],
        "l": [
            {
                "a": "bob",
                "b": -1,
                "c": [1,2,3,4]
            },
            {
                "a": "alice",
                "b": 1,
                "c": [2,3,4,5],
                "d": null
            }
        ],
		"strings": {
			"foo": 1,
			"bar": 2,
			"baz": 3
		},
		"three": 3,
        "m": [],
		"numbers": [
			"1",
			"2",
			"3",
			"4"
		]
    }`), &data)
	if err != nil {
		panic(err)
	}
	return data
}

func runTopDownTestCase(t *testing.T, data map[string]interface{}, note string, rules []string, expected interface{}) {
	imports := []string{}
	for k := range data {
		imports = append(imports, "data."+k)
	}

	compiler := compileRules(imports, rules)

	store := storage.New(storage.InMemoryWithJSONConfig(data))

	assertTopDown(t, compiler, store, note, []string{"p"}, "", expected)
}

func runTopDownTracingTestCase(t *testing.T, module string, n int, cases map[int]*Event) {

	ctx := context.Background()
	compiler := compileModules([]string{module})
	data := loadSmallTestData()
	store := storage.New(storage.InMemoryWithJSONConfig(data))
	txn := storage.NewTransactionOrDie(ctx, store)
	params := NewQueryParams(ctx, compiler, store, txn, nil, ast.MustParseRef("data.test.p"))
	buf := NewBufferTracer()
	params.Tracer = buf

	qidFactory.Reset()

	_, err := Query(params)
	if err != nil {
		panic(err)
	}

	if len(*buf) != n {
		t.Errorf("Expected %d events but got: %v\n%v", n, len(*buf), buf)
	}

	for i, expected := range cases {
		if len(*buf) <= i {
			continue
		}
		result := (*buf)[i]
		bindings := ast.NewValueMap()
		expected.Locals.Iter(func(k, _ ast.Value) bool {
			if v := result.Locals.Get(k); v != nil {
				bindings.Put(k, v)
			}
			return false
		})
		result.Locals = bindings
		if !result.Equal(expected) {
			t.Errorf("Expected event %d to equal %v but got: %v", i, expected, result)
		}
	}
}

func assertTopDown(t *testing.T, compiler *ast.Compiler, store *storage.Storage, note string, path []string, request string, expected interface{}) {

	var req ast.Value

	if len(request) > 0 {
		req = ast.MustParseTerm(request).Value
	}

	p := []interface{}{}
	for _, x := range path {
		p = append(p, x)
	}

	ctx := context.Background()

	txn := storage.NewTransactionOrDie(ctx, store)
	defer store.Close(ctx, txn)

	ref := ast.MustParseRef("data." + strings.Join(path, "."))
	params := NewQueryParams(ctx, compiler, store, txn, req, ref)

	testutil.Subtest(t, note, func(t *testing.T) {
		switch e := expected.(type) {
		case error:
			result, err := Query(params)
			if err == nil {
				t.Errorf("Expected error but got: %v", result)
				return
			}
			if err.Error() != e.Error() {
				t.Errorf("Expected error %v but got: %v", e, err)
			}

		case [][2]string:
			qrs, err := Query(params)
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			expected := parseQueryResultSetJSON(e)

			if !reflect.DeepEqual(expected, qrs) {
				t.Fatalf("Expected %v but got: %v", expected, qrs)
			}

		case string:
			qrs, err := Query(params)

			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			if len(e) == 0 {
				if !qrs.Undefined() {
					t.Fatalf("Expected undefined result but got: %v", qrs)
				}
				return
			}

			if qrs.Undefined() {
				t.Fatalf("Expected %v but got undefined", e)
			}

			expected := parseSortedJSON(e)

			// Sort set results so that comparisons are not dependant on order.
			p := ast.MustParseRef(fmt.Sprintf("data.%v", strings.Join(path, ".")))
			if rs := compiler.GetRulesExact(p); len(rs) > 0 && rs[0].DocKind() == ast.PartialSetDoc {
				sort.Sort(resultSet(qrs[0].Result.([]interface{})))
			}

			if !reflect.DeepEqual(qrs[0].Result, expected) {
				t.Errorf("Expected %v but got: %v", expected, qrs[0].Result)
			}
		}
	})
}

type resultSet []interface{}

func (rs resultSet) Less(i, j int) bool {
	return util.Compare(rs[i], rs[j]) < 0
}

func (rs resultSet) Swap(i, j int) {
	tmp := rs[i]
	rs[i] = rs[j]
	rs[j] = tmp
}

func (rs resultSet) Len() int {
	return len(rs)
}
