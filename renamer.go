package tfrename

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"math/big"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/zclconf/go-cty/cty"
)

// identRE matches a valid Terraform identifier:
// must start with a letter or underscore, then letters/digits/_/-.
var identRE = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_-]*$`)

// Kind is the kind of Terraform symbol being renamed.
type Kind string

const (
	KindResource Kind = "resource"
	KindData     Kind = "data"
	KindModule   Kind = "module"
	KindVariable Kind = "variable"
	KindOutput   Kind = "output"
	KindLocal    Kind = "local"
	KindUnindex  Kind = "unindex"
	KindAddindex Kind = "addindex"
)

// Target describes what to rename.
type Target struct {
	Kind    Kind
	OldType string // resource/data/unindex/addindex only
	OldName string
	NewType string // resource/data only
	NewName string
	Key     IndexKey // unindex/addindex only
}

// IndexKey is the literal key inside a TYPE.NAME[KEY] reference.
// When IsString is true the key is Str; otherwise it is the integer Int.
// The zero value represents the integer key 0.
type IndexKey struct {
	IsString bool
	Int      int64
	Str      string
}

// Format returns the original `[...]` text, used for log messages.
func (k IndexKey) Format() string {
	if k.IsString {
		return fmt.Sprintf("[%q]", k.Str)
	}
	return fmt.Sprintf("[%d]", k.Int)
}

// matches reports whether a TraverseIndex node has the same key as k.
func (k IndexKey) matches(idx hcl.TraverseIndex) bool {
	if k.IsString {
		return idx.Key.Type() == cty.String && idx.Key.AsString() == k.Str
	}
	if idx.Key.Type() != cty.Number {
		return false
	}
	n, acc := idx.Key.AsBigFloat().Int64()
	return acc == big.Exact && n == k.Int
}

// indexedRefRE matches `TYPE.NAME[KEY]`. KEY can be any non-empty content;
// validity (integer or quoted string) is checked separately.
var indexedRefRE = regexp.MustCompile(`^([A-Za-z_][A-Za-z0-9_-]*)\.([A-Za-z_][A-Za-z0-9_-]*)\[(.+)\]$`)

// ParseUnindexTarget parses a single TYPE.NAME[KEY] argument for the unindex
// command. KEY must be a non-negative integer literal or a double-quoted
// string (without embedded escape sequences).
func ParseUnindexTarget(s string) (*Target, error) {
	return parseIndexedRef(s, KindUnindex)
}

// ParseAddindexTarget parses a single TYPE.NAME[KEY] argument for the addindex
// command — the indexed form references should be rewritten to. Format and
// constraints match ParseUnindexTarget.
func ParseAddindexTarget(s string) (*Target, error) {
	return parseIndexedRef(s, KindAddindex)
}

func parseIndexedRef(s string, kind Kind) (*Target, error) {
	m := indexedRefRE.FindStringSubmatch(s)
	if m == nil {
		return nil, fmt.Errorf("%s argument must be in TYPE.NAME[KEY] format: %q", kind, s)
	}
	key, err := parseIndexKey(m[3])
	if err != nil {
		return nil, fmt.Errorf("invalid key in %q: %w", s, err)
	}
	return &Target{
		Kind:    kind,
		OldType: m[1],
		OldName: m[2],
		Key:     key,
	}, nil
}

func parseIndexKey(s string) (IndexKey, error) {
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		inner := s[1 : len(s)-1]
		if strings.ContainsRune(inner, '"') || strings.ContainsRune(inner, '\\') {
			return IndexKey{}, fmt.Errorf("string keys with escapes or embedded quotes are not supported: %q", s)
		}
		return IndexKey{IsString: true, Str: inner}, nil
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return IndexKey{}, fmt.Errorf("key %q is neither integer nor quoted string", s)
	}
	if n < 0 {
		return IndexKey{}, fmt.Errorf("integer key must be non-negative: %d", n)
	}
	return IndexKey{Int: n}, nil
}

// ParseTarget validates and parses raw old/new strings into a Target.
//
// For resource/data, old/new must be in TYPE.NAME format.
// For module/variable/output/local, old/new must be a simple identifier.
func ParseTarget(kind Kind, oldStr, newStr string) (*Target, error) {
	t := &Target{Kind: kind}
	switch kind {
	case KindResource, KindData:
		op, ok := splitQualified(oldStr)
		if !ok {
			return nil, fmt.Errorf("%s old name must be in TYPE.NAME format: %q", kind, oldStr)
		}
		np, ok := splitQualified(newStr)
		if !ok {
			return nil, fmt.Errorf("%s new name must be in TYPE.NAME format: %q", kind, newStr)
		}
		t.OldType, t.OldName = op[0], op[1]
		t.NewType, t.NewName = np[0], np[1]
	case KindModule, KindVariable, KindOutput, KindLocal:
		if !identRE.MatchString(oldStr) {
			return nil, fmt.Errorf("%s old name must be a valid identifier: %q", kind, oldStr)
		}
		if !identRE.MatchString(newStr) {
			return nil, fmt.Errorf("%s new name must be a valid identifier: %q", kind, newStr)
		}
		t.OldName = oldStr
		t.NewName = newStr
	case KindUnindex:
		return nil, fmt.Errorf("kind %q must be parsed with ParseUnindexTarget", kind)
	case KindAddindex:
		return nil, fmt.Errorf("kind %q must be parsed with ParseAddindexTarget", kind)
	default:
		return nil, fmt.Errorf("unknown kind %q", kind)
	}
	return t, nil
}

// splitQualified parses a TYPE.NAME string into its two parts, rejecting any
// input with a different number of dots or a non-identifier in either part.
func splitQualified(s string) ([2]string, bool) {
	parts := strings.Split(s, ".")
	if len(parts) != 2 {
		return [2]string{}, false
	}
	if !identRE.MatchString(parts[0]) || !identRE.MatchString(parts[1]) {
		return [2]string{}, false
	}
	return [2]string{parts[0], parts[1]}, true
}

// ListSymbols returns every defined symbol name of the given kind in *.tf files
// under dir. For resource and data, names are in TYPE.NAME form.
//
// Parse and I/O errors are silently ignored so this is safe to call from a
// shell-completion context — partial results are better than no completion.
func ListSymbols(dir string, kind Kind) []string {
	matches, err := filepath.Glob(filepath.Join(dir, "*.tf"))
	if err != nil {
		return nil
	}
	sort.Strings(matches)
	seen := map[string]struct{}{}
	add := func(name string) {
		if _, ok := seen[name]; !ok {
			seen[name] = struct{}{}
		}
	}
	for _, path := range matches {
		src, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		f, diags := hclsyntax.ParseConfig(src, path, hcl.Pos{Line: 1, Column: 1})
		if diags.HasErrors() {
			continue
		}
		collectSymbolNames(f.Body.(*hclsyntax.Body), kind, add)
	}
	out := make([]string, 0, len(seen))
	for name := range seen {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

func collectSymbolNames(body *hclsyntax.Body, kind Kind, add func(string)) {
	// HCL accepts quoted labels (e.g. "1abc") that aren't valid Terraform
	// identifiers and would be rejected by ParseTarget. Filter through the
	// same regex so completion never suggests an unrenameable name.
	addIdent := func(name string) {
		if identRE.MatchString(name) {
			add(name)
		}
	}
	addQualified := func(typ, name string) {
		if identRE.MatchString(typ) && identRE.MatchString(name) {
			add(typ + "." + name)
		}
	}
	for _, blk := range body.Blocks {
		switch {
		case kind == KindResource && blk.Type == "resource" && len(blk.Labels) == 2:
			addQualified(blk.Labels[0], blk.Labels[1])
		case kind == KindData && blk.Type == "data" && len(blk.Labels) == 2:
			addQualified(blk.Labels[0], blk.Labels[1])
		case kind == KindModule && blk.Type == "module" && len(blk.Labels) == 1:
			addIdent(blk.Labels[0])
		case kind == KindVariable && blk.Type == "variable" && len(blk.Labels) == 1:
			addIdent(blk.Labels[0])
		case kind == KindOutput && blk.Type == "output" && len(blk.Labels) == 1:
			addIdent(blk.Labels[0])
		case kind == KindLocal && blk.Type == "locals":
			for name := range blk.Body.Attributes {
				addIdent(name)
			}
		}
	}
}

// Renamer renames a single Target across all .tf files in Dir.
type Renamer struct {
	Dir     string
	Target  *Target
	Out     io.Writer
	Verbose bool

	files []*fileState
}

type fileState struct {
	path string
	src  []byte
	body *hclsyntax.Body
}

type edit struct {
	start, end int
	replace    []byte
}

// NewRenamer creates a new Renamer.
func NewRenamer(dir string, target *Target) *Renamer {
	return &Renamer{
		Dir:    dir,
		Target: target,
		Out:    os.Stdout,
	}
}

// Rename applies the rename to every *.tf file in Dir.
// When inPlace is true, files are rewritten on disk; otherwise the changed
// output is written to r.Out.
func (r *Renamer) Rename(inPlace bool) error {
	if err := r.load(); err != nil {
		return err
	}
	if r.Target.Kind == KindAddindex {
		for _, fs := range r.files {
			if err := r.checkNoExistingIndex(fs); err != nil {
				return err
			}
		}
	}
	for _, fs := range r.files {
		edits := r.collectEdits(fs)
		if len(edits) == 0 {
			continue
		}
		newSrc := applyEdits(fs.src, edits)
		if err := r.write(fs.path, newSrc, inPlace); err != nil {
			return err
		}
	}
	return nil
}

// checkNoExistingIndex aborts addindex if any TYPE.NAME reference already has
// an index — the user must explicitly resolve those before adding an index.
//
// Three HCL shapes are caught here:
//  1. literal index inside a ScopeTraversalExpr — `foo.bar[0]`
//  2. dynamic IndexExpr around a ScopeTraversalExpr — `foo.bar[var.i]`
//  3. SplatExpr around a ScopeTraversalExpr — `foo.bar[*]`
//
// Without (2) and (3), the inner traversal `foo.bar` looks bare to the
// matcher and would get a stray `[0]` inserted, producing nonsense like
// `foo.bar[0][var.i]`.
func (r *Renamer) checkNoExistingIndex(fs *fileState) error {
	var firstErr error
	abort := func(rng hcl.Range, label string) {
		if firstErr == nil {
			firstErr = fmt.Errorf("%s:%d,%d: %s.%s already has %s; addindex requires bare references only",
				fs.path, rng.Start.Line, rng.Start.Column, r.Target.OldType, r.Target.OldName, label)
		}
	}
	matchesTarget := func(tr hcl.Traversal) bool {
		if len(tr) < 2 {
			return false
		}
		root, ok := tr[0].(hcl.TraverseRoot)
		if !ok || root.Name != r.Target.OldType {
			return false
		}
		attr, ok := tr[1].(hcl.TraverseAttr)
		return ok && attr.Name == r.Target.OldName
	}
	hclsyntax.VisitAll(fs.body, func(node hclsyntax.Node) hcl.Diagnostics {
		if firstErr != nil {
			return nil
		}
		switch e := node.(type) {
		case *hclsyntax.ScopeTraversalExpr:
			tr := e.Traversal
			if len(tr) < 3 || !matchesTarget(tr) {
				return nil
			}
			if _, isIdx := tr[2].(hcl.TraverseIndex); isIdx {
				abort(e.Range(), "an index")
			}
		case *hclsyntax.IndexExpr:
			ste, ok := e.Collection.(*hclsyntax.ScopeTraversalExpr)
			if ok && matchesTarget(ste.Traversal) {
				abort(e.Range(), "a dynamic index")
			}
		case *hclsyntax.SplatExpr:
			ste, ok := e.Source.(*hclsyntax.ScopeTraversalExpr)
			if ok && matchesTarget(ste.Traversal) {
				abort(e.Range(), "a splat index")
			}
		}
		return nil
	})
	return firstErr
}

func (r *Renamer) load() error {
	r.files = nil
	pattern := filepath.Join(r.Dir, "*.tf")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return fmt.Errorf("glob: %w", err)
	}
	sort.Strings(matches)
	var diags hcl.Diagnostics
	for _, path := range matches {
		src, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read %s: %w", path, err)
		}
		f, parseDiags := hclsyntax.ParseConfig(src, path, hcl.Pos{Line: 1, Column: 1})
		if parseDiags.HasErrors() {
			diags = append(diags, parseDiags...)
			continue
		}
		body := f.Body.(*hclsyntax.Body)
		r.files = append(r.files, &fileState{path: path, src: src, body: body})
	}
	if diags.HasErrors() {
		return diags
	}
	return nil
}

func (r *Renamer) collectEdits(fs *fileState) []edit {
	var edits []edit
	edits = append(edits, r.collectDeclEdits(fs)...)
	edits = append(edits, r.collectRefEdits(fs)...)
	return edits
}

func (r *Renamer) collectDeclEdits(fs *fileState) []edit {
	var edits []edit
	switch r.Target.Kind {
	case KindResource, KindData:
		bt := "resource"
		if r.Target.Kind == KindData {
			bt = "data"
		}
		for _, blk := range fs.body.Blocks {
			if blk.Type != bt {
				continue
			}
			if len(blk.Labels) != 2 || blk.Labels[0] != r.Target.OldType || blk.Labels[1] != r.Target.OldName {
				continue
			}
			edits = append(edits,
				edit{
					start:   blk.LabelRanges[0].Start.Byte,
					end:     blk.LabelRanges[0].End.Byte,
					replace: rewriteLabel(fs.src, blk.LabelRanges[0], r.Target.NewType),
				},
				edit{
					start:   blk.LabelRanges[1].Start.Byte,
					end:     blk.LabelRanges[1].End.Byte,
					replace: rewriteLabel(fs.src, blk.LabelRanges[1], r.Target.NewName),
				},
			)
			if r.Verbose {
				log.Printf("  - %s %s.%s -> %s.%s in %s", bt, r.Target.OldType, r.Target.OldName, r.Target.NewType, r.Target.NewName, fs.path)
			}
		}
	case KindModule, KindVariable, KindOutput:
		bt := string(r.Target.Kind)
		for _, blk := range fs.body.Blocks {
			if blk.Type != bt {
				continue
			}
			if len(blk.Labels) != 1 || blk.Labels[0] != r.Target.OldName {
				continue
			}
			edits = append(edits, edit{
				start:   blk.LabelRanges[0].Start.Byte,
				end:     blk.LabelRanges[0].End.Byte,
				replace: rewriteLabel(fs.src, blk.LabelRanges[0], r.Target.NewName),
			})
			if r.Verbose {
				log.Printf("  - %s %s -> %s in %s", bt, r.Target.OldName, r.Target.NewName, fs.path)
			}
		}
	case KindLocal:
		for _, blk := range fs.body.Blocks {
			if blk.Type != "locals" {
				continue
			}
			attr, ok := blk.Body.Attributes[r.Target.OldName]
			if !ok {
				continue
			}
			edits = append(edits, edit{
				start:   attr.NameRange.Start.Byte,
				end:     attr.NameRange.End.Byte,
				replace: []byte(r.Target.NewName),
			})
			if r.Verbose {
				log.Printf("  - local %s -> %s in %s", r.Target.OldName, r.Target.NewName, fs.path)
			}
		}
	}
	return edits
}

// rewriteLabel preserves the surrounding quote style of the original label.
func rewriteLabel(src []byte, rng hcl.Range, name string) []byte {
	if rng.Start.Byte < len(src) && src[rng.Start.Byte] == '"' {
		return []byte(`"` + name + `"`)
	}
	return []byte(name)
}

func (r *Renamer) collectRefEdits(fs *fileState) []edit {
	if r.Target.Kind == KindOutput {
		return nil
	}
	var edits []edit
	diags := hclsyntax.VisitAll(fs.body, func(node hclsyntax.Node) hcl.Diagnostics {
		ste, ok := node.(*hclsyntax.ScopeTraversalExpr)
		if !ok {
			return nil
		}
		edits = append(edits, r.matchTraversal(ste.Traversal, fs)...)
		return nil
	})
	// VisitAll never produces diagnostics from our nil-returning visitor,
	// but keep the call result honest for the lint/race.
	_ = diags
	return edits
}

func (r *Renamer) matchTraversal(tr hcl.Traversal, fs *fileState) []edit {
	if len(tr) == 0 {
		return nil
	}
	root, ok := tr[0].(hcl.TraverseRoot)
	if !ok {
		return nil
	}
	switch r.Target.Kind {
	case KindVariable:
		return matchSimpleRef(tr, root, "var", r.Target.OldName, r.Target.NewName, fs, r.Verbose, "var")
	case KindLocal:
		return matchSimpleRef(tr, root, "local", r.Target.OldName, r.Target.NewName, fs, r.Verbose, "local")
	case KindModule:
		return matchSimpleRef(tr, root, "module", r.Target.OldName, r.Target.NewName, fs, r.Verbose, "module")
	case KindResource:
		return matchResourceRef(tr, root, r.Target, fs, r.Verbose)
	case KindData:
		return matchDataRef(tr, root, r.Target, fs, r.Verbose)
	case KindUnindex:
		return matchUnindexRef(tr, root, r.Target, fs, r.Verbose)
	case KindAddindex:
		return matchAddindexRef(tr, root, r.Target, fs, r.Verbose)
	}
	return nil
}

// matchAddindexRef matches bare `<type>.<name>...` references (without an
// index at position 2) and emits an edit that inserts `[<key>]` right after
// the name. References that already have an index are left untouched here;
// checkNoExistingIndex catches them before any edits are applied.
func matchAddindexRef(tr hcl.Traversal, root hcl.TraverseRoot, t *Target, fs *fileState, verbose bool) []edit {
	if root.Name != t.OldType || len(tr) < 2 {
		return nil
	}
	attr, ok := tr[1].(hcl.TraverseAttr)
	if !ok || attr.Name != t.OldName {
		return nil
	}
	if len(tr) >= 3 {
		if _, isIdx := tr[2].(hcl.TraverseIndex); isIdx {
			return nil
		}
	}
	if verbose {
		log.Printf("  - ref %s.%s -> %s.%s%s in %s", t.OldType, t.OldName, t.OldType, t.OldName, t.Key.Format(), fs.path)
	}
	return []edit{{
		start:   attr.SrcRange.End.Byte,
		end:     attr.SrcRange.End.Byte,
		replace: []byte(t.Key.Format()),
	}}
}

// matchUnindexRef matches `<type>.<name>[<key>]...` references and emits an
// edit that removes the `[<key>]` part, leaving `<type>.<name>...`.
func matchUnindexRef(tr hcl.Traversal, root hcl.TraverseRoot, t *Target, fs *fileState, verbose bool) []edit {
	if root.Name != t.OldType || len(tr) < 3 {
		return nil
	}
	attr, ok := tr[1].(hcl.TraverseAttr)
	if !ok || attr.Name != t.OldName {
		return nil
	}
	idx, ok := tr[2].(hcl.TraverseIndex)
	if !ok || !t.Key.matches(idx) {
		return nil
	}
	if verbose {
		log.Printf("  - ref %s.%s%s -> %s.%s in %s", t.OldType, t.OldName, t.Key.Format(), t.OldType, t.OldName, fs.path)
	}
	return []edit{{
		start: idx.SrcRange.Start.Byte,
		end:   idx.SrcRange.End.Byte,
	}}
}

// matchSimpleRef matches `<prefix>.<oldName>...` where prefix is fixed.
func matchSimpleRef(tr hcl.Traversal, root hcl.TraverseRoot, prefix, oldName, newName string, fs *fileState, verbose bool, kindLabel string) []edit {
	if root.Name != prefix || len(tr) < 2 {
		return nil
	}
	attr, ok := tr[1].(hcl.TraverseAttr)
	if !ok || attr.Name != oldName {
		return nil
	}
	if verbose {
		log.Printf("  - ref %s.%s -> %s.%s in %s", kindLabel, oldName, kindLabel, newName, fs.path)
	}
	return []edit{{
		start:   attr.SrcRange.Start.Byte,
		end:     attr.SrcRange.End.Byte,
		replace: []byte("." + newName),
	}}
}

// matchResourceRef matches `<oldType>.<oldName>...` references.
func matchResourceRef(tr hcl.Traversal, root hcl.TraverseRoot, t *Target, fs *fileState, verbose bool) []edit {
	if root.Name != t.OldType || len(tr) < 2 {
		return nil
	}
	attr, ok := tr[1].(hcl.TraverseAttr)
	if !ok || attr.Name != t.OldName {
		return nil
	}
	if verbose {
		log.Printf("  - ref %s.%s -> %s.%s in %s", t.OldType, t.OldName, t.NewType, t.NewName, fs.path)
	}
	return []edit{
		{
			start:   root.SrcRange.Start.Byte,
			end:     root.SrcRange.End.Byte,
			replace: []byte(t.NewType),
		},
		{
			start:   attr.SrcRange.Start.Byte,
			end:     attr.SrcRange.End.Byte,
			replace: []byte("." + t.NewName),
		},
	}
}

// matchDataRef matches `data.<oldType>.<oldName>...` references.
func matchDataRef(tr hcl.Traversal, root hcl.TraverseRoot, t *Target, fs *fileState, verbose bool) []edit {
	if root.Name != "data" || len(tr) < 3 {
		return nil
	}
	typeAttr, ok1 := tr[1].(hcl.TraverseAttr)
	nameAttr, ok2 := tr[2].(hcl.TraverseAttr)
	if !ok1 || !ok2 || typeAttr.Name != t.OldType || nameAttr.Name != t.OldName {
		return nil
	}
	if verbose {
		log.Printf("  - ref data.%s.%s -> data.%s.%s in %s", t.OldType, t.OldName, t.NewType, t.NewName, fs.path)
	}
	return []edit{
		{
			start:   typeAttr.SrcRange.Start.Byte,
			end:     typeAttr.SrcRange.End.Byte,
			replace: []byte("." + t.NewType),
		},
		{
			start:   nameAttr.SrcRange.Start.Byte,
			end:     nameAttr.SrcRange.End.Byte,
			replace: []byte("." + t.NewName),
		},
	}
}

func applyEdits(src []byte, edits []edit) []byte {
	sort.Slice(edits, func(i, j int) bool {
		if edits[i].start != edits[j].start {
			return edits[i].start < edits[j].start
		}
		return edits[i].end < edits[j].end
	})
	var out bytes.Buffer
	pos := 0
	for _, e := range edits {
		if e.start < pos {
			continue
		}
		out.Write(src[pos:e.start])
		out.Write(e.replace)
		pos = e.end
	}
	out.Write(src[pos:])
	return out.Bytes()
}

func (r *Renamer) write(path string, content []byte, inPlace bool) error {
	if !inPlace {
		suffix := ""
		if len(content) > 0 && content[len(content)-1] != '\n' {
			suffix = "\n"
		}
		if _, err := fmt.Fprintf(r.Out, "### %s ###\n%s%s", path, content, suffix); err != nil {
			return err
		}
		return nil
	}
	if err := os.WriteFile(path, content, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	if r.Verbose {
		log.Printf("rewrote %s", path)
	}
	return nil
}
