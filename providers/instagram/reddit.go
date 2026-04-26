package instagram

import (
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"telegram-bot/config"
)

func downloadRedditContents(link string) (filePaths []string, cleanup func(), err error) {
	tmpDir, err := os.MkdirTemp("", "reddit-dl-*")
	if err != nil {
		return nil, func() {}, fmt.Errorf("create reddit temp dir: %w", err)
	}

	cleanup = func() {
		_ = os.RemoveAll(tmpDir)
	}

	timeout := getRedditTimeoutSeconds()

	reddownloaderDir := filepath.Join(tmpDir, "reddownloader")
	if err := os.MkdirAll(reddownloaderDir, 0o755); err != nil {
		return nil, cleanup, fmt.Errorf("create reddownloader dir: %w", err)
	}

	filePaths, err = downloadWithRedditPython("reddownloader", link, reddownloaderDir, timeout)
	if err == nil && len(filePaths) > 0 {
		log.Printf("reddit downloader=RedDownloader files=%d", len(filePaths))
		return validateDownloadedFiles(filePaths, cleanup)
	}
	if err != nil {
		log.Printf("RedDownloader primary failed for %s: %v", link, err)
	}

	redditJSONDir := filepath.Join(tmpDir, "reddit-json")
	if err := os.MkdirAll(redditJSONDir, 0o755); err != nil {
		return nil, cleanup, fmt.Errorf("create reddit-json dir: %w", err)
	}

	filePaths, redditJSONErr := downloadWithRedditPython("reddit-json", link, redditJSONDir, timeout)
	if redditJSONErr == nil && len(filePaths) > 0 {
		log.Printf("reddit downloader=reddit-json files=%d", len(filePaths))
		return validateDownloadedFiles(filePaths, cleanup)
	}
	if redditJSONErr != nil {
		log.Printf("reddit-json fallback failed for %s: %v", link, redditJSONErr)
	}

	redditMediaDir := filepath.Join(tmpDir, "reddit-media")
	if err := os.MkdirAll(redditMediaDir, 0o755); err != nil {
		return nil, cleanup, fmt.Errorf("create reddit-media dir: %w", err)
	}

	filePaths, redditMediaErr := downloadWithRedditPython("reddit-media", link, redditMediaDir, timeout)
	if redditMediaErr == nil && len(filePaths) > 0 {
		log.Printf("reddit downloader=reddit-media files=%d", len(filePaths))
		return validateDownloadedFiles(filePaths, cleanup)
	}
	if redditMediaErr != nil {
		log.Printf("reddit-media fallback failed for %s: %v", link, redditMediaErr)
	}

	prawDir := filepath.Join(tmpDir, "praw")
	if err := os.MkdirAll(prawDir, 0o755); err != nil {
		return nil, cleanup, fmt.Errorf("create praw dir: %w", err)
	}

	filePaths, prawErr := downloadWithRedditPython("praw", link, prawDir, timeout)
	if prawErr == nil && len(filePaths) > 0 {
		log.Printf("reddit downloader=praw files=%d", len(filePaths))
		return validateDownloadedFiles(filePaths, cleanup)
	}
	if prawErr != nil {
		log.Printf("praw fallback failed for %s: %v", link, prawErr)
	}

	if err != nil {
		return nil, cleanup, fmt.Errorf("RedDownloader failed: %w; reddit-json failed: %v; reddit-media failed: %v; praw failed: %v", err, redditJSONErr, redditMediaErr, prawErr)
	}
	return nil, cleanup, fmt.Errorf("RedDownloader produced no files; reddit-json failed: %v; reddit-media failed: %v; praw failed: %v", redditJSONErr, redditMediaErr, prawErr)
}

func downloadRedditSingle(link string) (filePath string, cleanup func(), err error) {
	filePaths, cleanup, err := downloadRedditContents(link)
	if err != nil {
		return "", cleanup, err
	}
	if len(filePaths) != 1 {
		return "", cleanup, fmt.Errorf("expected a single reddit file, got %d", len(filePaths))
	}
	return filePaths[0], cleanup, nil
}

func downloadWithRedditPython(backend, link, tmpDir string, timeoutSeconds int) ([]string, error) {
	scriptPath, err := resolveRedditFallbackScript()
	if err != nil {
		return nil, err
	}

	cmd, err := newRedditPythonCommand(scriptPath, backend, link, tmpDir)
	if err != nil {
		return nil, err
	}

	rawOut, err := runCommandWithTimeout(cmd, timeoutSeconds)
	if err != nil {
		return nil, fmt.Errorf("%s failed: %w", backend, err)
	}

	filePaths := collectDownloadedFilesRecursive(tmpDir)
	if len(filePaths) == 0 {
		return nil, fmt.Errorf("%s produced no files (raw_output=%q)", backend, trimForLog(string(rawOut), 400))
	}
	return filePaths, nil
}

func resolveRedditFallbackScript() (string, error) {
	customPath := strings.TrimSpace(config.GetEnv("REDDIT_FALLBACK_SCRIPT", ""))
	candidates := []string{}
	if customPath != "" {
		candidates = append(candidates, customPath)
	}
	candidates = append(candidates,
		filepath.Join("providers", "instagram", "reddit_fallback.py"),
		filepath.Join(filepath.Dir(os.Args[0]), "providers", "instagram", "reddit_fallback.py"),
	)

	for _, candidate := range candidates {
		if strings.TrimSpace(candidate) == "" {
			continue
		}
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate, nil
		}
	}

	return "", errors.New("reddit fallback script not found")
}

func newRedditPythonCommand(scriptPath string, args ...string) (*exec.Cmd, error) {
	for _, key := range []string{"REDDIT_PYTHON_BIN", "INSTAGRAM_PYTHON_BIN"} {
		customBin := strings.TrimSpace(config.GetEnv(key, ""))
		if customBin == "" {
			continue
		}
		fullArgs := append([]string{scriptPath}, args...)
		return exec.Command(customBin, fullArgs...), nil
	}

	candidates := [][]string{
		{"py"},
		{"python"},
		{"python3"},
	}

	for _, c := range candidates {
		if _, err := exec.LookPath(c[0]); err == nil {
			fullArgs := append(c[1:], scriptPath)
			fullArgs = append(fullArgs, args...)
			return exec.Command(c[0], fullArgs...), nil
		}
	}

	return nil, errors.New("python executable not found (set REDDIT_PYTHON_BIN or add py/python to PATH)")
}

func getRedditTimeoutSeconds() int {
	if timeout := getIntEnv("REDDIT_DOWNLOAD_TIMEOUT_SECONDS", 0); timeout > 0 {
		return timeout
	}
	return getIntEnv("INSTAGRAM_DOWNLOAD_TIMEOUT_SECONDS", defaultTimeoutSeconds)
}

func isRedditLink(link string) bool {
	lowerLink := strings.ToLower(strings.TrimSpace(link))
	return strings.Contains(lowerLink, "reddit.com/") ||
		strings.Contains(lowerLink, "redd.it/") ||
		strings.Contains(lowerLink, "v.redd.it/") ||
		strings.Contains(lowerLink, "i.redd.it/")
}
