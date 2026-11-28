// Package main provides AST-based metric checks (file length, cyclomatic, cognitive, nesting, params).
package main

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/token"
	"os"
)

func checkFileLength(files []string, limit int) ([]Issue, error) {
	var issues []Issue
	for _, path := range files {
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		lines := bytes.Count(raw, []byte{'\n'})
		if len(raw) > 0 && raw[len(raw)-1] != '\n' {
			lines++
		}
		if lines > limit {
			issues = append(issues, Issue{
				File:    path,
				Line:    lines,
				Col:     1,
				Rule:    "FileLength",
				Message: fmt.Sprintf("文件行数 %d 超过阈值 %d", lines, limit),
				Score:   lines,
			})
		}
	}
	return issues, nil
}

func checkFuncMetric(files []string, rule string, limit int, scorer func(*ast.FuncDecl) int) ([]Issue, error) {
	fset := token.NewFileSet()
	var issues []Issue
	for _, path := range files {
		file, err := parseGoFile(fset, path)
		if err != nil {
			return nil, err
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			score := scorer(fn)
			if score > limit {
				pos := fset.Position(fn.Pos())
				issues = append(issues, Issue{
					File:    path,
					Line:    pos.Line,
					Col:     pos.Column,
					Rule:    rule,
					Message: fmt.Sprintf("函数 %s 指标为 %d（阈值 %d）", formatFuncName(fn), score, limit),
					Score:   score,
				})
			}
		}
	}
	return issues, nil
}

func checkFuncParams(files []string, limit int) ([]Issue, error) {
	fset := token.NewFileSet()
	var issues []Issue
	for _, path := range files {
		file, err := parseGoFile(fset, path)
		if err != nil {
			return nil, err
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Type == nil || fn.Type.Params == nil {
				continue
			}
			count := countFieldList(fn.Type.Params)
			if count > limit {
				pos := fset.Position(fn.Pos())
				issues = append(issues, Issue{
					File:    path,
					Line:    pos.Line,
					Col:     pos.Column,
					Rule:    "ParamCount",
					Message: fmt.Sprintf("函数 %s 参数个数为 %d（阈值 %d）", formatFuncName(fn), count, limit),
					Score:   count,
				})
			}
		}
	}
	return issues, nil
}

func countFieldList(fl *ast.FieldList) int {
	total := 0
	for _, field := range fl.List {
		if len(field.Names) == 0 {
			total++
		} else {
			total += len(field.Names)
		}
	}
	return total
}

func formatFuncName(fn *ast.FuncDecl) string {
	if fn.Recv == nil || len(fn.Recv.List) == 0 {
		return fn.Name.Name
	}
	recv := recvTypeName(fn.Recv.List[0].Type)
	if recv == "" {
		return fn.Name.Name
	}
	return recv + "." + fn.Name.Name
}

func recvTypeName(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.StarExpr:
		return recvTypeName(t.X)
	case *ast.Ident:
		return t.Name
	case *ast.IndexExpr:
		return recvTypeName(t.X)
	case *ast.IndexListExpr:
		return recvTypeName(t.X)
	default:
		return ""
	}
}

// isPureUIEventIf reports whether an if statement is a bare immediate-mode UI
// event binding (e.g. `if btn.Clicked() { ... }`, `if field.Changed() { ... }`)
// without Init, Else, or compound boolean conditions, analogous to `onClick = { ... }`
// in declarative UI toolkits. Control flow inside its body is still scored normally.
func isPureUIEventIf(s *ast.IfStmt) bool {
	if s.Init != nil || s.Else != nil {
		return false
	}
	call, ok := s.Cond.(*ast.CallExpr)
	if !ok || len(call.Args) != 0 {
		return false
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	switch sel.Sel.Name {
	case "Clicked", "Changed", "LongPressed", "RightClicked":
		return true
	}
	return false
}

// calcCyclomatic computes standard McCabe cyclomatic complexity for a function.
func calcCyclomatic(fn *ast.FuncDecl) int {
	score := 1
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.IfStmt:
			if !isPureUIEventIf(node) {
				score++
			}
		case *ast.ForStmt, *ast.RangeStmt:
			score++
		case *ast.CaseClause:
			if node.List != nil {
				score++
			}
		case *ast.CommClause:
			if node.Comm != nil {
				score++
			}
		case *ast.BinaryExpr:
			if node.Op == token.LAND || node.Op == token.LOR {
				score++
			}
		}
		return true
	})
	return score
}

// calcCognitive computes Sonar-style cognitive complexity for a function.
func calcCognitive(fn *ast.FuncDecl) int {
	w := &cognitiveWalker{fnName: fn.Name.Name}
	w.walkStmtList(fn.Body.List, 0)
	return w.score
}

type cognitiveWalker struct {
	fnName string
	score  int
}

func (w *cognitiveWalker) walkStmtList(list []ast.Stmt, depth int) {
	for _, stmt := range list {
		w.walkStmt(stmt, depth)
	}
}

func (w *cognitiveWalker) walkStmt(stmt ast.Stmt, depth int) {
	if stmt == nil {
		return
	}
	switch s := stmt.(type) {
	case *ast.BlockStmt:
		w.walkStmtList(s.List, depth)
	case *ast.IfStmt:
		w.walkIf(s, depth, false)
	case *ast.ForStmt:
		w.score += 1 + depth
		w.walkExpr(s.Cond, depth+1)
		w.walkStmt(s.Body, depth+1)
	case *ast.RangeStmt:
		w.score += 1 + depth
		w.walkExpr(s.X, depth+1)
		w.walkStmt(s.Body, depth+1)
	case *ast.SwitchStmt:
		w.score += 1 + depth
		w.walkExpr(s.Tag, depth+1)
		w.walkStmt(s.Body, depth+1)
	case *ast.TypeSwitchStmt:
		w.score += 1 + depth
		w.walkStmt(s.Body, depth+1)
	case *ast.SelectStmt:
		w.score += 1 + depth
		w.walkStmt(s.Body, depth+1)
	case *ast.CaseClause:
		w.walkStmtList(s.Body, depth)
	case *ast.CommClause:
		w.walkStmtList(s.Body, depth)
	case *ast.BranchStmt:
		if s.Label != nil {
			w.score++
		}
	case *ast.LabeledStmt:
		w.walkStmt(s.Stmt, depth)
	case *ast.DeferStmt:
		w.walkExpr(s.Call, depth)
	case *ast.GoStmt:
		w.walkExpr(s.Call, depth)
	case *ast.ReturnStmt:
		for _, r := range s.Results {
			w.walkExpr(r, depth)
		}
	case *ast.ExprStmt:
		w.walkExpr(s.X, depth)
	case *ast.AssignStmt:
		for _, rhs := range s.Rhs {
			w.walkExpr(rhs, depth)
		}
	case *ast.DeclStmt:
		w.walkDecl(s.Decl, depth)
	}
}

func (w *cognitiveWalker) walkIf(s *ast.IfStmt, depth int, isElseIf bool) {
	if !isElseIf && isPureUIEventIf(s) {
		w.walkExpr(s.Cond, depth)
		w.walkStmt(s.Body, depth)
		return
	}
	if isElseIf {
		w.score++
	} else {
		w.score += 1 + depth
	}
	w.walkExpr(s.Cond, depth+1)
	w.walkStmt(s.Body, depth+1)
	if s.Else == nil {
		return
	}
	if nextIf, ok := s.Else.(*ast.IfStmt); ok {
		w.walkIf(nextIf, depth, true)
		return
	}
	w.score++
	w.walkStmt(s.Else, depth+1)
}

func (w *cognitiveWalker) walkDecl(decl ast.Decl, depth int) {
	gd, ok := decl.(*ast.GenDecl)
	if !ok {
		return
	}
	for _, spec := range gd.Specs {
		vs, ok := spec.(*ast.ValueSpec)
		if !ok {
			continue
		}
		for _, v := range vs.Values {
			w.walkExpr(v, depth)
		}
	}
}

func (w *cognitiveWalker) walkExpr(expr ast.Expr, depth int) {
	if expr == nil {
		return
	}
	w.score += countLogicalSequences(expr)
	w.walkExprNode(expr, depth)
}

// isDeclarativeUICall reports whether call is a MyGo declarative container builder
// (e.g. .Children(func()), ui.Overlay(c, func()), AnimatedDialog/AnimatedSheet)
// whose trailing func() only describes widget hierarchy rather than control flow.
func isDeclarativeUICall(call *ast.CallExpr) bool {
	switch fn := call.Fun.(type) {
	case *ast.SelectorExpr:
		switch fn.Sel.Name {
		case "Children", "Overlay", "AnimatedDialog", "AnimatedSheet", "Sheet":
			return true
		}
	case *ast.Ident:
		switch fn.Name {
		case "Children", "Overlay", "AnimatedDialog", "AnimatedSheet":
			return true
		}
	}
	return false
}

func (w *cognitiveWalker) walkExprNode(expr ast.Node, depth int) {
	if expr == nil {
		return
	}
	ast.Inspect(expr, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.CallExpr:
			if id, ok := node.Fun.(*ast.Ident); ok && id.Name == w.fnName {
				w.score++
			}
			if isDeclarativeUICall(node) {
				w.walkExprNode(node.Fun, depth)
				for _, arg := range node.Args {
					if lit, ok := arg.(*ast.FuncLit); ok && lit.Body != nil {
						w.walkStmtList(lit.Body.List, depth)
					} else {
						w.walkExprNode(arg, depth)
					}
				}
				return false
			}
		case *ast.FuncLit:
			if node.Body != nil {
				w.walkStmtList(node.Body.List, depth+1)
			}
			return false
		}
		return true
	})
}

func countLogicalSequences(expr ast.Expr) int {
	ops := collectBoolOps(expr)
	if len(ops) == 0 {
		return 0
	}
	count := 1
	for i := 1; i < len(ops); i++ {
		if ops[i] != ops[i-1] {
			count++
		}
	}
	return count
}

func collectBoolOps(expr ast.Expr) []token.Token {
	bin, ok := expr.(*ast.BinaryExpr)
	if !ok {
		if p, ok := expr.(*ast.ParenExpr); ok {
			return collectBoolOps(p.X)
		}
		return nil
	}
	if bin.Op != token.LAND && bin.Op != token.LOR {
		return nil
	}
	var out []token.Token
	out = append(out, collectBoolOps(bin.X)...)
	out = append(out, bin.Op)
	out = append(out, collectBoolOps(bin.Y)...)
	return out
}

// calcMaxNesting computes maximum control-flow block nesting depth (else-if chains stay flat).
func calcMaxNesting(fn *ast.FuncDecl) int {
	maxDepth := 0
	walkNestingStmts(fn.Body.List, 0, &maxDepth)
	return maxDepth
}

func walkNestingStmts(list []ast.Stmt, depth int, maxDepth *int) {
	for _, stmt := range list {
		walkNestingStmt(stmt, depth, maxDepth)
	}
}

func walkNestingStmt(stmt ast.Stmt, depth int, maxDepth *int) {
	if stmt == nil {
		return
	}
	bump := func(next int) {
		if next > *maxDepth {
			*maxDepth = next
		}
	}
	switch s := stmt.(type) {
	case *ast.BlockStmt:
		walkNestingStmts(s.List, depth, maxDepth)
	case *ast.IfStmt:
		walkNestingIf(s, depth, maxDepth)
	case *ast.ForStmt:
		bump(depth + 1)
		walkNestingStmts(s.Body.List, depth+1, maxDepth)
	case *ast.RangeStmt:
		bump(depth + 1)
		walkNestingStmts(s.Body.List, depth+1, maxDepth)
	case *ast.SwitchStmt:
		bump(depth + 1)
		walkNestingStmts(s.Body.List, depth+1, maxDepth)
	case *ast.TypeSwitchStmt:
		bump(depth + 1)
		walkNestingStmts(s.Body.List, depth+1, maxDepth)
	case *ast.SelectStmt:
		bump(depth + 1)
		walkNestingStmts(s.Body.List, depth+1, maxDepth)
	case *ast.CaseClause:
		walkNestingStmts(s.Body, depth, maxDepth)
	case *ast.CommClause:
		walkNestingStmts(s.Body, depth, maxDepth)
	case *ast.LabeledStmt:
		walkNestingStmt(s.Stmt, depth, maxDepth)
	}
}

func walkNestingIf(s *ast.IfStmt, depth int, maxDepth *int) {
	if depth+1 > *maxDepth {
		*maxDepth = depth + 1
	}
	walkNestingStmts(s.Body.List, depth+1, maxDepth)
	if s.Else == nil {
		return
	}
	if nextIf, ok := s.Else.(*ast.IfStmt); ok {
		walkNestingIf(nextIf, depth, maxDepth)
		return
	}
	walkNestingStmt(s.Else, depth+1, maxDepth)
}
