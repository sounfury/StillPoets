package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

func parseSingleFunc(t *testing.T, src string) *ast.FuncDecl {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "sample.go", src, 0)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	for _, d := range file.Decls {
		if fn, ok := d.(*ast.FuncDecl); ok {
			return fn
		}
	}
	t.Fatalf("no function found in sample")
	return nil
}

func TestCognitiveExemptsDeclarativeChildrenCallbacks(t *testing.T) {
	uiSrc := `package sample
func render(a, b bool) {
	col().Children(func() {
		row().Children(func() {
			if a {
				if b {
					draw()
				}
			}
		})
	})
}`
	fn := parseSingleFunc(t, uiSrc)
	// 外层 if a (+1) + 内层 if b (+1+1=2) = 3；两层 .Children(func()) 不额外叠加嵌套惩罚
	if got := calcCognitive(fn); got != 3 {
		t.Fatalf("expected cognitive=3 for declarative Children callbacks, got %d", got)
	}

	plainSrc := `package sample
func logic(a bool) {
	run(func() {
		if a {
			work()
		}
	})
}`
	fn2 := parseSingleFunc(t, plainSrc)
	// 普通闭包仍叠加 depth+1：if a (+1+1=2)
	if got := calcCognitive(fn2); got != 2 {
		t.Fatalf("expected cognitive=2 for non-UI closure, got %d", got)
	}
}

func TestMetricsBaseline(t *testing.T) {
	src := `package sample
func demo(a, b, c, d, e, f int) int {
	if a > 0 && b > 0 {
		for i := 0; i < c; i++ {
			if d > 0 {
				return e + f
			}
		}
	}
	return 0
}`
	fn := parseSingleFunc(t, src)
	if got := calcCyclomatic(fn); got != 5 {
		t.Fatalf("expected cyclomatic=5, got %d", got)
	}
	if got := calcMaxNesting(fn); got != 3 {
		t.Fatalf("expected nesting=3, got %d", got)
	}
	if got := countFieldList(fn.Type.Params); got != 6 {
		t.Fatalf("expected params=6, got %d", got)
	}
}
