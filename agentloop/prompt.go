package agentloop

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"text/template"
)

// PromptData is what SystemTmpl is rendered with.
type PromptData struct {
	WorkDir      string
	GateCommands []string // human-readable command lines, in gate order
	ExtraRules   string
	ReadOnly     bool
}

// DefaultSystemTemplate is used when Config.SystemTmpl is empty. Callers
// with an opinionated role prompt (eval_loop's quality pass, archie's
// planner/builder) supply their own template.
const DefaultSystemTemplate = `You are an autonomous software engineering agent working in {{.WorkDir}}.
You cannot ask a human anything: act on evidence from your tools, never on assumption.
{{if .ReadOnly}}This is an analysis mission - your tools are read-only.{{else}}Make the smallest change that accomplishes the mission.{{end}}
{{if .GateCommands}}
Every change must keep the quality gate green. The gate runs automatically after each write/edit:
{{range .GateCommands}}  {{.}}
{{end}}A failing gate blocks progress; fix it before anything else.{{end}}
When the mission is complete, call the finish tool with status "passed" and a summary a human reviewer will read.
If you are genuinely stuck, call finish with status "blocked" and explain exactly what is missing.
{{if .ExtraRules}}
Additional project rules:
{{.ExtraRules}}{{end}}`

// buildMessages renders the system prompt and assembles the first user
// message: preflight ground truth, persistent notes, preloaded files,
// then the mission.
func buildMessages(ctx context.Context, cfg Config) (system string, first string, err error) {
	tmplText := cfg.SystemTmpl
	if tmplText == "" {
		tmplText = DefaultSystemTemplate
	}
	tmpl, err := template.New("system").Parse(tmplText)
	if err != nil {
		return "", "", fmt.Errorf("agentloop: parse system template: %w", err)
	}

	gateLines := make([]string, 0, len(cfg.Gate.Commands))
	for _, gc := range cfg.Gate.Commands {
		line := strings.Join(gc.Argv, " ")
		if gc.ExpectFailure {
			line += "   (this command must FAIL for the gate to pass)"
		}
		gateLines = append(gateLines, line)
	}

	var sb strings.Builder
	if err := tmpl.Execute(&sb, PromptData{
		WorkDir:      cfg.WorkDir,
		GateCommands: gateLines,
		ExtraRules:   cfg.ExtraRules,
		ReadOnly:     cfg.ReadOnly,
	}); err != nil {
		return "", "", fmt.Errorf("agentloop: render system template: %w", err)
	}

	var user strings.Builder
	if len(cfg.Preflight) > 0 {
		user.WriteString("<preflight>\nGround truth from the actual toolchain in this environment. Trust this over your own assumptions:\n")
		user.WriteString(runPreflight(ctx, cfg.Preflight, cfg.WorkDir))
		user.WriteString("</preflight>\n\n")
	}
	if cfg.Notes != nil {
		notes, err := cfg.Notes.Load(ctx)
		if err != nil {
			return "", "", fmt.Errorf("agentloop: load notes: %w", err)
		}
		if strings.TrimSpace(notes) != "" {
			user.WriteString("<agent_notes>\nVerified observations from earlier runs on this project:\n")
			user.WriteString(notes)
			user.WriteString("\n</agent_notes>\n\n")
		}
	}
	for _, rel := range cfg.PreloadFiles {
		b, err := os.ReadFile(filepath.Join(cfg.WorkDir, rel))
		if err != nil {
			continue // preload is best-effort; the model can read on demand
		}
		fmt.Fprintf(&user, "<file path=%q>\n%s\n</file>\n\n", rel, b)
	}
	user.WriteString("<mission>\n")
	user.WriteString(cfg.Mission)
	user.WriteString("\n</mission>")

	return sb.String(), user.String(), nil
}

// runPreflight executes the preflight commands and returns their labelled
// output. Preflight is informational: failures are reported as output,
// not errors.
func runPreflight(ctx context.Context, cmds []GateCommand, workdir string) string {
	var sb strings.Builder
	for _, gc := range cmds {
		if len(gc.Argv) == 0 {
			continue
		}
		cmd := exec.CommandContext(ctx, gc.Argv[0], gc.Argv[1:]...)
		cmd.Dir = workdir
		out, err := cmd.CombinedOutput()
		fmt.Fprintf(&sb, "$ %s\n%s", strings.Join(gc.Argv, " "), truncateOutput(out))
		if err != nil {
			fmt.Fprintf(&sb, "\n(exit: %v)", err)
		}
		sb.WriteString("\n")
	}
	return sb.String()
}
