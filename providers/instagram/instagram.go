package instagram

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"telegram-bot/config"
	"time"
)

const (
	defaultTimeoutSeconds = 120
	defaultMaxFileSizeMB  = 2048
)

type MediaProbe struct {
	HasVideo  bool
	HasAudio  bool
	ItemCount int
}

type VideoQualityOption struct {
	Key               string
	Label             string
	Selector          string
	EstimatedBytes    int64
	EstimatedMP3Bytes int64
}

func Download(link string) (filePath string, cleanup func(), err error) {
	return DownloadVideoMP4(link)
}

func DownloadBestContents(link string) (filePaths []string, cleanup func(), err error) {
	if isInstagramLink(link) {
		return downloadGalleryDLContents(link)
	}
	if isRedditLink(link) {
		return downloadRedditContents(link)
	}
	return downloadManyWithExtraArgs(link)
}

func DownloadBestContent(link string) (filePath string, cleanup func(), err error) {
	if isInstagramLink(link) {
		return downloadGalleryDLSingle(link)
	}
	if isRedditLink(link) {
		return downloadRedditSingle(link)
	}
	return downloadSingleWithExtraArgs(link)
}

func DownloadVideoMP4(link string) (filePath string, cleanup func(), err error) {
	return DownloadVideoBySelector(link, "")
}

func DownloadVideoBySelector(link, selector string) (filePath string, cleanup func(), err error) {
	if strings.TrimSpace(selector) == "" {
		selector = "mp4/best"
	}
	return downloadSingleWithExtraArgs(
		link,
		"-f", selector,
		"--merge-output-format", "mp4",
	)
}

func DownloadMP3(link string) (filePath string, cleanup func(), err error) {
	extraArgs, err := buildMP3DownloadArgs()
	if err != nil {
		return "", func() {}, err
	}
	return downloadSingleWithExtraArgs(link, extraArgs...)
}

type instagramAudioResult struct {
	Path  string `json:"path"`
	Title string `json:"title"`
}

func downloadGalleryDLContents(link string) (filePaths []string, cleanup func(), err error) {
	tmpDir, err := os.MkdirTemp("", "media-dl-*")
	if err != nil {
		return nil, func() {}, fmt.Errorf("create temp dir: %w", err)
	}

	cleanup = func() {
		_ = os.RemoveAll(tmpDir)
	}

	timeout := getIntEnv("INSTAGRAM_DOWNLOAD_TIMEOUT_SECONDS", defaultTimeoutSeconds)
	galleryDir := filepath.Join(tmpDir, "gallery-dl")
	if err := os.MkdirAll(galleryDir, 0o755); err != nil {
		return nil, cleanup, fmt.Errorf("create gallery-dl dir: %w", err)
	}

	filePaths, err = downloadWithGalleryDL(link, galleryDir, timeout)
	if err == nil && len(filePaths) > 0 {
		log.Printf("instagram downloader=gallery-dl files=%d", len(filePaths))
		return validateDownloadedFiles(filePaths, cleanup)
	}
	if err != nil {
		log.Printf("gallery-dl primary failed for %s: %v", link, err)
	}

	instaloaderDir := filepath.Join(tmpDir, "instaloader")
	if err := os.MkdirAll(instaloaderDir, 0o755); err != nil {
		return nil, cleanup, fmt.Errorf("create instaloader dir: %w", err)
	}

	filePaths, instaloaderErr := downloadWithInstaloader(link, instaloaderDir, timeout)
	if instaloaderErr == nil && len(filePaths) > 0 {
		log.Printf("instagram downloader=instaloader files=%d", len(filePaths))
		return validateDownloadedFiles(filePaths, cleanup)
	}
	if instaloaderErr != nil {
		log.Printf("instaloader fallback failed for %s: %v", link, instaloaderErr)
	}

	instagrapiDir := filepath.Join(tmpDir, "instagrapi")
	if err := os.MkdirAll(instagrapiDir, 0o755); err != nil {
		return nil, cleanup, fmt.Errorf("create instagrapi dir: %w", err)
	}

	filePaths, instagrapiErr := downloadWithInstagrapi(link, instagrapiDir, timeout)
	if instagrapiErr == nil && len(filePaths) > 0 {
		log.Printf("instagram downloader=instagrapi files=%d", len(filePaths))
		return validateDownloadedFiles(filePaths, cleanup)
	}
	if instagrapiErr != nil {
		log.Printf("instagrapi fallback failed for %s: %v", link, instagrapiErr)
	}

	if err != nil {
		return nil, cleanup, fmt.Errorf("gallery-dl failed: %w; instaloader failed: %v; instagrapi failed: %v", err, instaloaderErr, instagrapiErr)
	}
	return nil, cleanup, fmt.Errorf("gallery-dl produced no files; instaloader failed: %v; instagrapi failed: %v", instaloaderErr, instagrapiErr)
}

func downloadGalleryDLSingle(link string) (filePath string, cleanup func(), err error) {
	filePaths, cleanup, err := downloadGalleryDLContents(link)
	if err != nil {
		return "", cleanup, err
	}
	if len(filePaths) != 1 {
		return "", cleanup, fmt.Errorf("expected a single gallery-dl file, got %d", len(filePaths))
	}
	return filePaths[0], cleanup, nil
}

func DownloadInstagramAttachedAudio(link string) (filePath, title string, cleanup func(), err error) {
	tmpDir, err := os.MkdirTemp("", "instagram-audio-*")
	if err != nil {
		return "", "", func() {}, fmt.Errorf("create instagram audio temp dir: %w", err)
	}

	cleanup = func() {
		_ = os.RemoveAll(tmpDir)
	}

	timeout := getIntEnv("INSTAGRAM_DOWNLOAD_TIMEOUT_SECONDS", defaultTimeoutSeconds)
	filePath, title, err = downloadInstagramAttachedAudioWithInstagrapi(link, tmpDir, timeout)
	if err != nil {
		return "", "", cleanup, err
	}

	maxMB := getIntEnv("INSTAGRAM_MAX_FILE_SIZE_MB", defaultMaxFileSizeMB)
	if maxMB <= 0 {
		maxMB = defaultMaxFileSizeMB
	}
	maxBytes := int64(maxMB) * 1024 * 1024

	stat, statErr := os.Stat(filePath)
	if statErr != nil {
		return "", "", cleanup, fmt.Errorf("instagram audio stat failed for %s: %w", filepath.Base(filePath), statErr)
	}
	if stat.Size() > maxBytes {
		return "", "", cleanup, fmt.Errorf("instagram audio too large: %d bytes (max %d) for %s", stat.Size(), maxBytes, filepath.Base(filePath))
	}

	return filePath, title, cleanup, nil
}

func Probe(link string) (MediaProbe, error) {
	root, err := fetchInfoJSON(link)
	if err != nil {
		return MediaProbe{}, err
	}

	entries := mediaEntries(root)
	probe := MediaProbe{
		ItemCount: len(entries),
	}
	if probe.ItemCount == 0 {
		probe.ItemCount = 1
		entries = []ytRoot{root}
	}

	for _, entry := range entries {
		if isCodecPresent(entry.VCodec) {
			probe.HasVideo = true
		}
		if isCodecPresent(entry.ACodec) {
			probe.HasAudio = true
		}
	}
	return probe, nil
}

func ListVideoQualityOptions(link string) ([]VideoQualityOption, error) {
	root, err := fetchInfoJSON(link)
	if err != nil {
		return nil, err
	}

	target := root.primaryEntry()
	if len(target.Formats) == 0 {
		return defaultYouTubeQualityOptions(), nil
	}

	heightToFormat := map[int]ytFormat{}
	for _, f := range target.Formats {
		if !isCodecPresent(f.VCodec) {
			continue
		}
		if f.Height <= 0 {
			continue
		}

		cur, exists := heightToFormat[f.Height]
		if !exists || preferFormat(f, cur) {
			heightToFormat[f.Height] = f
		}
	}

	if len(heightToFormat) == 0 {
		return defaultYouTubeQualityOptions(), nil
	}

	heights := make([]int, 0, len(heightToFormat))
	for h := range heightToFormat {
		heights = append(heights, h)
	}
	sortIntsDesc(heights)
	if len(heights) > 3 {
		heights = heights[:3]
	}

	options := make([]VideoQualityOption, 0, len(heights))
	mp3Size := estimateMP3SizeBytes(target.Duration)
	for _, h := range heights {
		f := heightToFormat[h]
		selector := f.FormatID
		estimatedBytes := estimateFormatSizeBytes(f, target.Duration)
		if !isCodecPresent(f.ACodec) {
			selector = fmt.Sprintf("%s+bestaudio/%s", f.FormatID, f.FormatID)
			estimatedBytes += bestAudioSizeBytes(target.Formats, target.Duration)
		}
		options = append(options, VideoQualityOption{
			Key:               fmt.Sprintf("q%d", h),
			Label:             fmt.Sprintf("MP4 %dp", h),
			Selector:          selector,
			EstimatedBytes:    estimatedBytes,
			EstimatedMP3Bytes: mp3Size,
		})
	}
	return options, nil
}

func defaultYouTubeQualityOptions() []VideoQualityOption {
	return []VideoQualityOption{
		{Key: "q1080", Label: "MP4 1080p", Selector: "bestvideo[height<=1080][ext=mp4]+bestaudio[ext=m4a]/best[height<=1080][ext=mp4]/best[height<=1080]"},
		{Key: "q720", Label: "MP4 720p", Selector: "bestvideo[height<=720][ext=mp4]+bestaudio[ext=m4a]/best[height<=720][ext=mp4]/best[height<=720]"},
		{Key: "q480", Label: "MP4 480p", Selector: "bestvideo[height<=480][ext=mp4]+bestaudio[ext=m4a]/best[height<=480][ext=mp4]/best[height<=480]"},
	}
}

func estimateFormatSizeBytes(f ytFormat, duration float64) int64 {
	if f.FileSize > 0 {
		return f.FileSize
	}
	if f.FileSizeApprox > 0 {
		return f.FileSizeApprox
	}
	if duration > 0 && f.TBR > 0 {
		return int64((f.TBR * 1000 / 8) * duration)
	}
	return 0
}

func bestAudioSizeBytes(formats []ytFormat, duration float64) int64 {
	var best int64
	for _, f := range formats {
		if !isCodecPresent(f.ACodec) || isCodecPresent(f.VCodec) {
			continue
		}
		size := estimateFormatSizeBytes(f, duration)
		if size > best {
			best = size
		}
	}
	return best
}

func estimateMP3SizeBytes(duration float64) int64 {
	if duration <= 0 {
		return 0
	}
	const bitrateBytesPerSecond = 128000 / 8
	return int64(duration * bitrateBytesPerSecond)
}

func isCodecPresent(codec string) bool {
	codec = strings.TrimSpace(strings.ToLower(codec))
	return codec != "" && codec != "none" && codec != "unknown"
}

type ytRoot struct {
	Type     string     `json:"_type"`
	Title    string     `json:"title"`
	ID       string     `json:"id"`
	VCodec   string     `json:"vcodec"`
	ACodec   string     `json:"acodec"`
	Duration float64    `json:"duration"`
	Formats  []ytFormat `json:"formats"`
	Entries  []ytRoot   `json:"entries"`
}

type ytFormat struct {
	FormatID       string  `json:"format_id"`
	Ext            string  `json:"ext"`
	Height         int     `json:"height"`
	VCodec         string  `json:"vcodec"`
	ACodec         string  `json:"acodec"`
	FileSize       int64   `json:"filesize"`
	FileSizeApprox int64   `json:"filesize_approx"`
	TBR            float64 `json:"tbr"`
}

func (r ytRoot) primaryEntry() ytRoot {
	entries := mediaEntries(r)
	if len(entries) == 0 {
		return r
	}
	return entries[0]
}

func mediaEntries(root ytRoot) []ytRoot {
	if isEmptyPlaylist(root) {
		return []ytRoot{}
	}
	if len(root.Entries) == 0 {
		return []ytRoot{root}
	}

	entries := flattenMediaEntries(root, root.Title)
	if len(entries) == 0 {
		return []ytRoot{root}
	}
	return entries
}

func isEmptyPlaylist(root ytRoot) bool {
	return strings.EqualFold(strings.TrimSpace(root.Type), "playlist") && len(root.Entries) == 0
}

func flattenMediaEntries(node ytRoot, inheritedTitle string) []ytRoot {
	if strings.TrimSpace(node.Title) == "" {
		node.Title = inheritedTitle
	}

	if len(node.Entries) == 0 {
		return []ytRoot{node}
	}

	entries := make([]ytRoot, 0, len(node.Entries))
	for _, entry := range node.Entries {
		entries = append(entries, flattenMediaEntries(entry, node.Title)...)
	}
	return entries
}

func GetMediaTitle(link string) (string, error) {
	root, err := fetchInfoJSON(link)
	if err != nil {
		return "", err
	}

	target := root.primaryEntry()
	title := strings.TrimSpace(target.Title)
	if title == "" {
		title = strings.TrimSpace(root.Title)
	}
	if title == "" {
		return "", errors.New("empty media title")
	}
	return title, nil
}

func fetchInfoJSON(link string) (ytRoot, error) {
	timeout := getIntEnv("INSTAGRAM_DOWNLOAD_TIMEOUT_SECONDS", defaultTimeoutSeconds)

	cmdArgs := buildYTDLPBaseArgs(link)
	cmdArgs = append(cmdArgs,
		"--dump-single-json",
		link,
	)
	cmd, err := newYTDLPCommand(cmdArgs...)
	if err != nil {
		return ytRoot{}, err
	}

	rawOut, err := runCommandWithTimeout(cmd, timeout)
	if err != nil {
		return ytRoot{}, fmt.Errorf("yt-dlp probe failed: %w", err)
	}

	var root ytRoot
	if err := json.Unmarshal(rawOut, &root); err != nil {
		return ytRoot{}, fmt.Errorf("parse probe json: %w", err)
	}
	return root, nil
}

func preferFormat(candidate, current ytFormat) bool {
	if !isCodecPresent(current.ACodec) && isCodecPresent(candidate.ACodec) {
		return true
	}
	if isCodecPresent(current.ACodec) && !isCodecPresent(candidate.ACodec) {
		return false
	}
	if current.Ext != "mp4" && candidate.Ext == "mp4" {
		return true
	}
	if current.Ext == "mp4" && candidate.Ext != "mp4" {
		return false
	}
	return false
}

func sortIntsDesc(nums []int) {
	for i := 0; i < len(nums)-1; i++ {
		for j := i + 1; j < len(nums); j++ {
			if nums[j] > nums[i] {
				nums[i], nums[j] = nums[j], nums[i]
			}
		}
	}
}

func downloadSingleWithExtraArgs(link string, extraArgs ...string) (filePath string, cleanup func(), err error) {
	filePaths, cleanup, err := downloadManyWithExtraArgs(link, extraArgs...)
	if err != nil {
		return "", cleanup, err
	}
	if len(filePaths) != 1 {
		return "", cleanup, fmt.Errorf("expected a single downloaded file, got %d (collection flow required)", len(filePaths))
	}
	return filePaths[0], cleanup, nil
}

func downloadManyWithExtraArgs(link string, extraArgs ...string) (filePaths []string, cleanup func(), err error) {
	tmpDir, err := os.MkdirTemp("", "media-dl-*")
	if err != nil {
		return nil, func() {}, fmt.Errorf("create temp dir: %w", err)
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

	root, metaErr := fetchInfoJSON(link)

	outputTemplate := filepath.Join(tmpDir, "%(id)s.%(ext)s")
	cmdArgs := buildYTDLPBaseArgs(link)
	cmdArgs = append(cmdArgs,
		"--restrict-filenames",
		"-o", outputTemplate,
		"--print", "after_move:filepath",
	)
	cmdArgs = append(cmdArgs, extraArgs...)
	cmdArgs = append(cmdArgs, link)

	cmd, err := newYTDLPCommand(cmdArgs...)
	if err != nil {
		return nil, cleanup, err
	}

	rawOut, err := runCommandWithTimeout(cmd, timeout)
	if err != nil {
		return nil, cleanup, fmt.Errorf("yt-dlp failed: %w", err)
	}

	filePaths = collectDownloadedFiles(tmpDir, string(rawOut))
	if metaErr == nil {
		filePaths = orderDownloadedFiles(filePaths, root)
	}
	if len(filePaths) == 0 {
		if shouldTryGalleryDLFallback(link, extraArgs, root, metaErr) {
			galleryFiles, galleryErr := downloadWithGalleryDL(link, tmpDir, timeout)
			if galleryErr == nil && len(galleryFiles) > 0 {
				logGalleryDLFallback(link, galleryFiles)
				return galleryFiles, cleanup, nil
			}
			return nil, cleanup, fmt.Errorf("%w; gallery-dl fallback failed: %v", buildNoDownloadedFilesError(link, tmpDir, string(rawOut), root, metaErr), galleryErr)
		}
		return nil, cleanup, buildNoDownloadedFilesError(link, tmpDir, string(rawOut), root, metaErr)
	}

	for _, filePath := range filePaths {
		stat, statErr := os.Stat(filePath)
		if statErr != nil {
			return nil, cleanup, fmt.Errorf("downloaded file stat failed for %s: %w", filepath.Base(filePath), statErr)
		}
		if stat.Size() > maxBytes {
			return nil, cleanup, fmt.Errorf("file too large: %d bytes (max %d) for %s", stat.Size(), maxBytes, filepath.Base(filePath))
		}
	}

	return filePaths, cleanup, nil
}

func runCommandWithTimeout(cmd *exec.Cmd, timeoutSeconds int) ([]byte, error) {
	type result struct {
		out []byte
		err error
	}

	done := make(chan result, 1)
	go func() {
		out, err := cmd.CombinedOutput()
		done <- result{out: out, err: err}
	}()

	select {
	case res := <-done:
		if res.err != nil {
			return nil, fmt.Errorf("%w (%s)", res.err, strings.TrimSpace(string(res.out)))
		}
		return res.out, nil
	case <-time.After(time.Duration(timeoutSeconds) * time.Second):
		_ = cmd.Process.Kill()
		return nil, errors.New("command timed out")
	}
}

func extractLastNonEmptyLine(raw string) string {
	lines := strings.Split(strings.TrimSpace(raw), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line != "" {
			return line
		}
	}
	return ""
}

func collectDownloadedFiles(tmpDir, rawOut string) []string {
	seen := make(map[string]struct{})
	filePaths := make([]string, 0)

	for _, line := range strings.Split(rawOut, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if info, err := os.Stat(line); err == nil && !info.IsDir() {
			if _, exists := seen[line]; !exists {
				seen[line] = struct{}{}
				filePaths = append(filePaths, line)
			}
		}
	}

	if len(filePaths) > 0 {
		return filePaths
	}

	entries, err := os.ReadDir(tmpDir)
	if err != nil {
		return nil
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		lowerName := strings.ToLower(name)
		if strings.HasSuffix(lowerName, ".part") || strings.HasSuffix(lowerName, ".temp") || strings.HasSuffix(lowerName, ".ytdl") {
			continue
		}

		fullPath := filepath.Join(tmpDir, name)
		if _, exists := seen[fullPath]; exists {
			continue
		}
		seen[fullPath] = struct{}{}
		filePaths = append(filePaths, fullPath)
	}

	sort.Strings(filePaths)
	return filePaths
}

func orderDownloadedFiles(filePaths []string, root ytRoot) []string {
	if len(filePaths) < 2 {
		return filePaths
	}

	entries := mediaEntries(root)
	idOrder := make(map[string]int, len(entries))
	for idx, entry := range entries {
		id := strings.TrimSpace(strings.ToLower(entry.ID))
		if id != "" {
			idOrder[id] = idx
		}
	}
	if len(idOrder) == 0 {
		return filePaths
	}

	type orderedFile struct {
		path  string
		order int
		name  string
	}

	ordered := make([]orderedFile, 0, len(filePaths))
	for _, filePath := range filePaths {
		base := strings.ToLower(strings.TrimSuffix(filepath.Base(filePath), filepath.Ext(filePath)))
		order := len(filePaths) + 1000
		for id, idx := range idOrder {
			if strings.Contains(base, id) {
				order = idx
				break
			}
		}
		ordered = append(ordered, orderedFile{
			path:  filePath,
			order: order,
			name:  base,
		})
	}

	sort.SliceStable(ordered, func(i, j int) bool {
		if ordered[i].order != ordered[j].order {
			return ordered[i].order < ordered[j].order
		}
		return ordered[i].name < ordered[j].name
	})

	out := make([]string, 0, len(ordered))
	for _, item := range ordered {
		out = append(out, item.path)
	}
	return out
}

func buildNoDownloadedFilesError(link, tmpDir, rawOut string, root ytRoot, metaErr error) error {
	tempFiles := make([]string, 0)
	entries, readErr := os.ReadDir(tmpDir)
	if readErr == nil {
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			tempFiles = append(tempFiles, entry.Name())
		}
		sort.Strings(tempFiles)
	}

	diag := fmt.Sprintf("raw_output=%q temp_files=%v link=%q", trimForLog(rawOut, 400), tempFiles, trimForLog(link, 200))
	if metaErr != nil {
		return fmt.Errorf("yt-dlp produced no downloaded files (metadata probe failed: %v). %s", metaErr, diag)
	}
	if isEmptyPlaylist(root) {
		return fmt.Errorf("yt-dlp returned empty playlist (0 items)%s. %s", buildInstagramCookiesDiag(link), diag)
	}

	return fmt.Errorf("yt-dlp produced no downloaded files (entry_count=%d). %s", len(mediaEntries(root)), diag)
}

func trimForLog(raw string, maxLen int) string {
	raw = strings.TrimSpace(raw)
	if len(raw) <= maxLen {
		return raw
	}
	if maxLen <= 3 {
		return raw[:maxLen]
	}
	return raw[:maxLen-3] + "..."
}

func shouldTryGalleryDLFallback(link string, extraArgs []string, root ytRoot, metaErr error) bool {
	if !isInstagramLink(link) {
		return false
	}
	if len(extraArgs) != 0 {
		return false
	}
	if metaErr != nil {
		return false
	}
	return isEmptyPlaylist(root)
}

func downloadWithGalleryDL(link, tmpDir string, timeoutSeconds int) ([]string, error) {
	cmdArgs := appendGalleryDLCookiesArgs(nil, link)
	cmdArgs = append(cmdArgs, link)

	cmd, err := newGalleryDLCommand(cmdArgs...)
	if err != nil {
		return nil, err
	}
	cmd.Dir = tmpDir

	rawOut, err := runCommandWithTimeout(cmd, timeoutSeconds)
	if err != nil {
		return nil, fmt.Errorf("gallery-dl failed: %w", err)
	}

	filePaths := collectDownloadedFilesRecursive(tmpDir)
	if len(filePaths) == 0 {
		return nil, fmt.Errorf("gallery-dl produced no files (raw_output=%q)", trimForLog(string(rawOut), 400))
	}
	return filePaths, nil
}

func collectDownloadedFilesRecursive(rootDir string) []string {
	filePaths := make([]string, 0)
	_ = filepath.Walk(rootDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info == nil || info.IsDir() {
			return nil
		}

		lowerName := strings.ToLower(info.Name())
		if strings.HasSuffix(lowerName, ".part") || strings.HasSuffix(lowerName, ".temp") || strings.HasSuffix(lowerName, ".ytdl") || strings.HasSuffix(lowerName, ".json") {
			return nil
		}

		filePaths = append(filePaths, path)
		return nil
	})

	sort.Strings(filePaths)
	return filePaths
}

func logGalleryDLFallback(link string, filePaths []string) {
	names := make([]string, 0, len(filePaths))
	for _, path := range filePaths {
		names = append(names, filepath.Base(path))
	}
	log.Printf("gallery-dl fallback succeeded for %s with %d files: %v", link, len(filePaths), names)
}

func validateDownloadedFiles(filePaths []string, cleanup func()) ([]string, func(), error) {
	maxMB := getIntEnv("INSTAGRAM_MAX_FILE_SIZE_MB", defaultMaxFileSizeMB)
	if maxMB <= 0 {
		maxMB = defaultMaxFileSizeMB
	}
	maxBytes := int64(maxMB) * 1024 * 1024

	for _, filePath := range filePaths {
		stat, statErr := os.Stat(filePath)
		if statErr != nil {
			return nil, cleanup, fmt.Errorf("downloaded file stat failed for %s: %w", filepath.Base(filePath), statErr)
		}
		if stat.Size() > maxBytes {
			return nil, cleanup, fmt.Errorf("file too large: %d bytes (max %d) for %s", stat.Size(), maxBytes, filepath.Base(filePath))
		}
	}

	return filePaths, cleanup, nil
}

func buildYTDLPBaseArgs(link string) []string {
	args := []string{"--no-warnings"}
	if shouldUseNoPlaylist(link) {
		args = append(args, "--no-playlist")
	}
	args = appendFFmpegLocationArgs(args)
	args = appendInstagramCookiesArgs(args, link)
	return args
}

func buildMP3DownloadArgs() ([]string, error) {
	ffmpegPath, err := findFFmpeg()
	if err != nil {
		return nil, fmt.Errorf("mp3 download requires ffmpeg/ffprobe: %w", err)
	}

	args := []string{
		"-x",
		"--audio-format", "mp3",
		"--audio-quality", "0",
	}

	if strings.TrimSpace(config.GetEnv("FFMPEG_BIN", "")) != "" {
		args = append(args, "--ffmpeg-location", ffmpegPath)
	}

	return args, nil
}

func appendFFmpegLocationArgs(args []string) []string {
	customBin := strings.TrimSpace(config.GetEnv("FFMPEG_BIN", ""))
	if customBin == "" {
		return args
	}
	return append(args, "--ffmpeg-location", customBin)
}

func shouldUseNoPlaylist(link string) bool {
	lowerLink := strings.ToLower(strings.TrimSpace(link))
	if strings.Contains(lowerLink, "instagram.com/p/") {
		return false
	}
	return true
}

func appendInstagramCookiesArgs(args []string, link string) []string {
	// A browser cookie jar is Instagram-specific (configured for the IG login).
	// A cookies file is applied to every platform (X/Twitter, YouTube, etc.)
	// so that login-gated or sensitive posts are downloadable too.
	if isInstagramLink(link) {
		cookiesFromBrowser := strings.TrimSpace(config.GetEnv("INSTAGRAM_COOKIES_FROM_BROWSER", ""))
		if cookiesFromBrowser != "" {
			return append(args, "--cookies-from-browser", cookiesFromBrowser)
		}
	}

	cookiesFile := strings.TrimSpace(config.GetEnv("INSTAGRAM_COOKIES_FILE", ""))
	if cookiesFile != "" {
		return append(args, "--cookies", cookiesFile)
	}

	return args
}

func buildInstagramCookiesDiag(link string) string {
	if !isInstagramLink(link) {
		return ""
	}
	if strings.TrimSpace(config.GetEnv("INSTAGRAM_COOKIES_FROM_BROWSER", "")) != "" {
		return " (instagram cookies configured via browser)"
	}
	if strings.TrimSpace(config.GetEnv("INSTAGRAM_COOKIES_FILE", "")) != "" {
		return " (instagram cookies configured via file)"
	}
	return " (instagram cookies not configured)"
}

func isInstagramLink(link string) bool {
	lowerLink := strings.ToLower(strings.TrimSpace(link))
	return strings.Contains(lowerLink, "instagram.com/")
}

func appendGalleryDLCookiesArgs(args []string, link string) []string {
	if !isInstagramLink(link) {
		return args
	}

	cookiesFromBrowser := strings.TrimSpace(config.GetEnv("INSTAGRAM_COOKIES_FROM_BROWSER", ""))
	if cookiesFromBrowser != "" {
		return append(args, "--cookies-from-browser", cookiesFromBrowser)
	}

	cookiesFile := strings.TrimSpace(config.GetEnv("INSTAGRAM_COOKIES_FILE", ""))
	if cookiesFile != "" {
		return append(args, "--cookies", cookiesFile)
	}

	return args
}

func downloadWithInstaloader(link, tmpDir string, timeoutSeconds int) ([]string, error) {
	return downloadWithPythonFallback("instaloader", link, tmpDir, timeoutSeconds)
}

func downloadWithInstagrapi(link, tmpDir string, timeoutSeconds int) ([]string, error) {
	return downloadWithPythonFallback("instagrapi", link, tmpDir, timeoutSeconds)
}

func downloadWithPythonFallback(backend, link, tmpDir string, timeoutSeconds int) ([]string, error) {
	scriptPath, err := resolveInstagramFallbackScript()
	if err != nil {
		return nil, err
	}

	cmd, err := newInstagramPythonCommand(scriptPath, backend, link, tmpDir)
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

func downloadInstagramAttachedAudioWithInstagrapi(link, tmpDir string, timeoutSeconds int) (string, string, error) {
	scriptPath, err := resolveInstagramFallbackScript()
	if err != nil {
		return "", "", err
	}

	cmd, err := newInstagramPythonCommand(scriptPath, "instagrapi-audio", link, tmpDir)
	if err != nil {
		return "", "", err
	}

	rawOut, err := runCommandWithTimeout(cmd, timeoutSeconds)
	if err != nil {
		return "", "", fmt.Errorf("instagrapi audio failed: %w", err)
	}

	var result instagramAudioResult
	if err := json.Unmarshal([]byte(strings.TrimSpace(string(rawOut))), &result); err != nil {
		return "", "", fmt.Errorf("instagrapi audio parse failed: %w (raw_output=%q)", err, trimForLog(string(rawOut), 400))
	}
	if strings.TrimSpace(result.Path) == "" {
		return "", "", fmt.Errorf("instagrapi audio returned empty path (raw_output=%q)", trimForLog(string(rawOut), 400))
	}
	return result.Path, strings.TrimSpace(result.Title), nil
}

func resolveInstagramFallbackScript() (string, error) {
	customPath := strings.TrimSpace(config.GetEnv("INSTAGRAM_FALLBACK_SCRIPT", ""))
	candidates := []string{}
	if customPath != "" {
		candidates = append(candidates, customPath)
	}
	candidates = append(candidates,
		filepath.Join("providers", "instagram", "instagram_fallback.py"),
		filepath.Join(filepath.Dir(os.Args[0]), "providers", "instagram", "instagram_fallback.py"),
	)

	for _, candidate := range candidates {
		if strings.TrimSpace(candidate) == "" {
			continue
		}
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate, nil
		}
	}

	return "", errors.New("instagram fallback script not found")
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

func newGalleryDLCommand(args ...string) (*exec.Cmd, error) {
	customBin := strings.TrimSpace(config.GetEnv("INSTAGRAM_GALLERY_DL_BIN", ""))
	if customBin != "" {
		return exec.Command(customBin, args...), nil
	}

	candidates := [][]string{
		{"gallery-dl"},
		{"gallery-dl.exe"},
		{"py", "-m", "gallery_dl"},
		{"python", "-m", "gallery_dl"},
		{"python3", "-m", "gallery_dl"},
	}

	for _, c := range candidates {
		if _, err := exec.LookPath(c[0]); err == nil {
			fullArgs := append(c[1:], args...)
			return exec.Command(c[0], fullArgs...), nil
		}
	}

	return nil, errors.New("gallery-dl executable not found (set INSTAGRAM_GALLERY_DL_BIN or install gallery-dl)")
}

func newInstagramPythonCommand(scriptPath string, args ...string) (*exec.Cmd, error) {
	customBin := strings.TrimSpace(config.GetEnv("INSTAGRAM_PYTHON_BIN", ""))
	if customBin != "" {
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

	return nil, errors.New("python executable not found (set INSTAGRAM_PYTHON_BIN or add py/python to PATH)")
}
