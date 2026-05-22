# tfrename

[![CI](https://github.com/winebarrel/tfrename/actions/workflows/ci.yml/badge.svg)](https://github.com/winebarrel/tfrename/actions/workflows/ci.yml)
[![codecov](https://codecov.io/gh/winebarrel/tfrename/branch/main/graph/badge.svg)](https://codecov.io/gh/winebarrel/tfrename)
[![AI Generated](https://img.shields.io/badge/AI%20Generated-Claude-orange?logo=anthropic)](https://claude.ai/claude-code)

`tfrename` renames Terraform symbols — resources, data sources, modules, variables, outputs, and locals — across every `*.tf` file in a directory. Both the declaration and every reference site are rewritten.

It works at the byte level over an `hclsyntax`-parsed file, so whitespace, comments, and formatting are preserved exactly as-is.

## Installation

```
go install github.com/winebarrel/tfrename/cmd/tfrename@latest
```

### Shell completions

Append the output to your shell rc file (bash / zsh):

```sh
tfrename install-completions >> ~/.zshrc
```

## Usage

```
Usage: tfrename <command> [flags]

Rename Terraform resources, data sources, modules, variables, outputs, and
locals across .tf files.

Flags:
  -h, --help       Show help.
      --version    Show version.

Commands:
  resource <old> <new> [flags]    # old/new in TYPE.NAME form
  data     <old> <new> [flags]    # old/new in TYPE.NAME form
  module   <old> <new> [flags]
  variable <old> <new> [flags]
  output   <old> <new> [flags]
  local    <old> <new> [flags]

Per-command flags:
  -C, --dir="."     Directory containing *.tf files (default: ".").
  -i, --in-place    Write changes back to files instead of stdout.
  -v, --verbose     Verbose logging.
```

By default the result is printed to stdout. Pass `-i` / `--in-place` to rewrite the files on disk.

## Examples

### Rename a resource (and every reference to it)

```hcl
# main.tf
resource "aws_instance" "foo" {
  ami = "ami-123"
}

resource "aws_eip" "addr" {
  instance = aws_instance.foo.id
}
```

```sh
tfrename resource aws_instance.foo aws_instance.bar -i
```

```hcl
# main.tf (rewritten)
resource "aws_instance" "bar" {
  ami = "ami-123"
}

resource "aws_eip" "addr" {
  instance = aws_instance.bar.id
}
```

### Rename a data source

```sh
tfrename data aws_ami.ubuntu aws_ami.debian -i
```

Rewrites `data "aws_ami" "ubuntu"` → `data "aws_ami" "debian"` and every `data.aws_ami.ubuntu.*` reference.

### Rename a module / variable / output / local

```sh
tfrename module   vpc    network    -i
tfrename variable region aws_region -i
tfrename output   instance_id id    -i
tfrename local    region aws_region -i
```

For module, variable, and local, references (`module.X.…`, `var.X`, `local.X`) are rewritten as well. `output` renames the block label only; outputs aren't referenced from within the same module.

### Also change the resource type

The `TYPE.NAME` form lets you change the type at the same time:

```sh
tfrename resource aws_instance.foo aws_db_instance.bar -i
```

### Target a different directory

```sh
tfrename variable env environment -C ./infra -i
```

## Notes

- Comments and formatting are preserved (byte-level edits via `hclsyntax` ranges).
- Multi-file projects work — `*.tf` files are scanned together.
- References buried in interpolations (`"web-${var.region}"`) are rewritten.
- Parse errors are reported; nothing is written if any file fails to parse.
- `output` renames only the declaration. Outputs aren't referenced within the same module.
