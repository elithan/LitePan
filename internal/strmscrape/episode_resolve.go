package strmscrape

import (
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// 分集 NFO 定位（media-editor episode_nfo_path 同款），不用 .strm 对照表：
// 1. 文件名含 SxxExx token 的现有 NFO；2. 同 token 的媒体文件旁已存在的 NFO；
// 3. 内容里 season/episode 相符的 NFO；4. 同 token 的第一个媒体文件旁的 NFO；
// 5. 回落到 folder/SxxExx.nfo。

var mediaFileExtensions = map[string]bool{
	".strm": true, ".mkv": true, ".mp4": true, ".avi": true,
	".m2ts": true, ".ts": true, ".wmv": true, ".mov": true,
	".flv": true, ".webm": true,
}

func episodeTokenRegexp(season, episode int) *regexp.Regexp {
	return regexp.MustCompile(fmt.Sprintf(`(?i)(?:^|[^a-z0-9])s%02de%02d(?:[^a-z0-9]|$)`, season, episode))
}

func resolveEpisodeNFOPath(folder string, season, episode int) string {
	token := episodeTokenRegexp(season, episode)
	entries, err := os.ReadDir(folder)
	if err != nil {
		return filepath.Join(folder, fmt.Sprintf("S%02dE%02d.nfo", season, episode))
	}
	sort.Slice(entries, func(i, j int) bool {
		return strings.ToLower(entries[i].Name()) < strings.ToLower(entries[j].Name())
	})
	var nfos, media []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		ext := strings.ToLower(filepath.Ext(e.Name()))
		full := filepath.Join(folder, e.Name())
		if ext == ".nfo" {
			nfos = append(nfos, full)
			continue
		}
		if mediaFileExtensions[ext] && token.MatchString(strings.TrimSuffix(e.Name(), filepath.Ext(e.Name()))) {
			media = append(media, full)
		}
	}
	stemOf := func(path string) string {
		return strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	}
	for _, nfo := range nfos {
		if token.MatchString(stemOf(nfo)) {
			return nfo
		}
	}
	for _, m := range media {
		if candidate := strings.TrimSuffix(m, filepath.Ext(m)) + ".nfo"; fileExists(candidate) {
			return candidate
		}
	}
	for _, nfo := range nfos {
		if f, ok := parseEpisodeNFOFileLenient(nfo); ok && f.Season == season && f.Episode == episode {
			return nfo
		}
	}
	if len(media) > 0 {
		return strings.TrimSuffix(media[0], filepath.Ext(media[0])) + ".nfo"
	}
	return filepath.Join(folder, fmt.Sprintf("S%02dE%02d.nfo", season, episode))
}

// parseEpisodeNFOFileLenient 宽松解析分集 NFO：根标签不限，但必须含 season/episode 节点
//（media-editor scan_seasons 接受这类文件）。
func parseEpisodeNFOFileLenient(path string) (EpisodeFieldSet, bool) {
	f, ok := parseEpisodeNFOFile(path)
	if ok {
		return f, true
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return EpisodeFieldSet{}, false
	}
	var nfo episodeNFO
	if err := xml.Unmarshal(data, &nfo); err != nil {
		return EpisodeFieldSet{}, false
	}
	season, err1 := strconv.Atoi(strings.TrimSpace(nfo.Season))
	episode, err2 := strconv.Atoi(strings.TrimSpace(nfo.Episode))
	if err1 != nil || err2 != nil || season < 0 || episode < 0 {
		return EpisodeFieldSet{}, false
	}
	return EpisodeFieldSet{
		Season:  season,
		Episode: episode,
		Title:   strings.TrimSpace(nfo.Title),
		Plot:    strings.TrimSpace(nfo.Plot),
		Aired:   strings.TrimSpace(nfo.Aired),
		Thumb:   strings.TrimSpace(nfo.Thumb),
		Runtime: nfo.Runtime,
	}, true
}

// seasonDirPath 返回季子目录绝对路径，不存在时返回 ""。
func seasonDirPath(showDir string, season int) string {
	for _, d := range listLocalSeasonDirs(showDir) {
		if d.number == season {
			return d.absPath
		}
	}
	return ""
}

// seasonLocalFolder 返回某季写分集 NFO 的目录：优先季子目录；
// 没有季子目录时，若剧集根目录有该季的本地文件（媒体或 NFO），用根目录；
// 否则返回 ""（该季本地无文件，整剧保存时跳过，media-editor 同款）。
func seasonLocalFolder(g workGroup, season int) (string, bool) {
	if dir := seasonDirPath(g.absDir, season); dir != "" {
		return dir, true
	}
	entries, err := os.ReadDir(g.absDir)
	if err != nil {
		return "", false
	}
	seasonToken := regexp.MustCompile(fmt.Sprintf(`(?i)(?:^|[^a-z0-9])s%02de\d+(?:[^a-z0-9]|$)`, season))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		ext := strings.ToLower(filepath.Ext(name))
		stem := strings.TrimSuffix(name, filepath.Ext(name))
		if ext == ".nfo" {
			if f, ok := parseEpisodeNFOFileLenient(filepath.Join(g.absDir, name)); ok && f.Season == season {
				return g.absDir, true
			}
			continue
		}
		if mediaFileExtensions[ext] && seasonToken.MatchString(stem) {
			return g.absDir, true
		}
	}
	return "", false
}

// listLocalEpisodeFields 扫描目录下所有分集 NFO（宽松解析），返回指定季的本地分集栏位。
func listLocalEpisodeFields(folder string, season int) []EpisodeFieldSet {
	entries, err := os.ReadDir(folder)
	if err != nil {
		return nil
	}
	var out []EpisodeFieldSet
	for _, e := range entries {
		if e.IsDir() || !strings.EqualFold(filepath.Ext(e.Name()), ".nfo") {
			continue
		}
		f, ok := parseEpisodeNFOFileLenient(filepath.Join(folder, e.Name()))
		if !ok || f.Season != season {
			continue
		}
		out = append(out, f)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Episode < out[j].Episode })
	return out
}

// isGeneratedEpisodeTitle 判断本地分集标题是否为自动生成的“片名 SxxExx”式标题
//（media-editor mergedEpisodes 的 generatedEpisodeTitle 同款）：这类标题不覆盖 TMDB 标题。
func isGeneratedEpisodeTitle(title, showName string, season, episode int) bool {
	value := strings.TrimSpace(title)
	if value == "" {
		return true
	}
	base := strings.TrimSpace(showName)
	code := fmt.Sprintf("S%02dE%02d", season, episode)
	if value == base+" "+code {
		return true
	}
	return regexp.MustCompile(`(?i)\sS\d{2}E\d{2}$`).MatchString(value)
}
