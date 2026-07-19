package dynacat

import (
	"reflect"
	"strings"
)

// widgetFieldSchema describes one editable option of a widget for the editor UI.
type widgetFieldSchema struct {
	Name     string   `json:"name"`
	Label    string   `json:"label"`
	Kind     string   `json:"kind"` // text|number|checkbox|select|duration|icon|yaml
	Required bool     `json:"required,omitempty"`
	Advanced bool     `json:"advanced,omitempty"`
	Options  []string `json:"options,omitempty"`
}

type widgetTypeSchema struct {
	Type   string              `json:"type"`
	Label  string              `json:"label"`
	Icon   string              `json:"icon"`
	Hidden bool                `json:"hidden,omitempty"` // kept editable but omitted from the palette
	Fields []widgetFieldSchema `json:"fields"`
}

// hiddenWidgetTypes are excluded from the palette (still editable if already used).
var hiddenWidgetTypes = map[string]bool{
	"html": true,
}

type widgetTypeMeta struct {
	Type  string
	Label string
	Icon  string
}

// widgetTypeCatalog lists every user-selectable widget for the palette.
// Keep in sync with the newWidget switch in widget.go (the one manual sync point).
var widgetTypeCatalog = []widgetTypeMeta{
	{"calendar", "Calendar", "mdi:calendar"},
	{"clock", "Clock", "mdi:clock-outline"},
	{"weather", "Weather", "mdi:weather-partly-cloudy"},
	{"bookmarks", "Bookmarks", "mdi:bookmark-outline"},
	{"iframe", "IFrame", "mdi:application-brackets-outline"},
	{"html", "HTML", "mdi:language-html5"},
	{"hacker-news", "Hacker News", "mdi:newspaper-variant-outline"},
	{"releases", "Releases", "mdi:tag-outline"},
	{"videos", "Videos", "mdi:youtube"},
	{"markets", "Markets", "mdi:chart-line"},
	{"reddit", "Reddit", "mdi:reddit"},
	{"rss", "RSS", "mdi:rss"},
	{"monitor", "Monitor", "mdi:heart-pulse"},
	{"twitch-top-games", "Twitch Top Games", "mdi:twitch"},
	{"twitch-channels", "Twitch Channels", "mdi:twitch"},
	{"lobsters", "Lobsters", "mdi:message-text-outline"},
	{"change-detection", "Change Detection", "mdi:eye-outline"},
	{"repository", "Repository", "mdi:source-repository"},
	{"search", "Search", "mdi:magnify"},
	{"stopwatch", "Stopwatch", "mdi:timer-outline"},
	{"extension", "Extension", "mdi:puzzle-outline"},
	{"group", "Group", "mdi:tab"},
	{"dns-stats", "DNS Stats", "mdi:dns-outline"},
	{"split-column", "Split Column", "mdi:view-column-outline"},
	{"custom-api", "Custom API", "mdi:api"},
	{"dynawidgets", "Dynawidgets", "mdi:widgets-outline"},
	{"docker-containers", "Docker Containers", "mdi:docker"},
	{"docker-controller", "Docker Controller", "mdi:docker"},
	{"server-stats", "Server Stats", "mdi:server"},
	{"speedtest", "Speedtest", "mdi:speedometer"},
	{"to-do", "To-do", "mdi:checkbox-marked-outline"},
	{"playing", "Now Playing", "mdi:play-circle-outline"},
	{"latest-media", "Latest Media", "mdi:multimedia"},
	{"torrenting", "Torrenting", "mdi:download-network-outline"},
}

type fieldAnnotation struct {
	Advanced bool
	Kind     string
	Options  []string
	Required bool
}

// alwaysAdvancedFields are the shared plumbing options hidden under Advanced for
// every widget. Everything else defaults to basic (shown by default).
var alwaysAdvancedFields = map[string]bool{
	"title": true, "title-icon": true, "title-url": true, "hide-header": true,
	"css-class": true, "cache": true, "update-interval": true, "lazy-load": true,
	"frameless": true,
}

// fieldAnnotations overlays per-widget specifics reflection cannot know: which
// widget-specific fields are advanced (auth/network/low-level), select options and
// required flags. Fields not listed here (and not in alwaysAdvancedFields) are basic.
var fieldAnnotations = map[string]map[string]fieldAnnotation{
	"calendar": {
		"first-day-of-week": {Options: []string{"monday", "tuesday", "wednesday", "thursday", "friday", "saturday", "sunday"}},
	},
	"clock": {
		"hour-format": {Options: []string{"24h", "12h"}},
	},
	"weather": {
		"location":    {Required: true},
		"hour-format": {Options: []string{"12h", "24h"}},
		"units":       {Options: []string{"metric", "imperial"}},
	},
	"iframe": {
		"source": {Required: true},
	},
	"html": {
		"source": {Required: true},
	},
	"hacker-news": {
		"sort-by":               {Options: []string{"top", "new", "best"}},
		"extra-sort-by":         {Options: []string{"engagement"}},
		"comments-url-template": {Advanced: true},
	},
	"releases": {
		"repositories": {Required: true},
		"token":        {Advanced: true},
		"gitlab-token": {Advanced: true},
	},
	"videos": {
		"style":              {Options: []string{"grid-cards", "vertical-list"}},
		"video-url-template": {Advanced: true},
	},
	"markets": {
		"sort-by":              {Options: []string{"absolute-change", "change"}},
		"chart-link-template":  {Advanced: true},
		"symbol-link-template": {Advanced: true},
		"proxy":                {Advanced: true},
	},
	"reddit": {
		"subreddit":             {Required: true},
		"sort-by":               {Options: []string{"hot", "new", "top", "rising"}},
		"top-period":            {Options: []string{"hour", "day", "week", "month", "year", "all"}},
		"style":                 {Options: []string{"horizontal-cards", "vertical-cards"}},
		"extra-sort-by":         {Options: []string{"engagement"}},
		"proxy":                 {Advanced: true},
		"comments-url-template": {Advanced: true},
		"request-url-template":  {Advanced: true},
		"app-auth":              {Advanced: true},
	},
	"rss": {
		"feeds": {Required: true},
		"style": {Options: []string{"detailed-list", "horizontal-cards", "horizontal-cards-2"}},
	},
	"monitor": {
		"sites": {Required: true},
		"style": {Options: []string{"compact"}},
	},
	"twitch-channels": {
		"sort-by": {Options: []string{"viewers", "live"}},
	},
	"lobsters": {
		"sort-by": {Options: []string{"hot", "new"}},
	},
	"change-detection": {
		"allow-insecure": {Advanced: true},
		"token":          {Advanced: true},
	},
	"repository": {
		"repository": {Required: true},
		"token":      {Advanced: true},
	},
	"search": {
		"search-engine":         {Options: []string{"duckduckgo", "google", "bing", "perplexity", "kagi", "startpage", "qwant", "brave", "custom"}},
		"autocomplete-provider": {Options: []string{"duckduckgo", "brave"}},
	},
	"extension": {
		"url":                              {Required: true},
		"fallback-content-type":            {Options: []string{"html"}},
		"headers":                          {Advanced: true},
		"allow-potentially-dangerous-html": {Advanced: true},
	},
	"dns-stats": {
		"service":        {Required: true, Options: []string{"adguard", "pihole", "pihole-v6", "technitium", "blocky"}},
		"hour-format":    {Options: []string{"12h", "24h"}},
		"token":          {Advanced: true},
		"username":       {Advanced: true},
		"password":       {Advanced: true},
		"allow-insecure": {Advanced: true},
	},
	"custom-api": {
		"template":       {Required: true, Kind: "yaml"},
		"subrequests":    {Advanced: true},
		"method":         {Advanced: true},
		"body":           {Advanced: true},
		"body-type":      {Advanced: true, Options: []string{"json", "string"}},
		"headers":        {Advanced: true},
		"allow-insecure": {Advanced: true},
	},
	"dynawidgets": {
		"widget":         {Required: true},
		"repo":           {Advanced: true},
		"subrequests":    {Advanced: true},
		"method":         {Advanced: true},
		"body":           {Advanced: true},
		"body-type":      {Advanced: true, Options: []string{"json", "string"}},
		"headers":        {Advanced: true},
		"allow-insecure": {Advanced: true},
	},
	"docker-containers": {
		"sock-path": {Advanced: true},
	},
	"docker-controller": {
		"show":      {Options: []string{"both", "containers", "images"}},
		"sock-path": {Advanced: true},
	},
	"to-do": {
		"storage": {Options: []string{"local", "server"}},
		"id":      {Advanced: true},
	},
	"playing": {
		"hosts":                {Required: true},
		"play-state":           {Options: []string{"indicator", "text"}},
		"episode-title-format": {Options: []string{"series", "episode"}},
	},
	"latest-media": {
		"hosts": {Required: true},
	},
	"torrenting": {
		"hosts": {Required: true},
	},
	"speedtest": {
		"server": {Required: true},
	},
}

// widgetSchema reflects a widget struct into an editor schema, overlaying annotations.
func widgetSchema(meta widgetTypeMeta) (widgetTypeSchema, error) {
	w, err := newWidget(meta.Type)
	if err != nil {
		return widgetTypeSchema{}, err
	}

	fields := reflectWidgetFields(reflect.TypeOf(w).Elem())
	ann := fieldAnnotations[meta.Type]

	for i := range fields {
		f := &fields[i]
		f.Advanced = alwaysAdvancedFields[f.Name]
		f.Label = humanizeFieldName(f.Name)

		a, ok := ann[f.Name]
		if !ok {
			continue
		}
		if a.Advanced {
			f.Advanced = true
		}
		if a.Kind != "" {
			f.Kind = a.Kind
		}
		if len(a.Options) > 0 {
			f.Kind = "select"
			f.Options = a.Options
		}
		f.Required = a.Required
	}

	return widgetTypeSchema{
		Type:   meta.Type,
		Label:  meta.Label,
		Icon:   string(newCustomIconField(meta.Icon).URL),
		Hidden: hiddenWidgetTypes[meta.Type],
		Fields: fields,
	}, nil
}

func allWidgetSchemas() []widgetTypeSchema {
	schemas := make([]widgetTypeSchema, 0, len(widgetTypeCatalog))
	for _, meta := range widgetTypeCatalog {
		if s, err := widgetSchema(meta); err == nil {
			schemas = append(schemas, s)
		}
	}
	return schemas
}

// reflectWidgetFields walks yaml-tagged fields, recursing into inlined structs.
func reflectWidgetFields(t reflect.Type) []widgetFieldSchema {
	var fields []widgetFieldSchema

	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		name, inline := parseYAMLTag(f.Tag.Get("yaml"))

		ft := f.Type
		for ft.Kind() == reflect.Ptr {
			ft = ft.Elem()
		}

		if inline && ft.Kind() == reflect.Struct {
			fields = append(fields, reflectWidgetFields(ft)...)
			continue
		}
		if name == "" || name == "-" || name == "type" || !f.IsExported() {
			continue
		}

		fields = append(fields, widgetFieldSchema{Name: name, Kind: fieldKind(f.Type)})
	}

	return fields
}

func parseYAMLTag(tag string) (name string, inline bool) {
	parts := strings.Split(tag, ",")
	name = parts[0]
	for _, p := range parts[1:] {
		if p == "inline" {
			inline = true
		}
	}
	return name, inline
}

var (
	iconFieldType     = reflect.TypeOf(customIconField{})
	durationFieldType = reflect.TypeOf(durationField(0))
	intervalFieldType = reflect.TypeOf(updateIntervalField(0))
)

func fieldKind(t reflect.Type) string {
	for t.Kind() == reflect.Ptr {
		t = t.Elem()
	}

	switch t {
	case iconFieldType:
		return "icon"
	case durationFieldType, intervalFieldType:
		return "duration"
	}

	switch t.Kind() {
	case reflect.Bool:
		return "checkbox"
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return "number"
	case reflect.String:
		return "text"
	default:
		return "yaml" // slices, maps, nested structs get a raw yaml editor
	}
}

func humanizeFieldName(name string) string {
	words := strings.Split(name, "-")
	for i, w := range words {
		if w != "" {
			words[i] = strings.ToUpper(w[:1]) + w[1:]
		}
	}
	return strings.Join(words, " ")
}
