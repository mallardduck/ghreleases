package ghrelease

import (
	"fmt"
	"regexp"
	"strings"
)

// Render applies template variables to a pattern string.
// Supports variables: {{.name}}, {{.version}}, {{.os}}, {{.arch}}, {{.ext}}
// Supports modifiers: upper, lower, title, trimprefix:PREFIX, trimsuffix:SUFFIX, replace:FROM=TO
//
// Examples:
//
//	{{.name}}_{{.version}}_{{.os}}_{{.arch}}.{{.ext}}
//	{{.name | upper}}-{{.version}}.tar.gz
//	{{.os | replace:darwin=macos}}-binary
func Render(pattern string, vars TemplateVars, mode TemplateMode) (string, error) {
	// Regex to match {{.variable}} or {{.variable | modifier}}
	re := regexp.MustCompile(`\{\{\.(\w+)(?:\s*\|\s*([^}]+))?\}\}`)

	var renderErr error
	result := re.ReplaceAllStringFunc(pattern, func(match string) string {
		// Extract variable name and optional modifier
		parts := re.FindStringSubmatch(match)
		if len(parts) < 2 {
			if mode == TemplateStrict {
				renderErr = fmt.Errorf("%w: malformed template", ErrInvalidModifier)
			}
			return match // pass through in permissive mode
		}

		varName := parts[1]
		modifier := ""
		if len(parts) == 3 && parts[2] != "" {
			modifier = strings.TrimSpace(parts[2])
		}

		// Lookup variable value
		value, found := lookupVar(vars, varName)
		if !found {
			if mode == TemplateStrict {
				renderErr = fmt.Errorf("%w: %s", ErrUnknownVariable, varName)
			}
			return match // pass through in permissive mode
		}

		// Apply modifier if present
		if modifier != "" {
			var err error
			value, err = applyModifier(value, modifier)
			if err != nil {
				if mode == TemplateStrict {
					renderErr = err
				}
				return match // pass through in permissive mode
			}
		}

		return value
	})

	return result, renderErr
}

func lookupVar(vars TemplateVars, name string) (string, bool) {
	switch name {
	case "name":
		return vars.Name, true
	case "version":
		return vars.Version, true
	case "os":
		return vars.OS, true
	case "arch":
		return vars.Arch, true
	case "ext":
		return vars.Ext, true
	default:
		return "", false
	}
}

func toTitle(s string) string {
	if s == "" {
		return s
	}
	// Simple title case: capitalize first letter
	r := []rune(s)
	if len(r) > 0 {
		r[0] = []rune(strings.ToUpper(string(r[0])))[0]
	}
	return string(r)
}

func applyModifier(value, modifier string) (string, error) {
	// Handle modifiers with arguments
	if strings.Contains(modifier, ":") {
		parts := strings.SplitN(modifier, ":", 2)
		name := parts[0]
		arg := parts[1]

		switch name {
		case "trimprefix":
			return strings.TrimPrefix(value, arg), nil
		case "trimsuffix":
			return strings.TrimSuffix(value, arg), nil
		case "replace":
			replaceParts := strings.SplitN(arg, "=", 2)
			if len(replaceParts) != 2 {
				return "", fmt.Errorf("%w: replace requires FROM=TO", ErrInvalidModifier)
			}
			return strings.ReplaceAll(value, replaceParts[0], replaceParts[1]), nil
		default:
			return "", fmt.Errorf("%w: %s", ErrUnknownModifier, name)
		}
	}

	// Handle simple modifiers
	switch modifier {
	case "upper":
		return strings.ToUpper(value), nil
	case "lower":
		return strings.ToLower(value), nil
	case "title":
		return toTitle(value), nil
	default:
		return "", fmt.Errorf("%w: %s", ErrUnknownModifier, modifier)
	}
}
