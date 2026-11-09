package parse

import (
	"bufio"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var emailRegex = regexp.MustCompile(`(?i)[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}`)

func TestTestData_PrivacyAudit(t *testing.T) {
	testdataDir := filepath.Join("..", "..", "testdata")

	// 1. Check for any denylisted names from external denylist file (if configured via env var)
	denylistPath := os.Getenv("DENYLIST_FILE")
	if denylistPath == "" {
		denylistPath = os.Getenv("BADGEBOARD_DENYLIST_FILE")
	}

	var denylist []string
	if denylistPath != "" {
		f, err := os.Open(denylistPath)
		if err != nil {
			t.Fatalf("failed to open denylist file %q: %v", denylistPath, err)
		}
		defer f.Close()

		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line != "" && !strings.HasPrefix(line, "#") {
				denylist = append(denylist, line)
			}
		}
	}

	err := filepath.WalkDir(testdataDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}

		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		content := string(data)

		// Check 2: No email address should appear anywhere in testdata/
		if matches := emailRegex.FindAllString(content, -1); len(matches) > 0 {
			t.Errorf("privacy violation: found email address(es) %v in fixture %s", matches, path)
		}

		// Check 1: Denylist matches
		for _, forbidden := range denylist {
			if strings.Contains(strings.ToLower(content), strings.ToLower(forbidden)) {
				t.Errorf("privacy violation: found denylisted string %q in fixture %s", forbidden, path)
			}
		}

		return nil
	})

	if err != nil {
		t.Fatalf("walking testdata directory: %v", err)
	}
}
