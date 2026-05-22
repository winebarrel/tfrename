package tfrename

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/hashicorp/hcl/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type goldenCase struct {
	name string
	kind Kind
	old  string
	new  string
}

func TestRename_Golden(t *testing.T) {
	cases := []goldenCase{
		{"resource", KindResource, "aws_instance.foo", "aws_instance.bar"},
		{"data", KindData, "aws_ami.ubuntu", "aws_ami.debian"},
		{"module", KindModule, "vpc", "network"},
		{"variable", KindVariable, "region", "aws_region"},
		{"output", KindOutput, "instance_id", "id"},
		{"local", KindLocal, "region", "aws_region"},
		{"multi-file", KindVariable, "env", "environment"},
		{"preserve-comments", KindVariable, "region", "aws_region"},
		{"no-match", KindVariable, "no_such_var", "renamed"},
		{"type-rename", KindResource, "aws_instance.foo", "aws_db_instance.bar"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tmp := copyInputToTemp(t, filepath.Join("testdata", c.name, "input"))
			target, err := ParseTarget(c.kind, c.old, c.new)
			require.NoError(t, err)
			r := NewRenamer(tmp, target)
			r.Verbose = true
			require.NoError(t, r.Rename(true))
			compareDir(t, tmp, filepath.Join("testdata", c.name, "expected"))
		})
	}
}

func TestRename_StdoutMode(t *testing.T) {
	tmp := copyInputToTemp(t, "testdata/variable/input")
	var buf bytes.Buffer
	target, err := ParseTarget(KindVariable, "region", "aws_region")
	require.NoError(t, err)
	r := NewRenamer(tmp, target)
	r.Out = &buf
	require.NoError(t, r.Rename(false))
	// changed content should be on stdout
	assert.Contains(t, buf.String(), `variable "aws_region"`)
	assert.Contains(t, buf.String(), `var.aws_region`)
	// the original file should be untouched
	got, err := os.ReadFile(filepath.Join(tmp, "main.tf"))
	require.NoError(t, err)
	want, err := os.ReadFile("testdata/variable/input/main.tf")
	require.NoError(t, err)
	assert.Equal(t, string(want), string(got), "file must not be modified in stdout mode")
}

func TestRename_StdoutWriteError(t *testing.T) {
	tmp := copyInputToTemp(t, "testdata/variable/input")
	target, err := ParseTarget(KindVariable, "region", "aws_region")
	require.NoError(t, err)
	r := NewRenamer(tmp, target)
	r.Out = failingWriter{}
	err = r.Rename(false)
	require.Error(t, err)
}

func TestRename_NoMatchStdoutIsSilent(t *testing.T) {
	tmp := copyInputToTemp(t, "testdata/no-match/input")
	var buf bytes.Buffer
	target, err := ParseTarget(KindVariable, "no_such_var", "renamed")
	require.NoError(t, err)
	r := NewRenamer(tmp, target)
	r.Out = &buf
	require.NoError(t, r.Rename(false))
	assert.Empty(t, buf.String(), "stdout should be empty when no edits applied")
}

func TestRename_ReuseDoesNotAccumulate(t *testing.T) {
	tmp := copyInputToTemp(t, "testdata/variable/input")
	target, err := ParseTarget(KindVariable, "region", "aws_region")
	require.NoError(t, err)
	r := NewRenamer(tmp, target)
	var buf bytes.Buffer
	r.Out = &buf
	require.NoError(t, r.Rename(false))
	first := buf.Len()
	buf.Reset()
	require.NoError(t, r.Rename(false))
	assert.Equal(t, first, buf.Len(), "second Rename call must not duplicate output")
}

func TestRename_ParseError(t *testing.T) {
	tmp := copyInputToTemp(t, "testdata/parse-error/input")
	target, err := ParseTarget(KindVariable, "x", "y")
	require.NoError(t, err)
	r := NewRenamer(tmp, target)
	err = r.Rename(true)
	require.Error(t, err)
	var diags hcl.Diagnostics
	require.ErrorAs(t, err, &diags, "expected hcl.Diagnostics, got %T", err)
	assert.True(t, diags.HasErrors())
}

func TestRename_GlobError(t *testing.T) {
	target, err := ParseTarget(KindVariable, "x", "y")
	require.NoError(t, err)
	r := NewRenamer("[invalid", target)
	err = r.Rename(true)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "glob")
}

func TestRename_ReadError(t *testing.T) {
	tmp := t.TempDir()
	// Directory named "trap.tf" makes os.ReadFile fail.
	require.NoError(t, os.Mkdir(filepath.Join(tmp, "trap.tf"), 0o755))
	target, err := ParseTarget(KindVariable, "x", "y")
	require.NoError(t, err)
	r := NewRenamer(tmp, target)
	err = r.Rename(true)
	require.Error(t, err)
}

func TestRename_NoFiles(t *testing.T) {
	tmp := t.TempDir()
	target, err := ParseTarget(KindVariable, "x", "y")
	require.NoError(t, err)
	r := NewRenamer(tmp, target)
	require.NoError(t, r.Rename(true))
}

// ----------------- ParseTarget -----------------

func TestParseTarget_Resource(t *testing.T) {
	target, err := ParseTarget(KindResource, "aws_instance.foo", "aws_instance.bar")
	require.NoError(t, err)
	assert.Equal(t, "aws_instance", target.OldType)
	assert.Equal(t, "foo", target.OldName)
	assert.Equal(t, "aws_instance", target.NewType)
	assert.Equal(t, "bar", target.NewName)
}

func TestParseTarget_ResourceInvalid(t *testing.T) {
	for _, c := range []struct {
		old, new string
	}{
		{"foo", "aws_instance.bar"},
		{"aws_instance.foo", "bar"},
		{"", "aws_instance.bar"},
		{".foo", "aws_instance.bar"},
		{"aws_instance.", "aws_instance.bar"},
		{"aws_instance.foo.bar", "aws_instance.bar"},  // too many dots
		{"aws_instance.foo", "aws_instance.bar.baz"},  // too many dots
		{"aws instance.foo", "aws_instance.bar"},      // whitespace in type
		{"aws_instance.foo bar", "aws_instance.bar"},  // whitespace in name
		{"aws_instance.123foo", "aws_instance.bar"},   // digit-leading name
		{"aws_instance.foo", "aws_instance.foo$bar"},  // non-identifier char
		{"-aws_instance.foo", "aws_instance.bar"},     // hyphen-leading type
	} {
		_, err := ParseTarget(KindResource, c.old, c.new)
		require.Errorf(t, err, "old=%q new=%q", c.old, c.new)
	}
}

func TestParseTarget_Simple(t *testing.T) {
	target, err := ParseTarget(KindVariable, "foo", "bar")
	require.NoError(t, err)
	assert.Equal(t, "foo", target.OldName)
	assert.Equal(t, "bar", target.NewName)
}

func TestParseTarget_SimpleInvalid(t *testing.T) {
	for _, c := range []struct {
		kind     Kind
		old, new string
	}{
		{KindVariable, "foo.bar", "baz"},
		{KindLocal, "", "x"},
		{KindModule, "x", ""},
		{KindOutput, "x", "y z"},
		{KindVariable, "123foo", "bar"},   // digit-leading
		{KindLocal, "foo", "bar$baz"},     // non-identifier char
		{KindModule, "-leading", "bar"},   // hyphen-leading
	} {
		_, err := ParseTarget(c.kind, c.old, c.new)
		require.Errorf(t, err, "%v old=%q new=%q", c.kind, c.old, c.new)
	}
}

func TestParseTarget_SimpleAllowsHyphenInside(t *testing.T) {
	// HCL identifiers may include hyphens after the first character.
	target, err := ParseTarget(KindVariable, "foo-bar", "baz-qux")
	require.NoError(t, err)
	assert.Equal(t, "foo-bar", target.OldName)
	assert.Equal(t, "baz-qux", target.NewName)
}

func TestParseTarget_UnknownKind(t *testing.T) {
	_, err := ParseTarget(Kind("bogus"), "a", "b")
	require.Error(t, err)
}

// ----------------- rewriteLabel -----------------

func TestRewriteLabel_QuotedAndUnquoted(t *testing.T) {
	src := []byte(`"old" old`)
	quotedRng := hcl.Range{Start: hcl.Pos{Byte: 0}, End: hcl.Pos{Byte: 5}}
	unquotedRng := hcl.Range{Start: hcl.Pos{Byte: 6}, End: hcl.Pos{Byte: 9}}
	assert.Equal(t, []byte(`"new"`), rewriteLabel(src, quotedRng, "new"))
	assert.Equal(t, []byte(`new`), rewriteLabel(src, unquotedRng, "new"))
}

// ----------------- ListSymbols -----------------

func TestListSymbols(t *testing.T) {
	tmp := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "a.tf"), []byte(`
variable "region" {}
variable "env" {}

resource "aws_instance" "web" {}
resource "aws_eip" "addr" {}

data "aws_ami" "ubuntu" {}

module "vpc" {}
module "ec2" {}

output "ip" { value = "x" }

locals {
  prefix = "x"
  suffix = "y"
}
`), 0o644))
	// A second file to confirm names from multiple files merge and de-dupe.
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "b.tf"), []byte(`
variable "region" {}
variable "extra" {}
`), 0o644))

	assert.Equal(t, []string{"env", "extra", "region"}, ListSymbols(tmp, KindVariable))
	assert.Equal(t, []string{"aws_eip.addr", "aws_instance.web"}, ListSymbols(tmp, KindResource))
	assert.Equal(t, []string{"aws_ami.ubuntu"}, ListSymbols(tmp, KindData))
	assert.Equal(t, []string{"ec2", "vpc"}, ListSymbols(tmp, KindModule))
	assert.Equal(t, []string{"ip"}, ListSymbols(tmp, KindOutput))
	assert.Equal(t, []string{"prefix", "suffix"}, ListSymbols(tmp, KindLocal))
}

func TestListSymbols_GlobError(t *testing.T) {
	assert.Nil(t, ListSymbols("[invalid", KindVariable))
}

func TestListSymbols_SkipsParseErrorAndUnreadable(t *testing.T) {
	tmp := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "broken.tf"), []byte(`resource "x" "y" {`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "good.tf"), []byte(`variable "ok" {}`), 0o644))
	// A directory named "*.tf" makes os.ReadFile fail when load() iterates it.
	require.NoError(t, os.Mkdir(filepath.Join(tmp, "trap.tf"), 0o755))
	assert.Equal(t, []string{"ok"}, ListSymbols(tmp, KindVariable))
}

// ----------------- matchTraversal / matchResourceRef / matchDataRef -----------------

func TestMatchTraversal_EmptyTraversal(t *testing.T) {
	r := &Renamer{Target: &Target{Kind: KindVariable, OldName: "x", NewName: "y"}}
	assert.Nil(t, r.matchTraversal(hcl.Traversal{}, &fileState{}))
}

func TestMatchTraversal_FirstNotRoot(t *testing.T) {
	r := &Renamer{Target: &Target{Kind: KindVariable, OldName: "x", NewName: "y"}}
	tr := hcl.Traversal{hcl.TraverseAttr{Name: "x"}}
	assert.Nil(t, r.matchTraversal(tr, &fileState{}))
}

func TestMatchTraversal_OutputKindReturnsNil(t *testing.T) {
	r := &Renamer{Target: &Target{Kind: KindOutput, OldName: "x", NewName: "y"}}
	tr := hcl.Traversal{hcl.TraverseRoot{Name: "var"}, hcl.TraverseAttr{Name: "x"}}
	assert.Nil(t, r.matchTraversal(tr, &fileState{}))
}

func TestMatchResourceRef_Misses(t *testing.T) {
	target := &Target{Kind: KindResource, OldType: "aws_instance", OldName: "foo", NewType: "aws_instance", NewName: "bar"}
	fs := &fileState{path: "x.tf"}

	// root.Name mismatch
	tr := hcl.Traversal{hcl.TraverseRoot{Name: "var"}, hcl.TraverseAttr{Name: "foo"}}
	assert.Nil(t, matchResourceRef(tr, tr[0].(hcl.TraverseRoot), target, fs, false))

	// len(tr) < 2
	tr = hcl.Traversal{hcl.TraverseRoot{Name: "aws_instance"}}
	assert.Nil(t, matchResourceRef(tr, tr[0].(hcl.TraverseRoot), target, fs, false))

	// tr[1] not a TraverseAttr (e.g. TraverseIndex)
	tr = hcl.Traversal{hcl.TraverseRoot{Name: "aws_instance"}, hcl.TraverseIndex{}}
	assert.Nil(t, matchResourceRef(tr, tr[0].(hcl.TraverseRoot), target, fs, false))

	// attr.Name mismatch
	tr = hcl.Traversal{hcl.TraverseRoot{Name: "aws_instance"}, hcl.TraverseAttr{Name: "other"}}
	assert.Nil(t, matchResourceRef(tr, tr[0].(hcl.TraverseRoot), target, fs, false))
}

func TestMatchDataRef_Misses(t *testing.T) {
	target := &Target{Kind: KindData, OldType: "aws_ami", OldName: "ubuntu", NewType: "aws_ami", NewName: "debian"}
	fs := &fileState{path: "x.tf"}

	// root.Name != "data"
	tr := hcl.Traversal{hcl.TraverseRoot{Name: "var"}, hcl.TraverseAttr{Name: "aws_ami"}, hcl.TraverseAttr{Name: "ubuntu"}}
	assert.Nil(t, matchDataRef(tr, tr[0].(hcl.TraverseRoot), target, fs, false))

	// len(tr) < 3
	tr = hcl.Traversal{hcl.TraverseRoot{Name: "data"}, hcl.TraverseAttr{Name: "aws_ami"}}
	assert.Nil(t, matchDataRef(tr, tr[0].(hcl.TraverseRoot), target, fs, false))

	// tr[1] not TraverseAttr
	tr = hcl.Traversal{hcl.TraverseRoot{Name: "data"}, hcl.TraverseIndex{}, hcl.TraverseAttr{Name: "ubuntu"}}
	assert.Nil(t, matchDataRef(tr, tr[0].(hcl.TraverseRoot), target, fs, false))

	// type name mismatch
	tr = hcl.Traversal{hcl.TraverseRoot{Name: "data"}, hcl.TraverseAttr{Name: "other"}, hcl.TraverseAttr{Name: "ubuntu"}}
	assert.Nil(t, matchDataRef(tr, tr[0].(hcl.TraverseRoot), target, fs, false))
}

// ----------------- write -----------------

func TestWrite_StdoutAppendsMissingNewline(t *testing.T) {
	r := &Renamer{}
	var buf bytes.Buffer
	r.Out = &buf
	require.NoError(t, r.write("x.tf", []byte("no newline"), false))
	assert.Equal(t, "### x.tf ###\nno newline\n", buf.String())
}

// ----------------- applyEdits -----------------

func TestApplyEdits_OverlapIgnored(t *testing.T) {
	src := []byte("hello world")
	edits := []edit{
		{start: 0, end: 5, replace: []byte("HELLO")},
		{start: 2, end: 4, replace: []byte("XX")}, // overlaps with first → skipped
	}
	got := applyEdits(src, edits)
	assert.Equal(t, "HELLO world", string(got))
}

func TestApplyEdits_OrderedByStart(t *testing.T) {
	src := []byte("abcdef")
	edits := []edit{
		{start: 4, end: 5, replace: []byte("E")},
		{start: 1, end: 2, replace: []byte("B")},
	}
	got := applyEdits(src, edits)
	assert.Equal(t, "aBcdEf", string(got))
}

func TestApplyEdits_SameStartShorterFirst(t *testing.T) {
	// Two edits sharing a start byte exercise the secondary sort (by end).
	// The shorter edit is applied; the longer one becomes an overlap and is skipped.
	src := []byte("abcdef")
	edits := []edit{
		{start: 1, end: 3, replace: []byte("XX")},
		{start: 1, end: 2, replace: []byte("B")},
	}
	got := applyEdits(src, edits)
	assert.Equal(t, "aBcdef", string(got))
}

// ----------------- helpers -----------------

type failingWriter struct{}

func (failingWriter) Write(_ []byte) (int, error) { return 0, errors.New("boom") }

func copyInputToTemp(t *testing.T, srcDir string) string {
	t.Helper()
	tmp := t.TempDir()
	entries, err := os.ReadDir(srcDir)
	require.NoError(t, err)
	for _, ent := range entries {
		if ent.IsDir() {
			continue
		}
		data, err := os.ReadFile(filepath.Join(srcDir, ent.Name()))
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(filepath.Join(tmp, ent.Name()), data, 0o644))
	}
	return tmp
}

func compareDir(t *testing.T, gotDir, wantDir string) {
	t.Helper()
	entries, err := os.ReadDir(wantDir)
	require.NoError(t, err)
	for _, ent := range entries {
		if ent.IsDir() {
			continue
		}
		got, err := os.ReadFile(filepath.Join(gotDir, ent.Name()))
		require.NoError(t, err)
		want, err := os.ReadFile(filepath.Join(wantDir, ent.Name()))
		require.NoError(t, err)
		assert.Equal(t, string(want), string(got), "file %s", ent.Name())
	}
}
