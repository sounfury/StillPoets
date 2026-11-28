// Package main checks package-private unused declarations (functions, types, vars, consts).
package main

import (
	"fmt"
	"go/ast"
	"go/token"
	"path/filepath"
	"unicode"
)

type declSymbol struct {
	name   string
	kind   string
	file   string
	line   int
	col    int
	pos    token.Pos
	isTest bool
}

func checkDeadCode(cfg Config) ([]Issue, error) {
	allFiles, err := collectGoFiles(cfg, true)
	if err != nil {
		return nil, err
	}
	pkgFiles := map[string][]string{}
	for _, f := range allFiles {
		dir := filepath.Dir(f)
		pkgFiles[dir] = append(pkgFiles[dir], f)
	}

	var issues []Issue
	for _, files := range pkgFiles {
		pkgIssues, err := checkPackageDeadCode(files, cfg.ExcludeSuffixes)
		if err != nil {
			return nil, err
		}
		issues = append(issues, pkgIssues...)
	}
	return issues, nil
}

func checkPackageDeadCode(files []string, excludeSuffixes []string) ([]Issue, error) {
	fset := token.NewFileSet()
	var parsed []*ast.File
	var decls []declSymbol
	declPos := map[token.Pos]bool{}

	for _, path := range files {
		af, err := parseGoFile(fset, path)
		if err != nil {
			return nil, err
		}
		parsed = append(parsed, af)
		isTest := !shouldIncludeFile(path, excludeSuffixes, false)
		collectPackageDecls(fset, path, af, isTest, &decls, declPos)
	}

	used := map[string]bool{}
	for _, af := range parsed {
		ast.Inspect(af, func(n ast.Node) bool {
			switch node := n.(type) {
			case *ast.Ident:
				if !declPos[node.Pos()] {
					used[node.Name] = true
				}
			case *ast.SelectorExpr:
				used[node.Sel.Name] = true
			}
			return true
		})
	}

	var issues []Issue
	for _, d := range decls {
		if d.isTest || used[d.name] {
			continue
		}
		issues = append(issues, Issue{
			File:    d.file,
			Line:    d.line,
			Col:     d.col,
			Rule:    "UnusedPrivateDecl",
			Message: fmt.Sprintf("未使用的私有%s %s", d.kind, d.name),
			Score:   1,
		})
	}
	return issues, nil
}

func collectPackageDecls(fset *token.FileSet, path string, af *ast.File, isTest bool, decls *[]declSymbol, declPos map[token.Pos]bool) {
	for _, d := range af.Decls {
		switch decl := d.(type) {
		case *ast.FuncDecl:
			name := decl.Name.Name
			if isUnexportedCheckable(name) && name != "init" && name != "main" {
				pos := fset.Position(decl.Name.Pos())
				declPos[decl.Name.Pos()] = true
				kind := "函数"
				if decl.Recv != nil {
					kind = "方法"
				}
				*decls = append(*decls, declSymbol{
					name: name, kind: kind, file: path,
					line: pos.Line, col: pos.Column, pos: decl.Name.Pos(), isTest: isTest,
				})
			}
		case *ast.GenDecl:
			collectGenDecls(fset, path, decl, isTest, decls, declPos)
		}
	}
}

func collectGenDecls(fset *token.FileSet, path string, decl *ast.GenDecl, isTest bool, decls *[]declSymbol, declPos map[token.Pos]bool) {
	for _, spec := range decl.Specs {
		switch s := spec.(type) {
		case *ast.TypeSpec:
			if isUnexportedCheckable(s.Name.Name) {
				pos := fset.Position(s.Name.Pos())
				declPos[s.Name.Pos()] = true
				*decls = append(*decls, declSymbol{
					name: s.Name.Name, kind: "类型", file: path,
					line: pos.Line, col: pos.Column, pos: s.Name.Pos(), isTest: isTest,
				})
			}
		case *ast.ValueSpec:
			kind := "变量"
			if decl.Tok == token.CONST {
				kind = "常量"
			}
			for _, id := range s.Names {
				if isUnexportedCheckable(id.Name) {
					pos := fset.Position(id.Pos())
					declPos[id.Pos()] = true
					*decls = append(*decls, declSymbol{
						name: id.Name, kind: kind, file: path,
						line: pos.Line, col: pos.Column, pos: id.Pos(), isTest: isTest,
					})
				}
			}
		}
	}
}

func isUnexportedCheckable(name string) bool {
	if name == "" || name == "_" {
		return false
	}
	for _, r := range name {
		return unicode.IsLower(r)
	}
	return false
}
