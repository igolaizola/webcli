package webff

import (
	"flag"
	"fmt"

	"github.com/igolaizola/webcli"
	"github.com/peterbourgon/ff/v3/ffcli"
)

func Parse(cmds []*ffcli.Command) []*webcli.Command {
	var wcmds []*webcli.Command
	for _, cmd := range cmds {
		wcmds = append(wcmds, toCommand(cmd))
	}
	return wcmds
}

func toCommand(c *ffcli.Command) *webcli.Command {
	var subs []*webcli.Command
	for _, sub := range c.Subcommands {
		subs = append(subs, toCommand(sub))
	}
	return &webcli.Command{
		Fields:      toFields(c.FlagSet),
		Name:        c.Name,
		Description: c.ShortHelp + "\n" + c.LongHelp,
		Subcommands: subs,
	}
}

func toFields(fs *flag.FlagSet) []*webcli.Field {
	var fields []*webcli.Field
	fs.VisitAll(func(f *flag.Flag) {
		fields = append(fields, &webcli.Field{
			Name:        f.Name,
			Default:     f.Value.String(),
			Description: f.Usage,
			Type:        toType(f),
		})
	})
	return fields
}

func toType(f *flag.Flag) webcli.FieldType {
	t := fmt.Sprintf("%T", f.Value)
	switch t {
	case "*flag.boolValue":
		return webcli.Boolean
	case "*flag.durationValue":
		return webcli.Text
	case "*flag.float64Value":
		return webcli.Number
	case "*flag.intValue", "*flag.int64Value":
		return webcli.Number
	case "*flag.stringValue":
		return webcli.Text
	case "*flag.uintValue", "*flag.uint64Value":
		return webcli.Number
	default:
		return webcli.Text
	}
}

type Config struct {
	App      string
	Commands []*ffcli.Command
	Address  string
}

func New(cfg *Config) (*webcli.Server, error) {
	webcliConfig := &webcli.Config{
		App:      cfg.App,
		Commands: Parse(cfg.Commands),
		Address:  cfg.Address,
	}
	return webcli.New(webcliConfig)
}
