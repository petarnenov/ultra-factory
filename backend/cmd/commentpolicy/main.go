package main

import (
	"encoding/json"
	"fmt"
	"go/parser"
	"go/token"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

type comment struct {
	Start int `json:"start"`
	End   int `json:"end"`
	Lines int `json:"lines"`
	Long  int `json:"long"`
}

func main() {
	if len(os.Args) > 1 && os.Args[1] == "--stdin" {
		if err := runStdin(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func runStdin() error {
	source, err := io.ReadAll(os.Stdin)
	if err != nil {
		return err
	}
	comments, err := analyze(source)
	if err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(comments)
}

func run() (err error) {
	root := "."
	if len(os.Args) > 1 {
		root = os.Args[1]
	}
	files := make(map[string][]comment)
	rootFS, err := os.OpenRoot(root)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := rootFS.Close(); err == nil {
			err = closeErr
		}
	}()
	err = filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			switch entry.Name() {
			case ".git", "vendor", "node_modules", "gen", "dist", "out":
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) != ".go" {
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		file, err := rootFS.Open(relative)
		if err != nil {
			return err
		}
		source, err := io.ReadAll(file)
		closeErr := file.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
		if strings.HasPrefix(strings.TrimSpace(string(source)), "// Code generated ") {
			return nil
		}
		comments, err := analyze(source)
		if err != nil {
			return fmt.Errorf("parse %s: %w", path, err)
		}
		files[relative] = comments
		return nil
	})
	if err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(files)
}

func analyze(source []byte) ([]comment, error) {
	set := token.NewFileSet()
	tree, err := parser.ParseFile(set, "source.go", source, parser.ParseComments)
	if err != nil {
		return nil, err
	}
	lineOffsets := []int{0}
	for i, value := range source {
		if value == '\n' {
			lineOffsets = append(lineOffsets, i+1)
		}
	}
	comments := make([]comment, 0, len(tree.Comments))
	for _, group := range tree.Comments {
		start, end := 0, 0
		flush := func() {
			if start == 0 {
				return
			}
			item := comment{Start: start, End: end, Lines: end - start + 1}
			for line := start; line <= end; line++ {
				from := lineOffsets[line-1]
				to := len(source)
				if line < len(lineOffsets) {
					to = lineOffsets[line] - 1
				}
				if utf8.RuneCount(source[from:to]) > 80 {
					item.Long++
				}
			}
			comments = append(comments, item)
			start, end = 0, 0
		}
		for _, part := range group.List {
			if exempt(part.Text) {
				flush()
				continue
			}
			partStart := set.Position(part.Pos()).Line
			partEnd := set.Position(part.End()).Line
			if start == 0 {
				start = partStart
			}
			end = partEnd
		}
		flush()
	}
	return comments, nil
}

func exempt(text string) bool {
	trimmed := strings.TrimSpace(strings.TrimLeft(text, "/*"))
	for _, prefix := range []string{"//go:", "// +build", "//go:build", "// Copyright", "//nolint:", "// lint:ignore ", "/* Copyright", "/* SPDX-License-Identifier:"} {
		if strings.HasPrefix(strings.TrimSpace(text), prefix) {
			return true
		}
	}
	return strings.Contains(trimmed, "SPDX-License-Identifier:")
}
