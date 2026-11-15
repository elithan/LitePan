package strmscrape

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
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
	Season      int              `json:"season"`
	Episode     int              `json:"episode"`
	Title       string           `json:"title"`
	Overview    string           `json:"overview"`
	AirDate     string           `json:"air_date,omitempty"`
	StillPath   string           `json:"still_path,omitempty"`
	StillURL    string           `json:"still_url,omitempty"`
	Runtime     int              `json:"runtime,omitempty"`
	TMDBID      int              `json:"tmdb_id,omitempty"`
	LocalNFO    bool             `json:"local_nfo"`
	LocalThumb  bool             `json:"local_thumb"`
	LocalFields *EpisodeFieldSet `json:"local_fields,omitempty"`
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
	Item          Item            `json:"item"`
	MediaType     string          `json:"media_type"`
	TMDBID        string          `json:"tmdb_id,omitempty"`
	Local         WorkbenchLocal  `json:"local"`
	TMDB          WorkbenchTMDB   `json:"tmdb"`
	Seasons       []WorkbenchSeason `json:"seasons,omitempty"`
	TMDBAvailable bool            `json:"tmdb_available"`
	TMDBError     string          `json:"tmdb_error,omitempty"`
}

// SaveFieldsRequest 是 POST /items/{id}/save-fields 的请求。
type SaveFieldsRequest struct {
	StrmTaskID    int64           `json:"strm_task_id"`
	ItemID        string          `json:"item_id"`
	Kind          string          `json:"kind"` // work | episode
	Season        int             `json:"season,omitempty"`
	Episode       int             `json:"episode,omitempty"`
	Fields        FieldSet        `json:"fields"`
	EpisodeFields EpisodeFieldSet `json:"episode_fields"`
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
type SaveSeriesRequest struct {
	StrmTaskID int64  `json:"strm_task_id"`
	ItemID     string `json:"item_id"`
	TMDBID     string `json:"tmdb_id,omitempty"`
}

// GetWorkbench 组装工作台数据；TMDB 取不到时不整体失败，只填 TMDBError。
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

	nfoPath, _ := workMetaPaths(g, mediaType)
	if fields, actors, ok := parseWorkNFOFile(nfoPath); ok {
		data.Local.Fields = fields
		data.Local.Actors = workbenchActorsFromLocal(actors)
	} else {
		data.Local.Fields = FieldSet{Title: item.Title, TMDBID: item.TMDBID, UniqueID: item.TMDBID}
		if item.Year != nil {
			data.Local.Fields.Year = item.Year
		}
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
	if err := s.fillWorkbenchTMDB(wbCtx, client, data, g, mediaType); err != nil {
		data.TMDBAvailable = false
		data.TMDBError = err.Error()
	}
	return data, nil
}

func (s *Service) fillWorkbenchTMDB(ctx context.Context, client *tmdb.Client, data *WorkbenchData, g workGroup, mediaType string) error {
	tmdbID := data.TMDBID
	creditsKey := "credits"
	if mediaType == MediaTypeTV {
		creditsKey = "aggregate_credits"
	}
	raw, err := client.LookupWithAppend(ctx, tmdbID, mediaType, creditsKey)
	if err != nil {
		return fmt.Errorf("TMDB 查询失败：%w", err)
	}
	info, err := decodeTMDBInfo(raw, mediaType)
	if err != nil {
		return err
	}
	data.TMDB.Fields = fieldsFromTMDBInfo(info)
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err == nil {
		data.TMDB.Actors = workbenchActorsFromTMDB(decodeTMDBActors(payload[creditsKey], 20), client)
		localNames := map[string]bool{}
		for _, a := range data.Local.Actors {
			localNames[strings.ToLower(strings.TrimSpace(a.Name))] = true
		}
		for i := range data.TMDB.Actors {
			data.TMDB.Actors[i].Selected = localNames[strings.ToLower(strings.TrimSpace(data.TMDB.Actors[i].Name))]
		}
	}
	if imagesRaw, err := client.FetchImages(ctx, tmdbID, mediaType); err == nil {
		posters, backdrops, logos := parseTMDBImageLists(imagesRaw, client)
		data.TMDB.Posters, data.TMDB.Backdrops, data.TMDB.Logos = posters, backdrops, logos
	}
	if mediaType != MediaTypeTV {
		return nil
	}
	return s.fillWorkbenchSeasons(ctx, client, data, g)
}

func (s *Service) fillWorkbenchSeasons(ctx context.Context, client *tmdb.Client, data *WorkbenchData, g workGroup) error {
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
			Number:    *num,
			Name:      strings.TrimSpace(anyString(m["name"])),
			Overview:  strings.TrimSpace(anyString(m["overview"])),
			AirDate:   strings.TrimSpace(anyString(m["air_date"])),
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
	localSeasons := listLocalSeasonNumbers(g.absDir)
	localSet := map[int]bool{}
	for _, sn := range localSeasons {
		localSet[sn] = true
		if _, ok := byNum[sn]; !ok {
			byNum[sn] = &WorkbenchSeason{Number: sn}
			order = append(order, sn)
		}
	}
	sort.Ints(order)
	epFiles := episodeFileMap(g)
	seasonDirs := listLocalSeasonDirs(g.absDir)
	for _, sn := range order {
		ws := byNum[sn]
		ws.LocalPoster = fileExists(seasonPosterPath(g.absDir, sn))
		for _, d := range seasonDirs {
			if d.number == sn {
				ws.LocalSeasonPoster = fileExists(filepath.Join(d.absPath, "poster.jpg"))
				break
			}
		}
		if !localSet[sn] {
			continue
		}
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
				if strmPath, ok := epFiles[[2]int{sn, ep.EpisodeNumber}]; ok {
					stem := strings.TrimSuffix(strmPath, filepath.Ext(strmPath))
					we.LocalNFO = fileExists(stem + ".nfo")
					we.LocalThumb = fileExists(stem + "-thumb.jpg")
					if we.LocalNFO {
						if lf, ok := parseEpisodeNFOFile(stem + ".nfo"); ok {
							lf := lf
							we.LocalFields = &lf
						}
					}
				}
				ws.Episodes = append(ws.Episodes, we)
			}
		}
		if imgRaw, err := client.FetchTVSeasonImages(ctx, data.TMDBID, sn); err == nil {
			posters, _, _ := parseTMDBImageLists(imgRaw, client)
			ws.Posters = posters
		}
	}
	for _, sn := range order {
		data.Seasons = append(data.Seasons, *byNum[sn])
	}
	return nil
}

func (s *Service) workbenchLocalImages(taskID int64, root string, g workGroup, mediaType string) WorkbenchLocalImages {
	out := WorkbenchLocalImages{SeasonPosters: map[string]WorkbenchLocalImage{}}
	imageURL := func(path string) WorkbenchLocalImage {
		rel := filepath.ToSlash(relUnder(root, path))
		return WorkbenchLocalImage{Exists: true, Rel: rel, URL: posterURLFromRel(taskID, rel)}
	}
	if workHasPoster(g, mediaType) {
		out.Poster = imageURL(workPosterFile(g, mediaType))
	}
	if fanart := workFanartPath(g); fileExists(fanart) {
		out.Fanart = imageURL(fanart)
	}
	if logo := workClearLogoPath(g); fileExists(logo) {
		out.Logo = imageURL(logo)
	}
	for _, sn := range listLocalSeasonNumbers(g.absDir) {
		if p := seasonPosterPath(g.absDir, sn); fileExists(p) {
			out.SeasonPosters[strconv.Itoa(sn)] = imageURL(p)
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

// SaveWorkbenchFields 保存工作台编辑的栏位（根 NFO 或单集 NFO，合并写入）。
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
		if req.Season < 0 || req.Episode <= 0 {
			return nil, domain.Errorf(domain.CodeValidation, "季/集号无效")
		}
		strmPath, ok := episodeFileMap(g)[[2]int{req.Season, req.Episode}]
		if !ok {
			return nil, domain.Errorf(domain.CodeNotFound, "本地无此分集")
		}
		stem := strings.TrimSuffix(strmPath, filepath.Ext(strmPath))
		ep := req.EpisodeFields
		ep.Season = req.Season
		ep.Episode = req.Episode
		if err := mergeEpisodeNFOFields(stem+".nfo", ep); err != nil {
			return nil, domain.Errorf(domain.CodeDriverError, "写入分集 NFO：%v", err)
		}
	default:
		nfoPath, _ := workMetaPaths(g, mediaType)
		rootTag := "movie"
		if mediaType == MediaTypeTV {
			rootTag = "tvshow"
		}
		if err := mergeWorkNFOFields(nfoPath, rootTag, req.Fields, nil, false); err != nil {
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
		if req.Season < 0 || req.Episode <= 0 {
			return nil, domain.Errorf(domain.CodeValidation, "季/集号无效")
		}
		strmPath, ok := episodeFileMap(g)[[2]int{req.Season, req.Episode}]
		if !ok {
			return nil, domain.Errorf(domain.CodeNotFound, "本地无此分集")
		}
		stem := strings.TrimSuffix(strmPath, filepath.Ext(strmPath))
		targets = []string{stem + "-thumb.jpg"}
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

// SaveWorkbenchCast 替换根 NFO 的演员节点（media-editor 勾选演员同款）。
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
	actors := make([]nfoActor, 0, len(req.Actors))
	for i, a := range req.Actors {
		name := strings.TrimSpace(a.Name)
		if name == "" {
			continue
		}
		if len(actors) >= 20 {
			break
		}
		thumb := strings.TrimSpace(a.ThumbURL)
		if thumb == "" && strings.TrimSpace(a.ProfilePath) != "" {
			if client := s.newTMDBClient(); client != nil {
				thumb = client.ImageURL(a.ProfilePath, "h632")
			}
		}
		actors = append(actors, nfoActor{Name: name, Role: strings.TrimSpace(a.Role), Order: i, Thumb: thumb})
	}
	nfoPath, _ := workMetaPaths(g, mediaType)
	rootTag := "movie"
	if mediaType == MediaTypeTV {
		rootTag = "tvshow"
	}
	fields := FieldSet{}
	if existing, _, ok := parseWorkNFOFile(nfoPath); ok {
		fields = existing
	} else {
		item := buildItem(req.StrmTaskID, root, g)
		fields = FieldSet{Title: item.Title, TMDBID: item.TMDBID, UniqueID: item.TMDBID}
		if item.Year != nil {
			fields.Year = item.Year
		}
	}
	if err := mergeWorkNFOFields(nfoPath, rootTag, fields, actors, true); err != nil {
		return nil, domain.Errorf(domain.CodeDriverError, "写入演员：%v", err)
	}
	clearManualComplete(g)
	s.upsertIndexItem(ctx, req.StrmTaskID, root, g)
	item := buildItem(req.StrmTaskID, root, g)
	return &item, nil
}

// SaveWorkbenchSeries 整剧保存（media-editor tmdb-save-series 同款）：
// 按 TMDB 重写剧集 NFO 栏位 + 替换演员 + 补齐季海报/分集 NFO/剧照（覆盖模式）。
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
	raw, err := client.LookupWithAppend(wbCtx, tmdbID, MediaTypeTV, "aggregate_credits")
	if err != nil {
		return nil, domain.Errorf(domain.CodeDriverError, "TMDB 查询失败：%v", err)
	}
	info, err := decodeTMDBInfo(raw, MediaTypeTV)
	if err != nil {
		return nil, err
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err == nil {
		info.Actors = decodeTMDBActors(payload["aggregate_credits"], 20)
	}
	fields := fieldsFromTMDBInfo(info)
	nfoPath, _ := workMetaPaths(g, mediaType)
	actors := buildNFOActors(client, info.Actors)
	if err := mergeWorkNFOFields(nfoPath, "tvshow", fields, actors, true); err != nil {
		return nil, domain.Errorf(domain.CodeDriverError, "写入剧集 NFO：%v", err)
	}
	if err := s.writeTVExtras(wbCtx, client, g, info, true); err != nil {
		return nil, err
	}
	clearManualComplete(g)
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
