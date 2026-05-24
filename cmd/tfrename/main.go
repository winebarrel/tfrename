package main

import (
	"log"
	"os"
	"strings"

	"github.com/alecthomas/kong"
	"github.com/posener/complete"
	"github.com/willabides/kongplete"
	"github.com/winebarrel/tfrename"
)

// oldNamePredictor completes the first positional argument of a rename
// subcommand with the names of the given kind defined in the target
// directory's *.tf files.
type oldNamePredictor struct {
	kind tfrename.Kind
}

func (p oldNamePredictor) Predict(args complete.Args) []string {
	return tfrename.ListSymbols(dirFromCompletedArgs(args.Completed), p.kind)
}

func dirFromCompletedArgs(completed []string) string {
	dir := "."
	for i := 0; i < len(completed); i++ {
		a := completed[i]
		switch {
		case a == "-C" || a == "--dir":
			if i+1 < len(completed) {
				dir = completed[i+1]
				i++ // consume the value so it isn't re-scanned as a flag
			}
		case strings.HasPrefix(a, "-C="):
			dir = strings.TrimPrefix(a, "-C=")
		case strings.HasPrefix(a, "--dir="):
			dir = strings.TrimPrefix(a, "--dir=")
		}
	}
	return dir
}

var version string

func init() {
	log.SetFlags(0)
}

type commonFlags struct {
	Dir     string `short:"C" name:"dir" default:"." predictor:"dir" help:"Directory containing *.tf files (default: \".\")."`
	InPlace bool   `short:"i" help:"Write changes back to files instead of stdout."`
	Verbose bool   `short:"v" help:"Verbose logging."`
}

type resourceCmd struct {
	Old string `arg:"" predictor:"resource-name" help:"Old name in TYPE.NAME form."`
	New string `arg:"" help:"New name in TYPE.NAME form."`
	commonFlags
}

func (c *resourceCmd) Run() error {
	return runRename(tfrename.KindResource, c.Old, c.New, c.Dir, c.InPlace, c.Verbose)
}

type dataCmd struct {
	Old string `arg:"" predictor:"data-name" help:"Old name in TYPE.NAME form."`
	New string `arg:"" help:"New name in TYPE.NAME form."`
	commonFlags
}

func (c *dataCmd) Run() error {
	return runRename(tfrename.KindData, c.Old, c.New, c.Dir, c.InPlace, c.Verbose)
}

type moduleCmd struct {
	Old string `arg:"" predictor:"module-name" help:"Old name."`
	New string `arg:"" help:"New name."`
	commonFlags
}

func (c *moduleCmd) Run() error {
	return runRename(tfrename.KindModule, c.Old, c.New, c.Dir, c.InPlace, c.Verbose)
}

type variableCmd struct {
	Old string `arg:"" predictor:"variable-name" help:"Old name."`
	New string `arg:"" help:"New name."`
	commonFlags
}

func (c *variableCmd) Run() error {
	return runRename(tfrename.KindVariable, c.Old, c.New, c.Dir, c.InPlace, c.Verbose)
}

type outputCmd struct {
	Old string `arg:"" predictor:"output-name" help:"Old name."`
	New string `arg:"" help:"New name."`
	commonFlags
}

func (c *outputCmd) Run() error {
	return runRename(tfrename.KindOutput, c.Old, c.New, c.Dir, c.InPlace, c.Verbose)
}

type localCmd struct {
	Old string `arg:"" predictor:"local-name" help:"Old name."`
	New string `arg:"" help:"New name."`
	commonFlags
}

func (c *localCmd) Run() error {
	return runRename(tfrename.KindLocal, c.Old, c.New, c.Dir, c.InPlace, c.Verbose)
}

type unindexCmd struct {
	Ref string `arg:"" help:"Indexed reference in TYPE.NAME[KEY] form, e.g. 'foo.bar[0]' or 'zoo.baz[\"hoge\"]'."`
	commonFlags
}

func (c *unindexCmd) Run() error {
	target, err := tfrename.ParseUnindexTarget(c.Ref)
	if err != nil {
		return err
	}
	r := tfrename.NewRenamer(c.Dir, target)
	r.Verbose = c.Verbose
	return r.Rename(c.InPlace)
}

type addindexCmd struct {
	Ref string `arg:"" help:"Target indexed reference in TYPE.NAME[KEY] form, e.g. 'foo.bar[0]' or 'zoo.baz[\"hoge\"]'."`
	commonFlags
}

func (c *addindexCmd) Run() error {
	target, err := tfrename.ParseAddindexTarget(c.Ref)
	if err != nil {
		return err
	}
	r := tfrename.NewRenamer(c.Dir, target)
	r.Verbose = c.Verbose
	return r.Rename(c.InPlace)
}

type cli struct {
	Resource           resourceCmd                  `cmd:"" help:"Rename a resource (TYPE.NAME form)."`
	Data               dataCmd                      `cmd:"" help:"Rename a data source (TYPE.NAME form)."`
	Module             moduleCmd                    `cmd:"" help:"Rename a module."`
	Variable           variableCmd                  `cmd:"" help:"Rename a variable."`
	Output             outputCmd                    `cmd:"" help:"Rename an output."`
	Local              localCmd                     `cmd:"" help:"Rename a local."`
	Unindex            unindexCmd                   `cmd:"" help:"Strip [KEY] from references — use after deleting count/for_each."`
	Addindex           addindexCmd                  `cmd:"" help:"Insert [KEY] into bare references — use after adding count/for_each."`
	InstallCompletions kongplete.InstallCompletions `cmd:"" help:"Install shell completions."`
	Version            kong.VersionFlag             `help:"Show version."`
}

func runRename(kind tfrename.Kind, old, newName, dir string, inPlace, verbose bool) error {
	target, err := tfrename.ParseTarget(kind, old, newName)
	if err != nil {
		return err
	}
	r := tfrename.NewRenamer(dir, target)
	r.Verbose = verbose
	return r.Rename(inPlace)
}

func main() {
	c := &cli{}
	parser := kong.Must(c,
		kong.Name("tfrename"),
		kong.Description("Rename Terraform resources, data sources, modules, variables, outputs, and locals across .tf files."),
		kong.Vars{"version": version},
	)
	parser.Model.HelpFlag.Help = "Show help."

	kongplete.Complete(parser,
		kongplete.WithPredictor("dir", complete.PredictDirs("*")),
		kongplete.WithPredictor("resource-name", oldNamePredictor{tfrename.KindResource}),
		kongplete.WithPredictor("data-name", oldNamePredictor{tfrename.KindData}),
		kongplete.WithPredictor("module-name", oldNamePredictor{tfrename.KindModule}),
		kongplete.WithPredictor("variable-name", oldNamePredictor{tfrename.KindVariable}),
		kongplete.WithPredictor("output-name", oldNamePredictor{tfrename.KindOutput}),
		kongplete.WithPredictor("local-name", oldNamePredictor{tfrename.KindLocal}),
	)

	ctx, err := parser.Parse(os.Args[1:])
	if err != nil {
		parser.FatalIfErrorf(err)
	}
	if err := ctx.Run(); err != nil {
		log.Fatalf("error: %v", err)
	}
}
