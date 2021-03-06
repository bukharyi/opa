// Copyright 2016 The OPA Authors.  All rights reserved.
// Use of this source code is governed by an Apache2
// license that can be found in the LICENSE file.

package ast

import "testing"

func TestTransform(t *testing.T) {
	module := MustParseModule(`
    package ex["this"]
    import request.foo
    import data.bar["this"] as qux
    p :- "this" = "that"
    p = "this" :- false
    p["this"] :- false
    p[y] = {"this": ["this"]} :- false
    p :- ["this" | "this"]
	p = n :- count({"this", "that"}, n)
    `)

	result, err := Transform(&GenericTransformer{
		func(x interface{}) (interface{}, error) {
			if s, ok := x.(String); ok && s == String("this") {
				return String("that"), nil
			}
			return x, nil
		},
	}, module)

	if err != nil {
		t.Fatalf("Unexpected error during transfom: %v", err)
	}

	resultMod, ok := result.(*Module)
	if !ok {
		t.Fatalf("Expected module from transform but got: %v", result)
	}

	expected := MustParseModule(`
    package ex["that"]
    import request.foo
    import data.bar["that"] as qux
    p :- "that" = "that"
    p = "that" :- false
    p["that"] :- false
    p[y] = {"that": ["that"]} :- false
    p :- ["that" | "that"]
	p = n :- count({"that"}, n)
    `)

	if !expected.Equal(resultMod) {
		t.Fatalf("Expected module:\n%v\n\nGot:\n%v\n", expected, resultMod)
	}

}
