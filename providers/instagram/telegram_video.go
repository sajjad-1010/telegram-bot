package instagram

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"telegram-bot/config"
)

func NormalizeForTelegram(inputPath string) (outputPath string, cleanup func(), err error) {
	ffmpegPath, err := findFFmpeg()
	if err != nil {
		return "", func() {}, err
	}

	tmpDir, err := os.MkdirTemp("", "tg-normalize-*")
	if err != nil {
		return "", func() {}, fmt.Errorf("create normalize temp dir: %w", err)
	}

	cleanup = func() {
		_ = os.RemoveAll(tmpDir)
	}

	outputPath = filepath.Join(tmpDir, "normalized.mp4")
	cmd := exec.Command(
		ffmpegPath,
		"-y",
		"-i", inputPath,
		"-c:v", "libx264",
		"-pix_fmt", "yuv420p",
		"-profile:v", "high",
		"-level", "4.1",
		"-movflags", "+faststart",
		"-c:a", "aac",
		"-b:a", "128k",
		"-ac", "2",
		outputPath,
	)

	out, runErr := cmd.CombinedOutput()
	if runErr != nil {
		cleanup()
		return "", func() {}, fmt.Errorf("ffmpeg normalize failed: %w (%s)", runErr, strings.TrimSpace(string(out)))
	}

	return outputPath, cleanup, nil
}

func findFFmpeg() (string, error) {
	customBin := strings.TrimSpace(config.GetEnv("FFMPEG_BIN", ""))
	if customBin != "" {
		return customBin, nil
	}

	candidates := []string{"ffmpeg", "ffmpeg.exe"}
	for _, c := range candidates {
		if _, err := exec.LookPath(c); err == nil {
			return c, nil
		}
	}

	return "", errors.New("ffmpeg executable not found (set FFMPEG_BIN or add ffmpeg to PATH)")
}
