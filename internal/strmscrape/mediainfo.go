package strmscrape

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"litepan/internal/domain"
)

// 视频参数（media-editor media_info / save_media_info 同款，ffprobe 探测）。

type WorkbenchMediaVideo struct {
	Codec   string `json:"codec,omitempty"`
	Profile string `json:"profile,omitempty"`
	Width   int    `json:"width,omitempty"`
	Height  int    `json:"height,omitempty"`
	FPS     string `json:"fps,omitempty"`
	BitRate string `json:"bit_rate,omitempty"`
	PixFmt  string `json:"pix_fmt,omitempty"`
	HDR     string `json:"hdr,omitempty"`
}

type WorkbenchMediaAudio struct {
	Codec         string `json:"codec,omitempty"`
	Channels      int    `json:"channels,omitempty"`
	ChannelLayout string `json:"channel_layout,omitempty"`
	SampleRate    string `json:"sample_rate,omitempty"`
	BitRate       string `json:"bit_rate,omitempty"`
	Language      string `json:"language,omitempty"`
}

type WorkbenchMediaSubtitle struct {
	Codec    string `json:"codec,omitempty"`
	Language string `json:"language,omitempty"`
	Title    string `json:"title,omitempty"`
}

type WorkbenchMediaInfo struct {
	Filename  string                 `json:"filename,omitempty"`
	Source    string                 `json:"source,omitempty"`
	Format    string                 `json:"format,omitempty"`
	Duration  string                 `json:"duration,omitempty"`
	BitRate   string                 `json:"bit_rate,omitempty"`
	FileSize  *int64                 `json:"file_size,omitempty"`
	Video     []WorkbenchMediaVideo  `json:"video"`
	Audio     []WorkbenchMediaAudio  `json:"audio"`
	Subtitles []WorkbenchMediaSubtitle `json:"subtitles"`
}

type ffprobeStream struct {
	CodecType     string            `json:"codec_type"`
	CodecName     string            `json:"codec_name"`
	Profile       string            `json:"profile"`
	Width         int               `json:"width"`
	Height        int               `json:"height"`
	AvgFrameRate  string            `json:"avg_frame_rate"`
	RFrameRate    string            `json:"r_frame_rate"`
	BitRate       string            `json:"bit_rate"`
	PixFmt        string            `json:"pix_fmt"`
	ColorTransfer string            `json:"color_transfer"`
	Channels      int               `json:"channels"`
	ChannelLayout string            `json:"channel_layout"`
	SampleRate    string            `json:"sample_rate"`
	Tags          map[string]string `json:"tags"`
}

type ffprobeOutput struct {
	Format struct {
		FormatName string `json:"format_name"`
		Duration   string `json:"duration"`
		BitRate    string `json:"bit_rate"`
	} `json:"format"`
	Streams []ffprobeStream `json:"streams"`
}

func findFFprobe() (string, error) {
	p, err := exec.LookPath("ffprobe")
	if err != nil {
		return "", fmt.Errorf("ffprobe 不存在，请先安装 ffmpeg")
	}
	return p, nil
}

// workbenchMediaFile 按 media-editor media_for_nfo 的顺序找视频文件：
// 同名文件 → 目录下第一个非 STRM 媒体文件 → 目录下第一个媒体文件 → 作品的第一个 STRM。
func workbenchMediaFile(g workGroup, nfoPath string) (string, bool) {
	dir := filepath.Dir(nfoPath)
	stem := strings.TrimSuffix(filepath.Base(nfoPath), filepath.Ext(nfoPath))
	entries, err := os.ReadDir(dir)
	if err == nil {
		var first, firstNonStrm string
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			ext := strings.ToLower(filepath.Ext(e.Name()))
			if !mediaFileExtensions[ext] {
				continue
			}
			full := filepath.Join(dir, e.Name())
			if strings.EqualFold(strings.TrimSuffix(e.Name(), filepath.Ext(e.Name())), stem) {
				return full, true
			}
			if first == "" {
				first = full
			}
			if firstNonStrm == "" && ext != ".strm" {
				firstNonStrm = full
			}
		}
		if firstNonStrm != "" {
			return firstNonStrm, true
		}
		if first != "" {
			return first, true
		}
	}
	if len(g.entries) > 0 {
		return g.entries[0].absPath, true
	}
	return "", false
}

func probeMediaInfo(ctx context.Context, mediaPath string) (*WorkbenchMediaInfo, error) {
	ffprobe, err := findFFprobe()
	if err != nil {
		return nil, err
	}
	probeTarget := mediaPath
	source := mediaPath
	var fileSize *int64
	if st, err := os.Stat(mediaPath); err == nil && !st.IsDir() {
		size := st.Size()
		fileSize = &size
	}
	if strings.EqualFold(filepath.Ext(mediaPath), ".strm") {
		data, err := os.ReadFile(mediaPath)
		if err != nil {
			return nil, fmt.Errorf("STRM 文件读取失败")
		}
		url := ""
		for _, line := range strings.Split(strings.TrimPrefix(string(data), "\ufeff"), "\n") {
			if line = strings.TrimSpace(line); line != "" {
				url = line
				break
			}
		}
		if url == "" {
			return nil, fmt.Errorf("STRM 文件为空")
		}
		probeTarget = url
		source = url
		// media-editor 同款：filesize 取本地文件本身的大小，STRM 也一样（即 .strm 文件的字节数），不置空。
	}
	probeCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(probeCtx, ffprobe, "-v", "error", "-show_format", "-show_streams", "-of", "json", probeTarget)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if probeCtx.Err() != nil {
			return nil, fmt.Errorf("视频参数探测超时（20 秒）")
		}
		detail := ""
		if lines := strings.Split(strings.TrimSpace(stderr.String()), "\n"); len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) != "" {
			detail = "：" + strings.TrimSpace(lines[len(lines)-1])
		}
		return nil, fmt.Errorf("视频参数探测失败%s", detail)
	}
	var probed ffprobeOutput
	if err := json.Unmarshal(stdout.Bytes(), &probed); err != nil {
		return nil, fmt.Errorf("ffprobe 返回了无效数据")
	}
	info := &WorkbenchMediaInfo{
		Filename: filepath.Base(mediaPath),
		Source:   source,
		Format:   probed.Format.FormatName,
		Duration: probed.Format.Duration,
		BitRate:  probed.Format.BitRate,
		FileSize: fileSize,
		Video:    []WorkbenchMediaVideo{},
		Audio:    []WorkbenchMediaAudio{},
		Subtitles: []WorkbenchMediaSubtitle{},
	}
	for _, stream := range probed.Streams {
		switch stream.CodecType {
		case "video":
			rate := stream.AvgFrameRate
			if rate == "" {
				rate = stream.RFrameRate
			}
			hdr := "SDR"
			switch stream.ColorTransfer {
			case "", "unknown", "bt709", "smpte170m", "bt470bg":
			default:
				hdr = "HDR"
			}
			info.Video = append(info.Video, WorkbenchMediaVideo{
				Codec: stream.CodecName, Profile: stream.Profile,
				Width: stream.Width, Height: stream.Height,
				FPS: rate, BitRate: stream.BitRate, PixFmt: stream.PixFmt, HDR: hdr,
			})
		case "audio":
			info.Audio = append(info.Audio, WorkbenchMediaAudio{
				Codec: stream.CodecName, Channels: stream.Channels,
				ChannelLayout: stream.ChannelLayout, SampleRate: stream.SampleRate,
				BitRate: stream.BitRate, Language: stream.Tags["language"],
			})
		case "subtitle":
			info.Subtitles = append(info.Subtitles, WorkbenchMediaSubtitle{
				Codec: stream.CodecName, Language: stream.Tags["language"], Title: stream.Tags["title"],
			})
		}
	}
	return info, nil
}

// GetWorkbenchMediaInfo 探测作品视频参数（media-editor /api/media-info 同款）。
func (s *Service) GetWorkbenchMediaInfo(ctx context.Context, taskID int64, itemID string) (*WorkbenchMediaInfo, error) {
	if taskID <= 0 || strings.TrimSpace(itemID) == "" {
		return nil, domain.Errorf(domain.CodeValidation, "参数不完整")
	}
	_, root, err := s.resolveTask(ctx, taskID)
	if err != nil {
		return nil, err
	}
	g, err := findWorkByID(root, itemID)
	if err != nil {
		return nil, err
	}
	mediaType := resolveWorkMediaType(g)
	nfoPath, ok := findWorkNFOPath(g, mediaType)
	if !ok {
		return nil, domain.Errorf(domain.CodeNotFound, "本地暂无 NFO，请先刮削一次再精修")
	}
	mediaPath, ok := workbenchMediaFile(g, nfoPath)
	if !ok {
		return nil, domain.Errorf(domain.CodeNotFound, "未找到对应的视频或 STRM 文件")
	}
	return probeMediaInfo(ctx, mediaPath)
}

// SaveWorkbenchMediaInfo 探测视频参数并写入根 NFO 的 fileinfo/streamdetails
//（media-editor /api/media-info-save 同款）。
func (s *Service) SaveWorkbenchMediaInfo(ctx context.Context, taskID int64, itemID string) (*WorkbenchMediaInfo, error) {
	info, err := s.GetWorkbenchMediaInfo(ctx, taskID, itemID)
	if err != nil {
		return nil, err
	}
	_, root, err := s.resolveTask(ctx, taskID)
	if err != nil {
		return nil, err
	}
	g, err := findWorkByID(root, itemID)
	if err != nil {
		return nil, err
	}
	mediaType := resolveWorkMediaType(g)
	nfoPath, _ := findWorkNFOPath(g, mediaType)
	rootTag := "movie"
	if mediaType == MediaTypeTV {
		rootTag = "tvshow"
	}
	tree, err := readNFOTreeForMerge(nfoPath, rootTag)
	if err != nil {
		return nil, err
	}
	var kept []*xmlNode
	for _, c := range tree.Children {
		if c.Name.Local != "fileinfo" {
			kept = append(kept, c)
		}
	}
	tree.Children = kept
	tree.Children = append(tree.Children, mediaInfoFileNode(info))
	if err := writeXMLTree(nfoPath, tree); err != nil {
		return nil, err
	}
	return info, nil
}

func mediaInfoFileNode(info *WorkbenchMediaInfo) *xmlNode {
	add := func(parent *xmlNode, tag, value string) {
		if strings.TrimSpace(value) == "" {
			return
		}
		parent.Children = append(parent.Children, &xmlNode{Name: xml.Name{Local: tag}, Text: value})
	}
	fileinfo := &xmlNode{Name: xml.Name{Local: "fileinfo"}}
	streamdetails := &xmlNode{Name: xml.Name{Local: "streamdetails"}}
	fileinfo.Children = append(fileinfo.Children, streamdetails)
	for _, v := range info.Video {
		node := &xmlNode{Name: xml.Name{Local: "video"}}
		aspect := ""
		if v.Width > 0 && v.Height > 0 {
			aspect = strconv.FormatFloat(math.Round(float64(v.Width)/float64(v.Height)*1000)/1000, 'f', -1, 64)
		}
		add(node, "codec", v.Codec)
		add(node, "aspect", aspect)
		if v.Width > 0 {
			add(node, "width", strconv.Itoa(v.Width))
		}
		if v.Height > 0 {
			add(node, "height", strconv.Itoa(v.Height))
		}
		add(node, "durationinseconds", info.Duration)
		add(node, "bitrate", v.BitRate)
		add(node, "fps", v.FPS)
		add(node, "hdrtype", v.HDR)
		streamdetails.Children = append(streamdetails.Children, node)
	}
	for _, a := range info.Audio {
		node := &xmlNode{Name: xml.Name{Local: "audio"}}
		add(node, "codec", a.Codec)
		add(node, "language", a.Language)
		if a.Channels > 0 {
			add(node, "channels", strconv.Itoa(a.Channels))
		}
		add(node, "bitrate", a.BitRate)
		add(node, "samplerate", a.SampleRate)
		streamdetails.Children = append(streamdetails.Children, node)
	}
	for _, sub := range info.Subtitles {
		node := &xmlNode{Name: xml.Name{Local: "subtitle"}}
		add(node, "codec", sub.Codec)
		add(node, "language", sub.Language)
		add(node, "title", sub.Title)
		streamdetails.Children = append(streamdetails.Children, node)
	}
	add(fileinfo, "format", info.Format)
	add(fileinfo, "duration", info.Duration)
	add(fileinfo, "bitrate", info.BitRate)
	if info.FileSize != nil {
		add(fileinfo, "filesize", strconv.FormatInt(*info.FileSize, 10))
	}
	add(fileinfo, "source", info.Source)
	return fileinfo
}
