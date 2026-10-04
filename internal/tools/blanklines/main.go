package main

import (
	"context"
	"flag"
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"os"
	"sort"
	"strings"
	"time"

	"golang.org/x/tools/go/packages"
)

const (
	packageLoadTimeout = 5 * time.Minute
	failureExitCode    = 1
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), packageLoadTimeout)
	err := run(ctx, os.Args[1:])
	cancel()

	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(failureExitCode)
	}
}

func run(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("blanklines", flag.ContinueOnError)
	fix := flags.Bool("fix", false, "remove blank lines between consecutive simple statements")
	if err := flags.Parse(args); err != nil {
		return fmt.Errorf("parse blank-line checker arguments: %w", err)
	}

	patterns := flags.Args()
	if len(patterns) == 0 {
		patterns = []string{"./..."}
	}

	loaded, err := packages.Load(&packages.Config{Context: ctx, Mode: packages.LoadSyntax, Tests: true}, patterns...)
	if err != nil {
		return fmt.Errorf("load packages for blank-line checks: %w", err)
	}

	if packages.PrintErrors(loaded) != 0 {
		return fmt.Errorf("cannot check blank lines in packages with loading errors")
	}

	if len(loaded) == 0 {
		return fmt.Errorf("no packages matched %v", patterns)
	}

	seen := make(map[string]bool)
	found := false

	for _, pkg := range loaded {
		for _, file := range pkg.Syntax {
			filename := pkg.Fset.PositionFor(file.Pos(), false).Filename
			if seen[filename] || ast.IsGenerated(file) {
				continue
			}

			seen[filename] = true
			source, readErr := os.ReadFile(filename)
			if readErr != nil {
				return fmt.Errorf("read %s for blank-line checks: %w", filename, readErr)
			}

			findings := checkFile(pkg.Fset, file, pkg.TypesInfo, source)
			if len(findings) == 0 {
				continue
			}

			found = true
			if *fix {
				if err := writeFixedFile(filename, source, findings); err != nil {
					return err
				}

				fmt.Fprintf(os.Stderr, "%s: removed %d redundant blank lines\n", filename, len(findings))

				continue
			}

			for _, position := range findings {
				fmt.Fprintf(os.Stderr, "%s: remove blank line between consecutive simple statements\n", position)
			}
		}
	}

	if found && !*fix {
		return fmt.Errorf("blank-line checks failed")
	}

	return nil
}

func checkFile(fset *token.FileSet, file *ast.File, info *types.Info, source []byte) []token.Position {
	var findings []token.Position

	ast.Inspect(file, func(node ast.Node) bool {
		var statements []ast.Stmt

		switch block := node.(type) {
		case *ast.BlockStmt:
			statements = block.List
		case *ast.CaseClause:
			statements = block.Body
		case *ast.CommClause:
			statements = block.Body
		default:
			return true
		}

		for index := 0; index+1 < len(statements); index++ {
			first, second := statements[index], statements[index+1]
			if !simpleStatement(first, info) || !simpleStatement(second, info) ||
				hasCommentBetween(file, first.End(), second.Pos()) {
				continue
			}

			start := fset.PositionFor(first.End(), false)
			end := fset.PositionFor(second.Pos(), false)
			gapLines := strings.Split(string(source[start.Offset:end.Offset]), "\n")
			for offset := 1; offset+1 < len(gapLines); offset++ {
				if strings.TrimSpace(gapLines[offset]) == "" {
					position := fset.File(first.Pos()).LineStart(start.Line + offset)
					findings = append(findings, fset.PositionFor(position, false))
				}
			}
		}

		return true
	})

	return findings
}

func simpleStatement(statement ast.Stmt, info *types.Info) bool {
	containsCallback := false
	ast.Inspect(statement, func(node ast.Node) bool {
		if _, isCallback := node.(*ast.FuncLit); isCallback {
			containsCallback = true
		}

		return !containsCallback
	})
	if containsCallback {
		return false
	}

	switch statement := statement.(type) {
	case *ast.AssignStmt, *ast.IncDecStmt:
		return true
	case *ast.ExprStmt:
		call, isCall := statement.X.(*ast.CallExpr)

		return isCall && !ginkgoStep(call, info)
	default:
		return false
	}
}

func ginkgoStep(call *ast.CallExpr, info *types.Info) bool {
	var identifier *ast.Ident

	switch function := ast.Unparen(call.Fun).(type) {
	case *ast.Ident:
		identifier = function
	case *ast.SelectorExpr:
		identifier = function.Sel
	default:
		return false
	}

	object := info.Uses[identifier]

	return object != nil && object.Pkg() != nil &&
		object.Pkg().Path() == "github.com/onsi/ginkgo/v2" && object.Name() == "By"
}

func writeFixedFile(filename string, source []byte, findings []token.Position) error {
	stat, err := os.Stat(filename)
	if err != nil {
		return fmt.Errorf("stat %s before fixing blank lines: %w", filename, err)
	}

	if err := os.WriteFile(filename, removeBlankLines(source, findings), stat.Mode().Perm()); err != nil {
		return fmt.Errorf("fix blank lines in %s: %w", filename, err)
	}

	return nil
}

func removeBlankLines(source []byte, findings []token.Position) []byte {
	sort.Slice(findings, func(first, second int) bool {
		return findings[first].Offset > findings[second].Offset
	})

	fixed := append([]byte(nil), source...)
	for _, position := range findings {
		end := position.Offset + strings.IndexByte(string(fixed[position.Offset:]), '\n') + 1
		fixed = append(fixed[:position.Offset], fixed[end:]...)
	}

	return fixed
}

func hasCommentBetween(file *ast.File, start, end token.Pos) bool {
	for _, group := range file.Comments {
		if group.Pos() < end && group.End() > start {
			return true
		}
	}

	return false
}
