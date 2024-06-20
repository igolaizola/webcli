# webcli

**webcli** is a tool to generate an automatic web UI on top of a CLI

## Features

 - List all commands and subcommands
 - Edit the flags of the commands using input fields
 - Launch commands in the background
 - See the output of the commands in real-time
 - List and view the output of all the commands launched
 - Load command flags from configuration files
 - Save command flags to configuration files

## Compatibility

The tool is directly compatible with `github.com/peterbourgon/ff/v3` and `github.com/spf13/cobra` libraries, using `github.com/igolaizola/webcli/pkg/webff` and `github.com/igolaizola/webcli/pkg/webcobra` respectively.

You can also directly use the `github.com/igolaizola/webcli` package and pass your commands as `webcli.Command` to the `webcli.New` function.

## Usage

You can find an example using an `ff` CLI at `cmd/webff/main.go`, which you can run with:

```bash
go run cmd/webff/main.go
```

You can find an example using a `cobra` CLI at `cmd/webcobra/main.go`, which you can run with:

```bash
go run cmd/webcobra/main.go
```
