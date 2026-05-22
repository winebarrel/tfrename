package main

import (
	"log"
	"os"

	"github.com/alecthomas/kong"
	"github.com/posener/complete"
	"github.com/willabides/kongplete"
	"github.com/winebarrel/tfrename"
)

var version string

func init() {
	log.SetFlags(0)
}

type commonFlags struct {
	Dir     string `short:"C" name:"dir" default:"." predictor:"dir" help:"Directory containing *.tf files (default: \".\")."`
	InPlace bool   `short:"i" help:"Write changes back to files instead of stdout."`
	Verbose bool   `short:"v" help:"Verbose logging."`
}

type pairArgs struct {
	Old string `arg:"" help:"Old name."`
	New string `arg:"" help:"New name."`
}

type qualifiedArgs struct {
	Old string `arg:"" help:"Old name in TYPE.NAME form."`
	New string `arg:"" help:"New name in TYPE.NAME form."`
}

type resourceCmd struct {
	qualifiedArgs
	commonFlags
}

func (c *resourceCmd) Run() error {
	return runRename(tfrename.KindResource, c.Old, c.New, c.Dir, c.InPlace, c.Verbose)
}

type dataCmd struct {
	qualifiedArgs
	commonFlags
}

func (c *dataCmd) Run() error {
	return runRename(tfrename.KindData, c.Old, c.New, c.Dir, c.InPlace, c.Verbose)
}

type moduleCmd struct {
	pairArgs
	commonFlags
}

func (c *moduleCmd) Run() error {
	return runRename(tfrename.KindModule, c.Old, c.New, c.Dir, c.InPlace, c.Verbose)
}

type variableCmd struct {
	pairArgs
	commonFlags
}

func (c *variableCmd) Run() error {
	return runRename(tfrename.KindVariable, c.Old, c.New, c.Dir, c.InPlace, c.Verbose)
}

type outputCmd struct {
	pairArgs
	commonFlags
}

func (c *outputCmd) Run() error {
	return runRename(tfrename.KindOutput, c.Old, c.New, c.Dir, c.InPlace, c.Verbose)
}

type localCmd struct {
	pairArgs
	commonFlags
}

func (c *localCmd) Run() error {
	return runRename(tfrename.KindLocal, c.Old, c.New, c.Dir, c.InPlace, c.Verbose)
}

type cli struct {
	Resource           resourceCmd                  `cmd:"" help:"Rename a resource (TYPE.NAME form)."`
	Data               dataCmd                      `cmd:"" help:"Rename a data source (TYPE.NAME form)."`
	Module             moduleCmd                    `cmd:"" help:"Rename a module."`
	Variable           variableCmd                  `cmd:"" help:"Rename a variable."`
	Output             outputCmd                    `cmd:"" help:"Rename an output."`
	Local              localCmd                     `cmd:"" help:"Rename a local."`
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
	)

	ctx, err := parser.Parse(os.Args[1:])
	if err != nil {
		parser.FatalIfErrorf(err)
	}
	if err := ctx.Run(); err != nil {
		log.Fatalf("error: %v", err)
	}
}
