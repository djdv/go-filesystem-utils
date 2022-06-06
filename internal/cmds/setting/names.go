package settings

import (
	"fmt"
	"path"
	"runtime"
	"strings"
	"unicode"

	"github.com/fatih/camelcase"
)

type stringMapperFunc func(r rune) rune

func filter(components []string, filters ...stringMapperFunc) []string {
	for _, filter := range filters {
		filtered := make([]string, 0, len(components))
		for _, component := range components {
			if component := strings.Map(filter, component); component != "" {
				filtered = append(filtered, component)
			}
		}
		components = filtered
	}
	return components
}

func isExcludedRune(r rune) bool {
	return unicode.IsSpace(r) ||
		r == '=' ||
		r == '-' ||
		r == '.'
	// `.` is allowed in CLI, but I'm not dealing with custom splitting rules.
	// Anyone else is free to implement this if it's important to have `--args.with.dots`
}

func filterCli(parameterRune rune) rune {
	if isExcludedRune(parameterRune) {
		return -1
	}
	return parameterRune
}

func filterEnv(keyRune rune) rune {
	if isExcludedRune(keyRune) {
		return -1
	}
	return keyRune
}

func filterRuntime(refRune rune) rune {
	// NOTE: references from methods usually look like: `pkg.(*Type).Method`.
	if refRune == '(' ||
		refRune == '*' ||
		refRune == ')' {
		return -1
	}
	return refRune
}

func cliName(name string) string {
	var (
		splitName       = camelcase.Split(name)
		cleaned         = filter(splitName, filterCli)
		commandlineName = strings.ToLower(strings.Join(cleaned, "-"))
	)
	return commandlineName
}

func envName(prefix, namespace, name string) string {
	var (
		components []string
		splitName  = camelcase.Split(name)
	)
	if prefix != "" {
		splitPrefix := strings.Split(prefix, " ")
		components = append(components, splitPrefix...)
	}
	if namespace != "" {
		splitNamespace := strings.Split(namespace, " ")
		components = append(components, splitNamespace...)
	}
	components = append(components, splitName...)
	var (
		cleaned = filter(components, filterEnv)
		envName = strings.ToUpper(strings.Join(cleaned, "_"))
	)
	return envName
}

func pkgName(instructionPointer uintptr) (namespace string) {
	var (
		// Documentation refers to this as a
		// "package path-qualified function name".
		// Typically looks like: `pkgName.referenceName`,
		// `pkgName.referenceName.deeperReference-name`, etc.
		ppqfn = runtime.FuncForPC(instructionPointer).Name()
		names = strings.Split(path.Base(ppqfn), ".")
	)
	if len(names) < 1 {
		panic(fmt.Sprintf(
			"runtime returned non-standard function name"+
				"\n\tgot: `%s`"+
				"\n\twant format: `$pkgName.$funcName`",
			ppqfn,
		))
	}
	namespace = strings.Map(filterRuntime, names[0])
	return
}
