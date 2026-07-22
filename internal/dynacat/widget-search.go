package dynacat

import (
	"fmt"
	"html/template"
	"sort"
	"strings"
)

var searchWidgetTemplate = mustParseTemplate("search.html", "widget-base.html")

type SearchBang struct {
	Title    string
	Shortcut string
	URL      string
	Icon     customIconField `yaml:"icon"`
}

type searchBookmarkMatch struct {
	Title  string
	URL    string
	Target string
	Icon   customIconField
}

type searchWidget struct {
	widgetBase           `yaml:",inline"`
	cachedHTML           template.HTML `yaml:"-"`
	Frameless            bool          `yaml:"frameless"`
	SearchEngine         string        `yaml:"search-engine"`
	AutocompleteEnabled  *bool         `yaml:"autocomplete"`
	Autocomplete         bool          `yaml:"-"`
	AutocompleteProvider string        `yaml:"autocomplete-provider"`
	// Back-compat for the old autocomplete-provider: custom + autocomplete-url format.
	DeprecatedAutocompleteURL string                `yaml:"autocomplete-url"`
	Bangs                     []SearchBang          `yaml:"bangs"`
	NewTab                    bool                  `yaml:"new-tab"`
	Target                    string                `yaml:"target"`
	Autofocus                 bool                  `yaml:"autofocus"`
	Placeholder               string                `yaml:"placeholder"`
	IncludeBookmarks          bool                  `yaml:"include-bookmarks"`
	CrossPageBookmarks        bool                  `yaml:"cross-page-bookmarks"`
	BookmarkMatches           []searchBookmarkMatch `yaml:"-"`
}

func convertSearchUrl(url string) string {
	// html/template escapes the {QUERY} curlies regardless of type; dodge it with
	// a placeholder restored client-side.
	return strings.ReplaceAll(url, "{QUERY}", "!QUERY!")
}

var searchEngines = map[string]string{
	"duckduckgo": "https://duckduckgo.com/?q={QUERY}",
	"google":     "https://www.google.com/search?q={QUERY}",
	"bing":       "https://www.bing.com/search?q={QUERY}",
	"perplexity": "https://www.perplexity.ai/search?q={QUERY}",
	"kagi":       "https://kagi.com/search?q={QUERY}",
	"startpage":  "https://www.startpage.com/search?q={QUERY}",
	"qwant":      "https://www.qwant.com/?q={QUERY}&t=web",
	"brave":      "https://search.brave.com/search?q={QUERY}",
}

func (widget *searchWidget) initialize() error {
	widget.withTitle("Search").withError(nil)
	widget.UpdateInterval = nil // search widget is static, never poll

	if widget.CrossPageBookmarks {
		widget.IncludeBookmarks = true
	}

	if widget.SearchEngine == "" {
		widget.SearchEngine = "duckduckgo"
	}

	if widget.Placeholder == "" {
		widget.Placeholder = "Type here to search…"
	}

	if widget.AutocompleteEnabled == nil {
		widget.Autocomplete = true
	} else {
		widget.Autocomplete = *widget.AutocompleteEnabled
	}

	if widget.AutocompleteProvider == "custom" && widget.DeprecatedAutocompleteURL != "" {
		widget.AutocompleteProvider = widget.DeprecatedAutocompleteURL
	}

	if widget.AutocompleteProvider == "" {
		widget.AutocompleteProvider = "duckduckgo"
	} else if widget.AutocompleteProvider != "duckduckgo" && widget.AutocompleteProvider != "brave" &&
		!strings.Contains(widget.AutocompleteProvider, "{QUERY}") {
		return fmt.Errorf("autocomplete-provider must be \"duckduckgo\", \"brave\", or a custom URL containing {QUERY}")
	}

	if url, ok := searchEngines[widget.SearchEngine]; ok {
		widget.SearchEngine = url
	}

	widget.SearchEngine = convertSearchUrl(widget.SearchEngine)

	for i := range widget.Bangs {
		if widget.Bangs[i].Shortcut == "" {
			return fmt.Errorf("search bang #%d has no shortcut", i+1)
		}

		if widget.Bangs[i].URL == "" {
			return fmt.Errorf("search bang #%d has no URL", i+1)
		}

		widget.Bangs[i].URL = convertSearchUrl(widget.Bangs[i].URL)
	}

	return nil
}

// AutocompleteProviderKind maps the provider to a kind so a custom URL is never sent to the browser.
func (widget *searchWidget) AutocompleteProviderKind() string {
	if widget.AutocompleteProvider == "duckduckgo" || widget.AutocompleteProvider == "brave" {
		return widget.AutocompleteProvider
	}
	return "custom"
}

func (widget *searchWidget) setProviders(providers *widgetProviders) {
	widget.widgetBase.setProviders(providers)
	for i := range widget.Bangs {
		widget.Bangs[i].Icon.prepare(providers)
	}
	if widget.AutocompleteProviderKind() == "custom" && providers.app != nil {
		providers.app.searchAutocompleteURLs[widget.GetID()] = widget.AutocompleteProvider
	}
	widget.cachedHTML = widget.renderTemplate(widget, searchWidgetTemplate)
}

// collectBookmarks gathers bookmark links to surface as suggestions. Run only
// after every widget is registered, since target bookmarks widgets may not exist
// yet during setProviders. pageFilter restricts matches to one page; nil, all.
func (widget *searchWidget) collectBookmarks(app *application, pageFilter *page) {
	if !widget.IncludeBookmarks {
		return
	}

	var matches []searchBookmarkMatch
	seen := make(map[string]bool)
	for id, w := range app.widgetByID {
		bookmarks, ok := w.(*bookmarksWidget)
		if !ok {
			continue
		}

		if pageFilter != nil && app.widgetToPage[id] != pageFilter {
			continue
		}

		for _, group := range bookmarks.Groups {
			for _, link := range group.Links {
				key := link.Title + "\x00" + link.URL
				if seen[key] {
					continue
				}
				seen[key] = true

				matches = append(matches, searchBookmarkMatch{
					Title:  link.Title,
					URL:    link.URL,
					Target: link.Target,
					Icon:   link.Icon,
				})
			}
		}
	}

	sort.Slice(matches, func(i, j int) bool { return matches[i].Title < matches[j].Title })

	widget.BookmarkMatches = matches
	widget.cachedHTML = widget.renderTemplate(widget, searchWidgetTemplate)
}

func (widget *searchWidget) Render() template.HTML {
	if widget.cachedHTML == "" {
		widget.cachedHTML = widget.renderTemplate(widget, searchWidgetTemplate)
	}
	return widget.cachedHTML
}
