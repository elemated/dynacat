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

// The editor reads and writes the YAML source files directly using yaml.Node so
// comments, ordering and $include directives are preserved. Each page is either
// inline in the main file or pulled in via `- $include: file.yml`; edits are routed
// to the file that actually owns the page.

type editorConfigView struct {
	Pages        []editorPageView  `json:"pages"`
	Theme        map[string]string `json:"theme"`
	Branding     map[string]string `json:"branding"`
	MainWritable bool              `json:"mainWritable"`
}

type editorPageView struct {
	Title    string             `json:"title"`
	Slug     string             `json:"slug"`
	Width    string             `json:"width"`
	File     string             `json:"file"`
	Writable bool               `json:"writable"`
	Options  map[string]string  `json:"options"` // raw scalar page-level options (name-icon, key-bind, ...)
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
	Widgets []editorWidgetView `json:"widgets,omitempty"` // nested children for container widgets
}

type editorMutation struct {
	Op         string            `json:"op"`
	Page       int               `json:"page"`
	Column     int               `json:"column"` // addColumn/removeColumn
	Index      int               `json:"index"`  // addColumn insert position
	Path       []int             `json:"path"`   // widget location: [column, ...nested, index]
	ToPath     []int             `json:"toPath"` // moveWidget destination
	Size       string            `json:"size"`
	WidgetType string            `json:"widgetType"`
	Fields     map[string]any    `json:"fields"`    // scalar inputs
	RawFields  map[string]string `json:"rawFields"` // yaml-kind inputs, parsed as YAML
	Title      string            `json:"title"`     // addPage
	Layout     []string          `json:"layout"`    // addPage column sizes
	Theme      map[string]any    `json:"theme"`     // editStyling: theme section fields
	Branding   map[string]any    `json:"branding"`  // editStyling: branding section fields
}

type editorPermissionError struct{ path string }

func (e *editorPermissionError) Error() string {
	return fmt.Sprintf("cannot write %s: the config directory is read only or lacks write permission", filepath.Base(e.path))
}

// isWriteBlockedError reports errors meaning the file cannot be written: permission
// denied or a read-only filesystem (e.g. a read-only Docker bind mount).
func isWriteBlockedError(err error) bool {
	return os.IsPermission(err) || errors.Is(err, syscall.EROFS)
}

type editorValidationError struct{ err error }

func (e *editorValidationError) Error() string { return e.err.Error() }

// buildEditorConfigView builds the logical page/column/widget tree from the source files.
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
		// Slug is derived from the name at load time and absent from the source YAML,
		// so take the runtime slug/title (page order matches the source order).
		if i < len(a.Config.Pages) {
			pv.Slug = a.Config.Pages[i].Slug
			pv.Title = a.Config.Pages[i].Title
		}
		view.Pages = append(view.Pages, pv)
	}

	// Theme and branding live in the main config file's top-level mapping.
	root := documentRoot(mainDoc)
	view.Theme = sectionToStringMap(root, "theme")
	view.Branding = sectionToStringMap(root, "branding")
	view.MainWritable = pathWritable(mainPath)

	return view, nil
}

// sectionToStringMap reads the scalar children of a top-level mapping (e.g. theme
// or branding) into a flat map. Nested mappings such as theme presets are skipped.
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

func pageNodeToView(pageNode *yaml.Node, path string) editorPageView {
	pv := editorPageView{
		Title:    scalarValue(getMappingValue(pageNode, "name")),
		Slug:     scalarValue(getMappingValue(pageNode, "slug")),
		Width:    scalarValue(getMappingValue(pageNode, "width")),
		File:     filepath.Base(path),
		Writable: pathWritable(path),
		Options:  map[string]string{},
	}

	// All top-level scalar keys are surfaced so the editor can prefill the page
	// options modal (name-icon, key-bind, hide-from-navigation, ...).
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
			// Container children are surfaced as nested views, not a raw value.
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

// applyEditorMutation mutates the owning source file's node tree, then writes and
// validates. On validation failure the original bytes are restored.
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
		applyStylingSection(root, "theme", m.Theme)
		applyStylingSection(root, "branding", m.Branding)
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
		// Capture the included file (if any) before dropping the node so it can be
		// removed from disk once the main file is rewritten without it.
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

	// Keep the edited page in readable block style; a widget inserted into a `[]`
	// sequence would otherwise collapse to `[{type: reddit, ...}]` flow style.
	setBlockStyleDeep(pageNode)

	return a.writeConfigCandidate(path, marshalDocument(doc))
}

// setBlockStyleDeep forces block (multi-line) style on every mapping and sequence
// in the subtree so editor writes stay easy to read. Scalar nodes keep their own
// style so quoting (e.g. a single-quoted URL) is preserved.
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
		// Same parent: the target index was computed before removal.
		if dstWidgets == widgets && dstIndex > index {
			dstIndex--
		}
		dstWidgets.Content = insertNode(dstWidgets.Content, dstIndex, node)
	default:
		return fmt.Errorf("unknown op: %s", m.Op)
	}
	return nil
}

// resolveWidgets walks a widget path ([column, ...nested container indices, target
// index]) to the widgets sequence that owns the target and its index within it.
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

// isIntPrefix reports whether prefix is a leading slice of full (used to block
// moving a container into its own subtree).
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

// applyPageFields upserts page-level options. Empty strings and false booleans drop
// the key so the YAML stays minimal; `name` is never dropped since a page needs one.
// New keys land above `columns` so page metadata stays grouped at the top.
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

// setPageMappingKey updates an existing key in place, otherwise inserts it before
// the `columns` entry (falling back to append) so metadata precedes the layout.
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

// separatePageFilesEnabled reports whether a new page created in the editor gets
// its own file linked via `$include`. Set EDITOR_SEPARATE_PAGE_FILES to false/0/f
// to write new pages inline into the main config instead.
func separatePageFilesEnabled() bool {
	switch os.Getenv("EDITOR_SEPARATE_PAGE_FILES") {
	case "false", "0", "f":
		return false
	}
	return true
}

// addPageInline appends the new page directly to the main config's `pages` list.
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

// addPageFile writes the new page to its own YAML file and links it into the main
// config via `- $include: file.yml`, matching how the pre-shipped pages are kept in
// separate files. On validation failure both the new file and the main edit roll back.
func (a *application) addPageFile(mainDoc *yaml.Node, mainPath string, m editorMutation) error {
	pages := getMappingValue(documentRoot(mainDoc), "pages")
	if pages == nil {
		pages = sequenceNode()
		addPair(documentRoot(mainDoc), "pages", pages)
	}

	dir := filepath.Dir(mainPath)
	file := uniquePageFileName(dir, orDefault(m.Title, "New Page"))
	pagePath := filepath.Join(dir, file)

	// The included file holds a single-item sequence (`- name: ...`), the form the
	// pre-shipped page files use.
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
		os.Remove(pagePath) // drop the orphaned page file when the include is rejected
		return err
	}
	return nil
}

// uniquePageFileName turns a page title into a config-directory-relative filename
// that does not collide with an existing file.
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
		os.WriteFile(path, original, perm) // roll back invalid edit
		return &editorValidationError{err}
	}

	return nil
}

// resolvePageNode returns the file, document and page mapping node owning a page.
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

//
// yaml.Node helpers
//

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

// firstMapping returns the page mapping, whether the file root is that mapping or a
// single-item sequence (the `- name: ...` form used by included page files).
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

// applyFieldsToNode upserts scalar fields and parsed yaml fields; empty values drop the key.
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

// applyStylingSection upserts the scalar fields of a top-level section (theme or
// branding), creating it if needed. A field that is empty, false or zero drops the
// key so the YAML stays close to the defaults. An emptied section is removed.
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
