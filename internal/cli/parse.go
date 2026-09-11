package cli

import (
	"strings"

	"github.com/whalesalad/agentdrop-cli/internal/api"
)

var commandNames = map[string]bool{
	"open": true, "put": true, "get": true, "list": true, "info": true, "share": true,
	"delete": true, "revoke": true, "login": true, "logout": true, "whoami": true, "help": true,
}

type options struct {
	help, version, json, yes, noOpen                   bool
	output, name, typ, expires, limit, cursor, profile string
	positionals                                        []string
	// exec mode
	exec     bool
	execArgs []string
}

func usage(format string, args ...any) error { return api.Errorf("usage", format, args...) }

// parse implements the small, dependency-free flag grammar: --flag=value,
// --flag value, short flags with a following value, and -- for the rest.
func parse(args []string) (*options, error) {
	o := &options{profile: "default"}
	takeValue := func(i *int, name string, inline string, hasInline bool) (string, error) {
		if hasInline {
			return inline, nil
		}
		if *i+1 >= len(args) {
			return "", usage("%s requires a value.", name)
		}
		*i++
		return args[*i], nil
	}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if o.exec {
			// Only --profile is accepted before the mandatory separator.
			if arg == "--" {
				o.execArgs = append([]string{}, args[i+1:]...)
				return o, nil
			}
			if strings.HasPrefix(arg, "--profile") {
				name, inline, hasInline := strings.Cut(arg, "=")
				if name != "--profile" {
					return nil, usage("Unknown option %s.", name)
				}
				v, err := takeValue(&i, name, inline, hasInline)
				if err != nil {
					return nil, err
				}
				o.profile = v
				continue
			}
			return nil, usage("Use agentdrop exec [--profile NAME] -- COMMAND [ARGS].")
		}
		if arg == "--" {
			o.positionals = append(o.positionals, args[i+1:]...)
			break
		}
		if strings.HasPrefix(arg, "--") {
			name, inline, hasInline := strings.Cut(arg, "=")
			switch name {
			case "--help":
				o.help = true
			case "--version":
				o.version = true
			case "--json":
				o.json = true
			case "--yes":
				o.yes = true
			case "--no-open":
				o.noOpen = true
			case "--output", "--name", "--type", "--expires", "--limit", "--cursor", "--profile":
				v, err := takeValue(&i, name, inline, hasInline)
				if err != nil {
					return nil, err
				}
				switch name {
				case "--output":
					o.output = v
				case "--name":
					o.name = v
				case "--type":
					o.typ = v
				case "--expires":
					o.expires = v
				case "--limit":
					o.limit = v
				case "--cursor":
					o.cursor = v
				case "--profile":
					o.profile = v
				}
				continue
			default:
				return nil, usage("Unknown option %s. Run agentdrop --help.", name)
			}
			if hasInline {
				return nil, usage("%s does not take a value.", name)
			}
			continue
		}
		if len(arg) > 1 && arg[0] == '-' {
			// Short flags: -h -y -o FILE -n NAME; also -oFILE.
			flag, rest := arg[:2], arg[2:]
			switch flag {
			case "-h":
				o.help = true
			case "-y":
				o.yes = true
			case "-o", "-n":
				v := rest
				if v == "" {
					var err error
					v, err = takeValue(&i, flag, "", false)
					if err != nil {
						return nil, err
					}
				}
				if flag == "-o" {
					o.output = v
				} else {
					o.name = v
				}
				continue
			default:
				return nil, usage("Unknown option %s. Run agentdrop --help.", arg)
			}
			if rest != "" {
				return nil, usage("Unknown option %s. Run agentdrop --help.", arg)
			}
			continue
		}
		if len(o.positionals) == 0 && arg == "exec" {
			o.exec = true
			continue
		}
		o.positionals = append(o.positionals, arg)
	}
	if o.exec {
		return nil, usage("Use agentdrop exec [--profile NAME] -- COMMAND [ARGS].")
	}
	return o, nil
}
