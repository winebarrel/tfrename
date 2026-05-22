package tfrename

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
)

// Kind is the kind of Terraform symbol being renamed.
type Kind string

const (
	KindResource Kind = "resource"
	KindData     Kind = "data"
	KindModule   Kind = "module"
	KindVariable Kind = "variable"
	KindOutput   Kind = "output"
	KindLocal    Kind = "local"
)

// Target describes what to rename.
type Target struct {
	Kind    Kind
	OldType string // resource/data only
	OldName string
	NewType string // resource/data only
	NewName string
}

// ParseTarget validates and parses raw old/new strings into a Target.
//
// For resource/data, old/new must be in TYPE.NAME format.
// For module/variable/output/local, old/new must be a simple identifier.
func ParseTarget(kind Kind, oldStr, newStr string) (*Target, error) {
	t := &Target{Kind: kind}
	switch kind {
	case KindResource, KindData:
		op := strings.SplitN(oldStr, ".", 2)
		np := strings.SplitN(newStr, ".", 2)
		if len(op) != 2 || op[0] == "" || op[1] == "" {
			return nil, fmt.Errorf("%s old name must be in TYPE.NAME format: %q", kind, oldStr)
		}
		if len(np) != 2 || np[0] == "" || np[1] == "" {
			return nil, fmt.Errorf("%s new name must be in TYPE.NAME format: %q", kind, newStr)
		}
		t.OldType, t.OldName = op[0], op[1]
		t.NewType, t.NewName = np[0], np[1]
	case KindModule, KindVariable, KindOutput, KindLocal:
		if oldStr == "" || strings.ContainsAny(oldStr, ". \t\n") {
			return nil, fmt.Errorf("%s old name must be a simple identifier: %q", kind, oldStr)
		}
		if newStr == "" || strings.ContainsAny(newStr, ". \t\n") {
			return nil, fmt.Errorf("%s new name must be a simple identifier: %q", kind, newStr)
		}
		t.OldName = oldStr
		t.NewName = newStr
	default:
		return nil, fmt.Errorf("unknown kind %q", kind)
	}
	return t, nil
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

func (r *Renamer) load() error {
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
		body, ok := f.Body.(*hclsyntax.Body)
		if !ok {
			return fmt.Errorf("unexpected body type for %s", path)
		}
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
	}
	return nil
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
		if _, err := fmt.Fprintf(r.Out, "=== %s ===\n%s", path, content); err != nil {
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
