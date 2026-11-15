package strmscrape

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"litepan/internal/domain"
	"litepan/internal/mediaorganize/tmdb"
)

// 工作台（media-editor 精修刮削的 LitePan 版）：
// 本地 NFO 栏位 vs TMDB 栏位对照、挑图、勾选演员、按季按集编辑。
// 写入统一走 NFO 合并（保留 fileinfo 等未知节点），与 media-editor 的保存语义一致。

const maxWorkbenchRemoteImage = 15 << 20 // 直接贴 URL 设图时沿用 media-editor 的 15MB 上限

// WorkbenchImage 是 TMDB 图片候选项。
type WorkbenchImage struct {
	FilePath    string  `json:"file_path"`
	URL         string  `json:"url"`
	URLOriginal string  `json:"url_original"`
	Width       int     `json:"width"`
	Height      int     `json:"height"`
	Language    string  `json:"language,omitempty"`
	VoteAverage float64 `json:"vote_average,omitempty"`
}

// WorkbenchActor 是演员（TMDB 候选 + 本地已保存共用）。
type WorkbenchActor struct {
	Name        string `json:"name"`
	Role        string `json:"role"`
	Order       int    `json:"order"`
	ProfilePath string `json:"profile_path,omitempty"`
	ThumbURL    string `json:"thumb_url,omitempty"`
	Selected    bool   `json:"selected"`
}

// WorkbenchLocalImage 是本地已存在的图片。
type WorkbenchLocalImage struct {
	Exists bool   `json:"exists"`
	URL    string `json:"url,omitempty"`
	Rel    string `json:"rel,omitempty"`
}

// WorkbenchLocalImages 是本地图片存在情况。
type WorkbenchLocalImages struct {
	Poster        WorkbenchLocalImage            `json:"poster"`
	Fanart        WorkbenchLocalImage            `json:"fanart"`
	Logo          WorkbenchLocalImage            `json:"logo"`
	SeasonPosters map[string]WorkbenchLocalImage `json:"season_posters,omitempty"`
}

// WorkbenchLocal 是本地现状。
type WorkbenchLocal struct {
	Fields FieldSet            `json:"fields"`
	Actors []WorkbenchActor    `json:"actors"`
	Images WorkbenchLocalImages `json:"images"`
}

// WorkbenchTMDB 是 TMDB 侧数据。
type WorkbenchTMDB struct {
	Fields    FieldSet         `json:"fields"`
	Actors    []WorkbenchActor `json:"actors"`
	Posters   []WorkbenchImage `json:"posters"`
	Backdrops []WorkbenchImage `json:"backdrops"`
	Logos     []WorkbenchImage `json:"logos"`
}

// WorkbenchEpisode 是单集（TMDB 值 + 本地存在情况 + 本地已保存栏位）。
type WorkbenchEpisode struct {
	Season        int              `json:"season"`
	Episode       int              `json:"episode"`
	Title         string           `json:"title"`
	Overview      string           `json:"overview"`
	AirDate       string           `json:"air_date,omitempty"`
	StillPath     string           `json:"still_path,omitempty"`
	StillURL      string           `json:"still_url,omitempty"`
	Runtime       int              `json:"runtime,omitempty"`
	TMDBID        int              `json:"tmdb_id,omitempty"`
	LocalNFO      bool             `json:"local_nfo"`
	LocalThumb    bool             `json:"local_thumb"`
	LocalThumbURL string           `json:"local_thumb_url,omitempty"`
	LocalFields   *EpisodeFieldSet `json:"local_fields,omitempty"`
}

// WorkbenchSeason 是单季。
type WorkbenchSeason struct {
	Number            int               `json:"number"`
	Name              string            `json:"name"`
	Overview          string            `json:"overview"`
	AirDate           string            `json:"air_date,omitempty"`
	PosterPath        string            `json:"poster_path,omitempty"`
	PosterURL         string            `json:"poster_url,omitempty"`
	EpisodeCount      int               `json:"episode_count"`
	LocalPoster       bool              `json:"local_poster"`
	LocalSeasonPoster bool              `json:"local_season_poster"`
	Posters           []WorkbenchImage  `json:"posters,omitempty"`
	Episodes          []WorkbenchEpisode `json:"episodes,omitempty"`
}

// WorkbenchData 是 GET /items/{id}/workbench 的完整响应。
type WorkbenchData struct {
	Item          Item              `json:"item"`
	MediaType     string            `json:"media_type"`
	TMDBID        string            `json:"tmdb_id,omitempty"`
	Local         WorkbenchLocal    `json:"local"`
	TMDB          WorkbenchTMDB     `json:"tmdb"`
	Seasons       []WorkbenchSeason `json:"seasons,omitempty"`
	TMDBAvailable bool              `json:"tmdb_available"`
	TMDBError     string            `json:"tmdb_error,omitempty"`
	ImagesError   string            `json:"images_error,omitempty"`
	CreditsError  string            `json:"credits_error,omitempty"`
}

// SaveFieldsRequest 是 POST /items/{id}/save-fields 的请求。
// Fields 只含改动过的栏位（media-editor selectedFields 同款：只提交与本地不同的字段）；
// uniqueid 不在这里改（只走 rematch/match 流程）。
type SaveFieldsRequest struct {
	StrmTaskID    int64                        `json:"strm_task_id"`
	ItemID        string                       `json:"item_id"`
	Kind          string                       `json:"kind"` // work | episode
	Season        int                          `json:"season,omitempty"`
	Episode       int                          `json:"episode,omitempty"`
	Fields        map[string]json.RawMessage   `json:"fields"`
	EpisodeFields EpisodeFieldSet              `json:"episode_fields"`
}

// SetImageRequest 是 POST /items/{id}/set-image 的请求。
type SetImageRequest struct {
	StrmTaskID int64  `json:"strm_task_id"`
	ItemID     string `json:"item_id"`
	Kind       string `json:"kind"` // poster | backdrop | logo | season_poster | episode_thumb
	TMDBPath   string `json:"tmdb_path,omitempty"`
	ImageURL   string `json:"image_url,omitempty"`
	Season     int    `json:"season,omitempty"`
	Episode    int    `json:"episode,omitempty"`
}

// SaveCastRequest 是 POST /items/{id}/save-cast 的请求。
type SaveCastRequest struct {
	StrmTaskID int64            `json:"strm_task_id"`
	ItemID     string           `json:"item_id"`
	Actors     []WorkbenchActor `json:"actors"`
}

// SaveSeriesRequest 是 POST /items/{id}/save-series 的请求。
// Seasons 为空时保存全部季度（media-editor 同款）。
type SaveSeriesRequest struct {
	StrmTaskID int64  `json:"strm_task_id"`
	ItemID     string `json:"item_id"`
	TMDBID     string `json:"tmdb_id,omitempty"`
	Seasons    []int  `json:"seasons,omitempty"`
}

// MatchWorkbenchRequest 是 POST /items/{id}/match 的请求（media-editor /api/match 同款）：
// 只写 tmdbid + uniqueid，不动其他栏位。
type MatchWorkbenchRequest struct {
	StrmTaskID int64  `json:"strm_task_id"`
	ItemID     string `json:"item_id"`
	TMDBID     string `json:"tmdb_id"`
}

// UploadWorkbenchImageRequest 是 POST /items/{id}/upload-image 的请求
//（media-editor /api/image-upload、/api/episode-thumb-upload 同款，15MB 上限）。
type UploadWorkbenchImageRequest struct {
	StrmTaskID int64  `json:"strm_task_id"`
	ItemID     string `json:"item_id"`
	Kind       string `json:"kind"` // poster | backdrop | logo | season_poster | episode_thumb
	Data       string `json:"data"` // data:image/...;base64,...
	Season     int    `json:"season,omitempty"`
	Episode    int    `json:"episode,omitempty"`
}

// DeleteWorkbenchImageRequest 是 POST /items/{id}/delete-image 的请求
//（media-editor /api/image-delete 同款）。
type DeleteWorkbenchImageRequest struct {
	StrmTaskID int64  `json:"strm_task_id"`
	ItemID     string `json:"item_id"`
	Kind       string `json:"kind"` // poster | backdrop | logo | season_poster
	Season     int    `json:"season,omitempty"`
}

// CopyWorkbenchImageRequest 是 POST /items/{id}/copy-image 的请求
//（media-editor /api/image-copy 同款，如主海报复制为季海报）。
type CopyWorkbenchImageRequest struct {
	StrmTaskID int64  `json:"strm_task_id"`
	ItemID     string `json:"item_id"`
	From       string `json:"from"` // poster | backdrop | logo | season_poster
	To         string `json:"to"`
	Season     int    `json:"season,omitempty"`
}

// GetWorkbench 组装工作台数据；本地无 NFO 时报错，引导先刮削（A4，media-editor /api/workbench 同款）。
func (s *Service) GetWorkbench(ctx context.Context, taskID int64, itemID, tmdbIDOverride string) (*WorkbenchData, error) {
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
	item := buildItem(taskID, root, g)
	data := &WorkbenchData{Item: item, MediaType: mediaType}

	nfoPath, ok := findWorkNFOPath(g, mediaType)
	if !ok {
		return nil, domain.Errorf(domain.CodeNotFound, "本地暂无 NFO，请先刮削一次再精修")
	}
	if fields, actors, ok := parseWorkNFOFile(nfoPath); ok {
		data.Local.Fields = fields
		data.Local.Actors = workbenchActorsFromLocal(actors)
	} else {
		return nil, domain.Errorf(domain.CodeDriverError, "本地 NFO 解析失败，无法精修")
	}
	data.Local.Images = s.workbenchLocalImages(taskID, root, g, mediaType)

	tmdbID := strings.TrimSpace(tmdbIDOverride)
	if tmdbID == "" {
		tmdbID = strings.TrimSpace(data.Local.Fields.TMDBID)
	}
	if tmdbID == "" {
		tmdbID = strings.TrimSpace(item.TMDBID)
	}
	data.TMDBID = tmdbID
	client := s.newTMDBClient()
	if client == nil {
		data.TMDBError = "未配置 TMDB API Key，工作台仅可手动编辑本地栏位"
		return data, nil
	}
	if tmdbID == "" {
		data.TMDBError = "本地 NFO 暂无 TMDB ID，请先重新匹配"
		return data, nil
	}
	data.TMDBAvailable = true
	wbCtx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	if err := s.fillWorkbenchTMDB(wbCtx, client, data, g, mediaType, taskID, root); err != nil {
		data.TMDBAvailable = false
		data.TMDBError = err.Error()
	}
	return data, nil
}

func (s *Service) fillWorkbenchTMDB(ctx context.Context, client *tmdb.Client, data *WorkbenchData, g workGroup, mediaType string, taskID int64, root string) error {
	tmdbID := data.TMDBID
	// media-editor 同款：基础详情、演职人员、图片分三次单独拉取，不用 append_to_response。
	raw, err := client.Lookup(ctx, tmdbID, mediaType)
	if err != nil {
		return fmt.Errorf("TMDB 查询失败：%w", err)
	}
	info, err := decodeTMDBInfo(raw, mediaType)
	if err != nil {
		return err
	}
	data.TMDB.Fields = fieldsFromTMDBInfo(info)
	data.TMDB.Fields.Country = "" // I1：工作台不写 country，TMDB 侧也不展示
	if creditsRaw, err := client.FetchCredits(ctx, tmdbID, mediaType); err != nil {
		data.CreditsError = fmt.Sprintf("演职人员读取失败：%v", err)
	} else {
		var payload map[string]any
		if err := json.Unmarshal(creditsRaw, &payload); err == nil {
			data.TMDB.Actors = workbenchActorsFromTMDB(decodeTMDBActors(payload["cast"], 20), client)
			// media-editor 同款：TMDB 已加载时前 20 名默认全勾。
			for i := range data.TMDB.Actors {
				data.TMDB.Actors[i].Selected = true
			}
		}
	}
	if imagesRaw, err := client.FetchImages(ctx, tmdbID, mediaType); err != nil {
		data.ImagesError = fmt.Sprintf("图片读取失败：%v", err)
	} else {
		posters, backdrops, logos := parseTMDBImageLists(imagesRaw, client)
		data.TMDB.Posters, data.TMDB.Backdrops, data.TMDB.Logos = posters, backdrops, logos
	}
	if mediaType != MediaTypeTV {
		return nil
	}
	return s.fillWorkbenchSeasons(ctx, client, data, g, taskID, root)
}

func (s *Service) fillWorkbenchSeasons(ctx context.Context, client *tmdb.Client, data *WorkbenchData, g workGroup, taskID int64, root string) error {
	rawSeasons, err := client.FetchTVSeasons(ctx, data.TMDBID)
	if err != nil {
		return fmt.Errorf("获取季列表失败：%w", err)
	}
	byNum := map[int]*WorkbenchSeason{}
	var order []int
	for _, raw := range rawSeasons {
		var m map[string]any
		if json.Unmarshal(raw, &m) != nil {
			continue
		}
		num := asInt(m["season_number"])
		if num == nil {
			continue
		}
		ws := &WorkbenchSeason{
			Number:     *num,
			Name:       strings.TrimSpace(anyString(m["name"])),
			Overview:   strings.TrimSpace(anyString(m["overview"])),
			AirDate:    strings.TrimSpace(anyString(m["air_date"])),
			PosterPath: strings.TrimSpace(anyString(m["poster_path"])),
		}
		if n := asInt(m["episode_count"]); n != nil {
			ws.EpisodeCount = *n
		}
		if ws.PosterPath != "" {
			ws.PosterURL = client.ImageURL(ws.PosterPath, "w500")
		}
		byNum[*num] = ws
		order = append(order, *num)
	}
	for _, sn := range listLocalSeasonNumbers(g.absDir) {
		if _, ok := byNum[sn]; !ok {
			byNum[sn] = &WorkbenchSeason{Number: sn}
			order = append(order, sn)
		}
	}
	sort.Ints(order)
	showName := workDisplayName(g)
	for _, sn := range order {
		ws := byNum[sn]
		// I6：季海报存在 = 根 seasonXX-poster.jpg 或季目录 poster.jpg 任一存在。
		ws.LocalPoster = fileExists(seasonPosterPath(g.absDir, sn))
		seasonDir := seasonDirPath(g.absDir, sn)
		if seasonDir != "" && fileExists(filepath.Join(seasonDir, "poster.jpg")) {
			ws.LocalPoster = true
			ws.LocalSeasonPoster = true
		}
		// G3：TMDB 季即使本地无文件，也加载分集与季海报。
		if imgRaw, err := client.FetchTVSeasonImages(ctx, data.TMDBID, sn); err == nil {
			posters, _, _ := parseTMDBImageLists(imgRaw, client)
			ws.Posters = posters
		}
		folder, _ := seasonLocalFolder(g, sn)
		if folder == "" {
			folder = g.absDir
		}
		epByNum := map[int]*WorkbenchEpisode{}
		var epOrder []int
		if detail, err := fetchSeasonDetail(ctx, client, data.TMDBID, sn); err == nil && detail != nil {
			for _, ep := range detail.Episodes {
				we := WorkbenchEpisode{
					Season:    sn,
					Episode:   ep.EpisodeNumber,
					Title:     ep.Name,
					Overview:  ep.Overview,
					AirDate:   ep.AirDate,
					StillPath: ep.StillPath,
					Runtime:   ep.Runtime,
					TMDBID:    ep.ID,
				}
				if ep.StillPath != "" {
					we.StillURL = client.ImageURL(ep.StillPath, "w500")
				}
				epByNum[ep.EpisodeNumber] = &we
				epOrder = append(epOrder, ep.EpisodeNumber)
			}
		}
		// G2：合并只有本地 NFO 的分集（media-editor mergedEpisodes 同款）。
		for _, lf := range listLocalEpisodeFields(folder, sn) {
			lf := lf
			if we, ok := epByNum[lf.Episode]; ok {
				mergeLocalEpisodeFields(we, &lf, showName)
				continue
			}
			we := &WorkbenchEpisode{
				Season:      sn,
				Episode:     lf.Episode,
				Title:       lf.Title,
				Overview:    lf.Plot,
				AirDate:     lf.Aired,
				Runtime:     lf.Runtime,
				LocalNFO:    true,
				LocalFields: &lf,
			}
			epByNum[lf.Episode] = we
			epOrder = append(epOrder, lf.Episode)
		}
		sort.Ints(epOrder)
		for _, en := range epOrder {
			we := epByNum[en]
			nfoPath := resolveEpisodeNFOPath(folder, sn, en)
			if fileExists(nfoPath) {
				we.LocalNFO = true
				if lf, ok := parseEpisodeNFOFileLenient(nfoPath); ok {
					lf := lf
					mergeLocalEpisodeFields(we, &lf, showName)
					we.LocalFields = &lf
				}
			}
			thumbStem := strings.TrimSuffix(nfoPath, filepath.Ext(nfoPath))
			for _, ext := range []string{".jpg", ".png"} {
				if thumbPath := thumbStem + "-thumb" + ext; fileExists(thumbPath) {
					we.LocalThumb = true
					if we.LocalThumbURL == "" {
						we.LocalThumbURL = posterURLFromRel(taskID, filepath.ToSlash(relUnder(root, thumbPath)))
					}
					break
				}
			}
			ws.Episodes = append(ws.Episodes, *we)
		}
	}
	for _, sn := range order {
		data.Seasons = append(data.Seasons, *byNum[sn])
	}
	return nil
}

// mergeLocalEpisodeFields 把本地分集栏位并入 TMDB 分集（media-editor mergedEpisodes 同款）：
// 本地标题非空且非自动生成时覆盖；本地简介/播出日/剧照/片长非空时覆盖。
func mergeLocalEpisodeFields(we *WorkbenchEpisode, lf *EpisodeFieldSet, showName string) {
	if lf == nil {
		return
	}
	if strings.TrimSpace(lf.Title) != "" && !isGeneratedEpisodeTitle(lf.Title, showName, we.Season, we.Episode) {
		we.Title = lf.Title
	}
	if strings.TrimSpace(lf.Plot) != "" {
		we.Overview = lf.Plot
	}
	if strings.TrimSpace(lf.Aired) != "" {
		we.AirDate = lf.Aired
	}
	if strings.TrimSpace(lf.Thumb) != "" {
		we.StillURL = lf.Thumb
	}
	if lf.Runtime > 0 {
		we.Runtime = lf.Runtime
	}
}

func (s *Service) workbenchLocalImages(taskID int64, root string, g workGroup, mediaType string) WorkbenchLocalImages {
	out := WorkbenchLocalImages{SeasonPosters: map[string]WorkbenchLocalImage{}}
	imageURL := func(path string) WorkbenchLocalImage {
		rel := filepath.ToSlash(relUnder(root, path))
		return WorkbenchLocalImage{Exists: true, Rel: rel, URL: posterURLFromRel(taskID, rel)}
	}
	// I5：当前主海报只认 canonical 海报文件（TV/目录作品为 poster.jpg，扁平单文件为 <stem>-poster.jpg），
	// 不认 folder.jpg/cover.jpg 等模糊候选（media-editor 只认 poster.jpg）。
	if _, poster := workMetaPaths(g, mediaType); fileExists(poster) {
		out.Poster = imageURL(poster)
	}
	if fanart := workFanartPath(g); fileExists(fanart) {
		out.Fanart = imageURL(fanart)
	}
	if logo := workClearLogoPath(g); fileExists(logo) {
		out.Logo = imageURL(logo)
	}
	for _, sn := range listLocalSeasonNumbers(g.absDir) {
		// I6：季海报 = 根 seasonXX-poster.jpg，缺失时回落到季目录 poster.jpg。
		if p := seasonPosterPath(g.absDir, sn); fileExists(p) {
			out.SeasonPosters[strconv.Itoa(sn)] = imageURL(p)
			continue
		}
		if dir := seasonDirPath(g.absDir, sn); dir != "" {
			if p := filepath.Join(dir, "poster.jpg"); fileExists(p) {
				out.SeasonPosters[strconv.Itoa(sn)] = imageURL(p)
			}
		}
	}
	return out
}

func workbenchActorsFromLocal(actors []nfoActor) []WorkbenchActor {
	out := make([]WorkbenchActor, 0, len(actors))
	for _, a := range actors {
		out = append(out, WorkbenchActor{
			Name:     strings.TrimSpace(a.Name),
			Role:     strings.TrimSpace(a.Role),
			Order:    a.Order,
			ThumbURL: strings.TrimSpace(a.Thumb),
			Selected: true,
		})
	}
	return out
}

func workbenchActorsFromTMDB(actors []tmdbActor, client *tmdb.Client) []WorkbenchActor {
	out := make([]WorkbenchActor, 0, len(actors))
	for _, a := range actors {
		out = append(out, WorkbenchActor{
			Name:        strings.TrimSpace(a.Name),
			Role:        strings.TrimSpace(a.Role),
			Order:       a.Order,
			ProfilePath: strings.TrimSpace(a.ProfilePath),
			ThumbURL:    client.ImageURL(a.ProfilePath, "w185"),
		})
	}
	return out
}

type tmdbImageItem struct {
	FilePath    string  `json:"file_path"`
	Width       int     `json:"width"`
	Height      int     `json:"height"`
	Language    *string `json:"iso_639_1"`
	VoteAverage float64 `json:"vote_average"`
}

func parseTMDBImageLists(raw json.RawMessage, client *tmdb.Client) (posters, backdrops, logos []WorkbenchImage) {
	var payload struct {
		Posters   []tmdbImageItem `json:"posters"`
		Backdrops []tmdbImageItem `json:"backdrops"`
		Logos     []tmdbImageItem `json:"logos"`
	}
	if json.Unmarshal(raw, &payload) != nil {
		return nil, nil, nil
	}
	convert := func(items []tmdbImageItem) []WorkbenchImage {
		out := make([]WorkbenchImage, 0, len(items))
		for _, item := range items {
			path := strings.TrimSpace(item.FilePath)
			if path == "" {
				continue
			}
			img := WorkbenchImage{
				FilePath:    path,
				URL:         client.ImageURL(path, "w500"),
				URLOriginal: client.ImageURL(path, "original"),
				Width:       item.Width,
				Height:      item.Height,
				VoteAverage: item.VoteAverage,
			}
			if item.Language != nil {
				img.Language = strings.TrimSpace(*item.Language)
			}
			out = append(out, img)
		}
		return out
	}
	return convert(payload.Posters), convert(payload.Backdrops), convert(payload.Logos)
}

// SaveWorkbenchFields 保存工作台编辑的栏位（根 NFO 只改 patch 里的栏位；分集 NFO 写 7 个字段）。
func (s *Service) SaveWorkbenchFields(ctx context.Context, req SaveFieldsRequest) (*Item, error) {
	if req.StrmTaskID <= 0 || strings.TrimSpace(req.ItemID) == "" {
		return nil, domain.Errorf(domain.CodeValidation, "参数不完整")
	}
	if !s.operationMu.TryLock() {
		return nil, domain.Errorf(domain.CodeValidation, "刮削任务进行中")
	}
	defer s.operationMu.Unlock()
	_, root, err := s.resolveTask(ctx, req.StrmTaskID)
	if err != nil {
		return nil, err
	}
	g, err := findWorkByID(root, req.ItemID)
	if err != nil {
		return nil, err
	}
	mediaType := resolveWorkMediaType(g)
	switch strings.ToLower(strings.TrimSpace(req.Kind)) {
	case "episode":
		if req.Season < 0 || req.Episode < 0 {
			return nil, domain.Errorf(domain.CodeValidation, "季/集号无效")
		}
		folder, ok := seasonLocalFolder(g, req.Season)
		if !ok {
			return nil, domain.Errorf(domain.CodeNotFound, "Season 目录不存在")
		}
		ep := req.EpisodeFields
		ep.Season = req.Season
		ep.Episode = req.Episode
		if err := mergeEpisodeNFOFields(resolveEpisodeNFOPath(folder, req.Season, req.Episode), ep); err != nil {
			return nil, domain.Errorf(domain.CodeDriverError, "写入分集 NFO：%v", err)
		}
	default:
		if len(req.Fields) == 0 {
			return nil, domain.Errorf(domain.CodeValidation, "没有需要保存的字段改动")
		}
		nfoPath, ok := findWorkNFOPath(g, mediaType)
		if !ok {
			return nil, domain.Errorf(domain.CodeNotFound, "本地暂无 NFO，请先刮削一次再精修")
		}
		rootTag := "movie"
		if mediaType == MediaTypeTV {
			rootTag = "tvshow"
		}
		if err := mergeWorkNFOPatch(nfoPath, rootTag, req.Fields); err != nil {
			return nil, domain.Errorf(domain.CodeDriverError, "写入 NFO：%v", err)
		}
		clearManualComplete(g)
	}
	s.upsertIndexItem(ctx, req.StrmTaskID, root, g)
	item := buildItem(req.StrmTaskID, root, g)
	return &item, nil
}

// SetWorkbenchImage 下载指定图片并写入现有文件名（海报/背景/Logo/季海报/分集剧照）。
func (s *Service) SetWorkbenchImage(ctx context.Context, req SetImageRequest) (*Item, error) {
	if req.StrmTaskID <= 0 || strings.TrimSpace(req.ItemID) == "" {
		return nil, domain.Errorf(domain.CodeValidation, "参数不完整")
	}
	kind := strings.ToLower(strings.TrimSpace(req.Kind))
	switch kind {
	case "poster", "backdrop", "logo", "season_poster", "episode_thumb":
	default:
		return nil, domain.Errorf(domain.CodeValidation, "图片类型无效")
	}
	if !s.operationMu.TryLock() {
		return nil, domain.Errorf(domain.CodeValidation, "刮削任务进行中")
	}
	defer s.operationMu.Unlock()
	_, root, err := s.resolveTask(ctx, req.StrmTaskID)
	if err != nil {
		return nil, err
	}
	g, err := findWorkByID(root, req.ItemID)
	if err != nil {
		return nil, err
	}
	mediaType := resolveWorkMediaType(g)

	size := "original"
	if kind == "episode_thumb" {
		size = "w500"
	}
	var data []byte
	tmdbPath := strings.TrimSpace(req.TMDBPath)
	imageURL := strings.TrimSpace(req.ImageURL)
	switch {
	case tmdbPath != "":
		if err := validateTMDBImagePath(tmdbPath); err != nil {
			return nil, err
		}
		client := s.newTMDBClient()
		if client == nil {
			return nil, domain.Errorf(domain.CodeValidation, "未配置 TMDB API Key")
		}
		dlCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		defer cancel()
		data, err = client.DownloadImage(dlCtx, tmdbPath, size)
		if err != nil {
			return nil, domain.Errorf(domain.CodeDriverError, "下载图片失败：%v", err)
		}
	case imageURL != "":
		if !strings.HasPrefix(imageURL, "http://") && !strings.HasPrefix(imageURL, "https://") {
			return nil, domain.Errorf(domain.CodeValidation, "图片地址无效")
		}
		dlCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		defer cancel()
		data, err = downloadRemoteImage(dlCtx, imageURL)
		if err != nil {
			return nil, domain.Errorf(domain.CodeDriverError, "下载图片失败：%v", err)
		}
	default:
		return nil, domain.Errorf(domain.CodeValidation, "请指定 TMDB 图片或图片地址")
	}

	var targets []string
	var epNFOPath string
	switch kind {
	case "poster":
		_, poster := workMetaPaths(g, mediaType)
		targets = []string{poster}
	case "backdrop":
		targets = []string{workFanartPath(g)}
	case "logo":
		targets = []string{workClearLogoPath(g)}
	case "season_poster":
		if req.Season < 0 {
			return nil, domain.Errorf(domain.CodeValidation, "季号无效")
		}
		targets = []string{seasonPosterPath(g.absDir, req.Season)}
		for _, d := range listLocalSeasonDirs(g.absDir) {
			if d.number == req.Season {
				targets = append(targets, filepath.Join(d.absPath, "poster.jpg"))
			}
		}
	case "episode_thumb":
		if req.Season < 0 || req.Episode < 0 {
			return nil, domain.Errorf(domain.CodeValidation, "季/集号无效")
		}
		folder, ok := seasonLocalFolder(g, req.Season)
		if !ok {
			return nil, domain.Errorf(domain.CodeNotFound, "Season 目录不存在")
		}
		epNFOPath = resolveEpisodeNFOPath(folder, req.Season, req.Episode)
		stem := strings.TrimSuffix(epNFOPath, filepath.Ext(epNFOPath))
		targets = []string{stem + "-thumb.jpg"}
	}
	for _, target := range targets {
		if err := writeImageFile(target, data); err != nil {
			return nil, domain.Errorf(domain.CodeDriverError, "写入图片：%v", err)
		}
	}
	// G5：设分集剧照时，把 w500 TMDB 地址（或原图地址）同步写入分集 NFO 的 <thumb>；
	// NFO 不存在时不新建，等用户保存本集时再写（media-editor 先写 NFO 再下载剧照）。
	if kind == "episode_thumb" && epNFOPath != "" && fileExists(epNFOPath) {
		thumbURL := imageURL
		if tmdbPath != "" {
			if client := s.newTMDBClient(); client != nil {
				thumbURL = client.ImageURL(tmdbPath, "w500")
			}
		}
		if thumbURL != "" {
			if err := updateEpisodeNFOThumb(epNFOPath, req.Season, req.Episode, thumbURL); err != nil {
				return nil, domain.Errorf(domain.CodeDriverError, "写入分集 NFO：%v", err)
			}
		}
	}
	s.upsertIndexItem(ctx, req.StrmTaskID, root, g)
	item := buildItem(req.StrmTaskID, root, g)
	return &item, nil
}

// updateEpisodeNFOThumb 只改分集 NFO 的 thumb 栏位（其余栏位原样保留）；
// NFO 不存在或不是标准 episodedetails 时跳过（不新建、不报错，保存本集时再写）。
func updateEpisodeNFOThumb(nfoPath string, season, episode int, thumbURL string) error {
	data, err := os.ReadFile(nfoPath)
	if err != nil {
		return nil
	}
	if !strings.Contains(strings.ToLower(string(data)), "<episodedetails") {
		return nil
	}
	tree, err := parseXMLTree(data)
	if err != nil || tree == nil || tree.Name.Local != "episodedetails" {
		return nil
	}
	tree.setText("thumb", thumbURL)
	return writeXMLTree(nfoPath, tree)
}

// SaveWorkbenchCast 替换根 NFO 的演员节点（media-editor /api/nfo actor 同款）：
// 先截取前 20 名再过滤空名；thumb 一律从 profile_path 派生 h632，绝不复用前端的 w185 预览地址；
// 只动演员节点，不动任何栏位。
func (s *Service) SaveWorkbenchCast(ctx context.Context, req SaveCastRequest) (*Item, error) {
	if req.StrmTaskID <= 0 || strings.TrimSpace(req.ItemID) == "" {
		return nil, domain.Errorf(domain.CodeValidation, "参数不完整")
	}
	if !s.operationMu.TryLock() {
		return nil, domain.Errorf(domain.CodeValidation, "刮削任务进行中")
	}
	defer s.operationMu.Unlock()
	_, root, err := s.resolveTask(ctx, req.StrmTaskID)
	if err != nil {
		return nil, err
	}
	g, err := findWorkByID(root, req.ItemID)
	if err != nil {
		return nil, err
	}
	mediaType := resolveWorkMediaType(g)
	actors := make([]nfoActor, 0, 20)
	submitted := req.Actors
	if len(submitted) > 20 {
		submitted = submitted[:20]
	}
	client := s.newTMDBClient()
	for _, a := range submitted {
		name := strings.TrimSpace(a.Name)
		if name == "" {
			continue
		}
		thumb := ""
		if profilePath := strings.TrimSpace(a.ProfilePath); profilePath != "" && client != nil {
			thumb = client.ImageURL(profilePath, "h632")
		} else {
			thumb = strings.TrimSpace(a.ThumbURL)
		}
		actors = append(actors, nfoActor{Name: name, Role: strings.TrimSpace(a.Role), Type: "Actor", Order: a.Order, Thumb: thumb})
	}
	nfoPath, ok := findWorkNFOPath(g, mediaType)
	if !ok {
		return nil, domain.Errorf(domain.CodeNotFound, "本地暂无 NFO，请先刮削一次再精修")
	}
	rootTag := "movie"
	if mediaType == MediaTypeTV {
		rootTag = "tvshow"
	}
	if err := mergeWorkNFOActors(nfoPath, rootTag, actors); err != nil {
		return nil, domain.Errorf(domain.CodeDriverError, "写入演员：%v", err)
	}
	clearManualComplete(g)
	s.upsertIndexItem(ctx, req.StrmTaskID, root, g)
	item := buildItem(req.StrmTaskID, root, g)
	return &item, nil
}

// SaveWorkbenchSeries 整剧保存（media-editor /api/tmdb-save-series 同款）：
// 根 NFO 只写 year/premiered/status/sorttitle/episodeguide/uniqueid + 重建 genre；
// 演员按 TMDB 替换；已勾选季度（默认全部）的每一集都写分集 NFO 并下载剧照；
// 不下载季海报、不写 season.nfo、不写季目录 poster.jpg。
func (s *Service) SaveWorkbenchSeries(ctx context.Context, req SaveSeriesRequest) (*Item, error) {
	if req.StrmTaskID <= 0 || strings.TrimSpace(req.ItemID) == "" {
		return nil, domain.Errorf(domain.CodeValidation, "参数不完整")
	}
	if !s.operationMu.TryLock() {
		return nil, domain.Errorf(domain.CodeValidation, "刮削任务进行中")
	}
	defer s.operationMu.Unlock()
	_, root, err := s.resolveTask(ctx, req.StrmTaskID)
	if err != nil {
		return nil, err
	}
	g, err := findWorkByID(root, req.ItemID)
	if err != nil {
		return nil, err
	}
	mediaType := resolveWorkMediaType(g)
	if mediaType != MediaTypeTV {
		return nil, domain.Errorf(domain.CodeValidation, "仅剧集支持整剧保存")
	}
	tmdbID := strings.TrimSpace(req.TMDBID)
	if tmdbID == "" {
		if meta, ok := readWorkNFOMeta(g, mediaType); ok {
			tmdbID = strings.TrimSpace(meta.TMDBID)
		}
	}
	if tmdbID == "" {
		return nil, domain.Errorf(domain.CodeValidation, "缺少 TMDB ID")
	}
	client := s.newTMDBClient()
	if client == nil {
		return nil, domain.Errorf(domain.CodeValidation, "未配置 TMDB API Key")
	}
	wbCtx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	raw, err := client.Lookup(wbCtx, tmdbID, MediaTypeTV)
	if err != nil {
		return nil, domain.Errorf(domain.CodeDriverError, "TMDB 查询失败：%v", err)
	}
	info, err := decodeTMDBInfo(raw, MediaTypeTV)
	if err != nil {
		return nil, err
	}
	seasons := make([]int, 0, len(req.Seasons))
	seen := map[int]bool{}
	for _, sn := range req.Seasons {
		if sn >= 0 && !seen[sn] {
			seen[sn] = true
			seasons = append(seasons, sn)
		}
	}
	if len(seasons) == 0 {
		var payload map[string]any
		if err := json.Unmarshal(raw, &payload); err == nil {
			if list, _ := payload["seasons"].([]any); len(list) > 0 {
				for _, item := range list {
					if m, ok := item.(map[string]any); ok {
						if n := asInt(m["season_number"]); n != nil && *n >= 0 && !seen[*n] {
							seen[*n] = true
							seasons = append(seasons, *n)
						}
					}
				}
			}
		}
	}
	sort.Ints(seasons)
	nfoPath, ok := findWorkNFOPath(g, mediaType)
	if !ok {
		return nil, domain.Errorf(domain.CodeNotFound, "本地暂无 NFO，请先刮削一次再精修")
	}
	if err := mergeSeriesNFO(nfoPath, info.Premiered, info.Status, firstNonEmpty(info.Title, info.Original), tmdbID, info.Genres); err != nil {
		return nil, domain.Errorf(domain.CodeDriverError, "写入剧集 NFO：%v", err)
	}
	creditsRaw, err := client.FetchCredits(wbCtx, tmdbID, MediaTypeTV)
	if err != nil {
		return nil, domain.Errorf(domain.CodeDriverError, "TMDB 演职人员查询失败：%v", err)
	}
	var creditsPayload map[string]any
	if err := json.Unmarshal(creditsRaw, &creditsPayload); err == nil {
		actors := buildNFOActors(client, decodeTMDBActors(creditsPayload["cast"], 20))
		if err := mergeWorkNFOActors(nfoPath, "tvshow", actors); err != nil {
			return nil, domain.Errorf(domain.CodeDriverError, "写入演员：%v", err)
		}
	}
	if _, _, _, err := s.writeWorkbenchSeriesEpisodes(wbCtx, client, g, tmdbID, seasons); err != nil {
		return nil, err
	}
	clearManualComplete(g)
	s.upsertIndexItem(ctx, req.StrmTaskID, root, g)
	item := buildItem(req.StrmTaskID, root, g)
	return &item, nil
}

// MatchWorkbench 在工作台内重新匹配 TMDB（media-editor /api/match 同款）：
// 只写 tmdbid + uniqueid，不动其他栏位。
func (s *Service) MatchWorkbench(ctx context.Context, req MatchWorkbenchRequest) (*Item, error) {
	if req.StrmTaskID <= 0 || strings.TrimSpace(req.ItemID) == "" || strings.TrimSpace(req.TMDBID) == "" {
		return nil, domain.Errorf(domain.CodeValidation, "参数不完整")
	}
	tmdbID := strings.TrimSpace(req.TMDBID)
	if _, err := strconv.Atoi(tmdbID); err != nil || tmdbID == "" {
		return nil, domain.Errorf(domain.CodeValidation, "无效的 TMDB ID")
	}
	if !s.operationMu.TryLock() {
		return nil, domain.Errorf(domain.CodeValidation, "刮削任务进行中")
	}
	defer s.operationMu.Unlock()
	_, root, err := s.resolveTask(ctx, req.StrmTaskID)
	if err != nil {
		return nil, err
	}
	g, err := findWorkByID(root, req.ItemID)
	if err != nil {
		return nil, err
	}
	mediaType := resolveWorkMediaType(g)
	nfoPath, ok := findWorkNFOPath(g, mediaType)
	if !ok {
		return nil, domain.Errorf(domain.CodeNotFound, "本地暂无 NFO，请先刮削一次再精修")
	}
	rootTag := "movie"
	if mediaType == MediaTypeTV {
		rootTag = "tvshow"
	}
	tree, err := readNFOTreeForMerge(nfoPath, rootTag)
	if err != nil {
		return nil, err
	}
	tree.setText("tmdbid", tmdbID)
	tree.setUniqueID(tmdbID)
	if err := writeXMLTree(nfoPath, tree); err != nil {
		return nil, domain.Errorf(domain.CodeDriverError, "写入 NFO：%v", err)
	}
	clearManualComplete(g)
	s.upsertIndexItem(ctx, req.StrmTaskID, root, g)
	item := buildItem(req.StrmTaskID, root, g)
	return &item, nil
}

// GetWorkbenchEpisodeImages 返回单集的全部 TMDB 剧照（media-editor /api/tmdb/episode 同款，全部语言）。
func (s *Service) GetWorkbenchEpisodeImages(ctx context.Context, taskID int64, itemID string, season, episode int) ([]WorkbenchImage, error) {
	if taskID <= 0 || strings.TrimSpace(itemID) == "" {
		return nil, domain.Errorf(domain.CodeValidation, "参数不完整")
	}
	if season < 0 || episode < 0 {
		return nil, domain.Errorf(domain.CodeValidation, "季/集号无效")
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
	tmdbID := ""
	if meta, ok := readWorkNFOMeta(g, mediaType); ok {
		tmdbID = strings.TrimSpace(meta.TMDBID)
	}
	if tmdbID == "" {
		return nil, domain.Errorf(domain.CodeValidation, "本地 NFO 暂无 TMDB ID，请先重新匹配")
	}
	client := s.newTMDBClient()
	if client == nil {
		return nil, domain.Errorf(domain.CodeValidation, "未配置 TMDB API Key")
	}
	raw, err := client.FetchTVEpisodeImages(ctx, tmdbID, season, episode)
	if err != nil {
		return nil, domain.Errorf(domain.CodeDriverError, "TMDB 剧照查询失败：%v", err)
	}
	var payload struct {
		Stills []tmdbImageItem `json:"stills"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, domain.Errorf(domain.CodeDriverError, "TMDB 剧照解析失败：%v", err)
	}
	out := make([]WorkbenchImage, 0, len(payload.Stills))
	for _, item := range payload.Stills {
		path := strings.TrimSpace(item.FilePath)
		if path == "" {
			continue
		}
		img := WorkbenchImage{
			FilePath:    path,
			URL:         client.ImageURL(path, "w500"),
			URLOriginal: client.ImageURL(path, "original"),
			Width:       item.Width,
			Height:      item.Height,
			VoteAverage: item.VoteAverage,
		}
		if item.Language != nil {
			img.Language = strings.TrimSpace(*item.Language)
		}
		out = append(out, img)
	}
	return out, nil
}

// workbenchImageTargets 返回某图片种类的写入目标（季海报同时写根文件与季目录 poster.jpg，
// media-editor /api/image 同款）。
func workbenchImageTargets(g workGroup, mediaType, kind string, season int) ([]string, error) {
	switch kind {
	case "poster":
		_, poster := workMetaPaths(g, mediaType)
		return []string{poster}, nil
	case "backdrop":
		return []string{workFanartPath(g)}, nil
	case "logo":
		return []string{workClearLogoPath(g)}, nil
	case "season_poster":
		if season < 0 {
			return nil, domain.Errorf(domain.CodeValidation, "季号无效")
		}
		targets := []string{seasonPosterPath(g.absDir, season)}
		if dir := seasonDirPath(g.absDir, season); dir != "" {
			targets = append(targets, filepath.Join(dir, "poster.jpg"))
		}
		return targets, nil
	default:
		return nil, domain.Errorf(domain.CodeValidation, "图片类型无效")
	}
}

// workbenchImageSource 返回某图片种类的读取源（季海报缺失时回落到季目录 poster.jpg，
// media-editor /api/image-copy 同款）。
func workbenchImageSource(g workGroup, mediaType, kind string, season int) (string, error) {
	targets, err := workbenchImageTargets(g, mediaType, kind, season)
	if err != nil {
		return "", err
	}
	for _, t := range targets {
		if fileExists(t) {
			return t, nil
		}
	}
	return "", domain.Errorf(domain.CodeNotFound, "源图片不存在")
}

// UploadWorkbenchImage 上传本地图片设为海报/背景/Logo/季海报/分集剧照
//（media-editor /api/image-upload、/api/episode-thumb-upload 同款，15MB 上限）。
func (s *Service) UploadWorkbenchImage(ctx context.Context, req UploadWorkbenchImageRequest) (*Item, error) {
	if req.StrmTaskID <= 0 || strings.TrimSpace(req.ItemID) == "" {
		return nil, domain.Errorf(domain.CodeValidation, "参数不完整")
	}
	kind := strings.ToLower(strings.TrimSpace(req.Kind))
	switch kind {
	case "poster", "backdrop", "logo", "season_poster", "episode_thumb":
	default:
		return nil, domain.Errorf(domain.CodeValidation, "图片类型无效")
	}
	dataURI := strings.TrimSpace(req.Data)
	if !strings.HasPrefix(dataURI, "data:image/") || !strings.Contains(dataURI, ",") {
		return nil, domain.Errorf(domain.CodeValidation, "无效的图片数据")
	}
	imageData, err := base64.StdEncoding.DecodeString(strings.SplitN(dataURI, ",", 2)[1])
	if err != nil {
		return nil, domain.Errorf(domain.CodeValidation, "图片解码失败")
	}
	if len(imageData) > maxWorkbenchRemoteImage {
		return nil, domain.Errorf(domain.CodeValidation, "图片超过 15MB")
	}
	if !s.operationMu.TryLock() {
		return nil, domain.Errorf(domain.CodeValidation, "刮削任务进行中")
	}
	defer s.operationMu.Unlock()
	_, root, err := s.resolveTask(ctx, req.StrmTaskID)
	if err != nil {
		return nil, err
	}
	g, err := findWorkByID(root, req.ItemID)
	if err != nil {
		return nil, err
	}
	mediaType := resolveWorkMediaType(g)
	var targets []string
	if kind == "episode_thumb" {
		if req.Season < 0 || req.Episode < 0 {
			return nil, domain.Errorf(domain.CodeValidation, "季/集号无效")
		}
		folder, ok := seasonLocalFolder(g, req.Season)
		if !ok {
			return nil, domain.Errorf(domain.CodeNotFound, "Season 目录不存在")
		}
		epNFOPath := resolveEpisodeNFOPath(folder, req.Season, req.Episode)
		stem := strings.TrimSuffix(epNFOPath, filepath.Ext(epNFOPath))
		targets = []string{stem + "-thumb.jpg"}
	} else {
		targets, err = workbenchImageTargets(g, mediaType, kind, req.Season)
		if err != nil {
			return nil, err
		}
	}
	for _, target := range targets {
		if err := writeImageFile(target, imageData); err != nil {
			return nil, domain.Errorf(domain.CodeDriverError, "写入图片：%v", err)
		}
	}
	s.upsertIndexItem(ctx, req.StrmTaskID, root, g)
	item := buildItem(req.StrmTaskID, root, g)
	return &item, nil
}

// DeleteWorkbenchImage 删除本地图片（media-editor /api/image-delete 同款；
// 季海报同时删除根文件与季目录 poster.jpg）。
func (s *Service) DeleteWorkbenchImage(ctx context.Context, req DeleteWorkbenchImageRequest) (*Item, error) {
	if req.StrmTaskID <= 0 || strings.TrimSpace(req.ItemID) == "" {
		return nil, domain.Errorf(domain.CodeValidation, "参数不完整")
	}
	kind := strings.ToLower(strings.TrimSpace(req.Kind))
	switch kind {
	case "poster", "backdrop", "logo", "season_poster":
	default:
		return nil, domain.Errorf(domain.CodeValidation, "图片类型无效")
	}
	if !s.operationMu.TryLock() {
		return nil, domain.Errorf(domain.CodeValidation, "刮削任务进行中")
	}
	defer s.operationMu.Unlock()
	_, root, err := s.resolveTask(ctx, req.StrmTaskID)
	if err != nil {
		return nil, err
	}
	g, err := findWorkByID(root, req.ItemID)
	if err != nil {
		return nil, err
	}
	mediaType := resolveWorkMediaType(g)
	targets, err := workbenchImageTargets(g, mediaType, kind, req.Season)
	if err != nil {
		return nil, err
	}
	for _, target := range targets {
		_ = os.Remove(target)
	}
	s.upsertIndexItem(ctx, req.StrmTaskID, root, g)
	item := buildItem(req.StrmTaskID, root, g)
	return &item, nil
}

// CopyWorkbenchImage 复制本地图片（media-editor /api/image-copy 同款，
// 如把主海报复制为季海报；目标为季海报时同时写根文件与季目录 poster.jpg）。
func (s *Service) CopyWorkbenchImage(ctx context.Context, req CopyWorkbenchImageRequest) (*Item, error) {
	if req.StrmTaskID <= 0 || strings.TrimSpace(req.ItemID) == "" {
		return nil, domain.Errorf(domain.CodeValidation, "参数不完整")
	}
	from := strings.ToLower(strings.TrimSpace(req.From))
	to := strings.ToLower(strings.TrimSpace(req.To))
	for _, kind := range []string{from, to} {
		switch kind {
		case "poster", "backdrop", "logo", "season_poster":
		default:
			return nil, domain.Errorf(domain.CodeValidation, "图片类型无效")
		}
	}
	if !s.operationMu.TryLock() {
		return nil, domain.Errorf(domain.CodeValidation, "刮削任务进行中")
	}
	defer s.operationMu.Unlock()
	_, root, err := s.resolveTask(ctx, req.StrmTaskID)
	if err != nil {
		return nil, err
	}
	g, err := findWorkByID(root, req.ItemID)
	if err != nil {
		return nil, err
	}
	mediaType := resolveWorkMediaType(g)
	src, err := workbenchImageSource(g, mediaType, from, req.Season)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(src)
	if err != nil {
		return nil, domain.Errorf(domain.CodeDriverError, "读取源图片：%v", err)
	}
	targets, err := workbenchImageTargets(g, mediaType, to, req.Season)
	if err != nil {
		return nil, err
	}
	for _, target := range targets {
		if err := writeImageFile(target, data); err != nil {
			return nil, domain.Errorf(domain.CodeDriverError, "写入图片：%v", err)
		}
	}
	s.upsertIndexItem(ctx, req.StrmTaskID, root, g)
	item := buildItem(req.StrmTaskID, root, g)
	return &item, nil
}

func validateTMDBImagePath(p string) error {
	p = strings.TrimSpace(p)
	if !strings.HasPrefix(p, "/") || strings.Contains(p, "..") {
		return domain.Errorf(domain.CodeValidation, "TMDB 图片路径无效")
	}
	switch strings.ToLower(filepath.Ext(p)) {
	case ".jpg", ".jpeg", ".png", ".webp":
		return nil
	default:
		return domain.Errorf(domain.CodeValidation, "仅支持 jpg/png/webp 图片")
	}
}

// downloadRemoteImage 直接贴 URL 设图时下载（15MB 上限，与 media-editor 一致）。
func downloadRemoteImage(ctx context.Context, rawURL string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "litepan-workbench/1.0")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("图片地址返回 HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxWorkbenchRemoteImage+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxWorkbenchRemoteImage {
		return nil, fmt.Errorf("图片超过 15MB")
	}
	return data, nil
}
