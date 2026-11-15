package api

import (
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"litepan/internal/domain"
	"litepan/internal/strmscrape"
)

func parseStrmScrapeListQuery(r *http.Request) strmscrape.ItemListQuery {
	q := r.URL.Query()
	return strmscrape.ItemListQuery{
		Offset:    parseInt(q.Get("offset")),
		Limit:     parseInt(q.Get("limit")),
		Keyword:   strings.TrimSpace(q.Get("keyword")),
		Status:    strings.TrimSpace(q.Get("status")),
		MediaType: strings.TrimSpace(q.Get("media_type")),
		TVState:   strings.TrimSpace(q.Get("tv_state")),
		Sort:      strmscrape.ItemListSort(strings.TrimSpace(q.Get("sort"))),
	}
}

func parseInt(raw string) int {
	v, _ := strconv.Atoi(strings.TrimSpace(raw))
	return v
}

// strmTaskIDQuery 统一解析并校验 strm_task_id 参数。
func strmTaskIDQuery(r *http.Request) (int64, error) {
	taskID, err := strconv.ParseInt(strings.TrimSpace(r.URL.Query().Get("strm_task_id")), 10, 64)
	if err != nil || taskID <= 0 {
		return 0, domain.Errorf(domain.CodeValidation, "strm_task_id 无效")
	}
	return taskID, nil
}

func (h *Handler) getStrmScrapeSettings(w http.ResponseWriter, r *http.Request) {
	if !ensureServiceReady(w, h.strmScrape != nil) {
		return
	}
	cfg := h.strmScrape.GetSettings()
	if cfg.ProxyPassword != "" {
		cfg.ProxyPassword = ""
	}
	writeOK(w, cfg)
}

func (h *Handler) updateStrmScrapeSettings(w http.ResponseWriter, r *http.Request) {
	if !ensureServiceReady(w, h.strmScrape != nil) {
		return
	}
	var req strmscrape.Settings
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, err)
		return
	}
	if err := h.strmScrape.UpdateSettings(r.Context(), req); err != nil {
		writeErr(w, err)
		return
	}
	h.getStrmScrapeSettings(w, r)
}

func (h *Handler) getStrmScrapeScope(w http.ResponseWriter, r *http.Request) {
	if !ensureServiceReady(w, h.strmScrape != nil) {
		return
	}
	taskID, err := strmTaskIDQuery(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeOK(w, h.strmScrape.GetScope(taskID))
}

func (h *Handler) updateStrmScrapeScope(w http.ResponseWriter, r *http.Request) {
	if !ensureServiceReady(w, h.strmScrape != nil) {
		return
	}
	var req strmscrape.Scope
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, err)
		return
	}
	result, err := h.strmScrape.UpdateScope(r.Context(), req)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeOK(w, result)
}

func (h *Handler) listStrmScrapeScopeDirectories(w http.ResponseWriter, r *http.Request) {
	if !ensureServiceReady(w, h.strmScrape != nil) {
		return
	}
	taskID, err := strmTaskIDQuery(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	dirs, err := h.strmScrape.ListScopeDirectories(r.Context(), taskID, r.URL.Query().Get("parent"))
	if err != nil {
		writeErr(w, err)
		return
	}
	writeOK(w, dirs)
}

func (h *Handler) runStrmScrape(w http.ResponseWriter, r *http.Request) {
	if !ensureServiceReady(w, h.strmScrape != nil) {
		return
	}
	var req strmscrape.RunRequest
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, err)
		return
	}
	if err := h.strmScrape.RunAsync(r.Context(), req); err != nil {
		writeErr(w, err)
		return
	}
	writeOK(w, h.strmScrape.GetProgress())
}

func (h *Handler) stopStrmScrape(w http.ResponseWriter, r *http.Request) {
	if !ensureServiceReady(w, h.strmScrape != nil) {
		return
	}
	h.strmScrape.Stop()
	writeOK(w, h.strmScrape.GetProgress())
}

func (h *Handler) getStrmScrapeProgress(w http.ResponseWriter, r *http.Request) {
	if !ensureServiceReady(w, h.strmScrape != nil) {
		return
	}
	writeOK(w, h.strmScrape.GetProgress())
}

func (h *Handler) listStrmScrapeItems(w http.ResponseWriter, r *http.Request) {
	if !ensureServiceReady(w, h.strmScrape != nil) {
		return
	}
	taskID, err := strmTaskIDQuery(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	items, err := h.strmScrape.ListItems(r.Context(), taskID, parseStrmScrapeListQuery(r))
	if err != nil {
		writeErr(w, err)
		return
	}
	writeOK(w, items)
}

func (h *Handler) refreshStrmScrapeIndex(w http.ResponseWriter, r *http.Request) {
	if !ensureServiceReady(w, h.strmScrape != nil) {
		return
	}
	var req struct {
		StrmTaskID int64                   `json:"strm_task_id"`
		Offset     int                     `json:"offset"`
		Limit      int                     `json:"limit"`
		Keyword    string                  `json:"keyword"`
		Status     string                  `json:"status"`
		MediaType  string                  `json:"media_type"`
		TVState    string                  `json:"tv_state"`
		Sort       strmscrape.ItemListSort `json:"sort"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, err)
		return
	}
	items, err := h.strmScrape.RefreshIndex(r.Context(), req.StrmTaskID, strmscrape.ItemListQuery{
		Offset:    req.Offset,
		Limit:     req.Limit,
		Keyword:   strings.TrimSpace(req.Keyword),
		Status:    strings.TrimSpace(req.Status),
		MediaType: strings.TrimSpace(req.MediaType),
		TVState:   strings.TrimSpace(req.TVState),
		Sort:      req.Sort,
	})
	if err != nil {
		writeErr(w, err)
		return
	}
	writeOK(w, items)
}

func (h *Handler) rematchStrmScrapeItem(w http.ResponseWriter, r *http.Request) {
	if !ensureServiceReady(w, h.strmScrape != nil) {
		return
	}
	var req strmscrape.RematchRequest
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, err)
		return
	}
	item, started, err := h.strmScrape.Rematch(r.Context(), req)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeOK(w, map[string]any{
		"item":     item,
		"started":  started,
		"progress": h.strmScrape.GetProgress(),
	})
}

func (h *Handler) markStrmScrapeNormal(w http.ResponseWriter, r *http.Request) {
	if !ensureServiceReady(w, h.strmScrape != nil) {
		return
	}
	var req strmscrape.MarkNormalRequest
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, err)
		return
	}
	item, err := h.strmScrape.MarkNormal(r.Context(), req)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeOK(w, item)
}

func (h *Handler) rescrapeStrmScrapeItem(w http.ResponseWriter, r *http.Request) {
	if !ensureServiceReady(w, h.strmScrape != nil) {
		return
	}
	var req strmscrape.RescrapeRequest
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, err)
		return
	}
	item, started, err := h.strmScrape.Rescrape(r.Context(), req)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeOK(w, map[string]any{
		"item":     item,
		"started":  started,
		"progress": h.strmScrape.GetProgress(),
	})
}

func (h *Handler) getStrmScrapePoster(w http.ResponseWriter, r *http.Request) {
	if !ensureServiceReady(w, h.strmScrape != nil) {
		return
	}
	taskID, err := strmTaskIDQuery(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	rel := strings.TrimSpace(r.URL.Query().Get("rel"))
	path, err := h.strmScrape.ResolvePosterFile(r.Context(), taskID, rel)
	if err != nil {
		writeErr(w, err)
		return
	}
	f, err := os.Open(path)
	if err != nil {
		writeErr(w, err)
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		writeErr(w, err)
		return
	}
	// 换图后 URL 不变：必须让浏览器每次都重新验证。之前是 private,max-age=3600
	// 且没有任何 ETag/Last-Modified，浏览器一小时内根本不会再请求，换图后一直
	// 显示旧图。改成 no-cache + ETag：没变回 304（几乎不花流量），变了回新图。
	etag := fmt.Sprintf(`"%x-%x"`, info.ModTime().UnixNano(), info.Size())
	w.Header().Set("ETag", etag)
	w.Header().Set("Last-Modified", info.ModTime().UTC().Format(http.TimeFormat))
	w.Header().Set("Cache-Control", "no-cache")
	if match := r.Header.Get("If-None-Match"); match != "" && match == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	if ct := mime.TypeByExtension(filepath.Ext(path)); ct != "" {
		w.Header().Set("Content-Type", ct)
	} else {
		w.Header().Set("Content-Type", "image/jpeg")
	}
	_, _ = io.Copy(w, f)
}

// ===== 工作台（media-editor 精修刮削）=====

func strmScrapeWorkbenchItemID(r *http.Request) (int64, string, error) {
	taskID, err := strmTaskIDQuery(r)
	if err != nil {
		return 0, "", err
	}
	itemID := strings.TrimSpace(chi.URLParam(r, "id"))
	if itemID == "" {
		return 0, "", domain.Errorf(domain.CodeValidation, "item_id 无效")
	}
	return taskID, itemID, nil
}

func (h *Handler) getStrmScrapeWorkbench(w http.ResponseWriter, r *http.Request) {
	if !ensureServiceReady(w, h.strmScrape != nil) {
		return
	}
	taskID, itemID, err := strmScrapeWorkbenchItemID(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	data, err := h.strmScrape.GetWorkbench(r.Context(), taskID, itemID, strings.TrimSpace(r.URL.Query().Get("tmdb_id")))
	if err != nil {
		writeErr(w, err)
		return
	}
	writeOK(w, data)
}

// getStrmScrapeWorkbenchModule 按模块重载工作台数据（media-editor 各模块独立「读取」同款）：
// GET /items/{id}/workbench/module?module=fields|credits|images|seasons|season|local&season=N。
// 只返回该模块的数据，前端合併时只更新该模块。
func (h *Handler) getStrmScrapeWorkbenchModule(w http.ResponseWriter, r *http.Request) {
	if !ensureServiceReady(w, h.strmScrape != nil) {
		return
	}
	taskID, itemID, err := strmScrapeWorkbenchItemID(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	module := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("module")))
	season := -1
	if raw := strings.TrimSpace(r.URL.Query().Get("season")); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil {
			season = n
		}
	}
	data, err := h.strmScrape.GetWorkbenchModule(r.Context(), taskID, itemID, module, season)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeOK(w, data)
}

func (h *Handler) saveStrmScrapeWorkbenchFields(w http.ResponseWriter, r *http.Request) {
	if !ensureServiceReady(w, h.strmScrape != nil) {
		return
	}
	taskID, itemID, err := strmScrapeWorkbenchItemID(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	var req strmscrape.SaveFieldsRequest
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, err)
		return
	}
	req.StrmTaskID = taskID
	req.ItemID = itemID
	item, err := h.strmScrape.SaveWorkbenchFields(r.Context(), req)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeOK(w, item)
}

func (h *Handler) setStrmScrapeWorkbenchImage(w http.ResponseWriter, r *http.Request) {
	if !ensureServiceReady(w, h.strmScrape != nil) {
		return
	}
	taskID, itemID, err := strmScrapeWorkbenchItemID(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	var req strmscrape.SetImageRequest
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, err)
		return
	}
	req.StrmTaskID = taskID
	req.ItemID = itemID
	item, err := h.strmScrape.SetWorkbenchImage(r.Context(), req)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeOK(w, item)
}

func (h *Handler) saveStrmScrapeWorkbenchCast(w http.ResponseWriter, r *http.Request) {
	if !ensureServiceReady(w, h.strmScrape != nil) {
		return
	}
	taskID, itemID, err := strmScrapeWorkbenchItemID(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	var req strmscrape.SaveCastRequest
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, err)
		return
	}
	req.StrmTaskID = taskID
	req.ItemID = itemID
	item, err := h.strmScrape.SaveWorkbenchCast(r.Context(), req)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeOK(w, item)
}

func (h *Handler) saveStrmScrapeWorkbenchSeries(w http.ResponseWriter, r *http.Request) {
	if !ensureServiceReady(w, h.strmScrape != nil) {
		return
	}
	taskID, itemID, err := strmScrapeWorkbenchItemID(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	var req strmscrape.SaveSeriesRequest
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, err)
		return
	}
	req.StrmTaskID = taskID
	req.ItemID = itemID
	item, err := h.strmScrape.SaveWorkbenchSeries(r.Context(), req)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeOK(w, item)
}

func (h *Handler) matchStrmScrapeWorkbench(w http.ResponseWriter, r *http.Request) {
	if !ensureServiceReady(w, h.strmScrape != nil) {
		return
	}
	taskID, itemID, err := strmScrapeWorkbenchItemID(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	var req strmscrape.MatchWorkbenchRequest
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, err)
		return
	}
	req.StrmTaskID = taskID
	req.ItemID = itemID
	item, err := h.strmScrape.MatchWorkbench(r.Context(), req)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeOK(w, item)
}

func (h *Handler) uploadStrmScrapeWorkbenchImage(w http.ResponseWriter, r *http.Request) {
	if !ensureServiceReady(w, h.strmScrape != nil) {
		return
	}
	taskID, itemID, err := strmScrapeWorkbenchItemID(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	var req strmscrape.UploadWorkbenchImageRequest
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, err)
		return
	}
	req.StrmTaskID = taskID
	req.ItemID = itemID
	item, err := h.strmScrape.UploadWorkbenchImage(r.Context(), req)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeOK(w, item)
}

func (h *Handler) deleteStrmScrapeWorkbenchImage(w http.ResponseWriter, r *http.Request) {
	if !ensureServiceReady(w, h.strmScrape != nil) {
		return
	}
	taskID, itemID, err := strmScrapeWorkbenchItemID(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	var req strmscrape.DeleteWorkbenchImageRequest
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, err)
		return
	}
	req.StrmTaskID = taskID
	req.ItemID = itemID
	item, err := h.strmScrape.DeleteWorkbenchImage(r.Context(), req)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeOK(w, item)
}

func (h *Handler) copyStrmScrapeWorkbenchImage(w http.ResponseWriter, r *http.Request) {
	if !ensureServiceReady(w, h.strmScrape != nil) {
		return
	}
	taskID, itemID, err := strmScrapeWorkbenchItemID(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	var req strmscrape.CopyWorkbenchImageRequest
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, err)
		return
	}
	req.StrmTaskID = taskID
	req.ItemID = itemID
	item, err := h.strmScrape.CopyWorkbenchImage(r.Context(), req)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeOK(w, item)
}

func (h *Handler) getStrmScrapeWorkbenchMediaInfo(w http.ResponseWriter, r *http.Request) {
	if !ensureServiceReady(w, h.strmScrape != nil) {
		return
	}
	taskID, itemID, err := strmScrapeWorkbenchItemID(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	info, err := h.strmScrape.GetWorkbenchMediaInfo(r.Context(), taskID, itemID)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeOK(w, info)
}

func (h *Handler) saveStrmScrapeWorkbenchMediaInfo(w http.ResponseWriter, r *http.Request) {
	if !ensureServiceReady(w, h.strmScrape != nil) {
		return
	}
	taskID, itemID, err := strmScrapeWorkbenchItemID(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	info, err := h.strmScrape.SaveWorkbenchMediaInfo(r.Context(), taskID, itemID)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeOK(w, info)
}

func (h *Handler) getStrmScrapeWorkbenchEpisodeImages(w http.ResponseWriter, r *http.Request) {
	if !ensureServiceReady(w, h.strmScrape != nil) {
		return
	}
	taskID, itemID, err := strmScrapeWorkbenchItemID(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	season, err := strconv.Atoi(strings.TrimSpace(r.URL.Query().Get("season")))
	if err != nil || season < 0 {
		writeErr(w, domain.Errorf(domain.CodeValidation, "季号无效"))
		return
	}
	episode, err := strconv.Atoi(strings.TrimSpace(r.URL.Query().Get("episode")))
	if err != nil || episode < 0 {
		writeErr(w, domain.Errorf(domain.CodeValidation, "集号无效"))
		return
	}
	stills, err := h.strmScrape.GetWorkbenchEpisodeImages(r.Context(), taskID, itemID, season, episode)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeOK(w, map[string]any{"stills": stills})
}
