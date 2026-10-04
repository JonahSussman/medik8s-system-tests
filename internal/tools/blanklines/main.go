package main

import (
	"context"
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"os"
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

func run(ctx context.Context, patterns []string) error {
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

			for _, position := range checkFile(pkg.Fset, file, pkg.TypesInfo, source) {
				fmt.Fprintf(os.Stderr, "%s: remove blank line between assignment and delete on the same variable\n", position)

				found = true
			}
		}
	}

	if found {
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
			if !assignmentThenDelete(first, second, info) || hasCommentBetween(file, first.End(), second.Pos()) {
				continue
			}

			start := fset.PositionFor(first.End(), false)
			end := fset.PositionFor(second.Pos(), false)

			gapLines := strings.Split(string(source[start.Offset:end.Offset]), "\n")
			for offset := 1; offset+1 < len(gapLines); offset++ {
				if strings.TrimSpace(gapLines[offset]) == "" {
					position := fset.File(first.Pos()).LineStart(start.Line + offset)
					findings = append(findings, fset.PositionFor(position, false))

					break
				}
			}
		}

		return true
	})

	return findings
}

func assignmentThenDelete(first, second ast.Stmt, info *types.Info) bool {
	assignment, isAssignment := first.(*ast.AssignStmt)
	if !isAssignment {
		return false
	}

	expression, isExpression := second.(*ast.ExprStmt)
	if !isExpression {
		return false
	}

	call, isCall := expression.X.(*ast.CallExpr)
	if !isCall || len(call.Args) == 0 {
		return false
	}

	function, isFunction := call.Fun.(*ast.Ident)
	if !isFunction {
		return false
	}

	builtin, isBuiltin := info.Uses[function].(*types.Builtin)
	if !isBuiltin || builtin.Name() != "delete" {
		return false
	}

	target, isTarget := call.Args[0].(*ast.Ident)
	if !isTarget {
		return false
	}

	variable, isVariable := info.Uses[target].(*types.Var)
	if !isVariable {
		return false
	}

	for _, left := range assignment.Lhs {
		identifier, isIdentifier := left.(*ast.Ident)
		if isIdentifier && info.ObjectOf(identifier) == variable {
			return true
		}
	}

	return false
}

func hasCommentBetween(file *ast.File, start, end token.Pos) bool {
	for _, group := range file.Comments {
		if group.Pos() < end && group.End() > start {
			return true
		}
	}

	return false
}
