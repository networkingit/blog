package main

import (
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/kjk/notionapi"
)

var (
	flgRecursive bool
	useCache     = true
	destDir      = "notion_www"
	toVisit      = []string{
		// 57-MicroConf-videos-for-self-funded-software-businesses
		"0c896ea2efd24ec7be1d1f6e3b22d254",
	}
)

// Metadata describes meta information extracted from the page
type Metadata struct {
	ID           string
	Tags         []string
	DateStr      string
	CreatedAtStr string
	UpdatedAtStr string
	Date         time.Time
	CreatedAt    time.Time
	UpdatedAt    time.Time
	Description  string
	HeaderImage  string
	Collection   string
	Status       string // hidden, notimportant
}

// IsHidden returns true if page is hidden/deleted
func (m *Metadata) IsHidden() bool {
	return strings.EqualFold(m.Status, "hidden")
}

func (m *Metadata) IsNotImportant() bool {
	return strings.EqualFold(m.Status, "notimportant")
}

// NotionDoc represents a notion page and additional info we need about it
type NotionDoc struct {
	pageInfo *notionapi.PageInfo
	meta     *Metadata
}

// convert 2131b10c-ebf6-4938-a127-7089ff02dbe4 to 2131b10cebf64938a1277089ff02dbe4
func normalizeID(s string) string {
	return strings.Replace(s, "-", "", -1)
}

func openLogFileForPageID(pageID string) (io.WriteCloser, error) {
	name := fmt.Sprintf("%s.go.log.txt", pageID)
	path := filepath.Join("log", name)
	f, err := os.Create(path)
	if err != nil {
		fmt.Printf("os.Create('%s') failed with %s\n", path, err)
		return nil, err
	}
	notionapi.Logger = f
	return f, nil
}

// exttract metadata from blocks
func extractMetadata(pageInfo *notionapi.PageInfo) *Metadata {
	page := pageInfo.Page
	//title := page.Title
	id := normalizeID(page.ID)
	blocks := pageInfo.Page.Content
	//fmt.Printf("extractMetadata: %s-%s, %d blocks\n", title, id, len(blocks))
	// metadata blocks are always at the beginning. They are TypeText blocks and
	// have only one plain string as content
	res := Metadata{}
	nBlock := 0
	for len(blocks) > 0 {
		block := blocks[0]
		//fmt.Printf("  %d %s '%s'\n", nBlock, block.Type, block.Title)

		if block.Type != notionapi.BlockText {
			//fmt.Printf("extractMetadata: ending look because block %d is of type %s\n", nBlock, block.Type)
			break
		}

		if len(block.InlineContent) == 0 {
			//fmt.Printf("block %d of type %s and has no InlineContent\n", nBlock, block.Type)
			blocks = blocks[1:]
			break
		} else {
			//fmt.Printf("block %d has %d InlineContent\n", nBlock, len(block.InlineContent))
		}

		inline := block.InlineContent[0]
		// must be plain text
		if !inline.IsPlain() {
			//fmt.Printf("block: %d of type %s: inline has attributes\n", nBlock, block.Type)
			break
		}

		blocks = blocks[1:]

		// remove empty lines at the top
		s := strings.TrimSpace(inline.Text)
		if s == "" {
			//fmt.Printf("block: %d of type %s: inline.Text is empty\n", nBlock, block.Type)
			blocks = blocks[1:]
			break
		}
		//fmt.Printf("  %d %s '%s'\n", nBlock, block.Type, s)

		parts := strings.SplitN(s, ":", 2)
		if len(parts) != 2 {
			//fmt.Printf("block: %d of type %s: inline.Text is not key/value. s='%s'\n", nBlock, block.Type, s)
			break
		}
		key := strings.ToLower(strings.TrimSpace(parts[0]))
		val := strings.TrimSpace(parts[1])
		switch key {
		case "tags":
			res.Tags = strings.Split(val, ",")
			for i, tag := range res.Tags {
				res.Tags[i] = strings.TrimSpace(tag)
			}
			//fmt.Printf("Tags: %v\n", res.Tags)
		case "id":
			res.ID = val
			//fmt.Printf("ID: %s\n", res.ID)
		case "date":
			decodeDate(val, &res.DateStr, &res.Date, id)
		case "createdat":
			decodeDate(val, &res.CreatedAtStr, &res.CreatedAt, id)
		case "updatedat":
			decodeDate(val, &res.UpdatedAtStr, &res.UpdatedAt, id)
		case "status":
			res.Status = val
		case "description":
			res.Description = val
			//fmt.Printf("Description: %s\n", res.Description)
		case "headerimage":
			res.HeaderImage = val
		case "collection":
			res.Collection = val
		default:
			rmCached(pageInfo.ID)
			panicMsg("Unsupported meta '%s' in notion page with id '%s'", key, pageInfo.ID)
		}
		nBlock++
	}
	pageInfo.Page.Content = blocks
	return &res
}

func decodeDate(s string, date *string, dateParsed *time.Time, pageID string) {
	*date = s
	// 2002-06-21T04:15:29-07:00
	parsed, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panicMsg("Failed to parse date '%s' in page '%s'. Error: %s", s, pageID, err)
	}
	*dateParsed = parsed
}

func rmFile(path string) {
	err := os.Remove(path)
	if err != nil {
		fmt.Printf("os.Remove(%s) failed with %s\n", path, err)
	}
}

func rmCached(pageID string) {
	id := normalizeID(pageID)
	rmFile(filepath.Join("log", id+".go.log.txt"))
	rmFile(filepath.Join(cacheDir, id+".json"))
}

func genHTML(pageInfo *notionapi.PageInfo) []byte {
	title := pageInfo.Page.Title
	title = template.HTMLEscapeString(title)

	gen := NewHTMLGenerator(pageInfo)
	html := string(gen.Gen())

	s := fmt.Sprintf(`<!doctype html>
<html>
	<head>
		<meta http-equiv="Content-Type" content="text/html; charset=utf-8" />
		<meta name="viewport" content="width=device-width, initial-scale=1">
		<title>%s</title>
		<link href="/main.css" rel="stylesheet">
	</head>
<body>
<div id="tophdr">
<ul id="nav">
  <li><a href="/software/">Software</a></li>
  <li><span style="color:#aaa">&bull;</span></li>
  <li><a href="/archives.html">Articles</a></li>
  <li><span style="color:#aaa">&bull;</span></li>
  <li><a href="/documents.html">Documents</a></li>
  <li><span style="color:#aaa">&bull;</span></li>
  <li><a href="/dailynotes">Daily Notes</a></li>
  <li><span style="color:#aaa">&bull;</span></li>
  <li><a href="/resume.html">Résumé</a></li>
</ul>
</div>

<div id="content">
  <div id="post" style="margin-left:auto;margin-right:auto;margin-top:2em;">
    <div class="title">
      <a href="/">Home</a>  / %s
    </div>
    <div>
      %s
    </div>
  </div>
</div>
</body>
</html>
`, title, title, html)

	d := prettyHTML([]byte(s))
	return d
}

func loadPageFromCache(pageID string) *notionapi.PageInfo {
	var pageInfo notionapi.PageInfo
	cachedPath := filepath.Join(cacheDir, pageID+".json")
	if useCache {
		d, err := ioutil.ReadFile(cachedPath)
		if err == nil {
			err = json.Unmarshal(d, &pageInfo)
			panicIfErr(err)
			fmt.Printf("Got data for pageID %s from cache file %s\n", pageID, cachedPath)
			return &pageInfo
		}
	}
	return nil
}

func downloadAndCachePage(pageID string) (*notionapi.PageInfo, error) {
	fmt.Printf("downloading page with id %s\n", pageID)
	cachedPath := filepath.Join(cacheDir, pageID+".json")
	lf, _ := openLogFileForPageID(pageID)
	if lf != nil {
		defer lf.Close()
	}
	res, err := notionapi.GetPageInfo(pageID)
	if err != nil {
		return nil, err
	}
	d, err := json.MarshalIndent(res, "", "  ")
	if err == nil {
		err = ioutil.WriteFile(cachedPath, d, 0644)
		panicIfErr(err)
	} else {
		// not a fatal error, just a warning
		fmt.Printf("json.Marshal() on pageID '%s' failed with %s\n", pageID, err)
	}
	return res, nil
}

func loadPage(pageID string) (*NotionDoc, error) {
	var err error
	pageInfo := loadPageFromCache(pageID)
	if pageInfo == nil {
		pageInfo, err = downloadAndCachePage(pageID)
		if err != nil {
			return nil, err
		}
	}
	doc := &NotionDoc{
		pageInfo: pageInfo,
	}
	doc.meta = extractMetadata(pageInfo)
	return doc, nil
}

func toHTML(pageID, path string) (*NotionDoc, error) {
	fmt.Printf("toHTML: pageID=%s, path=%s\n", pageID, path)
	doc, err := loadPage(pageID)
	if err != nil {
		return nil, err
	}
	d := genHTML(doc.pageInfo)
	err = ioutil.WriteFile(path, d, 0644)
	return doc, err
}

func findSubPageIDs(blocks []*notionapi.Block) []string {
	var res []string
	for _, block := range blocks {
		if block.Type == notionapi.BlockPage {
			res = append(res, block.ID)
		}
	}
	return res
}

func copyCSS() {
	src := filepath.Join("www", "css", "main.css")
	dst := filepath.Join(destDir, "main.css")
	err := copyFile(dst, src)
	panicIfErr(err)
}

func loadOne(id string) {
	id = normalizeID(id)
	_, err := loadPage(id)
	panicIfErr(err)
}

func genIndexHTML(docs []*NotionDoc) []byte {
	lines := []string{}
	for _, doc := range docs {
		meta := doc.meta
		if meta.IsNotImportant() {
			continue
		}
		page := doc.pageInfo.Page
		id := normalizeID(page.ID)
		title := page.Title
		s := fmt.Sprintf(`<div>
		<a href="/article/%s/index.html">%s</a>
			<span style="font-size:80%%">
				<span class="taglink">in:</span> <a href="/tag/go" class="taglink">go</a>, <a href="/tag/programming" class="taglink">programming</a>
			</span>
</div>`, id, title)
		lines = append(lines, s)
	}
	html := strings.Join(lines, "\n")

	s := fmt.Sprintf(`<!doctype html>
<html>
	<head>
		<meta http-equiv="Content-Type" content="text/html; charset=utf-8" />
		<meta name="viewport" content="width=device-width, initial-scale=1">
		<title>Krzysztof Kowalczyk's external brain</title>
		<link href="/main.css" rel="stylesheet">
	</head>
<body>
<div id="tophdr">
<ul id="nav">
  <li><a href="/software/">Software</a></li>
  <li><span style="color:#aaa">&bull;</span></li>
  <li><a href="/archives.html">Articles</a></li>
  <li><span style="color:#aaa">&bull;</span></li>
  <li><a href="/documents.html">Documents</a></li>
  <li><span style="color:#aaa">&bull;</span></li>
  <li><a href="/dailynotes">Daily Notes</a></li>
  <li><span style="color:#aaa">&bull;</span></li>
  <li><a href="/resume.html">Résumé</a></li>
</ul>
</div>

<div id="content">
  <div id="post" style="margin-left:auto;margin-right:auto;margin-top:2em;">
    <div class="title">
      <a href="/">Home</a>
    </div>
    <div class="articles-list-wrap">
      %s
    </div>
  </div>
</div>
</body>
</html>
`, html)

	d := prettyHTML([]byte(s))
	return d
}

func genNotionBasic(pages map[string]*NotionDoc) {
	docs := make([]*NotionDoc, 0)
	for _, doc := range pages {
		docs = append(docs, doc)
	}
	sort.Slice(docs, func(i, j int) bool {
		d1 := docs[i].meta.DateParsed
		d2 := docs[j].meta.DateParsed
		return d1.Sub(d2) > 0
	})
	d := genIndexHTML(docs)
	path := filepath.Join(destDir, "index.html")
	err := ioutil.WriteFile(path, d, 0644)
	panicIfErr(err)
	for _, doc := range docs {
		d := genHTML(doc.pageInfo)
		id := normalizeID(doc.pageInfo.Page.ID)
		path := filepath.Join(destDir, id+".html")
		err = ioutil.WriteFile(path, d, 0644)
	}
}

func importNotion() {
	os.MkdirAll("log", 0755)
	os.MkdirAll(cacheDir, 0755)
	os.MkdirAll(destDir, 0755)

	if false {
		//loadOne("431295a5-4f7e-4208-869f-4763862c1f05")
		docs := loadNotionBlogPosts()
		genNotionBasic(docs)
		return
	}

	notionapi.DebugLog = true
	seen := map[string]struct{}{}
	firstPage := true
	for len(toVisit) > 0 {
		pageID := toVisit[0]
		toVisit = toVisit[1:]
		id := normalizeID(pageID)
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		name := id + ".html"
		if firstPage {
			name = "index.html"
		}
		path := filepath.Join(destDir, name)
		doc, err := toHTML(id, path)
		if err != nil {
			fmt.Printf("toHTML('%s') failed with %s\n", id, err)
		}
		if flgRecursive {
			subPages := findSubPageIDs(doc.pageInfo.Page.Content)
			toVisit = append(toVisit, subPages...)
		}
		firstPage = false
	}
	copyCSS()
}