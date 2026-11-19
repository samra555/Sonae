package services

import (
	"embed"
	texttemplate "text/template"
)

//go:embed prompt.tmpl risks.tmpl refine.tmpl
var promptFiles embed.FS

// ParsedPrompts holds the parsed prompt text templates.
type ParsedPrompts struct {
	Estimate *texttemplate.Template
	Risks    *texttemplate.Template
	Refine   *texttemplate.Template
}

// ParsePrompts parses the embedded prompt text templates.
func ParsePrompts() (*ParsedPrompts, error) {
	estimate, err := texttemplate.ParseFS(promptFiles, "prompt.tmpl")
	if err != nil {
		return nil, err
	}
	risks, err := texttemplate.ParseFS(promptFiles, "risks.tmpl")
	if err != nil {
		return nil, err
	}
	refine, err := texttemplate.ParseFS(promptFiles, "refine.tmpl")
	if err != nil {
		return nil, err
	}
	return &ParsedPrompts{Estimate: estimate, Risks: risks, Refine: refine}, nil
}
