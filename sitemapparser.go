package main

import (
	"encoding/xml"
	"io"
	"strings"
	"sync"
)

// SitemapParser is a built-in class that walks an XML sitemap document
// and splits the `<loc>` entries into two buckets per the sitemaps.org
// schema:
//
//   page_urls  — `<urlset><url><loc>…</loc></url></urlset>`
//   index_urls — `<sitemapindex><sitemap><loc>…</loc></sitemap></sitemapindex>`
//
// Surface:
//
//   (set p (new SitemapParser))
//   (send p parse: xmlBody)        → Dictionary {page_urls: Array, index_urls: Array}
//   (send p pageUrls)              → Array (last parse, for re-reading)
//   (send p indexUrls)             → Array
//   (send p empty)                 → bool — true when parse: never ran or saw no <loc>
//
// Decisions worth remembering:
//
//   - Streaming `xml.Decoder` so a multi-MB sitemap doesn't have to fit
//     in memory twice.
//   - Whitespace inside `<loc>` is stripped (sitemaps in the wild often
//     wrap the URL onto its own line with indentation).
//   - Malformed entries are skipped silently; we keep going to the
//     next entity.  Matches the Rust port's `SiteMapEntity::Err` arm.
//   - We don't try to detect gzip — callers fetch the body via
//     `HttpClient`, which already accepts `gzip` `Content-Encoding`
//     transparently, and most CDNs gunzip server-side anyway.
//     `.xml.gz` URLs that arrive as raw gzip bytes will not parse and
//     will return empty arrays — caller can spot it by `body
//     startsWith` `<` if it cares.

type sitemapParserState struct {
	mu        sync.Mutex
	pageURLs  []string
	indexURLs []string
}

func (obj *ObjectServer) callSitemapParserMethod(name string, args []Value) Value {
	switch name {
	case "initialize":
		return Nil
	case "class":
		return StrVal(UserClassSitemapParser)
	case "yourself":
		return ObjVal(obj.id)
	case "isNil":
		return False
	case "notNil":
		return True
	case "respondsTo:":
		if len(args) < 1 {
			return False
		}
		switch args[0].String() {
		case "parse:", "pageUrls", "indexUrls", "empty":
			return True
		}
		return False
	case "inspect":
		st := obj.sitemapParserState()
		st.mu.Lock()
		obj.transcriptf("SitemapParser: %d page urls, %d index urls", len(st.pageURLs), len(st.indexURLs))
		st.mu.Unlock()
		return Nil
	case "free":
		obj.deregister()
		return Nil

	case "parse:":
		if len(args) < 1 {
			return obj.sitemapParserResult(nil, nil)
		}
		page, idx := parseSitemapXML(args[0].String())
		st := obj.sitemapParserState()
		st.mu.Lock()
		st.pageURLs = page
		st.indexURLs = idx
		st.mu.Unlock()
		return obj.sitemapParserResult(page, idx)

	case "pageUrls":
		st := obj.sitemapParserState()
		st.mu.Lock()
		out := append([]string(nil), st.pageURLs...)
		st.mu.Unlock()
		return obj.sitemapParserArray(out)

	case "indexUrls":
		st := obj.sitemapParserState()
		st.mu.Lock()
		out := append([]string(nil), st.indexURLs...)
		st.mu.Unlock()
		return obj.sitemapParserArray(out)

	case "empty":
		st := obj.sitemapParserState()
		st.mu.Lock()
		defer st.mu.Unlock()
		return BoolVal(len(st.pageURLs) == 0 && len(st.indexURLs) == 0)
	}

	obj.transcriptf("SitemapParser doesNotUnderstand: %s", name)
	return Nil
}

func (obj *ObjectServer) sitemapParserState() *sitemapParserState {
	if obj.sitemapState == nil {
		obj.sitemapState = &sitemapParserState{}
	}
	return obj.sitemapState
}

// sitemapParserResult builds the response Dictionary callers see from
// `parse:`.  Spawns one Dictionary and two Arrays — the cost is fine
// because parse: is called once per fetch, not in a hot loop.
func (obj *ObjectServer) sitemapParserResult(page, idx []string) Value {
	dictID := obj.spawnBuiltin(BuiltinClassDictionary)
	if dictID == "" {
		return Nil
	}
	ref := ObjVal(dictID)
	obj.sendMessage(ref, "at:put:", []Value{StrVal("page_urls"), obj.sitemapParserArray(page)})
	obj.sendMessage(ref, "at:put:", []Value{StrVal("index_urls"), obj.sitemapParserArray(idx)})
	return ref
}

func (obj *ObjectServer) sitemapParserArray(items []string) Value {
	arrID := obj.spawnBuiltin(BuiltinClassArray)
	if arrID == "" {
		return Nil
	}
	ref := ObjVal(arrID)
	for _, s := range items {
		obj.sendMessage(ref, "add:", []Value{StrVal(s)})
	}
	return ref
}

// parseSitemapXML walks the body once with encoding/xml's streaming
// decoder.  It tracks the current `<url>` / `<sitemap>` parent so a
// `<loc>` is bucketed correctly.  Anything outside one of those two
// shapes is ignored (no <lastmod>, <priority>, <changefreq>, etc.).
func parseSitemapXML(body string) (pageURLs, indexURLs []string) {
	dec := xml.NewDecoder(strings.NewReader(body))
	dec.Strict = false
	dec.AutoClose = xml.HTMLAutoClose

	var (
		inURL     int // depth of <url> elements (1 when we're inside one)
		inSitemap int // depth of <sitemap> elements
		inLoc     bool
		buf       strings.Builder
	)

	for {
		tok, err := dec.Token()
		if err != nil {
			if err == io.EOF {
				return
			}
			// XML errors mid-stream: skip the bad token and keep going.
			// The encoding/xml decoder advances past the offending byte
			// on its own, so the loop won't spin.
			continue
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "url":
				inURL++
			case "sitemap":
				inSitemap++
			case "loc":
				inLoc = true
				buf.Reset()
			}
		case xml.CharData:
			if inLoc {
				buf.Write(t)
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "url":
				if inURL > 0 {
					inURL--
				}
			case "sitemap":
				if inSitemap > 0 {
					inSitemap--
				}
			case "loc":
				if inLoc {
					url := strings.TrimSpace(buf.String())
					if url != "" {
						switch {
						case inURL > 0:
							pageURLs = append(pageURLs, url)
						case inSitemap > 0:
							indexURLs = append(indexURLs, url)
						}
					}
					inLoc = false
					buf.Reset()
				}
			}
		}
	}
}
