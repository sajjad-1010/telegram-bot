package instagram

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"telegram-bot/config"
	"time"
)

const (
	defaultTimeoutSeconds = 120
	defaultMaxFileSizeMB  = 49
)

func Download(link string) (filePath string, cleanup func(), err error) {
	tmpDir, err := os.MkdirTemp("", "insta-dl-*")
	if err != nil {
		return "", func() {}, fmt.Errorf("create temp dir: %w", err)
	}

	cleanup = func() {
		_ = os.RemoveAll(tmpDir)
	}

	timeout := getIntEnv("INSTAGRAM_DOWNLOAD_TIMEOUT_SECONDS", defaultTimeoutSeconds)
	maxMB := getIntEnv("INSTAGRAM_MAX_FILE_SIZE_MB", defaultMaxFileSizeMB)
	if maxMB <= 0 {
		maxMB = defaultMaxFileSizeMB
	}
	maxBytes := int64(maxMB) * 1024 * 1024

	outputTemplate := filepath.Join(tmpDir, "%(id)s.%(ext)s")
	cmdArgs := []string{
		"--no-playlist",
		"--no-warnings",
		"--restrict-filenames",
		"-f", "mp4/best",
		"--merge-output-format", "mp4",
		"-o", outputTemplate,
		"--print", "after_move:filepath",
		link,
	}
	cmd, err := newYTDLPCommand(cmdArgs...)
	if err != nil {
		return "", cleanup, err
	}

	done := make(chan error, 1)
	var rawOut []byte
	go func() {
		rawOut, err = cmd.CombinedOutput()
		done <- err
	}()

	select {
	case runErr := <-done:
		if runErr != nil {
			return "", cleanup, fmt.Errorf("yt-dlp failed: %w (%s)", runErr, strings.TrimSpace(string(rawOut)))
		}
	case <-time.After(time.Duration(timeout) * time.Second):
		_ = cmd.Process.Kill()
		return "", cleanup, errors.New("yt-dlp timed out")
	}

	lines := strings.Split(strings.TrimSpace(string(rawOut)), "\n")
	if len(lines) == 0 {
		return "", cleanup, errors.New("yt-dlp produced no output path")
	}

	filePath = strings.TrimSpace(lines[len(lines)-1])
	if filePath == "" {
		return "", cleanup, errors.New("yt-dlp output path is empty")
	}

	stat, err := os.Stat(filePath)
	if err != nil {
		return "", cleanup, fmt.Errorf("downloaded file stat failed: %w", err)
	}
	if stat.Size() > maxBytes {
		return "", cleanup, fmt.Errorf("file too large: %d bytes (max %d)", stat.Size(), maxBytes)
	}

	return filePath, cleanup, nil
}

func getIntEnv(key string, fallback int) int {
	raw := strings.TrimSpace(config.GetEnv(key, ""))
	if raw == "" {
		return fallback
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	return v
}

func newYTDLPCommand(args ...string) (*exec.Cmd, error) {
	customBin := strings.TrimSpace(config.GetEnv("INSTAGRAM_YTDLP_BIN", ""))
	if customBin != "" {
		return exec.Command(customBin, args...), nil
	}

	candidates := [][]string{
		{"yt-dlp"},
		{"yt-dlp.exe"},
		{"py", "-m", "yt_dlp"},
		{"python", "-m", "yt_dlp"},
		{"python3", "-m", "yt_dlp"},
	}

	for _, c := range candidates {
		if _, err := exec.LookPath(c[0]); err == nil {
			fullArgs := append(c[1:], args...)
			return exec.Command(c[0], fullArgs...), nil
		}
	}

	return nil, errors.New("yt-dlp executable not found (set INSTAGRAM_YTDLP_BIN or add yt-dlp/py/python to PATH)")
}
