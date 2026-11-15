package strmscrape

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// nfoUniqueID 是 <uniqueid type="tmdb">123</uniqueid> 的寫法（media-editor 同款）。
type nfoUniqueID struct {
	XMLName xml.Name `xml:"uniqueid"`
	Type    string   `xml:"type,attr,omitempty"`
	Value   string   `xml:",chardata"`
}

type workNFO struct {
	XMLName       xml.Name
	Title         string       `xml:"title"`
	OriginalTitle string       `xml:"originaltitle,omitempty"`
	Year          string       `xml:"year,omitempty"`
	Plot          string       `xml:"plot,omitempty"`
	Genres        []string     `xml:"genre,omitempty"`
	Premiered     string       `xml:"premiered,omitempty"`
	Status        string       `xml:"status,omitempty"`
	SortTitle     string       `xml:"sorttitle,omitempty"`
	TMDBID        string       `xml:"tmdbid,omitempty"`
	UniqueID      *nfoUniqueID `xml:"uniqueid,omitempty"`
	Rating        float64      `xml:"rating,omitempty"`
	Votes         int          `xml:"votes,omitempty"`
	MPAA          string       `xml:"mpaa,omitempty"`
	Country       string       `xml:"country,omitempty"`
	Tags          []string     `xml:"tag,omitempty"`
	Actors        []nfoActor   `xml:"actor,omitempty"`
}

type nfoActor struct {
	XMLName xml.Name `xml:"actor"`
	Name    string   `xml:"name"`
	Role    string   `xml:"role,omitempty"`
	Order   int      `xml:"order"`
	Thumb   string   `xml:"thumb,omitempty"`
}

type seasonNFO struct {
	XMLName      xml.Name `xml:"season"`
	Title        string   `xml:"title,omitempty"`
	SeasonNumber string   `xml:"seasonnumber"`
	Plot         string   `xml:"plot,omitempty"`
	Premiered    string   `xml:"premiered,omitempty"`
}

type episodeNFO struct {
	XMLName   xml.Name `xml:"episodedetails"`
	Title     string   `xml:"title"`
	Season    string   `xml:"season"`
	Episode   string   `xml:"episode"`
	Plot      string   `xml:"plot,omitempty"`
	Aired     string   `xml:"aired,omitempty"`
	TMDBID    string   `xml:"tmdbid,omitempty"`
	ShowTitle string   `xml:"showtitle,omitempty"`
	Runtime   int      `xml:"runtime,omitempty"`
	Thumb     string   `xml:"thumb,omitempty"`
}

// FieldSet 是工作台用的根 NFO 欄位集，對齊 media-editor 的 ROOT_FIELDS。
type FieldSet struct {
	Title         string   `json:"title"`
	OriginalTitle string   `json:"originaltitle"`
	Year          *int     `json:"year,omitempty"`
	Plot          string   `json:"plot"`
	Genres        []string `json:"genre"`
	Premiered     string   `json:"premiered"`
	Status        string   `json:"status"`
	SortTitle     string   `json:"sorttitle"`
	TMDBID        string   `json:"tmdbid"`
	UniqueID      string   `json:"uniqueid"`
	Rating        float64  `json:"rating"`
	Votes         int      `json:"votes"`
	MPAA          string   `json:"mpaa"`
	Country       string   `json:"country"`
	Tags          []string `json:"tag"`
}

// EpisodeFieldSet 是分集 NFO 欄位集，對齊 media-editor 的 EPISODE_FIELDS。
type EpisodeFieldSet struct {
	Season  int    `json:"season"`
	Episode int    `json:"episode"`
	Title   string `json:"title"`
	Plot    string `json:"plot"`
	Aired   string `json:"aired"`
	Thumb   string `json:"thumb"`
	Runtime int    `json:"runtime"`
}

func fieldsFromTMDBInfo(info tmdbInfo) FieldSet {
	f := FieldSet{
		Title:         info.Title,
		OriginalTitle: info.Original,
		Plot:          info.Plot,
		Genres:        info.Genres,
		Premiered:     info.Premiered,
		Status:        info.Status,
		SortTitle:     info.Title,
		TMDBID:        info.TMDBID,
		UniqueID:      info.TMDBID,
		Rating:        info.Rating,
		Votes:         info.Votes,
		Country:       strings.Join(info.Countries, " / "),
	}
	if info.Year != nil {
		y := *info.Year
		f.Year = &y
	}
	return f
}

func (f FieldSet) toWorkNFO(root string) workNFO {
	nfo := workNFO{
		XMLName:       xml.Name{Local: root},
		Title:         strings.TrimSpace(f.Title),
		OriginalTitle: strings.TrimSpace(f.OriginalTitle),
		Plot:          strings.TrimSpace(f.Plot),
		Genres:        trimNonEmpty(f.Genres),
		Premiered:     strings.TrimSpace(f.Premiered),
		Status:        strings.TrimSpace(f.Status),
		SortTitle:     strings.TrimSpace(f.SortTitle),
		TMDBID:        strings.TrimSpace(f.TMDBID),
		Rating:        f.Rating,
		Votes:         f.Votes,
		MPAA:          strings.TrimSpace(f.MPAA),
		Country:       strings.TrimSpace(f.Country),
		Tags:          trimNonEmpty(f.Tags),
	}
	if f.Year != nil && *f.Year > 0 {
		nfo.Year = strconv.Itoa(*f.Year)
	}
	uniqueID := strings.TrimSpace(firstNonEmpty(f.UniqueID, f.TMDBID))
	if uniqueID != "" {
		nfo.UniqueID = &nfoUniqueID{Type: "tmdb", Value: uniqueID}
	}
	return nfo
}

func trimNonEmpty(in []string) []string {
	out := make([]string, 0, len(in))
	for _, v := range in {
		if v = strings.TrimSpace(v); v != "" {
			out = append(out, v)
		}
	}
	return out
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

var nfoRootCloseRe = regexp.MustCompile(`(?i)</(?:movie|tvshow)\s*>`)

// nfoLooksStandard 文件含 movie/tvshow 根节点才算可用 NFO，压制组的 MediaInfo 文本不算。
func nfoLooksStandard(path string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	lower := strings.ToLower(string(data))
	return strings.Contains(lower, "<movie") || strings.Contains(lower, "<tvshow")
}

// nfoWriteNeeded 目标 NFO 不存在或不是标准 NFO 时都要重写，压制组文本对 Kodi/Emby 无用。
func nfoWriteNeeded(overwrite bool, nfo string) bool {
	return overwrite || !nfoLooksStandard(nfo)
}

// workMetaPaths 返回电影或剧集的兼容元数据路径。
func workMetaPaths(g workGroup, mediaType string) (nfoPath, posterPath string) {
	if mediaType == MediaTypeTV && g.flatFile == "" {
		return filepath.Join(g.absDir, "tvshow.nfo"), filepath.Join(g.absDir, "poster.jpg")
	}
	stemPath := primaryStrmStem(g)
	if stemPath == "" {
		return filepath.Join(g.absDir, "movie.nfo"), filepath.Join(g.absDir, "poster.jpg")
	}
	nfoPath = stemPath + ".nfo"
	if g.flatFile != "" {
		return nfoPath, stemPath + "-poster.jpg"
	}
	return nfoPath, filepath.Join(g.absDir, "poster.jpg")
}

func primaryStrmStem(g workGroup) string {
	if g.flatFile != "" {
		return strings.TrimSuffix(g.flatFile, filepath.Ext(g.flatFile))
	}
	if len(g.entries) == 0 {
		return ""
	}
	return strings.TrimSuffix(g.entries[0].absPath, filepath.Ext(g.entries[0].absPath))
}

func workHasNFO(g workGroup, mediaType string) bool {
	for _, p := range workNFOCandidates(g, mediaType) {
		if nfoLooksStandard(p) {
			return true
		}
	}
	return false
}

func workHasActors(g workGroup, mediaType string) bool {
	for _, path := range workNFOCandidates(g, mediaType) {
		if nfoLooksStandard(path) && nfoHasActors(path) {
			return true
		}
	}
	return false
}

func workHasPoster(g workGroup, mediaType string) bool {
	for _, p := range workPosterCandidates(g, mediaType) {
		if fileExists(p) {
			return true
		}
	}
	return false
}

func workNFOCandidates(g workGroup, mediaType string) []string {
	if mediaType == MediaTypeTV && g.flatFile == "" {
		return []string{filepath.Join(g.absDir, "tvshow.nfo")}
	}
	out := make([]string, 0, len(g.entries)+2)
	if g.flatFile != "" {
		stem := strings.TrimSuffix(g.flatFile, filepath.Ext(g.flatFile))
		return []string{stem + ".nfo"}
	}
	for _, e := range g.entries {
		stem := strings.TrimSuffix(e.absPath, filepath.Ext(e.absPath))
		out = append(out, stem+".nfo")
	}
	// 兼容上一版误写的 movie.nfo
	out = append(out, filepath.Join(g.absDir, "movie.nfo"))
	return out
}

func workPosterCandidates(g workGroup, mediaType string) []string {
	_ = mediaType
	if g.flatFile != "" {
		stem := strings.TrimSuffix(g.flatFile, filepath.Ext(g.flatFile))
		return []string{stem + "-poster.jpg", stem + ".jpg"}
	}
	out := []string{
		filepath.Join(g.absDir, "poster.jpg"),
		filepath.Join(g.absDir, "folder.jpg"),
		filepath.Join(g.absDir, "cover.jpg"),
	}
	for _, e := range g.entries {
		stem := strings.TrimSuffix(e.absPath, filepath.Ext(e.absPath))
		out = append(out, stem+"-poster.jpg", stem+".jpg")
	}
	return out
}

func workPosterFile(g workGroup, mediaType string) string {
	for _, p := range workPosterCandidates(g, mediaType) {
		if fileExists(p) {
			return p
		}
	}
	_, poster := workMetaPaths(g, mediaType)
	return poster
}

func workFanartPath(g workGroup) string {
	if g.flatFile != "" {
		return primaryStrmStem(g) + "-fanart.jpg"
	}
	return filepath.Join(g.absDir, "fanart.jpg")
}

func workHasFanart(g workGroup) bool {
	if g.flatFile != "" {
		stem := primaryStrmStem(g)
		return fileExists(stem+"-fanart.jpg") || fileExists(stem+"-fanart.png")
	}
	for _, name := range []string{"fanart.jpg", "fanart.png", "backdrop.jpg", "backdrop.png", "background.jpg", "background.png"} {
		if fileExists(filepath.Join(g.absDir, name)) {
			return true
		}
	}
	return false
}

func workClearLogoPath(g workGroup) string {
	if g.flatFile != "" {
		return primaryStrmStem(g) + "-clearlogo.png"
	}
	return filepath.Join(g.absDir, "clearlogo.png")
}

func workHasClearLogo(g workGroup) bool {
	if g.flatFile != "" {
		return fileExists(primaryStrmStem(g) + "-clearlogo.png")
	}
	return fileExists(filepath.Join(g.absDir, "clearlogo.png"))
}

func seasonPosterPath(showDir string, season int) string {
	if season <= 0 {
		return filepath.Join(showDir, "season-specials-poster.jpg")
	}
	return filepath.Join(showDir, fmt.Sprintf("season%02d-poster.jpg", season))
}

func listLocalSeasonNumbers(showDir string) []int {
	return seasonNumbersFromDirs(listLocalSeasonDirs(showDir))
}

func seasonNumbersFromDirs(dirs []seasonDir) []int {
	seen := map[int]struct{}{}
	var out []int
	for _, dir := range dirs {
		if _, ok := seen[dir.number]; ok {
			continue
		}
		seen[dir.number] = struct{}{}
		out = append(out, dir.number)
	}
	return out
}

func writeMovieNFO(path string, fields FieldSet, actors ...nfoActor) error {
	return writeWorkNFO(path, "movie", fields, actors)
}

func writeTVShowNFO(path string, fields FieldSet, actors ...nfoActor) error {
	return writeWorkNFO(path, "tvshow", fields, actors)
}

func writeWorkNFO(path, root string, fields FieldSet, actors []nfoActor) error {
	nfo := fields.toWorkNFO(root)
	nfo.Actors = actors
	return writeXML(path, nfo)
}

func nfoHasActors(path string) bool {
	data, err := os.ReadFile(path)
	return err == nil && strings.Contains(strings.ToLower(string(data)), "<actor>")
}

// appendNFOActors 在仅补缺模式下保留已有 NFO 的全部内容，只补入演员节点。
func appendNFOActors(path string, actors []nfoActor) error {
	if len(actors) == 0 || nfoHasActors(path) {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	matches := nfoRootCloseRe.FindAllIndex(data, -1)
	if len(matches) == 0 {
		return fmt.Errorf("NFO 缺少 movie/tvshow 根节点")
	}
	idx := matches[len(matches)-1][0]
	var fragments strings.Builder
	for _, actor := range actors {
		raw, err := xml.Marshal(actor)
		if err != nil {
			return err
		}
		fragments.WriteString("  ")
		fragments.Write(raw)
		fragments.WriteByte('\n')
	}
	updated := append([]byte{}, data[:idx]...)
	updated = append(updated, []byte(fragments.String())...)
	updated = append(updated, data[idx:]...)
	return os.WriteFile(path, updated, 0o644)
}

func writeSeasonNFO(path string, season int, title, plot, premiered string) error {
	nfo := seasonNFO{
		Title:        strings.TrimSpace(title),
		SeasonNumber: fmt.Sprintf("%d", season),
		Plot:         strings.TrimSpace(plot),
		Premiered:    strings.TrimSpace(premiered),
	}
	return writeXML(path, nfo)
}

func writeEpisodeNFO(path, title, showTitle, plot, aired, tmdbID string, season, episode int, runtime int, thumb string) error {
	nfo := episodeNFO{
		Title:     strings.TrimSpace(title),
		Season:    fmt.Sprintf("%d", season),
		Episode:   fmt.Sprintf("%d", episode),
		Plot:      strings.TrimSpace(plot),
		Aired:     strings.TrimSpace(aired),
		TMDBID:    strings.TrimSpace(tmdbID),
		ShowTitle: strings.TrimSpace(showTitle),
		Runtime:   runtime,
		Thumb:     strings.TrimSpace(thumb),
	}
	return writeXML(path, nfo)
}

func writeXML(path string, v any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := xml.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	body := append([]byte(xml.Header), data...)
	body = append(body, '\n')
	return os.WriteFile(path, body, 0o644)
}

func fileExists(path string) bool {
	st, err := os.Stat(path)
	return err == nil && !st.IsDir()
}

func writeImageFile(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// parseWorkNFOFile 解析本地根 NFO（movie/tvshow）为工作台栏位集。
func parseWorkNFOFile(path string) (FieldSet, []nfoActor, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return FieldSet{}, nil, false
	}
	var nfo workNFO
	if xml.Unmarshal(data, &nfo) != nil {
		return FieldSet{}, nil, false
	}
	if nfo.XMLName.Local != "movie" && nfo.XMLName.Local != "tvshow" {
		return FieldSet{}, nil, false
	}
	f := FieldSet{
		Title:         strings.TrimSpace(nfo.Title),
		OriginalTitle: strings.TrimSpace(nfo.OriginalTitle),
		Plot:          strings.TrimSpace(nfo.Plot),
		Genres:        trimNonEmpty(nfo.Genres),
		Premiered:     strings.TrimSpace(nfo.Premiered),
		Status:        strings.TrimSpace(nfo.Status),
		SortTitle:     strings.TrimSpace(nfo.SortTitle),
		TMDBID:        strings.TrimSpace(nfo.TMDBID),
		Rating:        nfo.Rating,
		Votes:         nfo.Votes,
		MPAA:          strings.TrimSpace(nfo.MPAA),
		Country:       strings.TrimSpace(nfo.Country),
		Tags:          trimNonEmpty(nfo.Tags),
	}
	if y, err := strconv.Atoi(strings.TrimSpace(nfo.Year)); err == nil && y > 0 {
		f.Year = &y
	}
	if nfo.UniqueID != nil {
		f.UniqueID = strings.TrimSpace(nfo.UniqueID.Value)
	}
	if f.TMDBID == "" {
		f.TMDBID = f.UniqueID
	}
	return f, nfo.Actors, true
}

// parseEpisodeNFOFile 解析本地分集 NFO。
func parseEpisodeNFOFile(path string) (EpisodeFieldSet, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return EpisodeFieldSet{}, false
	}
	var nfo episodeNFO
	if xml.Unmarshal(data, &nfo) != nil || nfo.XMLName.Local != "episodedetails" {
		return EpisodeFieldSet{}, false
	}
	season, _ := strconv.Atoi(strings.TrimSpace(nfo.Season))
	episode, _ := strconv.Atoi(strings.TrimSpace(nfo.Episode))
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

// ===== 以下为工作台「合并写 NFO」用的小 XML 树 =====
// 目标：像 media-editor 那样只改指定栏位，保留 NFO 里其他节点（如 fileinfo/streamdetails）。

type xmlNode struct {
	Name     xml.Name
	Attr     []xml.Attr
	Text     string
	Children []*xmlNode
}

func parseXMLTree(data []byte) (*xmlNode, error) {
	dec := xml.NewDecoder(bytes.NewReader(data))
	for {
		tok, err := dec.Token()
		if err != nil {
			return nil, err
		}
		if se, ok := tok.(xml.StartElement); ok {
			return decodeXMLNode(dec, se)
		}
	}
}

func decodeXMLNode(dec *xml.Decoder, start xml.StartElement) (*xmlNode, error) {
	n := &xmlNode{Name: start.Name, Attr: start.Attr}
	for {
		tok, err := dec.Token()
		if err != nil {
			return nil, err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			child, err := decodeXMLNode(dec, t)
			if err != nil {
				return nil, err
			}
			n.Children = append(n.Children, child)
		case xml.CharData:
			if s := strings.TrimSpace(string(t)); s != "" {
				n.Text += s
			}
		case xml.EndElement:
			return n, nil
		}
	}
}

func (n *xmlNode) setText(name, value string) {
	value = strings.TrimSpace(value)
	for i, c := range n.Children {
		if c.Name.Local == name {
			if value == "" {
				n.Children = append(n.Children[:i], n.Children[i+1:]...)
			} else {
				c.Text = value
				c.Children = nil
			}
			return
		}
	}
	if value != "" {
		n.Children = append(n.Children, &xmlNode{Name: xml.Name{Local: name}, Text: value})
	}
}

func (n *xmlNode) setList(name string, values []string) {
	var kept []*xmlNode
	for _, c := range n.Children {
		if c.Name.Local != name {
			kept = append(kept, c)
		}
	}
	n.Children = kept
	for _, v := range trimNonEmpty(values) {
		n.Children = append(n.Children, &xmlNode{Name: xml.Name{Local: name}, Text: v})
	}
}

func (n *xmlNode) setUniqueID(value string) {
	value = strings.TrimSpace(value)
	var target *xmlNode
	for _, c := range n.Children {
		if c.Name.Local != "uniqueid" {
			continue
		}
		if target == nil {
			target = c
		}
		for _, a := range c.Attr {
			if a.Name.Local == "type" && strings.EqualFold(a.Value, "tmdb") {
				target = c
			}
		}
	}
	if value == "" {
		if target == nil {
			return
		}
		for i, c := range n.Children {
			if c == target {
				n.Children = append(n.Children[:i], n.Children[i+1:]...)
				return
			}
		}
		return
	}
	if target == nil {
		target = &xmlNode{Name: xml.Name{Local: "uniqueid"}}
		n.Children = append(n.Children, target)
	}
	found := false
	for i, a := range target.Attr {
		if a.Name.Local == "type" {
			target.Attr[i].Value = "tmdb"
			found = true
		}
	}
	if !found {
		target.Attr = append(target.Attr, xml.Attr{Name: xml.Name{Local: "type"}, Value: "tmdb"})
	}
	target.Text = value
	target.Children = nil
}

func (n *xmlNode) replaceActors(actors []nfoActor) error {
	var kept []*xmlNode
	for _, c := range n.Children {
		if c.Name.Local != "actor" {
			kept = append(kept, c)
		}
	}
	n.Children = kept
	for _, a := range actors {
		raw, err := xml.Marshal(a)
		if err != nil {
			return err
		}
		node, err := parseXMLTree(raw)
		if err != nil {
			return err
		}
		n.Children = append(n.Children, node)
	}
	return nil
}

func writeXMLTree(path string, root *xmlNode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	var buf bytes.Buffer
	buf.WriteString(xml.Header)
	enc := xml.NewEncoder(&buf)
	enc.Indent("", "  ")
	if err := encodeXMLNode(enc, root); err != nil {
		return err
	}
	if err := enc.Flush(); err != nil {
		return err
	}
	buf.WriteByte('\n')
	return os.WriteFile(path, buf.Bytes(), 0o644)
}

func encodeXMLNode(enc *xml.Encoder, n *xmlNode) error {
	if err := enc.EncodeToken(xml.StartElement{Name: n.Name, Attr: n.Attr}); err != nil {
		return err
	}
	if n.Text != "" {
		if err := enc.EncodeToken(xml.CharData(n.Text)); err != nil {
			return err
		}
	}
	for _, c := range n.Children {
		if err := encodeXMLNode(enc, c); err != nil {
			return err
		}
	}
	return enc.EncodeToken(xml.EndElement{Name: n.Name})
}

func applyFieldSet(n *xmlNode, f FieldSet) {
	n.setText("title", f.Title)
	n.setText("originaltitle", f.OriginalTitle)
	year := ""
	if f.Year != nil && *f.Year > 0 {
		year = strconv.Itoa(*f.Year)
	}
	n.setText("year", year)
	n.setText("plot", f.Plot)
	n.setList("genre", f.Genres)
	n.setText("premiered", f.Premiered)
	n.setText("status", f.Status)
	n.setText("sorttitle", f.SortTitle)
	n.setText("tmdbid", f.TMDBID)
	n.setUniqueID(firstNonEmpty(f.UniqueID, f.TMDBID))
	rating := ""
	if f.Rating > 0 {
		rating = strconv.FormatFloat(f.Rating, 'f', 1, 64)
	}
	n.setText("rating", rating)
	votes := ""
	if f.Votes > 0 {
		votes = strconv.Itoa(f.Votes)
	}
	n.setText("votes", votes)
	n.setText("mpaa", f.MPAA)
	n.setText("country", f.Country)
	n.setList("tag", f.Tags)
}

// mergeWorkNFOFields 只改指定栏位并保留 NFO 其余节点；文件不存在或非标准 NFO 时整份重写。
func mergeWorkNFOFields(path, root string, f FieldSet, actors []nfoActor, replaceActors bool) error {
	data, err := os.ReadFile(path)
	if err == nil && nfoLooksStandard(path) {
		if tree, perr := parseXMLTree(data); perr == nil && tree != nil && tree.Name.Local == root {
			applyFieldSet(tree, f)
			if replaceActors {
				if err := tree.replaceActors(actors); err != nil {
					return err
				}
			}
			return writeXMLTree(path, tree)
		}
	}
	return writeWorkNFO(path, root, f, actors)
}

// mergeEpisodeNFOFields 只改指定分集栏位并保留其余节点；文件不存在时新建。
func mergeEpisodeNFOFields(path string, f EpisodeFieldSet) error {
	data, err := os.ReadFile(path)
	if err == nil && strings.Contains(strings.ToLower(string(data)), "<episodedetails") {
		if tree, perr := parseXMLTree(data); perr == nil && tree != nil && tree.Name.Local == "episodedetails" {
			tree.setText("title", f.Title)
			tree.setText("season", strconv.Itoa(f.Season))
			tree.setText("episode", strconv.Itoa(f.Episode))
			tree.setText("plot", f.Plot)
			tree.setText("aired", f.Aired)
			tree.setText("thumb", f.Thumb)
			runtime := ""
			if f.Runtime > 0 {
				runtime = strconv.Itoa(f.Runtime)
			}
			tree.setText("runtime", runtime)
			return writeXMLTree(path, tree)
		}
	}
	return writeEpisodeNFO(path, f.Title, "", f.Plot, f.Aired, "", f.Season, f.Episode, f.Runtime, f.Thumb)
}
