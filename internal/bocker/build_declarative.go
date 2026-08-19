package bocker

import (
	"fmt"
	"strings"
)

// downloadCommand turns the declarative download step into one small, audited
// shell transaction. The YAML describes sources, checksums and archive formats;
// shell syntax never has to be repeated in every project.
func downloadCommand(spec DownloadSpec) string {
	action := ""
	for i := len(spec.Attempts) - 1; i >= 0; i-- {
		attempt := spec.Attempts[i]
		var current strings.Builder
		current.WriteString("if wget --timeout=" + fmt.Sprint(attempt.Timeout) + " --tries=" + fmt.Sprint(attempt.Tries) + " " + shellQuote(attempt.URL) + " -O \"$archive\"; then ")
		current.WriteString("printf '%s  %s\\n' " + shellQuote(attempt.SHA256) + " \"$archive\" | sha256sum -c -; ")
		switch attempt.Format {
		case "zip":
			current.WriteString("unzip -q \"$archive\" -d " + shellQuote(spec.Extract) + "; ")
		default:
			current.WriteString("tar -xzf \"$archive\" -C " + shellQuote(spec.Extract) + "; ")
		}
		if attempt.Move != nil {
			current.WriteString("mv " + shellQuote(attempt.Move.From) + " " + shellQuote(attempt.Move.To) + "; ")
		}
		current.WriteString(":; else ")
		if action == "" {
			current.WriteString("exit 1")
		} else {
			current.WriteString(action)
		}
		current.WriteString("; fi")
		action = current.String()
	}
	body := strings.Builder{}
	body.WriteString("set -eu; archive=" + shellQuote(spec.Output) + "; trap 'rm -f \"$archive\"' EXIT HUP INT TERM; " + action)
	if spec.Verify != nil {
		body.WriteString("; actual=$(sed -n " + shellQuote(spec.Verify.Pattern) + " " + shellQuote(spec.Verify.Path) + "); test \"$actual\" = " + shellQuote(spec.Verify.Value))
	}
	return body.String()
}

type serviceAction struct {
	Name     string
	Services []string
}

func serviceActions(spec ServiceSpec) []serviceAction {
	actions := make([]serviceAction, 0, 3)
	if len(spec.Start) > 0 {
		actions = append(actions, serviceAction{Name: "start", Services: spec.Start})
	}
	if len(spec.Stop) > 0 {
		actions = append(actions, serviceAction{Name: "stop", Services: spec.Stop})
	}
	if len(spec.Enable) > 0 {
		actions = append(actions, serviceAction{Name: "enable", Services: spec.Enable})
	}
	return actions
}
