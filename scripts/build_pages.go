package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

func main() {
	assetsDir := filepath.Join("internal", "web", "assets")
	docsDir := "docs"

	if err := os.MkdirAll(docsDir, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "failed to create docs dir: %v\n", err)
		os.Exit(1)
	}

	// Copy all files and directories from internal/web/assets to docs
	// excluding test files
	entries, err := os.ReadDir(assetsDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to read assets dir: %v\n", err)
		os.Exit(1)
	}

	for _, entry := range entries {
		name := entry.Name()
		if name == "frontend.test.js" || name == "csv_export.test.js" {
			continue
		}
		srcPath := filepath.Join(assetsDir, name)
		destPath := filepath.Join(docsDir, name)

		if entry.IsDir() {
			if err := copyDir(srcPath, destPath); err != nil {
				fmt.Fprintf(os.Stderr, "failed copying dir %s: %v\n", name, err)
				os.Exit(1)
			}
		} else {
			if err := copyFile(srcPath, destPath); err != nil {
				fmt.Fprintf(os.Stderr, "failed copying file %s: %v\n", name, err)
				os.Exit(1)
			}
		}
	}

	// Ensure any stale test files are removed from docs
	_ = os.Remove(filepath.Join(docsDir, "frontend.test.js"))
	_ = os.Remove(filepath.Join(docsDir, "csv_export.test.js"))

	fmt.Println("Synced internal/web/assets -> docs/ for GitHub Pages successfully.")
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	return err
}

func copyDir(src, dst string) error {
	if err := os.MkdirAll(dst, 0755); err != nil {
		return err
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		s := filepath.Join(src, entry.Name())
		d := filepath.Join(dst, entry.Name())
		if entry.IsDir() {
			if err := copyDir(s, d); err != nil {
				return err
			}
		} else {
			if err := copyFile(s, d); err != nil {
				return err
			}
		}
	}
	return nil
}
