# tfrename

[![CI](https://github.com/winebarrel/tfrename/actions/workflows/ci.yml/badge.svg)](https://github.com/winebarrel/tfrename/actions/workflows/ci.yml)
[![codecov](https://codecov.io/gh/winebarrel/tfrename/branch/main/graph/badge.svg)](https://codecov.io/gh/winebarrel/tfrename)
[![AI Generated](https://img.shields.io/badge/AI%20Generated-Claude-orange?logo=anthropic)](https://claude.ai/claude-code)

`tfrename` renames Terraform symbols — resources, data sources, modules, variables, outputs, and locals — across every `*.tf` file in a directory. Both the declaration and every reference site are rewritten.

It works at the byte level over an `hclsyntax`-parsed file, so whitespace, comments, and formatting are preserved exactly as-is.

## Installation

```sh
brew install winebarrel/tfrename/tfrename
```

### Shell completions

Append the output to your shell rc file (bash / zsh):

```sh
tfrename install-completions >> ~/.zshrc
```

In addition to the subcommand and flag names, the first positional argument
of each rename subcommand completes from the symbols actually defined in the
target directory's `*.tf` files:

```
$ tfrename variable <TAB>
env  region
$ tfrename resource <TAB>
aws_eip.addr  aws_instance.web
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
  resource <old> <new> [flags]
    Rename a resource (TYPE.NAME form).

  data <old> <new> [flags]
    Rename a data source (TYPE.NAME form).

  module <old> <new> [flags]
    Rename a module.

  variable <old> <new> [flags]
    Rename a variable.

  output <old> <new> [flags]
    Rename an output.

  local <old> <new> [flags]
    Rename a local.

  unindex <ref> [flags]
    Strip [KEY] from references — use after deleting count/for_each.

  addindex <ref> [flags]
    Insert [KEY] into bare references — use after adding count/for_each.

  install-completions [flags]
    Install shell completions.

Run "tfrename <command> --help" for more information on a command.
```

By default the result is printed to stdout. Pass `-i` / `--in-place` to rewrite the files on disk. The `resource` and `module` subcommands accept `--moved`, which inserts a `moved` block above the renamed declaration so Terraform recognizes the state move rather than destroying and recreating.

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

### Emit a `moved` block (resource / module only)

Pass `--moved` to `resource` or `module` to drop a `moved` block above the
renamed declaration. Without it, Terraform will plan to destroy the old
address and create the new one; with it, the rename becomes a state move.

```sh
tfrename resource aws_instance.foo aws_db_instance.bar --moved -i
```

```hcl
# main.tf (rewritten)
moved {
  from = aws_instance.foo
  to   = aws_db_instance.bar
}

resource "aws_db_instance" "bar" {
  ami = "ami-123"
}
```

For `module`, a single block at the parent level covers every resource inside
the module — Terraform automatically rewrites addresses like
`module.old.aws_instance.x` to `module.new.aws_instance.x`. You don't need a
per-resource `moved` block.

```sh
tfrename module vpc network --moved -i
```

```hcl
# main.tf (rewritten)
moved {
  from = module.vpc
  to   = module.network
}

module "network" {
  source = "./modules/vpc"
}
```

### Strip an index after deleting `count` / `for_each`

When you remove `count` or `for_each` from a resource, references that used the
index (`foo.bar[0]`, `zoo.baz["hoge"]`) no longer resolve and must be flattened
to the bare form. `unindex` rewrites only those references; the declaration
block is left alone (you delete the `count` / `for_each` line yourself).

```hcl
# main.tf
resource "aws_instance" "foo" {
  count = 1
  ami   = "ami-123"
}

output "ip" { value = aws_instance.foo[0].public_ip }
```

```sh
tfrename unindex 'aws_instance.foo[0]' -i
# then manually delete `count = 1`
```

```hcl
# main.tf (rewritten)
resource "aws_instance" "foo" {
  ami = "ami-123"
}

output "ip" { value = aws_instance.foo.public_ip }
```

String keys (from `for_each`) work the same way — quote the whole argument so
the shell doesn't eat the brackets:

```sh
tfrename unindex 'zoo_thing.baz["hoge"]' -i
```

### Add an index after adding `count` / `for_each`

The inverse of `unindex`. When you add `count` or `for_each` to a previously
single resource, bare `foo.bar` references must gain the index. `addindex`
takes the target indexed form and inserts it everywhere the bare reference
appears. The declaration block is left alone — you add the `count` /
`for_each` line yourself.

```hcl
# main.tf
resource "aws_instance" "foo" {
  ami = "ami-123"
}

output "ip" { value = aws_instance.foo.public_ip }
```

```sh
# after manually adding `count = 1` to the resource block:
tfrename addindex 'aws_instance.foo[0]' -i
```

```hcl
# main.tf (rewritten)
resource "aws_instance" "foo" {
  count = 1
  ami   = "ami-123"
}

output "ip" { value = aws_instance.foo[0].public_ip }
```

If any reference already has an index (e.g. a mix of `foo.bar` and
`foo.bar[0]`), `addindex` aborts without touching any file — fix those
references first.

### Target a different directory

```sh
tfrename variable env environment -C ./infra -i
```

## Notes

- Comments and formatting are preserved (byte-level edits via `hclsyntax` ranges).
- Multi-file projects work — `*.tf` files are scanned together.
- References buried in interpolations (`"web-${var.region}"`) are rewritten.
- Parse errors are reported; nothing is written if any file fails to parse.
- The command exits non-zero if the target directory contains no `*.tf` files (`no *.tf files found in "<dir>"`) or if nothing in those files matches the target (`no matches found for <target> in "<dir>"`) — silent no-ops are almost always typos.
- `output` renames only the declaration. Outputs aren't referenced within the same module.

## Related Links

- [vscode-tfrename](https://github.com/winebarrel/vscode-tfrename)
