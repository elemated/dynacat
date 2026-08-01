package dynacat

import (
	"reflect"
	"slices"
	"strings"
)

type widgetFieldSchema struct {
	Name     string              `json:"name"`
	Label    string              `json:"label"`
	Kind     string              `json:"kind"` // text|number|checkbox|select|duration|icon|yaml|list
	Required bool                `json:"required,omitempty"`
	Advanced bool                `json:"advanced,omitempty"`
	Options  []string            `json:"options,omitempty"`
	Item     []widgetFieldSchema `json:"item,omitempty"`
	ItemKind string              `json:"itemKind,omitempty"`
}

type widgetTypeSchema struct {
	Type   string              `json:"type"`
	Label  string              `json:"label"`
	Icon   string              `json:"icon"`
	Hidden bool                `json:"hidden,omitempty"`
	Fields []widgetFieldSchema `json:"fields"`
}

var hiddenWidgetTypes = map[string]bool{
	"html": true,
}

type widgetTypeMeta struct {
	Type  string
	Label string
	Icon  string
}

// Keep in sync with the newWidget switch in widget.go.
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
}

var alwaysAdvancedFields = map[string]bool{
	"title": true, "title-icon": true, "title-url": true, "hide-header": true,
	"css-class": true, "cache": true, "update-interval": true, "lazy-load": true,
	"frameless": true, "api-id": true,
}

// Fields the user must fill in for the widget to work. Fields of a list entry are addressed
// as "<list-field>.<entry-field>". Mirrors the "required" column in docs/docs/configuration.md.
var requiredFields = map[string][]string{
	"bookmarks":       {"groups", "groups.title", "groups.links", "groups.links.title", "groups.links.url"},
	"calendar":        {"hosts.url", "hosts.token"},
	"clock":           {"timezones.timezone"},
	"custom-api":      {"template"},
	"dns-stats":       {"url"},
	"dynawidgets":     {"widget"},
	"extension":       {"url"},
	"html":            {"source"},
	"iframe":          {"source"},
	"latest-media":    {"hosts", "hosts.url", "hosts.token"},
	"markets":         {"markets", "markets.symbol", "stocks.symbol"},
	"monitor":         {"sites", "sites.title", "sites.url"},
	"playing":         {"hosts", "hosts.url", "hosts.token"},
	"reddit":          {"subreddit"},
	"releases":        {"repositories", "repositories.repository"},
	"repository":      {"repository"},
	"rss":             {"feeds", "feeds.url"},
	"search":          {"bangs.shortcut", "bangs.url"},
	"server-stats":    {"servers.url"},
	"torrenting":      {"hosts", "hosts.url"},
	"twitch-channels": {"channels"},
	"videos":          {"channels"},
	"weather":         {"location"},
}

// Per-entry fields tucked behind the "Advanced" toggle of a list card.
var itemAdvancedFields = map[string]bool{
	"check-url": true, "error-url": true, "method": true, "timeout": true,
	"allow-insecure": true, "basic-auth": true, "token": true, "headers": true,
	"alt-status-codes": true, "same-tab": true, "target": true, "hide-arrow": true,
	"disabled": true, "item-link-prefix": true, "public-url": true, "invert-colors": true,
}

const maxListDepth = 2

var fieldAnnotations = map[string]map[string]fieldAnnotation{
	"calendar": {
		"first-day-of-week": {Options: []string{"monday", "tuesday", "wednesday", "thursday", "friday", "saturday", "sunday"}},
	},
	"clock": {
		"hour-format": {Options: []string{"24h", "12h"}},
	},
	"weather": {
		"hour-format": {Options: []string{"12h", "24h"}},
		"units":       {Options: []string{"metric", "imperial"}},
	},
	"hacker-news": {
		"sort-by":               {Options: []string{"top", "new", "best"}},
		"extra-sort-by":         {Options: []string{"engagement"}},
		"comments-url-template": {Advanced: true},
	},
	"releases": {
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
		"style": {Options: []string{"detailed-list", "horizontal-cards", "horizontal-cards-2"}},
	},
	"monitor": {
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
		"token": {Advanced: true},
	},
	"search": {
		"search-engine":         {Options: []string{"duckduckgo", "google", "bing", "perplexity", "kagi", "startpage", "qwant", "brave", "custom"}},
		"autocomplete-provider": {Options: []string{"duckduckgo", "brave", "custom"}},
	},
	"extension": {
		"fallback-content-type":            {Options: []string{"html"}},
		"headers":                          {Advanced: true},
		"allow-potentially-dangerous-html": {Advanced: true},
	},
	"dns-stats": {
		"service":        {Options: []string{"adguard", "pihole", "pihole-v6", "technitium", "blocky"}},
		"hour-format":    {Options: []string{"12h", "24h"}},
		"token":          {Advanced: true},
		"username":       {Advanced: true},
		"password":       {Advanced: true},
		"allow-insecure": {Advanced: true},
	},
	"custom-api": {
		"template":       {Kind: "yaml"},
		"subrequests":    {Advanced: true},
		"method":         {Advanced: true},
		"body":           {Advanced: true},
		"body-type":      {Advanced: true, Options: []string{"json", "string"}},
		"headers":        {Advanced: true},
		"allow-insecure": {Advanced: true},
	},
	"dynawidgets": {
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
		"play-state":           {Options: []string{"indicator", "text"}},
		"episode-title-format": {Options: []string{"series", "episode"}},
	},
}

func widgetSchema(meta widgetTypeMeta) (widgetTypeSchema, error) {
	w, err := newWidget(meta.Type)
	if err != nil {
		return widgetTypeSchema{}, err
	}

	fields := reflectWidgetFields(reflect.TypeOf(w).Elem(), 0)
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
	}

	markRequiredFields(fields, "", requiredFields[meta.Type])

	return widgetTypeSchema{
		Type:   meta.Type,
		Label:  meta.Label,
		Icon:   string(newCustomIconField(meta.Icon).URL),
		Hidden: hiddenWidgetTypes[meta.Type],
		Fields: fields,
	}, nil
}

func markRequiredFields(fields []widgetFieldSchema, prefix string, required []string) {
	for i := range fields {
		path := prefix + fields[i].Name
		fields[i].Required = slices.Contains(required, path)
		if fields[i].Required {
			fields[i].Advanced = false
		}
		markRequiredFields(fields[i].Item, path+".", required)
	}
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

var deprecatedSchemaFields = map[string]bool{
	"autocomplete-url": true,
}

func reflectWidgetFields(t reflect.Type, depth int) []widgetFieldSchema {
	var fields []widgetFieldSchema

	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		name, inline := parseYAMLTag(f.Tag.Get("yaml"))

		ft := f.Type
		for ft.Kind() == reflect.Ptr {
			ft = ft.Elem()
		}

		if inline && ft.Kind() == reflect.Struct {
			fields = append(fields, reflectWidgetFields(ft, depth)...)
			continue
		}
		// List entries often come from external structs with no yaml tags, which yaml.v3 maps by lowercased name.
		if name == "" && depth > 0 && f.IsExported() {
			name = strings.ToLower(f.Name)
		}
		if name == "" || name == "-" || name == "type" || !f.IsExported() || deprecatedSchemaFields[name] {
			continue
		}

		fields = append(fields, reflectField(name, f.Type, depth))
	}

	return fields
}

func reflectField(name string, t reflect.Type, depth int) widgetFieldSchema {
	field := widgetFieldSchema{Name: name, Kind: fieldKind(t)}
	if field.Kind != "yaml" || depth >= maxListDepth {
		return field
	}

	for t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	if t.Kind() != reflect.Slice {
		return field
	}

	elem := t.Elem()
	for elem.Kind() == reflect.Ptr {
		elem = elem.Elem()
	}

	if elemKind := fieldKind(elem); elemKind != "yaml" {
		field.Kind, field.ItemKind = "list", elemKind
		return field
	}
	if elem.Kind() != reflect.Struct {
		return field
	}

	item := reflectWidgetFields(elem, depth+1)
	if len(item) == 0 {
		return field
	}

	for i := range item {
		item[i].Label = humanizeFieldName(item[i].Name)
		item[i].Advanced = itemAdvancedFields[item[i].Name]
	}
	slices.SortStableFunc(item, func(a, b widgetFieldSchema) int {
		return fieldOrderRank(a.Name) - fieldOrderRank(b.Name)
	})
	field.Kind, field.Item = "list", item

	return field
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
	colorFieldType    = reflect.TypeOf(hslColorField{})
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
	case colorFieldType:
		return "text"
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
		return "yaml"
	}
}

var fieldLabelAcronyms = map[string]string{"url": "URL", "urls": "URLs", "css": "CSS", "api": "API", "dns": "DNS", "id": "ID", "rss": "RSS", "html": "HTML"}

func humanizeFieldName(name string) string {
	words := strings.Split(name, "-")
	for i, w := range words {
		if a, ok := fieldLabelAcronyms[w]; ok {
			words[i] = a
		} else if w != "" {
			words[i] = strings.ToUpper(w[:1]) + w[1:]
		}
	}
	return strings.Join(words, " ")
}

var preferredFieldOrder = []string{"title", "name", "url", "repository", "symbol", "timezone", "shortcut", "label", "icon", "description"}

func fieldOrderRank(name string) int {
	if i := slices.Index(preferredFieldOrder, name); i >= 0 {
		return i
	}
	return len(preferredFieldOrder)
}
