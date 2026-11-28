// Command gocheck implements zero-dependency Go AST quality gates for StillPoets Woodpecker CI.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type Config struct {
	Roots           []string `json:"roots"`
	ExcludeSuffixes []string `json:"excludeSuffixes"`
	FileLength      int      `json:"fileLength"`
	Cyclomatic      int      `json:"cyclomatic"`
	Cognitive       int      `json:"cognitive"`
	Nesting         int      `json:"nesting"`
	Params          int      `json:"params"`
}

type Issue struct {
	File    string
	Line    int
	Col     int
	Rule    string
	Message string
	Score   int
}

func main() {
	cfgPath := flag.String("config", "quality/go-quality.json", "path to go quality config json")
	flag.Parse()
	if flag.NArg() < 1 {
		fmt.Fprintln(os.Stderr, "usage: gocheck [--config path] <file-length|cyclomatic|cognitive|nesting|params|dead-code>")
		os.Exit(2)
	}
	check := flag.Arg(0)

	cfg, err := loadConfig(*cfgPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "load config %s: %v\n", *cfgPath, err)
		os.Exit(2)
	}

	issues, err := runCheck(check, cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "gocheck %s failed: %v\n", check, err)
		os.Exit(2)
	}

	sort.Slice(issues, func(i, j int) bool {
		if issues[i].Score != issues[j].Score {
			return issues[i].Score > issues[j].Score
		}
		if issues[i].File != issues[j].File {
			return issues[i].File < issues[j].File
		}
		return issues[i].Line < issues[j].Line
	})

	for _, iss := range issues {
		fmt.Printf("%s:%d:%d: %s [%s]\n", iss.File, iss.Line, iss.Col, iss.Message, iss.Rule)
	}
	if len(issues) > 0 {
		fmt.Printf("\n共发现 %d 处违规 [%s]\n", len(issues), check)
		os.Exit(1)
	}
	fmt.Printf("通过：未发现违规 [%s]\n", check)
}

func loadConfig(path string) (Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	var cfg Config
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func runCheck(check string, cfg Config) ([]Issue, error) {
	if check == "dead-code" {
		return checkDeadCode(cfg)
	}
	files, err := collectGoFiles(cfg, false)
	if err != nil {
		return nil, err
	}
	switch check {
	case "file-length":
		return checkFileLength(files, cfg.FileLength)
	case "cyclomatic":
		return checkFuncMetric(files, "CyclomaticComplexity", cfg.Cyclomatic, calcCyclomatic)
	case "cognitive":
		return checkFuncMetric(files, "CognitiveComplexity", cfg.Cognitive, calcCognitive)
	case "nesting":
		return checkFuncMetric(files, "NestedBlockDepth", cfg.Nesting, calcMaxNesting)
	case "params":
		return checkFuncParams(files, cfg.Params)
	default:
		return nil, fmt.Errorf("unknown check %q", check)
	}
}

func collectGoFiles(cfg Config, includeTests bool) ([]string, error) {
	seen := map[string]bool{}
	var out []string
	for _, root := range cfg.Roots {
		info, err := os.Stat(root)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
		if !info.IsDir() {
			if shouldIncludeFile(root, cfg.ExcludeSuffixes, includeTests) && !seen[root] {
				seen[root] = true
				out = append(out, filepath.ToSlash(root))
			}
			continue
		}
		err = filepath.WalkDir(root, func(p string, d fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if d.IsDir() {
				name := d.Name()
				if strings.HasPrefix(name, ".") || name == "vendor" || name == "build" {
					return filepath.SkipDir
				}
				return nil
			}
			clean := filepath.ToSlash(p)
			if shouldIncludeFile(clean, cfg.ExcludeSuffixes, includeTests) && !seen[clean] {
				seen[clean] = true
				out = append(out, clean)
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	sort.Strings(out)
	return out, nil
}

func shouldIncludeFile(path string, excludeSuffixes []string, includeTests bool) bool {
	if !strings.HasSuffix(path, ".go") {
		return false
	}
	if includeTests {
		return true
	}
	for _, suf := range excludeSuffixes {
		if strings.HasSuffix(path, suf) {
			return false
		}
	}
	return true
}

func parseGoFile(fset *token.FileSet, path string) (*ast.File, error) {
	return parser.ParseFile(fset, path, nil, 0)
}
