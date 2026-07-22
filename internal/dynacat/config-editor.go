package dynacat

import (
	"bytes"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"

	"gopkg.in/yaml.v3"
)

type editorConfigView struct {
	Pages        []editorPageView   `json:"pages"`
	Theme        map[string]string  `json:"theme"`
	Branding     map[string]string  `json:"branding"`
	ThemePresets []editorPresetView `json:"themePresets"`
	MainWritable bool               `json:"mainWritable"`
}

type editorPresetView struct {
	Key    string            `json:"key"`
	Values map[string]string `json:"values"`
}

type editorPageView struct {
	Title    string             `json:"title"`
	Slug     string             `json:"slug"`
	Width    string             `json:"width"`
	File     string             `json:"file"`
	Writable bool               `json:"writable"`
	Options  map[string]string  `json:"options"`
	Columns  []editorColumnView `json:"columns"`
}

type editorColumnView struct {
	Size    string             `json:"size"`
	Widgets []editorWidgetView `json:"widgets"`
}

type editorWidgetView struct {
	Type    string             `json:"type"`
	Title   string             `json:"title"`
	Values  map[string]string  `json:"values"`
	Widgets []editorWidgetView `json:"widgets,omitempty"`
}

type editorMutation struct {
	Op         string            `json:"op"`
	Page       int               `json:"page"`
	Column     int               `json:"column"`
	Index      int               `json:"index"`
	Path       []int             `json:"path"`
	ToPath     []int             `json:"toPath"`
	Size       string            `json:"size"`
	WidgetType string            `json:"widgetType"`
	Fields     map[string]any    `json:"fields"`
	RawFields  map[string]string `json:"rawFields"`
	Title      string            `json:"title"`
	Layout     []string          `json:"layout"`
	Theme      map[string]any    `json:"theme"`
	Branding   map[string]any    `json:"branding"`
	PresetKey  string            `json:"presetKey"`
}

type editorPermissionError struct{ path string }

func (e *editorPermissionError) Error() string {
	return fmt.Sprintf("cannot write %s: the config directory is read only or lacks write permission", filepath.Base(e.path))
}

func isWriteBlockedError(err error) bool {
	return os.IsPermission(err) || errors.Is(err, syscall.EROFS)
}

type editorValidationError struct{ err error }

func (e *editorValidationError) Error() string { return e.err.Error() }

func (a *application) buildEditorConfigView() (editorConfigView, error) {
	mainPath := a.configPath
	mainDoc, err := loadYAMLDocument(mainPath)
	if err != nil {
		return editorConfigView{}, err
	}

	pagesNode := getMappingValue(documentRoot(mainDoc), "pages")
	if pagesNode == nil || pagesNode.Kind != yaml.SequenceNode {
		return editorConfigView{}, fmt.Errorf("config has no pages")
	}

	view := editorConfigView{}
	for i := range pagesNode.Content {
		path, _, pageNode, err := resolvePageNode(mainDoc, mainPath, i)
		if err != nil {
			return editorConfigView{}, err
		}
		pv := pageNodeToView(pageNode, path)
		if i < len(a.Config.Pages) {
			pv.Slug = a.Config.Pages[i].Slug
			pv.Title = a.Config.Pages[i].Title
		}
		view.Pages = append(view.Pages, pv)
	}

	root := documentRoot(mainDoc)
	view.Theme = sectionToStringMap(root, "theme")
	view.Branding = sectionToStringMap(root, "branding")
	view.ThemePresets = presetsToViews(root)
	view.MainWritable = pathWritable(mainPath)

	return view, nil
}

func sectionToStringMap(root *yaml.Node, key string) map[string]string {
	out := map[string]string{}
	section := getMappingValue(root, key)
	if section == nil || section.Kind != yaml.MappingNode {
		return out
	}
	for i := 0; i+1 < len(section.Content); i += 2 {
		if v := section.Content[i+1]; v.Kind == yaml.ScalarNode {
			out[section.Content[i].Value] = v.Value
		}
	}
	return out
}

func presetsToViews(root *yaml.Node) []editorPresetView {
	out := []editorPresetView{}
	theme := getMappingValue(root, "theme")
	if theme == nil {
		return out
	}
	presets := getMappingValue(theme, "presets")
	if presets == nil || presets.Kind != yaml.MappingNode {
		return out
	}
	for i := 0; i+1 < len(presets.Content); i += 2 {
		keyNode, valNode := presets.Content[i], presets.Content[i+1]
		if valNode.Kind != yaml.MappingNode {
			continue
		}
		vals := map[string]string{}
		for j := 0; j+1 < len(valNode.Content); j += 2 {
			if s := valNode.Content[j+1]; s.Kind == yaml.ScalarNode {
				vals[valNode.Content[j].Value] = s.Value
			}
		}
		out = append(out, editorPresetView{Key: keyNode.Value, Values: vals})
	}
	return out
}

func pageNodeToView(pageNode *yaml.Node, path string) editorPageView {
	pv := editorPageView{
		Title:    scalarValue(getMappingValue(pageNode, "name")),
		Slug:     scalarValue(getMappingValue(pageNode, "slug")),
		Width:    scalarValue(getMappingValue(pageNode, "width")),
		File:     filepath.Base(path),
		Writable: pathWritable(path),
		Options:  map[string]string{},
	}

	for i := 0; i+1 < len(pageNode.Content); i += 2 {
		if v := pageNode.Content[i+1]; v.Kind == yaml.ScalarNode {
			pv.Options[pageNode.Content[i].Value] = v.Value
		}
	}

	columns := getMappingValue(pageNode, "columns")
	if columns == nil {
		return pv
	}

	for _, col := range columns.Content {
		cv := editorColumnView{Size: scalarValue(getMappingValue(col, "size"))}
		if widgets := getMappingValue(col, "widgets"); widgets != nil {
			for _, w := range widgets.Content {
				cv.Widgets = append(cv.Widgets, widgetNodeToView(w))
			}
		}
		pv.Columns = append(pv.Columns, cv)
	}

	return pv
}

func widgetNodeToView(w *yaml.Node) editorWidgetView {
	wv := editorWidgetView{Values: map[string]string{}}
	for i := 0; i+1 < len(w.Content); i += 2 {
		key, val := w.Content[i].Value, w.Content[i+1]
		switch key {
		case "type":
			wv.Type = val.Value
		case "widgets":
			if val.Kind == yaml.SequenceNode {
				for _, child := range val.Content {
					wv.Widgets = append(wv.Widgets, widgetNodeToView(child))
				}
			}
		case "title":
			wv.Title = val.Value
			wv.Values[key] = nodeToText(val)
		default:
			wv.Values[key] = nodeToText(val)
		}
	}
	return wv
}

func (a *application) applyEditorMutation(m editorMutation) error {
	mainPath := a.configPath
	mainDoc, err := loadYAMLDocument(mainPath)
	if err != nil {
		return err
	}

	if m.Op == "addPage" {
		if separatePageFilesEnabled() {
			return a.addPageFile(mainDoc, mainPath, m)
		}
		return a.addPageInline(mainDoc, mainPath, m)
	}

	if m.Op == "editStyling" {
		root := documentRoot(mainDoc)
		if m.PresetKey != "" {
			applyThemePreset(root, m.PresetKey, m.Theme)
		} else {
			applyStylingSection(root, "theme", m.Theme)
			applyStylingSection(root, "branding", m.Branding)
		}
		return a.writeConfigCandidate(mainPath, marshalDocument(mainDoc))
	}

	if m.Op == "deleteThemePreset" {
		root := documentRoot(mainDoc)
		if m.PresetKey == "" {
			resetBaseTheme(root)
		} else {
			removeThemePreset(root, m.PresetKey)
		}
		return a.writeConfigCandidate(mainPath, marshalDocument(mainDoc))
	}

	if m.Op == "editPage" {
		path, doc, pageNode, err := resolvePageNode(mainDoc, mainPath, m.Page)
		if err != nil {
			return err
		}
		applyPageFields(pageNode, m.Fields)
		setBlockStyleDeep(pageNode)
		return a.writeConfigCandidate(path, marshalDocument(doc))
	}

	if m.Op == "removePage" {
		pages := getMappingValue(documentRoot(mainDoc), "pages")
		if pages == nil || !validIndex(pages.Content, m.Page) {
			return fmt.Errorf("page %d out of range", m.Page)
		}
		includeFile := includeTarget(pages.Content[m.Page])
		pages.Content = removeNode(pages.Content, m.Page)
		if err := a.writeConfigCandidate(mainPath, marshalDocument(mainDoc)); err != nil {
			return err
		}
		if includeFile != "" {
			includePath := includeFile
			if !filepath.IsAbs(includePath) {
				includePath = filepath.Join(filepath.Dir(mainPath), includeFile)
			}
			os.Remove(includePath)
		}
		return nil
	}

	path, doc, pageNode, err := resolvePageNode(mainDoc, mainPath, m.Page)
	if err != nil {
		return err
	}

	columns := getMappingValue(pageNode, "columns")
	if columns == nil || columns.Kind != yaml.SequenceNode {
		return fmt.Errorf("page %d has no columns", m.Page)
	}

	if err := mutateColumns(columns, m); err != nil {
		return err
	}

	setBlockStyleDeep(pageNode)

	return a.writeConfigCandidate(path, marshalDocument(doc))
}

func setBlockStyleDeep(n *yaml.Node) {
	if n == nil {
		return
	}
	if n.Kind == yaml.MappingNode || n.Kind == yaml.SequenceNode {
		n.Style = 0
	}
	for _, c := range n.Content {
		setBlockStyleDeep(c)
	}
}

func mutateColumns(columns *yaml.Node, m editorMutation) error {
	switch m.Op {
	case "addColumn":
		col := newMappingNode()
		addPair(col, "size", scalarNode(orDefault(m.Size, "full")))
		addPair(col, "widgets", sequenceNode())
		columns.Content = insertNode(columns.Content, m.Index, col)
		return nil
	case "removeColumn":
		if !validIndex(columns.Content, m.Column) {
			return fmt.Errorf("column %d out of range", m.Column)
		}
		columns.Content = removeNode(columns.Content, m.Column)
		return nil
	}

	widgets, index, err := resolveWidgets(columns, m.Path)
	if err != nil {
		return err
	}

	switch m.Op {
	case "addWidget":
		node, err := buildWidgetNode(m.WidgetType, m.Fields, m.RawFields)
		if err != nil {
			return err
		}
		widgets.Content = insertNode(widgets.Content, index, node)
	case "editWidget":
		if !validIndex(widgets.Content, index) {
			return fmt.Errorf("widget index out of range")
		}
		if err := editWidgetNode(widgets.Content[index], m.Fields, m.RawFields); err != nil {
			return err
		}
	case "removeWidget":
		if !validIndex(widgets.Content, index) {
			return fmt.Errorf("widget index out of range")
		}
		widgets.Content = removeNode(widgets.Content, index)
	case "moveWidget":
		if isIntPrefix(m.Path, m.ToPath) {
			return fmt.Errorf("cannot move a container into itself")
		}
		if !validIndex(widgets.Content, index) {
			return fmt.Errorf("widget index out of range")
		}
		node := widgets.Content[index]
		dstWidgets, dstIndex, err := resolveWidgets(columns, m.ToPath)
		if err != nil {
			return err
		}
		widgets.Content = removeNode(widgets.Content, index)
		if dstWidgets == widgets && dstIndex > index {
			dstIndex--
		}
		dstWidgets.Content = insertNode(dstWidgets.Content, dstIndex, node)
	default:
		return fmt.Errorf("unknown op: %s", m.Op)
	}
	return nil
}

func resolveWidgets(columns *yaml.Node, path []int) (*yaml.Node, int, error) {
	if len(path) < 2 {
		return nil, 0, fmt.Errorf("invalid widget path")
	}

	widgets := columnWidgets(columns, path[0])
	if widgets == nil {
		return nil, 0, fmt.Errorf("column %d out of range", path[0])
	}

	for _, wi := range path[1 : len(path)-1] {
		if !validIndex(widgets.Content, wi) {
			return nil, 0, fmt.Errorf("widget %d out of range", wi)
		}
		container := widgets.Content[wi]
		inner := getMappingValue(container, "widgets")
		if inner == nil {
			inner = sequenceNode()
			addPair(container, "widgets", inner)
		}
		widgets = inner
	}

	return widgets, path[len(path)-1], nil
}

func isIntPrefix(prefix, full []int) bool {
	if len(prefix) > len(full) {
		return false
	}
	for i := range prefix {
		if prefix[i] != full[i] {
			return false
		}
	}
	return true
}

func applyPageFields(pageNode *yaml.Node, fields map[string]any) {
	for _, k := range sortedKeys(fields) {
		v := fields[k]
		if k == "name" && isEmptyValue(v) {
			continue
		}
		if isEmptyValue(v) || v == false {
			removeMappingKey(pageNode, k)
			continue
		}
		setPageMappingKey(pageNode, k, valueNode(v))
	}
}

func setPageMappingKey(m *yaml.Node, key string, value *yaml.Node) {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			m.Content[i+1] = value
			return
		}
	}
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == "columns" {
			out := make([]*yaml.Node, 0, len(m.Content)+2)
			out = append(out, m.Content[:i]...)
			out = append(out, scalarNode(key), value)
			out = append(out, m.Content[i:]...)
			m.Content = out
			return
		}
	}
	addPair(m, key, value)
}

func newPageMapping(m editorMutation) *yaml.Node {
	page := newMappingNode()
	addPair(page, "name", scalarNode(orDefault(m.Title, "New Page")))

	columns := sequenceNode()
	layout := m.Layout
	if len(layout) == 0 {
		layout = []string{"full"}
	}
	for _, size := range layout {
		col := newMappingNode()
		addPair(col, "size", scalarNode(size))
		addPair(col, "widgets", sequenceNode())
		columns.Content = append(columns.Content, col)
	}
	addPair(page, "columns", columns)
	return page
}

func separatePageFilesEnabled() bool {
	switch os.Getenv("EDITOR_SEPARATE_PAGE_FILES") {
	case "false", "0", "f":
		return false
	}
	return true
}

func (a *application) addPageInline(mainDoc *yaml.Node, mainPath string, m editorMutation) error {
	root := documentRoot(mainDoc)
	pages := getMappingValue(root, "pages")
	if pages == nil {
		pages = sequenceNode()
		addPair(root, "pages", pages)
	}

	page := newPageMapping(m)
	setBlockStyleDeep(page)
	pages.Content = append(pages.Content, page)

	return a.writeConfigCandidate(mainPath, marshalDocument(mainDoc))
}

func (a *application) addPageFile(mainDoc *yaml.Node, mainPath string, m editorMutation) error {
	pages := getMappingValue(documentRoot(mainDoc), "pages")
	if pages == nil {
		pages = sequenceNode()
		addPair(documentRoot(mainDoc), "pages", pages)
	}

	dir := filepath.Dir(mainPath)
	file := uniquePageFileName(dir, orDefault(m.Title, "New Page"))
	pagePath := filepath.Join(dir, file)

	page := newPageMapping(m)
	setBlockStyleDeep(page)
	pageDoc := &yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{
		{Kind: yaml.SequenceNode, Content: []*yaml.Node{page}},
	}}
	if err := os.WriteFile(pagePath, marshalDocument(pageDoc), 0o644); err != nil {
		if isWriteBlockedError(err) {
			return &editorPermissionError{pagePath}
		}
		return err
	}

	include := newMappingNode()
	addPair(include, "$include", scalarNode(file))
	pages.Content = append(pages.Content, include)

	if err := a.writeConfigCandidate(mainPath, marshalDocument(mainDoc)); err != nil {
		os.Remove(pagePath)
		return err
	}
	return nil
}

func uniquePageFileName(dir, title string) string {
	base := titleToSlug(title)
	base = pageFileNamePattern.ReplaceAllString(base, "")
	base = strings.Trim(base, "-")
	if base == "" {
		base = "page"
	}

	name := base + ".yml"
	for i := 2; ; i++ {
		if _, err := os.Stat(filepath.Join(dir, name)); os.IsNotExist(err) {
			return name
		}
		name = fmt.Sprintf("%s-%d.yml", base, i)
	}
}

func (a *application) writeConfigCandidate(path string, candidate []byte) error {
	perm := os.FileMode(0o644)
	if info, err := os.Stat(path); err == nil {
		perm = info.Mode()
	}

	original, _ := os.ReadFile(path)

	if err := os.WriteFile(path, candidate, perm); err != nil {
		if isWriteBlockedError(err) {
			return &editorPermissionError{path}
		}
		return err
	}

	merged, _, err := parseYAMLIncludes(a.configPath)
	if err == nil {
		_, err = newConfigFromYAML(merged)
	}
	if err != nil {
		os.WriteFile(path, original, perm)
		return &editorValidationError{err}
	}

	return nil
}

func resolvePageNode(mainDoc *yaml.Node, mainPath string, idx int) (string, *yaml.Node, *yaml.Node, error) {
	pages := getMappingValue(documentRoot(mainDoc), "pages")
	if pages == nil || pages.Kind != yaml.SequenceNode || !validIndex(pages.Content, idx) {
		return "", nil, nil, fmt.Errorf("page %d out of range", idx)
	}

	item := pages.Content[idx]
	if inc := includeTarget(item); inc != "" {
		path := inc
		if !filepath.IsAbs(path) {
			path = filepath.Join(filepath.Dir(mainPath), path)
		}
		doc, err := loadYAMLDocument(path)
		if err != nil {
			return "", nil, nil, err
		}
		return path, doc, firstMapping(documentRoot(doc)), nil
	}

	return mainPath, mainDoc, item, nil
}

func loadYAMLDocument(path string) (*yaml.Node, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(b, &doc); err != nil {
		return nil, err
	}
	return &doc, nil
}

func documentRoot(doc *yaml.Node) *yaml.Node {
	if doc.Kind == yaml.DocumentNode && len(doc.Content) > 0 {
		return doc.Content[0]
	}
	return doc
}

func firstMapping(root *yaml.Node) *yaml.Node {
	if root.Kind == yaml.SequenceNode && len(root.Content) > 0 {
		return root.Content[0]
	}
	return root
}

func includeTarget(item *yaml.Node) string {
	if item.Kind != yaml.MappingNode {
		return ""
	}
	for i := 0; i+1 < len(item.Content); i += 2 {
		if k := item.Content[i].Value; k == "$include" || k == "!include" {
			return strings.TrimSpace(item.Content[i+1].Value)
		}
	}
	return ""
}

func getMappingValue(m *yaml.Node, key string) *yaml.Node {
	if m == nil || m.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i+1]
		}
	}
	return nil
}

func setMappingKey(m *yaml.Node, key string, value *yaml.Node) {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			m.Content[i+1] = value
			return
		}
	}
	addPair(m, key, value)
}

func removeMappingKey(m *yaml.Node, key string) {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			m.Content = append(m.Content[:i], m.Content[i+2:]...)
			return
		}
	}
}

func columnWidgets(columns *yaml.Node, index int) *yaml.Node {
	if !validIndex(columns.Content, index) {
		return nil
	}
	widgets := getMappingValue(columns.Content[index], "widgets")
	if widgets == nil {
		widgets = sequenceNode()
		addPair(columns.Content[index], "widgets", widgets)
	}
	return widgets
}

func buildWidgetNode(widgetType string, fields map[string]any, rawFields map[string]string) (*yaml.Node, error) {
	if widgetType == "" {
		return nil, fmt.Errorf("widget type is required")
	}
	node := newMappingNode()
	addPair(node, "type", scalarNode(widgetType))
	return node, applyFieldsToNode(node, fields, rawFields)
}

func editWidgetNode(node *yaml.Node, fields map[string]any, rawFields map[string]string) error {
	return applyFieldsToNode(node, fields, rawFields)
}

func applyFieldsToNode(node *yaml.Node, fields map[string]any, rawFields map[string]string) error {
	for _, k := range sortedKeys(fields) {
		if isEmptyValue(fields[k]) {
			removeMappingKey(node, k)
			continue
		}
		setMappingKey(node, k, valueNode(fields[k]))
	}

	for _, k := range sortedStringKeys(rawFields) {
		if strings.TrimSpace(rawFields[k]) == "" {
			removeMappingKey(node, k)
			continue
		}
		parsed, err := parseYAMLValue(rawFields[k])
		if err != nil {
			return fmt.Errorf("field %s: %w", k, err)
		}
		setMappingKey(node, k, parsed)
	}

	return nil
}

func applyStylingSection(root *yaml.Node, key string, fields map[string]any) {
	if fields == nil {
		return
	}
	section := getMappingValue(root, key)
	if section == nil {
		section = newMappingNode()
		setMappingKey(root, key, section)
	}
	for _, k := range sortedKeys(fields) {
		v := fields[k]
		if isEmptyValue(v) || v == false || v == float64(0) {
			removeMappingKey(section, k)
			continue
		}
		setMappingKey(section, k, valueNode(v))
	}
	if len(section.Content) == 0 {
		removeMappingKey(root, key)
	}
}

func applyThemePreset(root *yaml.Node, key string, fields map[string]any) {
	theme := getMappingValue(root, "theme")
	if theme == nil {
		theme = newMappingNode()
		setMappingKey(root, "theme", theme)
	}
	presets := getMappingValue(theme, "presets")
	if presets == nil {
		presets = newMappingNode()
		setMappingKey(theme, "presets", presets)
	}
	preset := getMappingValue(presets, key)
	if preset == nil {
		preset = newMappingNode()
		setMappingKey(presets, key, preset)
	}
	for _, k := range sortedKeys(fields) {
		v := fields[k]
		if isEmptyValue(v) || v == false || v == float64(0) {
			removeMappingKey(preset, k)
			continue
		}
		setMappingKey(preset, k, valueNode(v))
	}
}

func removeThemePreset(root *yaml.Node, key string) {
	theme := getMappingValue(root, "theme")
	if theme == nil {
		return
	}
	presets := getMappingValue(theme, "presets")
	if presets == nil {
		return
	}
	removeMappingKey(presets, key)
	if len(presets.Content) == 0 {
		removeMappingKey(theme, "presets")
	}
}

func resetBaseTheme(root *yaml.Node) {
	theme := getMappingValue(root, "theme")
	if theme == nil {
		return
	}
	kept := theme.Content[:0:0]
	for i := 0; i+1 < len(theme.Content); i += 2 {
		if theme.Content[i+1].Kind == yaml.ScalarNode {
			continue
		}
		kept = append(kept, theme.Content[i], theme.Content[i+1])
	}
	theme.Content = kept
	if len(theme.Content) == 0 {
		removeMappingKey(root, "theme")
	}
}

func parseYAMLValue(text string) (*yaml.Node, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(text), &doc); err != nil {
		return nil, err
	}
	if len(doc.Content) == 0 {
		return scalarNode(""), nil
	}
	return doc.Content[0], nil
}

func marshalDocument(doc *yaml.Node) []byte {
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	enc.Encode(doc)
	enc.Close()
	return buf.Bytes()
}

func nodeToText(n *yaml.Node) string {
	if n.Kind == yaml.ScalarNode {
		return n.Value
	}
	return strings.TrimRight(string(marshalDocument(n)), "\n")
}

func scalarValue(n *yaml.Node) string {
	if n == nil {
		return ""
	}
	return n.Value
}

func newMappingNode() *yaml.Node { return &yaml.Node{Kind: yaml.MappingNode} }

func sequenceNode() *yaml.Node { return &yaml.Node{Kind: yaml.SequenceNode} }

func scalarNode(v string) *yaml.Node { return &yaml.Node{Kind: yaml.ScalarNode, Value: v} }

func addPair(m *yaml.Node, key string, value *yaml.Node) {
	m.Content = append(m.Content, scalarNode(key), value)
}

func valueNode(v any) *yaml.Node {
	switch x := v.(type) {
	case bool:
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: strconv.FormatBool(x)}
	case float64:
		if x == math.Trunc(x) {
			return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: strconv.FormatInt(int64(x), 10)}
		}
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!float", Value: strconv.FormatFloat(x, 'f', -1, 64)}
	case string:
		return scalarNode(x)
	default:
		return scalarNode(fmt.Sprintf("%v", v))
	}
}

func isEmptyValue(v any) bool {
	switch x := v.(type) {
	case nil:
		return true
	case string:
		return strings.TrimSpace(x) == ""
	default:
		return false
	}
}

func insertNode(nodes []*yaml.Node, index int, node *yaml.Node) []*yaml.Node {
	if index < 0 || index > len(nodes) {
		return append(nodes, node)
	}
	nodes = append(nodes, nil)
	copy(nodes[index+1:], nodes[index:])
	nodes[index] = node
	return nodes
}

func removeNode(nodes []*yaml.Node, index int) []*yaml.Node {
	return append(nodes[:index], nodes[index+1:]...)
}

func validIndex(nodes []*yaml.Node, index int) bool {
	return index >= 0 && index < len(nodes)
}

func pathWritable(path string) bool {
	f, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err != nil {
		return false
	}
	f.Close()
	return true
}

func orDefault(v, fallback string) string {
	if strings.TrimSpace(v) == "" {
		return fallback
	}
	return v
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func sortedStringKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
